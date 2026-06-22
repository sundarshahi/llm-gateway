// Package llmgateway is a provider-agnostic gateway for driving local LLM CLIs
// (Claude Code, Gemini CLI, or any custom Provider) as a fast, cacheable,
// structured-output service.
//
// It can be used two ways:
//
//   - As a Go library: construct a Gateway with New and call Run / RunBatch.
//   - As an HTTP service: wrap a Gateway with NewHandler and serve it.
//
// The core is intentionally free of any application-specific logic. Concerns
// that vary per application — where prompts live, how responses are cached, how
// per-stage tuning is resolved, and post-processing of structured replies — are
// injected through interfaces on Config (PromptStore, Cache, Tuner) and the
// PostProcess hook. Providers (Claude, Gemini, custom) are pluggable too.
//
// Runtime dependency: the configured providers shell out to their CLI binaries
// (e.g. "claude", "gemini"), which must be installed, on PATH, and authenticated
// on the host. This is a CLI-orchestration library, not a pure HTTP SDK.
package llmgateway
