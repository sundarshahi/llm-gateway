# llm-gateway

**Run Claude on your own server — authenticated by your Claude subscription, no API key required.**

`llm-gateway` turns the [Claude Code](https://claude.com/claude-code) CLI (and optionally the Gemini CLI) into a small, fast HTTP service you self-host. Point any app — n8n, a Worker, a Python script, another Go service — at one endpoint and get cached, retried, schema-validated JSON back.

Because it drives the Claude Code CLI, it authenticates with a **Claude subscription OAuth token** (`CLAUDE_CODE_OAUTH_TOKEN`) — so you can run it on a server **without an Anthropic API key and without per-token API billing**. An API key works too if you prefer pay-as-you-go.

> ⚠️ Runtime dependency: the `claude` (and `gemini`) CLI must be installed, on `PATH`, and authenticated on the host. This is a CLI-orchestration service, not a hosted API SDK.

---

## Two kinds of "key" — don't mix them up

| Key | Env var | What it's for |
|-----|---------|---------------|
| **Server AUTH token** | `LLM_AUTH_TOKEN` | A bearer token **you choose**. Clients must send it to use *your* gateway. This is what locks the endpoint down on a public server. |
| **Claude auth** | `CLAUDE_CODE_OAUTH_TOKEN` *(recommended)* **or** `ANTHROPIC_API_KEY` | How the `claude` CLI authenticates to Anthropic. The OAuth token uses your **Claude subscription** (no API key); the API key uses pay-as-you-go billing. |

So a locked-down, subscription-auth server sets **both**: `LLM_AUTH_TOKEN` (your gate) and `CLAUDE_CODE_OAUTH_TOKEN` (Claude's auth — no API key).

### Get a Claude OAuth token (no API key)

On a machine where you're logged into Claude Code:

```bash
claude setup-token        # prints a long-lived OAuth token
```

Put it on the server as `CLAUDE_CODE_OAUTH_TOKEN`. That's it — Claude now runs on your subscription, no API key involved.

---

## Features

- **Run Claude headless on a server** — subscription OAuth token *or* API key.
- **Structured output** — native JSON-schema mode (Claude), or schema-less JSON with tolerant extraction/repair (double-escaped envelopes, interior code fences, invalid string escapes).
- **Content-addressed cache** — identical requests return byte-identical replies and skip the subprocess entirely (see the 0.01s cache hits below). Pluggable: file, in-memory, or your own.
- **Self-consistency voting** — run N samples, return the JSON majority; the winner is cached.
- **Per-stage tuning** — model, vote count, and thinking budget resolved per named prompt (env by default, or a custom `Tuner`).
- **Retries + timeouts** — transient errors and invalid JSON retried; timeouts never are. Honors request-context cancellation (a disconnected client kills the spawn).
- **Concurrency limiting**, graceful drain, bearer auth, structured JSON logs, and a metrics hook.
- **Pluggable everything** — providers, cache, prompt store, post-processing, metrics.

---

## Quick start

### Docker (recommended for a server)

```bash
docker build -t llm-gateway .

docker run -p 8787:8787 \
  -e LLM_AUTH_TOKEN="$(openssl rand -hex 24)" \
  -e CLAUDE_CODE_OAUTH_TOKEN="<from: claude setup-token>" \
  llm-gateway
```

The image bakes the Claude Code binary; you supply auth at runtime.

### Binary

```bash
go build -o llm-gateway ./cmd/llm-gateway
LLM_AUTH_TOKEN=secret CLAUDE_CODE_OAUTH_TOKEN=... ./llm-gateway
```

### Call it

```bash
TOKEN=secret

# Plain text
curl -s localhost:8787/v1/llm -H "Authorization: Bearer $TOKEN" \
  -d '{"model":"claude","prompt":"Reply with exactly: PONG"}'
# {"model":"claude","ok":true,"text":"PONG"}

# Schema-validated JSON (native structured output)
curl -s localhost:8787/v1/llm -H "Authorization: Bearer $TOKEN" \
  -d '{"model":"claude","prompt":"Capital of Japan and its population in millions.",
       "json_schema":{"type":"object","properties":{"capital":{"type":"string"},
       "population_millions":{"type":"number"}},"required":["capital","population_millions"]}}'
# {"data":{"capital":"Tokyo","population_millions":37.4},"model":"claude","ok":true,...}
```

---

## HTTP API

| Method & path | Purpose |
|---------------|---------|
| `GET /health` | Liveness (no auth). |
| `POST /v1/llm` | Run one job. Body = a `Job`. |
| `POST /v1/llm/batch` | Run many concurrently. Body = `{"jobs":[Job, ...]}`. |

Auth: send `Authorization: Bearer <LLM_AUTH_TOKEN>` (or `X-LLM-Token`) on everything except `/health`. The route prefix is configurable (`LLM_BASE_PATH`, default `/v1`).

### The `Job` object

| Field | Type | Meaning |
|-------|------|---------|
| `model` | string | Provider: `"claude"` or `"gemini"`. |
| `prompt` | string | User prompt. |
| `input` | any | Replaces `prompt`: a string is used as-is, anything else is JSON-encoded. |
| `system` | string | System prompt (ignored when `prompt_name` resolves one). |
| `prompt_name` | string | Loads a stored system prompt + optional schema; keys per-stage tuning. |
| `json` | bool | Expect a JSON reply (extracted from text). |
| `json_schema` | object | Native structured output (Claude). Implies `json`. |
| `samples` | int | `>1` → self-consistency voting. |
| `max_thinking_tokens` | int | Cap extended thinking for this call. |
| `model_name` | string | Specific model id (overrides defaults). |
| `tools` | string/array | Tool allow-list (default: none → pure, fast LLM call). |
| `skip` / `echo` | bool / any | Short-circuit: return `echo` as data (pipeline plumbing). |

---

## Use as a Go library

```go
cfg := llmgateway.Config{
    MaxConcurrency: 4,
    Cache:          llmgateway.NewMemoryCache(),
    // Providers default to Claude + Gemini.
}
gw, _ := llmgateway.New(cfg)

res := gw.Run(ctx, llmgateway.Job{
    Model:  "claude",
    Prompt: "Summarize this in one sentence: ...",
    JSON:   true,
})
fmt.Println(res.OK, res.Data)
```

## Extension points

The core carries **no** application logic. Everything app-specific is injected on `Config`:

| Seam | Interface | Default |
|------|-----------|---------|
| Providers | `Provider` | Claude + Gemini |
| Where prompts/schemas live | `PromptStore` | `FilePromptStore` (optional learned-rules addendum) |
| Per-stage model/votes/thinking | `Tuner` | `EnvTuner` |
| Response cache | `Cache` | none (set `FileCache` / `MemoryCache`) |
| Reply rewriting | `PostProcess` hook | none |
| Metrics | `Metrics` | no-op |

See [`examples/seohooks`](examples/seohooks) for a real `PostProcess` hook and [`examples/customserver`](examples/customserver) for composing the generic routes with your own endpoints (here, a `/prompt/learn` tuning route).

---

## Configuration (env)

| Var | Default | Purpose |
|-----|---------|---------|
| `LLM_AUTH_TOKEN` | *(none)* | Bearer token clients must send. **Set this on any public server.** |
| `CLAUDE_CODE_OAUTH_TOKEN` | *(none)* | Claude subscription auth (no API key). |
| `ANTHROPIC_API_KEY` | *(none)* | Alternative Claude auth (pay-as-you-go). |
| `LLM_HOST` / `LLM_PORT` | `127.0.0.1` / `8787` | Bind address. |
| `LLM_BASE_PATH` | `/v1` | Route prefix for the LLM endpoints. |
| `LLM_MAX_CONCURRENCY` | `4` | Max simultaneous CLI spawns. |
| `LLM_TIMEOUT_MS` | `600000` | Per-call timeout. |
| `LLM_RETRIES` | `2` | Extra attempts on transient error / invalid JSON. |
| `LLM_CACHE_DIR` | *(off)* | Enable the on-disk response cache. |
| `LLM_DEFAULT_MODEL` | *(CLI default)* | Fallback model id. |
| `LLM_MODEL_<STAGE>` · `LLM_VOTE_<STAGE>` · `LLM_THINK_<STAGE>` / `LLM_THINK_DEFAULT` | — | Per-stage tuning by `prompt_name`. |

---

## License

MIT
