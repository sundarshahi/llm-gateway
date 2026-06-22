// Command customserver shows how to compose the generic gateway with your own
// application endpoints, instead of forking it. It:
//
//   - builds a Gateway from env (FromEnv wires a file-backed PromptStore),
//   - registers a PostProcess hook (the internal-linker from examples/seohooks),
//   - mounts the generic LLM routes (/llm, /llm/batch, /health),
//   - adds an application route (/prompt/learn) that reuses the gateway's
//     PromptStore to layer learned rules onto a base prompt at runtime.
//
// This is the pattern to follow for any app-specific surface (content storage,
// rendering, tuning loops, ...): keep the reusable core as an imported library
// and add your own handlers alongside NewHandler.
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"

	llmgateway "github.com/sundarshahi/llm-gateway"
	"github.com/sundarshahi/llm-gateway/examples/seohooks"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := llmgateway.FromEnv()
	if err != nil {
		logger.Error("config", "err", err)
		os.Exit(1)
	}
	cfg.Logger = logger
	cfg.PostProcess = seohooks.InternalLinker // an example app-specific reply rewrite

	gw, err := llmgateway.New(cfg)
	if err != nil {
		logger.Error("gateway", "err", err)
		os.Exit(1)
	}

	token := os.Getenv("LLM_AUTH_TOKEN")

	mux := http.NewServeMux()
	// Generic LLM routes from the library. BasePath "/" exposes POST /llm and
	// POST /llm/batch (use "/v1" for versioned paths).
	mux.Handle("/", llmgateway.NewHandler(gw, llmgateway.HandlerOptions{
		AuthToken: token,
		BasePath:  "/",
	}))

	// Application route example: prompt tuning. Reuses the same PromptStore the
	// gateway resolves prompt_name against, so a learned addendum takes effect on
	// the next call.
	if store, ok := cfg.Prompts.(*llmgateway.FilePromptStore); ok {
		mux.HandleFunc("/prompt/learn", learnHandler(store, token))
	}

	addr := envOr("LLM_HOST", "127.0.0.1") + ":" + envOr("LLM_PORT", "8787")
	logger.Info("customserver listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		logger.Error("serve", "err", err)
		os.Exit(1)
	}
}

// learnHandler writes (or clears) a learned addendum for a stage via the shared
// PromptStore and busts its cache. Body: {"name":"<stage>","addendum":"<rules>"}.
func learnHandler(store *llmgateway.FilePromptStore, token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token != "" && r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, `{"ok":false,"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		var body struct {
			Name     string `json:"name"`
			Addendum string `json:"addendum"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
			http.Error(w, `{"ok":false,"error":"bad body"}`, http.StatusBadRequest)
			return
		}
		if !llmgateway.ValidPromptName(body.Name) {
			http.Error(w, `{"ok":false,"error":"invalid name"}`, http.StatusBadRequest)
			return
		}
		if err := store.SetAddendum(body.Name, body.Addendum); err != nil {
			http.Error(w, `{"ok":false,"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "name": body.Name, "learned": body.Addendum != ""})
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
