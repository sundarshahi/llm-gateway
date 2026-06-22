package llmgateway

import (
	"log/slog"
	"os"
	"strconv"
	"time"
)

// Config configures a Gateway. The zero value is usable for an in-process
// library caller (no cache, no prompt store, default providers, default
// concurrency/timeout). FromEnv reproduces the original wrapper's environment.
type Config struct {
	// MaxConcurrency caps simultaneous CLI spawns. <1 → 4.
	MaxConcurrency int
	// Timeout per spawn. 0 → 10 minutes.
	Timeout time.Duration
	// Retries: extra attempts on a transient error or invalid JSON. A timeout is
	// never retried. <0 → 0.
	Retries int

	// Providers overrides the provider set. nil → DefaultProviders (claude+gemini).
	Providers []Provider

	// Prompts resolves prompt_name → system prompt + schema. nil → prompt_name is
	// rejected as unsupported.
	Prompts PromptStore
	// Tuner resolves per-stage model/votes/thinking. nil → EnvTuner{}.
	Tuner Tuner
	// Cache stores validated CLI stdout. nil → caching disabled.
	Cache Cache
	// PostProcess rewrites structured replies (per prompt_name). nil → no-op.
	PostProcess PostProcessFunc

	// Logger for structured logs. nil → slog.Default().
	Logger *slog.Logger
	// Metrics sink. nil → no-op.
	Metrics Metrics
}

// FromEnv builds a Config from the same environment variables as the original
// wrapper, including a FilePromptStore wired with the SEO-style learned-rules
// addendum header (override Prompts to change it). It does NOT construct a Cache
// unless LLM_CACHE_DIR is set.
func FromEnv() (Config, error) {
	cfg := Config{
		MaxConcurrency: envInt("LLM_MAX_CONCURRENCY", 4),
		Timeout:        time.Duration(envInt("LLM_TIMEOUT_MS", 600000)) * time.Millisecond,
		Retries:        envInt("LLM_RETRIES", 2),
		Tuner:          EnvTuner{DefaultModel: os.Getenv("LLM_DEFAULT_MODEL")},
	}

	promptsDir := env("PROMPTS_DIR", "prompts")
	schemasDir := env("SCHEMAS_DIR", "schemas")
	learnedDir := env("LEARNED_DIR", promptsDir+"/learned")
	ps := NewFilePromptStore(promptsDir, schemasDir, learnedDir)
	ps.AddendumHeader = "## LEARNED RULES (auto-tuned from QA failures — additive, do NOT ignore)"
	cfg.Prompts = ps

	if dir := os.Getenv("LLM_CACHE_DIR"); dir != "" {
		fc, err := NewFileCache(dir)
		if err != nil {
			return Config{}, err
		}
		cfg.Cache = fc
	}

	// Claude flag opt-outs preserved from the original env switches.
	claude := NewClaudeProvider(ClaudeOptions{
		NoVarianceFlags: os.Getenv("LLM_NO_VARIANCE_FLAGS") == "1",
		Bare:            os.Getenv("LLM_BARE") == "1",
	})
	cfg.Providers = []Provider{claude, NewGeminiProvider()}
	return cfg, nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
