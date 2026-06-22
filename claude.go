package llmgateway

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ClaudeOptions configures the Claude Code provider. The defaults match the
// proven headless configuration: variance-reducing flags on, --bare off.
type ClaudeOptions struct {
	// Binary is the CLI name/path. Default "claude".
	Binary string
	// NoVarianceFlags drops --strict-mcp-config and
	// --exclude-dynamic-system-prompt-sections (which otherwise keep a job
	// byte-identical across runs/boxes and improve prompt-cache reuse).
	NoVarianceFlags bool
	// Bare adds --bare. Off by default because --bare bypasses
	// CLAUDE_CODE_OAUTH_TOKEN authentication.
	Bare bool
}

// ClaudeProvider drives the Claude Code headless CLI ("claude -p"). Prompt is
// delivered on stdin; the system prompt is appended; native structured output is
// supported via --json-schema under the JSON output envelope.
type ClaudeProvider struct{ opt ClaudeOptions }

// NewClaudeProvider builds a Claude provider. The zero ClaudeOptions is the
// recommended production configuration.
func NewClaudeProvider(o ClaudeOptions) *ClaudeProvider {
	if o.Binary == "" {
		o.Binary = "claude"
	}
	return &ClaudeProvider{opt: o}
}

func (c *ClaudeProvider) Name() string         { return "claude" }
func (c *ClaudeProvider) SupportsSchema() bool { return true }

func (c *ClaudeProvider) Invocation(r SpawnRequest) (Invocation, error) {
	args := []string{"-p"}
	// Structured-output mode: --json-schema enforces the shape, but ONLY under the
	// JSON output envelope (under --output-format text the schema is ignored).
	if r.Schema != "" {
		args = append(args, "--output-format", "json", "--json-schema", r.Schema)
	} else {
		args = append(args, "--output-format", "text")
	}
	if !c.opt.NoVarianceFlags {
		args = append(args, "--strict-mcp-config", "--exclude-dynamic-system-prompt-sections")
	}
	if c.opt.Bare {
		args = append(args, "--bare")
	}
	if r.System != "" {
		args = append(args, "--append-system-prompt", r.System)
	}
	if r.ModelName != "" {
		args = append(args, "--model", r.ModelName)
	}
	// CRITICAL: default to NO tools so each stage is a pure, fast LLM call.
	ts := toolsStr(r.Tools)
	args = append(args, "--tools", ts)
	// In headless mode an available tool is still permission-gated; pre-approve
	// exactly the tools we enable or the model's tool calls are auto-denied.
	if ts != "" {
		args = append(args, "--allowedTools", ts)
	}

	inv := Invocation{Cmd: c.opt.Binary, Args: args, Stdin: r.Prompt}
	if r.Thinking != "" {
		inv.Env = []string{"MAX_THINKING_TOKENS=" + r.Thinking}
	}
	return inv, nil
}

// ExtractStructured parses the Claude Code JSON envelope and returns the
// "structured_output" payload.
func (c *ClaudeProvider) ExtractStructured(stdout string) (any, string, bool, error) {
	var env struct {
		Result           string          `json:"result"`
		StructuredOutput json.RawMessage `json:"structured_output"`
		IsError          bool            `json:"is_error"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		return nil, "", true, fmt.Errorf("schema mode: CLI envelope parse failed: %w", err)
	}
	if env.IsError {
		return nil, "", true, fmt.Errorf("schema mode: CLI reported is_error")
	}
	if len(env.StructuredOutput) == 0 || string(env.StructuredOutput) == "null" {
		return nil, "", true, fmt.Errorf("schema mode: no structured_output in CLI envelope")
	}
	var data any
	if err := json.Unmarshal(env.StructuredOutput, &data); err != nil {
		return nil, "", true, fmt.Errorf("schema mode: structured_output not valid JSON: %w", err)
	}
	return data, strings.TrimSpace(string(env.StructuredOutput)), true, nil
}

// toolsStr mirrors `tools == null ? "" : String(tools)`.
func toolsStr(t any) string {
	switch v := t.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		parts := make([]string, len(v))
		for i, e := range v {
			parts[i] = fmt.Sprint(e)
		}
		return strings.Join(parts, ",")
	case []string:
		return strings.Join(v, ",")
	default:
		return fmt.Sprint(v)
	}
}
