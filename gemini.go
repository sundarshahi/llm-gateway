package llmgateway

// GeminiProvider drives the Gemini CLI. It has no separate system-prompt flag,
// so the system prompt is folded into the stdin payload. It has no native
// structured-output mode, so JSON (when requested) is extracted from text.
type GeminiProvider struct{ binary string }

// NewGeminiProvider builds a Gemini provider using the "gemini" binary.
func NewGeminiProvider() *GeminiProvider { return &GeminiProvider{binary: "gemini"} }

func (g *GeminiProvider) Name() string         { return "gemini" }
func (g *GeminiProvider) SupportsSchema() bool { return false }

func (g *GeminiProvider) Invocation(r SpawnRequest) (Invocation, error) {
	args := []string{}
	if r.ModelName != "" {
		args = append(args, "-m", r.ModelName)
	}
	payload := r.Prompt
	if r.System != "" {
		payload = r.System + "\n\n---\n\n" + r.Prompt
	}
	return Invocation{Cmd: g.binary, Args: args, Stdin: payload}, nil
}

// ExtractStructured is never reached (SupportsSchema is false) but satisfies the
// interface; it always defers to generic JSON extraction.
func (g *GeminiProvider) ExtractStructured(string) (any, string, bool, error) {
	return nil, "", false, nil
}
