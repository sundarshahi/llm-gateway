package llmgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Gateway runs Jobs through providers with caching, retries, structured output,
// and self-consistency voting. It is safe for concurrent use. Construct it with
// New; the zero value is not usable.
type Gateway struct {
	cfg       Config
	sem       chan struct{}
	providers providerSet
	log       *slog.Logger
	metrics   Metrics
}

// New builds a Gateway, filling defaults for any zero Config field.
func New(cfg Config) (*Gateway, error) {
	if cfg.MaxConcurrency < 1 {
		cfg.MaxConcurrency = 4
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Minute
	}
	if cfg.Retries < 0 {
		cfg.Retries = 0
	}
	if cfg.Tuner == nil {
		cfg.Tuner = EnvTuner{}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Metrics == nil {
		cfg.Metrics = nopMetrics{}
	}
	provs := cfg.Providers
	if provs == nil {
		provs = DefaultProviders()
	}
	set := providerSet{}
	for _, p := range provs {
		set[p.Name()] = p
	}
	if len(set) == 0 {
		return nil, errors.New("no providers configured")
	}
	return &Gateway{
		cfg:       cfg,
		sem:       make(chan struct{}, cfg.MaxConcurrency),
		providers: set,
		log:       cfg.Logger,
		metrics:   cfg.Metrics,
	}, nil
}

// resolved holds a fully-prepared invocation for a Job.
type resolved struct {
	provider  Provider
	req       SpawnRequest
	wantJSON  bool
	schema    bool // native structured-output mode active
	modelName string
	samples   int
	cacheKey  string
}

// Run executes a single Job. It never panics; a failed LLM call returns a Result
// with OK=false and a suggested Status. ctx cancellation aborts the spawn.
func (g *Gateway) Run(ctx context.Context, j Job) Result {
	if j.Skip {
		model := j.Model
		if model == "" {
			model = "skip"
		}
		return Result{OK: true, Status: 200, Model: model, Text: "", Data: j.Echo}
	}

	r, res, ok := g.resolve(j)
	if !ok {
		return res
	}

	t0 := time.Now()
	// Cache: a hit is a previously-validated reply.
	if g.cfg.Cache != nil {
		if cached, hit := g.cfg.Cache.Get(r.cacheKey); hit {
			if out, parsedOK := g.parseOutput(j, r, cached); parsedOK {
				g.observe(j, r, nowMS(t0), Event{CacheHit: true, OK: true})
				g.log.Info("cache hit", "stage", labelOf(j), "model", orDefault(r.modelName))
				return out
			}
		}
	}

	if r.samples > 1 && r.wantJSON {
		return g.runVoted(ctx, j, r, t0)
	}

	maxAttempts := 1 + g.cfg.Retries
	last := errResult(502, j.Model, "no attempts")
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		ta := time.Now()
		text, err := g.runCLI(ctx, r.provider, r.req)
		if err != nil {
			g.log.Error("cli failed", "stage", labelOf(j), "model", orDefault(r.modelName),
				"ms", nowMS(ta), "attempt", attempt, "of", maxAttempts, "err", err.Error())
			last = errResult(502, j.Model, err.Error())
			if errors.Is(err, errCLITimeout) || ctx.Err() != nil || attempt == maxAttempts {
				g.observe(j, r, nowMS(t0), Event{Attempts: attempt, OK: false})
				return last
			}
			continue
		}
		out, parsedOK := g.parseOutput(j, r, text)
		if parsedOK {
			g.log.Info("ok", "stage", labelOf(j), "model", orDefault(r.modelName),
				"ms", nowMS(ta), "attempt", attempt, "of", maxAttempts)
			if g.cfg.Cache != nil {
				g.cfg.Cache.Put(r.cacheKey, text)
			}
			g.observe(j, r, nowMS(t0), Event{Attempts: attempt, OK: true})
			return out
		}
		g.log.Error("invalid output", "stage", labelOf(j), "attempt", attempt,
			"of", maxAttempts, "err", out.Error, "head", head(text, 200))
		last = out
	}
	g.observe(j, r, nowMS(t0), Event{Attempts: maxAttempts, OK: false})
	return last
}

// RunBatch runs jobs concurrently (each still bounded by the global concurrency
// gate) and returns results in order.
func (g *Gateway) RunBatch(ctx context.Context, jobs []Job) []Result {
	results := make([]Result, len(jobs))
	var wg sync.WaitGroup
	for i := range jobs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = g.Run(ctx, jobs[i])
		}(i)
	}
	wg.Wait()
	return results
}

