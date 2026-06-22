package llmgateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeProvider spawns a real `sh -c printf` so the full spawn path runs, but with
// a canned, controllable stdout. Pointer-held so a test can mutate it mid-run.
type fakeProvider struct {
	name           string
	out            *atomic.Pointer[string]
	supportsSchema bool
}

func newFake(name string, supportsSchema bool, out string) *fakeProvider {
	p := &fakeProvider{name: name, supportsSchema: supportsSchema, out: &atomic.Pointer[string]{}}
	p.out.Store(&out)
	return p
}

func (f *fakeProvider) Name() string         { return f.name }
func (f *fakeProvider) SupportsSchema() bool { return f.supportsSchema }

func (f *fakeProvider) Invocation(SpawnRequest) (Invocation, error) {
	s := *f.out.Load()
	return Invocation{Cmd: "sh", Args: []string{"-c", "printf %s " + shellQuote(s)}}, nil
}

func (f *fakeProvider) ExtractStructured(stdout string) (any, string, bool, error) {
	var data any
	if err := json.Unmarshal([]byte(stdout), &data); err != nil {
		return nil, "", true, err
	}
	return data, stdout, true, nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func newGW(t *testing.T, p Provider, cfg Config) *Gateway {
	t.Helper()
	cfg.Providers = []Provider{p}
	g, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestRun_TextPassthrough(t *testing.T) {
	g := newGW(t, newFake("echo", false, "hello world"), Config{})
	res := g.Run(context.Background(), Job{Model: "echo", Prompt: "x"})
	if !res.OK || res.Text != "hello world" {
		t.Fatalf("got %+v", res)
	}
}

func TestRun_JSONMode(t *testing.T) {
	g := newGW(t, newFake("echo", false, "Sure:\n{\"a\":1}\nthanks"), Config{})
	res := g.Run(context.Background(), Job{Model: "echo", Prompt: "x", JSON: true})
	if !res.OK {
		t.Fatalf("not ok: %+v", res)
	}
	m, _ := res.Data.(map[string]any)
	if m["a"] != float64(1) {
		t.Fatalf("data=%v", res.Data)
	}
}

func TestRun_SchemaMode(t *testing.T) {
	g := newGW(t, newFake("echo", true, `{"title":"hi"}`), Config{})
	res := g.Run(context.Background(), Job{Model: "echo", Prompt: "x", JSONSchema: json.RawMessage(`{"type":"object"}`)})
	if !res.OK {
		t.Fatalf("not ok: %+v", res)
	}
	if m, _ := res.Data.(map[string]any); m["title"] != "hi" {
		t.Fatalf("data=%v", res.Data)
	}
}

func TestRun_Skip(t *testing.T) {
	g := newGW(t, newFake("echo", false, "ignored"), Config{})
	res := g.Run(context.Background(), Job{Skip: true, Echo: map[string]any{"k": "v"}})
	if !res.OK || res.Text != "" {
		t.Fatalf("got %+v", res)
	}
	if m, _ := res.Data.(map[string]any); m["k"] != "v" {
		t.Fatalf("echo not returned: %v", res.Data)
	}
}

func TestRun_CacheServesStaleOutput(t *testing.T) {
	fp := newFake("echo", false, `{"v":1}`)
	g := newGW(t, fp, Config{Cache: NewMemoryCache()})
	job := Job{Model: "echo", Prompt: "same", JSON: true}

	first := g.Run(context.Background(), job)
	if m, _ := first.Data.(map[string]any); m["v"] != float64(1) {
		t.Fatalf("first=%v", first.Data)
	}
	// Mutate the live output; a cache HIT must still return the old value.
	v2 := `{"v":2}`
	fp.out.Store(&v2)
	second := g.Run(context.Background(), job)
	if m, _ := second.Data.(map[string]any); m["v"] != float64(1) {
		t.Fatalf("expected cached v=1, got %v", second.Data)
	}
}

func TestRun_PostProcessHook(t *testing.T) {
	hookRan := false
	cfg := Config{PostProcess: func(name string, input json.RawMessage, data any) any {
		if name != "rewriter" {
			return data
		}
		hookRan = true
		return map[string]any{"rewritten": true}
	}}
	g := newGW(t, newFake("echo", false, `{"original":true}`), cfg)
	res := g.Run(context.Background(), Job{Model: "echo", Prompt: "x", PromptName: "", JSON: true})
	if hookRan {
		t.Fatal("hook should not run for empty prompt_name in this case")
	}
	_ = res

	// Now with the matching prompt_name (needs a prompt store to resolve a system
	// prompt; use an in-memory one).
	g2 := newGW(t, newFake("echo", false, `{"original":true}`), Config{
		Prompts:     staticPrompts{"rewriter": ""},
		PostProcess: cfg.PostProcess,
	})
	res2 := g2.Run(context.Background(), Job{Model: "echo", PromptName: "rewriter", Prompt: "x", JSON: true})
	if !hookRan {
		t.Fatal("hook did not run")
	}
	if m, _ := res2.Data.(map[string]any); m["rewritten"] != true {
		t.Fatalf("data not rewritten: %v", res2.Data)
	}
	// text must mirror the rewritten data.
	if !strings.Contains(res2.Text, "rewritten") {
		t.Fatalf("text not mirrored: %q", res2.Text)
	}
}

func TestRunBatch(t *testing.T) {
	g := newGW(t, newFake("echo", false, "ok"), Config{})
	jobs := []Job{{Model: "echo", Prompt: "a"}, {Skip: true, Echo: 5}}
	res := g.RunBatch(context.Background(), jobs)
	if len(res) != 2 || !res[0].OK || res[1].Data != 5 {
		t.Fatalf("batch=%+v", res)
	}
}

func TestRun_UnknownModel(t *testing.T) {
	g := newGW(t, newFake("echo", false, "x"), Config{})
	res := g.Run(context.Background(), Job{Model: "nope", Prompt: "x"})
	if res.OK || res.Status != 400 {
		t.Fatalf("want 400 for unknown model, got %+v", res)
	}
}

func TestHTTPHandler(t *testing.T) {
	g := newGW(t, newFake("echo", false, "pong"), Config{})
	h := NewHandler(g, HandlerOptions{AuthToken: "secret"})

	// /health needs no auth.
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rr.Code != 200 {
		t.Fatalf("health=%d", rr.Code)
	}

	// /v1/llm without auth → 401.
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/llm", strings.NewReader(`{"model":"echo","prompt":"x"}`)))
	if rr.Code != 401 {
		t.Fatalf("want 401, got %d", rr.Code)
	}

	// /v1/llm with auth → 200 pong.
	req := httptest.NewRequest(http.MethodPost, "/v1/llm", strings.NewReader(`{"model":"echo","prompt":"x"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	body, _ := io.ReadAll(rr.Body)
	if !strings.Contains(string(body), "pong") {
		t.Fatalf("body=%s", body)
	}
}

func TestHTTPHandler_BasePaths(t *testing.T) {
	g := newGW(t, newFake("echo", false, "ok"), Config{})
	cases := []struct {
		base, path string
		want       int
	}{
		{"", "/v1/llm", 200}, // default → /v1
		{"/", "/llm", 200},   // root mount (the regression: must NOT be //llm)
		{"", "/llm", 404},    // /llm absent under default base
		{"/api/v2", "/api/v2/llm", 200},
	}
	for _, c := range cases {
		h := NewHandler(g, HandlerOptions{BasePath: c.base})
		req := httptest.NewRequest(http.MethodPost, c.path, strings.NewReader(`{"model":"echo","prompt":"x"}`))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != c.want {
			t.Fatalf("base=%q path=%q: want %d, got %d", c.base, c.path, c.want, rr.Code)
		}
	}
}

// staticPrompts is a trivial in-memory PromptStore for tests.
type staticPrompts map[string]string

func (s staticPrompts) Prompt(name string) (string, error) { return s[name], nil }
func (s staticPrompts) Schema(string) string               { return "" }
