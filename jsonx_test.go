package llmgateway

import (
	"encoding/json"
	"testing"
)

func wantObj(t *testing.T, in, want string) {
	t.Helper()
	got, err := extractJSON(in)
	if err != nil {
		t.Fatalf("extractJSON(%q) errored: %v", in, err)
	}
	gb, _ := json.Marshal(got)
	var w any
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("bad want fixture: %v", err)
	}
	wb, _ := json.Marshal(w)
	if string(gb) != string(wb) {
		t.Fatalf("extractJSON(%q)\n got: %s\nwant: %s", in, gb, wb)
	}
}

func TestExtractJSON_Clean(t *testing.T) {
	wantObj(t, `{"a":1,"b":"x"}`, `{"a":1,"b":"x"}`)
	wantObj(t, `[1,2,3]`, `[1,2,3]`)
}

func TestExtractJSON_FencedAndChatty(t *testing.T) {
	wantObj(t, "```json\n{\"a\":1}\n```", `{"a":1}`)
	wantObj(t, "Sure! Here you go:\n{\"a\":1}\nHope that helps", `{"a":1}`)
}

func TestExtractJSON_DoubleEscaped(t *testing.T) {
	wantObj(t, `{\"markdown\":\"hello\",\"word_count\":5}`, `{"markdown":"hello","word_count":5}`)
	wantObj(t, `{\"markdown\":\"L1\\nL2\"}`, `{"markdown":"L1\nL2"}`)
}

func TestExtractJSON_InvalidStringEscapes(t *testing.T) {
	wantObj(t, `{"markdown":"cost is \$5"}`, `{"markdown":"cost is $5"}`)
	wantObj(t, `{"markdown":"range \[a,b\] and \( x \)"}`, `{"markdown":"range [a,b] and ( x )"}`)
	wantObj(t, `{"q":"he said \"hi\" \% done"}`, `{"q":"he said \"hi\" % done"}`)
}

func TestExtractJSON_InteriorCodeFence(t *testing.T) {
	in := "{\"markdown\":\"Intro.\\n\\n```python\\nfrom x import y\\nz = {1: 2}\\n```\\n\\nOutro.\",\"word_count\":3}"
	wantObj(t, in, `{"markdown":"Intro.\n\n`+"```python\\nfrom x import y\\nz = {1: 2}\\n```"+`\n\nOutro.","word_count":3}`)
	in2 := "{\"markdown\":\"```js\\nconst a=1;\\n```\\n\\nmid\\n\\n```sh\\nls -la\\n```\"}"
	wantObj(t, in2, `{"markdown":"`+"```js\\nconst a=1;\\n```"+`\n\nmid\n\n`+"```sh\\nls -la\\n```"+`"}`)
}

func TestExtractJSON_WrappingFenceWithInteriorFence(t *testing.T) {
	in := "```json\n{\"markdown\":\"see ```py\\ncode\\n``` here\"}\n```"
	wantObj(t, in, `{"markdown":"see `+"```py\\ncode\\n```"+` here"}`)
}

func TestExtractJSON_Unrecoverable(t *testing.T) {
	if _, err := extractJSON(`this is not json at all`); err == nil {
		t.Fatal("want error for non-JSON text")
	}
	if _, err := extractJSON(`{"a": }`); err == nil {
		t.Fatal("want error for genuinely broken JSON")
	}
}
