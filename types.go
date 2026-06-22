package llmgateway

import "encoding/json"

// Job is one LLM invocation. It is the public request contract and is decoded
// verbatim from the HTTP /llm body, so JSON tags are part of the API.
type Job struct {
	// Skip short-circuits the job: it returns OK with empty text and Echo as data.
	Skip bool `json:"skip"`
	// Echo is returned as the data field of a skipped job (passthrough plumbing).
	Echo any `json:"echo"`

	// Model selects the provider by name ("claude", "gemini", or a custom one).
	Model string `json:"model"`
	// System is the system prompt. Ignored when PromptName resolves a stored prompt.
	System string `json:"system"`
	// Prompt is the user prompt. Ignored when Input is present.
	Prompt string `json:"prompt"`
	// Input, when present, replaces Prompt: a string is used as-is, anything else
	// is minified to JSON. RawMessage lets "present" differ from "absent".
	Input json.RawMessage `json:"input"`

	// PromptName resolves a stored system prompt (and optional JSON schema) via the
	// configured PromptStore, and keys per-stage tuning (model/votes/thinking).
	PromptName string `json:"prompt_name"`

	// JSON requests a JSON reply (extracted/repaired from text output).
	JSON bool `json:"json"`
	// JSONSchema, when set, requests provider-native structured output. Takes
	// precedence over any schema the PromptStore would supply for PromptName.
	JSONSchema json.RawMessage `json:"json_schema"`

	// Samples > 1 enables self-consistency: run N samples, return the JSON majority.
	Samples int `json:"samples"`
	// MaxThinkingTokens caps extended-thinking for this call. nil falls back to tuning.
	MaxThinkingTokens *int `json:"max_thinking_tokens"`
	// ModelName overrides the provider's default model (e.g. a specific model id).
	ModelName string `json:"model_name"`
	// Tools is passed through to providers that accept a tool allow-list.
	Tools any `json:"tools"`
}

// Result is the outcome of a Job. Status is a suggested HTTP status for the HTTP
// layer; library callers can ignore it and read OK/Error.
type Result struct {
	OK     bool   `json:"ok"`
	Model  string `json:"model,omitempty"`
	Text   string `json:"text,omitempty"`
	Data   any    `json:"data,omitempty"`
	Error  string `json:"error,omitempty"`
	Status int    `json:"-"`
}

// body renders the wire shape for the HTTP layer. Text is always present on a
// successful reply (including the empty string of a skipped job), matching the
// long-standing API; errors omit it.
func (r Result) body() map[string]any {
	out := map[string]any{"ok": r.OK}
	if r.Model != "" {
		out["model"] = r.Model
	}
	if r.Error != "" {
		out["error"] = r.Error
		return out
	}
	out["text"] = r.Text
	if r.Data != nil {
		out["data"] = r.Data
	}
	return out
}

func errResult(status int, model, msg string) Result {
	return Result{OK: false, Model: model, Error: msg, Status: status}
}
