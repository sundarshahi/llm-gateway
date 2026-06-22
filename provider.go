package llmgateway

import "fmt"

// SpawnRequest is the resolved, provider-agnostic description of a single CLI call.
type SpawnRequest struct {
	System    string
	Prompt    string
	ModelName string // "" → provider/CLI default
	Tools     any    // provider-specific; string or []string of tool names
	Schema    string // compact JSON Schema; "" → no structured-output mode
	WantJSON  bool   // a JSON reply is expected (schema implies this)
	Thinking  string // MAX_THINKING_TOKENS for this spawn; "" → inherit
}

// Invocation is a fully-built subprocess spec.
type Invocation struct {
	Cmd   string
	Args  []string
	Stdin string
	Env   []string // extra environment entries ("KEY=VALUE"), appended to os.Environ()
}

// Provider builds the subprocess for a request and, in native structured-output
// mode, extracts the payload from that provider's stdout envelope.
//
// Implementations must be safe for concurrent use.
type Provider interface {
	// Name is the provider key matched against Job.Model.
	Name() string

	// Invocation builds the command for a request.
	Invocation(SpawnRequest) (Invocation, error)

	// SupportsSchema reports whether this provider implements native structured
	// output. When false, a Job's schema is ignored and JSON (if requested) is
	// extracted from free-form text instead.
	SupportsSchema() bool

	// ExtractStructured parses stdout produced under native structured-output
	// mode. It returns the decoded payload and the raw JSON to cache. Returning
	// ok=false tells the gateway to fall back to generic JSON extraction.
	ExtractStructured(stdout string) (data any, raw string, ok bool, err error)
}

// providerSet indexes providers by name.
type providerSet map[string]Provider

func (s providerSet) get(name string) (Provider, error) {
	p, ok := s[name]
	if !ok {
		return nil, fmt.Errorf("unknown model %q (no provider registered)", name)
	}
	return p, nil
}

// DefaultProviders returns the built-in Claude + Gemini providers with default
// settings. Callers wanting to tune Claude's flags should build ClaudeProvider
// directly and pass a custom provider map via Config.
func DefaultProviders() []Provider {
	return []Provider{NewClaudeProvider(ClaudeOptions{}), NewGeminiProvider()}
}
