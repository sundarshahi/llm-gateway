package llmgateway

import (
	"bytes"
	"encoding/json"
	"strings"
)

// marshalNoEscape serializes like JSON.stringify (no HTML escaping of <, >, &).
func marshalNoEscape(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// inputToString mirrors `typeof input === 'string' ? input : JSON.stringify(input)`.
func inputToString(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := marshalNoEscape(v)
	if err != nil {
		return string(raw)
	}
	return string(b)
}

// extractJSON pulls JSON out of a model reply that may be fenced, chatty, or
// malformed in the ways schema-less stages reliably produce on long free-form
// output: double-escaped envelopes, interior code fences, and invalid string
// escapes. See the per-recovery comments below.
func extractJSON(text string) (any, error) {
	t := strings.TrimSpace(text)

	// Try the text as-is, then with a whole-reply fence removed. The as-is pass
	// MUST come first so a valid envelope carrying an interior code fence parses
	// on its fast path before any fence touch.
	for _, cand := range []string{t, stripWrappingFence(t)} {
		c := narrowToJSON(cand)
		if v, ok := tryParseJSON(c); ok {
			return v, nil
		}
		// Recovery 1 — double-escaped JSON: the model stringified its own JSON.
		if v, ok := tryUnescapeOnce(c); ok {
			return v, nil
		}
		// Recovery 2 — structurally-valid JSON carrying invalid backslash escapes.
		if v, ok := tryParseJSON(sanitizeStringEscapes(c)); ok {
			return v, nil
		}
	}
	var v any
	err := json.Unmarshal([]byte(narrowToJSON(t)), &v)
	if err == nil {
		return v, nil
	}
	return nil, err
}

// stripWrappingFence removes a Markdown code fence that wraps the ENTIRE reply
// (```json … ```), returning the inner body. No-op unless the text BEGINS with a
// fence, so a fence inside a JSON string value is never touched.
func stripWrappingFence(t string) string {
	if !strings.HasPrefix(t, "```") {
		return t
	}
	nl := strings.IndexByte(t, '\n')
	if nl == -1 {
		return t
	}
	body := t[nl+1:]
	if i := strings.LastIndex(body, "```"); i != -1 {
		body = body[:i]
	}
	return strings.TrimSpace(body)
}

// narrowToJSON trims chatter around a JSON body.
func narrowToJSON(t string) string {
	if strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
		return t
	}
	if s := strings.IndexByte(t, '{'); s != -1 {
		if e := strings.LastIndexByte(t, '}'); e > s {
			return t[s : e+1]
		}
	}
	if s := strings.IndexByte(t, '['); s != -1 {
		if e := strings.LastIndexByte(t, ']'); e > s {
			return t[s : e+1]
		}
	}
	return t
}

func tryParseJSON(t string) (any, bool) {
	var v any
	if err := json.Unmarshal([]byte(t), &v); err != nil {
		return nil, false
	}
	return v, true
}

// tryUnescapeOnce reverses one level of JSON-string escaping. Guarded on an
// escaped quote being present and on the unwrapped result parsing as JSON.
func tryUnescapeOnce(t string) (any, bool) {
	if !strings.Contains(t, "\\\"") {
		return nil, false
	}
	var s string
	if err := json.Unmarshal([]byte(`"`+t+`"`), &s); err != nil {
		return nil, false
	}
	return tryParseJSON(strings.TrimSpace(s))
}

// sanitizeStringEscapes drops backslash escapes JSON forbids from inside string
// values, preserving valid escapes. Best-effort: only invoked after a clean
// parse has already failed.
func sanitizeStringEscapes(t string) string {
	var b strings.Builder
	b.Grow(len(t))
	inStr := false
	for i := 0; i < len(t); i++ {
		c := t[i]
		if !inStr {
			b.WriteByte(c)
			if c == '"' {
				inStr = true
			}
			continue
		}
		if c == '"' {
			inStr = false
			b.WriteByte(c)
			continue
		}
		if c == '\\' {
			if i+1 >= len(t) {
				continue
			}
			switch n := t[i+1]; n {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't', 'u':
				b.WriteByte(c)
				b.WriteByte(n)
			default:
				b.WriteByte(n)
			}
			i++
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