// resolve prepares a Job into a runnable invocation, or returns a 400 Result.
func (g *Gateway) resolve(j Job) (resolved, Result, bool) {
	system := j.System
	prompt := j.Prompt
	if j.PromptName != "" {
		if g.cfg.Prompts == nil {
			return resolved{}, errResult(400, j.Model, "prompt_name set but no PromptStore configured"), false
		}
		p, err := g.cfg.Prompts.Prompt(j.PromptName)
		if err != nil {
			return resolved{}, errResult(400, j.Model, "prompt load failed: "+err.Error()), false
		}
		system = p
	}
	if j.Input != nil {
		prompt = inputToString(j.Input)
	}
	if j.Model == "" || prompt == "" {
		return resolved{}, errResult(400, j.Model, "require model + (prompt | input), optionally prompt_name"), false
	}
	provider, err := g.providers.get(j.Model)
	if err != nil {
		return resolved{}, errResult(400, j.Model, err.Error()), false
	}

	rawSchema := compactJSON(j.JSONSchema)
	if rawSchema == "" && j.PromptName != "" && g.cfg.Prompts != nil {
		rawSchema = g.cfg.Prompts.Schema(j.PromptName)
	}
	schemaMode := rawSchema != "" && provider.SupportsSchema()
	wantJSON := j.JSON || rawSchema != ""

	if wantJSON {
		pre := ""
		if system != "" {
			pre = system + "\n\n"
		}
		system = pre + "You MUST respond with ONLY valid minified JSON. No prose, no markdown, no code fences."
	}

	modelName := j.ModelName
	if modelName == "" {
		modelName = g.cfg.Tuner.ModelName(j.PromptName)
	}

	spawnSchema := ""
	if schemaMode {
		spawnSchema = rawSchema
	}
	req := SpawnRequest{
		System:    system,
		Prompt:    prompt,
		ModelName: modelName,
		Tools:     j.Tools,
		Schema:    spawnSchema,
		WantJSON:  wantJSON,
		Thinking:  g.resolveThinking(j),
	}

	r := resolved{
		provider:  provider,
		req:       req,
		wantJSON:  wantJSON,
		schema:    schemaMode,
		modelName: modelName,
		samples:   g.resolveSamples(j),
	}
	if g.cfg.Cache != nil {
		r.cacheKey = cacheKey(j.Model, req)
	}
	return r, Result{}, true
}

func (g *Gateway) resolveSamples(j Job) int {
	if j.Samples > 0 {
		return j.Samples
	}
	return g.cfg.Tuner.Samples(j.PromptName)
}

func (g *Gateway) resolveThinking(j Job) string {
	if j.MaxThinkingTokens != nil {
		n := *j.MaxThinkingTokens
		if n < 0 {
			n = 0
		}
		return strconv.Itoa(n)
	}
	if v, ok := g.cfg.Tuner.Thinking(j.PromptName); ok {
		return v
	}
	return ""
}

// parseOutput turns raw CLI stdout into a Result. parsedOK=false marks an
// invalid reply the retry loop should re-attempt (and must NOT be cached).
func (g *Gateway) parseOutput(j Job, r resolved, raw string) (Result, bool) {
	if r.schema {
		data, rawStruct, handled, err := r.provider.ExtractStructured(raw)
		if handled {
			if err != nil {
				return Result{OK: false, Status: 502, Model: j.Model, Text: raw, Error: err.Error()}, false
			}
			return g.jsonSuccess(j, rawStruct, data), true
		}
		// Provider declared no envelope; fall through to generic extraction.
	}
	if r.wantJSON {
		data, err := extractJSON(raw)
		if err != nil {
			return Result{OK: false, Status: 502, Model: j.Model, Text: raw,
				Error: "model did not return valid JSON: " + err.Error()}, false
		}
		return g.jsonSuccess(j, raw, data), true
	}
	return Result{OK: true, Status: 200, Model: j.Model, Text: raw}, true
}

// jsonSuccess applies the PostProcess hook and mirrors the returned data into
// text when the hook rewrote it.
func (g *Gateway) jsonSuccess(j Job, fallbackText string, data any) Result {
	text := fallbackText
	if g.cfg.PostProcess != nil {
		before, _ := marshalNoEscape(data)
		data = g.cfg.PostProcess(j.PromptName, j.Input, data)
		if after, err := marshalNoEscape(data); err == nil && !bytes.Equal(before, after) {
			text = string(after)
		}
	}
	return Result{OK: true, Status: 200, Model: j.Model, Text: text, Data: data}
}

func (g *Gateway) observe(j Job, r resolved, ms int64, e Event) {
	e.PromptName = j.PromptName
	e.Model = j.Model
	e.ModelName = r.modelName
	e.DurationMS = ms
	g.metrics.Observe(e)
}

// compactJSON minifies a raw JSON schema. Empty/null → "".
func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return strings.TrimSpace(string(raw))
	}
	if s := buf.String(); s != "null" {
		return s
	}
	return ""
}

func labelOf(j Job) string {
	if j.PromptName != "" {
		return j.PromptName
	}
	return j.Model
}

func orDefault(s string) string {
	if s == "" {
		return "default"
	}
	return s
}

func voteKey(data any) string {
	b, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	return string(b)
}
