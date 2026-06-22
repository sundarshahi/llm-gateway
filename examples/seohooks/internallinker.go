// Package seohooks holds application-specific PostProcess hooks that live
// OUTSIDE the generic gateway core. InternalLinker is the SEO internal-linker:
// it splices the model's chosen links into the input markdown. It is registered
// via llmgateway.Config.PostProcess and demonstrates how an app extends the
// gateway without forking it.
package seohooks

import (
	"encoding/json"
	"strings"

	llmgateway "github.com/sundarshahi/llm-gateway"
)

// Compile-time proof that InternalLinker plugs into Config.PostProcess.
var _ llmgateway.PostProcessFunc = InternalLinker

// InternalLinker is a llmgateway.PostProcessFunc. For the "internal-linker"
// stage it rewrites {links_added:[{anchor,url}]} into {markdown, links_added}
// by splicing each link into the job's input markdown; every other stage passes
// through unchanged.
func InternalLinker(promptName string, input json.RawMessage, data any) any {
	if promptName != "internal-linker" {
		return data
	}
	obj, ok := data.(map[string]any)
	if !ok {
		return data
	}
	links, _ := obj["links_added"].([]any)
	md := inputMarkdown(input)
	if md == "" {
		return data
	}
	newMD, inserted := insertInternalLinks(md, links)
	return map[string]any{"markdown": newMD, "links_added": inserted}
}

func inputMarkdown(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v map[string]any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	s, _ := v["markdown"].(string)
	return s
}

// insertInternalLinks wraps each {anchor,url} as a Markdown link at the first
// verbatim, not-already-linked occurrence of anchor. Returns the new markdown +
// only the links actually placed (unmatched anchors are skipped).
func insertInternalLinks(md string, links []any) (string, []any) {
	inserted := []any{}
	for _, x := range links {
		l, ok := x.(map[string]any)
		if !ok {
			continue
		}
		anchor, _ := l["anchor"].(string)
		url, _ := l["url"].(string)
		if anchor == "" || url == "" {
			continue
		}
		pos := findUnlinkedOccurrence(md, anchor)
		if pos < 0 {
			continue
		}
		md = md[:pos] + "[" + anchor + "](" + url + ")" + md[pos+len(anchor):]
		inserted = append(inserted, map[string]any{"anchor": anchor, "url": url})
	}
	return md, inserted
}

// findUnlinkedOccurrence returns the index of the first verbatim anchor safe to
// wrap: not already a link anchor and not on a heading line. -1 if none.
func findUnlinkedOccurrence(md, anchor string) int {
	from := 0
	for {
		i := strings.Index(md[from:], anchor)
		if i < 0 {
			return -1
		}
		pos := from + i
		var before byte
		if pos > 0 {
			before = md[pos-1]
		}
		if before == '[' {
			from = pos + len(anchor)
			continue
		}
		lineStart := strings.LastIndexByte(md[:pos], '\n') + 1
		if strings.HasPrefix(strings.TrimSpace(md[lineStart:pos]), "#") {
			from = pos + len(anchor)
			continue
		}
		return pos
	}
}
