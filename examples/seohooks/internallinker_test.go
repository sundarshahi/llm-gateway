package seohooks

import (
	"encoding/json"
	"testing"
)

func TestInternalLinker_Splices(t *testing.T) {
	input := json.RawMessage(`{"markdown":"Intro about widgets and gadgets here."}`)
	data := map[string]any{"links_added": []any{
		map[string]any{"anchor": "widgets", "url": "/widgets"},
		map[string]any{"anchor": "missing", "url": "/missing"},
	}}
	out, ok := InternalLinker("internal-linker", input, data).(map[string]any)
	if !ok {
		t.Fatalf("unexpected type %T", out)
	}
	md, _ := out["markdown"].(string)
	if want := "[widgets](/widgets)"; !contains(md, want) {
		t.Fatalf("anchor not spliced: %q", md)
	}
	inserted, _ := out["links_added"].([]any)
	if len(inserted) != 1 {
		t.Fatalf("expected 1 placed link (missing skipped), got %d", len(inserted))
	}
}

func TestInternalLinker_PassThroughOtherStages(t *testing.T) {
	data := map[string]any{"x": 1}
	got, ok := InternalLinker("writer", nil, data).(map[string]any)
	if !ok || got["x"] != 1 {
		t.Fatalf("should pass through unchanged, got %v", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
