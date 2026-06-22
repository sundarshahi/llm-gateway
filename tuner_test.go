package llmgateway

import (
	"encoding/json"
	"testing"
)

// thinkingFor mirrors the gateway's job-vs-tuner precedence for testing.
func thinkingFor(t *testing.T, j Job) string {
	t.Helper()
	g, err := New(Config{Tuner: EnvTuner{}})
	if err != nil {
		t.Fatal(err)
	}
	return g.resolveThinking(j)
}

func TestResolveThinking(t *testing.T) {
	if got := thinkingFor(t, Job{PromptName: "editor"}); got != "" {
		t.Fatalf("no env → want \"\", got %q", got)
	}
	t.Setenv("LLM_THINK_DEFAULT", "4096")
	if got := thinkingFor(t, Job{PromptName: "qa"}); got != "4096" {
		t.Fatalf("default → want 4096, got %q", got)
	}
	t.Setenv("LLM_THINK_EDITOR", "0")
	if got := thinkingFor(t, Job{PromptName: "editor"}); got != "0" {
		t.Fatalf("per-stage → want 0, got %q", got)
	}
	t.Setenv("LLM_THINK_CONTENT_REVIEW", "100")
	if got := thinkingFor(t, Job{PromptName: "content-review"}); got != "100" {
		t.Fatalf("hyphen stage → want 100, got %q", got)
	}
	n := 7
	if got := thinkingFor(t, Job{PromptName: "editor", MaxThinkingTokens: &n}); got != "7" {
		t.Fatalf("job override → want 7, got %q", got)
	}
	neg := -5
	if got := thinkingFor(t, Job{MaxThinkingTokens: &neg}); got != "0" {
		t.Fatalf("negative → want 0, got %q", got)
	}
}

func TestResolveModelName(t *testing.T) {
	g, _ := New(Config{Tuner: EnvTuner{DefaultModel: "fallback"}})
	if got := g.cfg.Tuner.ModelName(""); got != "fallback" {
		t.Fatalf("default model → want fallback, got %q", got)
	}
	t.Setenv("LLM_MODEL_WRITER", "specific")
	if got := g.cfg.Tuner.ModelName("writer"); got != "specific" {
		t.Fatalf("per-stage model → want specific, got %q", got)
	}
}

func TestCacheKeyThinking(t *testing.T) {
	a := SpawnRequest{Prompt: "hi"}
	b := SpawnRequest{Prompt: "hi", Thinking: "0"}
	if cacheKey("claude", a) == cacheKey("claude", b) {
		t.Fatal("thinking budget must change the cache key")
	}
}

func TestCacheKeyStable(t *testing.T) {
	r := SpawnRequest{System: "s", Prompt: "p", ModelName: "m", Schema: "{}"}
	if cacheKey("claude", r) != cacheKey("claude", r) {
		t.Fatal("cache key must be deterministic")
	}
	if cacheKey("claude", r) == cacheKey("gemini", r) {
		t.Fatal("model must be part of the cache key")
	}
}

func TestCompactJSON(t *testing.T) {
	if got := compactJSON(json.RawMessage(`{ "a" : 1 }`)); got != `{"a":1}` {
		t.Fatalf("compact → %q", got)
	}
	if got := compactJSON(json.RawMessage(`null`)); got != "" {
		t.Fatalf("null → %q", got)
	}
	if got := compactJSON(nil); got != "" {
		t.Fatalf("nil → %q", got)
	}
}
