# syntax=docker/dockerfile:1
# Generic LLM gateway image: the static Go server + the Claude Code native binary
# (spawned as a subprocess). No application prompts are baked in — mount your own
# PROMPTS_DIR/SCHEMAS_DIR at runtime, or build an app-specific image FROM this one.
#
# Build:  docker build -t llm-gateway:latest .

# ---- go builder: compile the static server (no CGO -> runs on bare alpine) ----
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /llm-gateway ./cmd/llm-gateway

# ---- claude builder: fetch the self-contained Claude Code native binary ----
FROM alpine:3.24 AS claude
RUN apk add --no-cache curl bash unzip ca-certificates
ENV HOME=/home/node
RUN mkdir -p /home/node
ARG CLAUDE_VERSION=2.1.181
RUN curl -fsSL https://claude.ai/install.sh | bash -s -- "${CLAUDE_VERSION}"

# ---- final: bare alpine + claude binary + go binary ----
FROM alpine:3.24
RUN apk add --no-cache ca-certificates libstdc++ libgcc gcompat bash
RUN addgroup -g 1000 node && adduser -u 1000 -G node -s /bin/sh -D node \
 && mkdir -p /app && chown -R node:node /app
USER node
ENV HOME=/home/node
COPY --from=claude --chown=node:node /home/node/.local /home/node/.local
COPY --from=build  /llm-gateway /usr/local/bin/llm-gateway
ENV PATH=/home/node/.local/bin:/usr/local/bin:/usr/bin:/bin
WORKDIR /app
ENV LLM_HOST=0.0.0.0 LLM_PORT=8787
EXPOSE 8787
HEALTHCHECK --interval=10s --timeout=3s --start-period=25s --retries=3 \
  CMD wget -qO- "http://localhost:${LLM_PORT}/health" >/dev/null 2>&1 || exit 1
CMD ["llm-gateway"]
