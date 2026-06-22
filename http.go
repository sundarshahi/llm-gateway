package llmgateway

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// HandlerOptions configures the HTTP surface built by NewHandler.
type HandlerOptions struct {
	// AuthToken, when non-empty, requires a matching "Authorization: Bearer <t>"
	// or "X-LLM-Token: <t>" header on everything except /health.
	AuthToken string
	// MaxBodyBytes caps request bodies. 0 → 25 MiB.
	MaxBodyBytes int64
	// BasePath prefixes the LLM routes. Default "/v1" → POST /v1/llm,
	// POST /v1/llm/batch. /health is always served at the root.
	BasePath string
}

// NewHandler builds an http.Handler exposing the generic LLM routes for a
// Gateway: GET /health, POST <base>/llm, POST <base>/llm/batch. Application
// endpoints are intentionally absent — compose this handler into your own mux
// (see Mux) and add them alongside.
func NewHandler(g *Gateway, opt HandlerOptions) http.Handler {
	if opt.MaxBodyBytes <= 0 {
		opt.MaxBodyBytes = 25 * 1024 * 1024
	}
	base := opt.BasePath
	if base == "" {
		base = "/v1"
	}
	base = "/" + strings.Trim(base, "/")
	llmPath := base + "/llm"
	batchPath := base + "/llm/batch"

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if opt.AuthToken != "" && !authorized(r, opt.AuthToken) {
				writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
				return
			}
			next(w, r)
		}
	}

	mux.HandleFunc(batchPath, auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "not found"})
			return
		}
		var body struct {
			Jobs []Job `json:"jobs"`
		}
		if !readJSON(w, r, opt.MaxBodyBytes, &body) {
			return
		}
		if len(body.Jobs) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "require jobs:[]"})
			return
		}
		results := g.RunBatch(r.Context(), body.Jobs)
		out := make([]map[string]any, len(results))
		for i, res := range results {
			out[i] = res.body()
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "results": out})
	}))

	mux.HandleFunc(llmPath, auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "not found"})
			return
		}
		var j Job
		if !readJSON(w, r, opt.MaxBodyBytes, &j) {
			return
		}
		res := g.Run(r.Context(), j)
		status := res.Status
		if status == 0 {
			status = http.StatusOK
		}
		writeJSON(w, status, res.body())
	}))

	return mux
}

func authorized(r *http.Request, token string) bool {
	tok := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tok = h[len("Bearer "):]
	} else {
		tok = r.Header.Get("X-LLM-Token")
	}
	return len(tok) == len(token) && subtle.ConstantTimeCompare([]byte(tok), []byte(token)) == 1
}

func readJSON(w http.ResponseWriter, r *http.Request, max int64, dst any) bool {
	b, err := io.ReadAll(io.LimitReader(r.Body, max+1))
	if err != nil || int64(len(b)) > max {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid or oversized body"})
		return false
	}
	if len(b) == 0 {
		b = []byte("{}")
	}
	if err := json.Unmarshal(b, dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid JSON body"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, obj any) {
	b, err := marshalNoEscape(obj)
	if err != nil {
		b = []byte(`{"ok":false,"error":"encode failed"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(b)
}
