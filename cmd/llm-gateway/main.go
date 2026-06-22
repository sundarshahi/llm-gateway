// Command llm-gateway is the generic HTTP server for the llmgateway library:
// POST /v1/llm, POST /v1/llm/batch, GET /health. It carries no application
// endpoints — to add your own (storage, prompt tuning, etc.), import the
// library and compose llmgateway.NewHandler into your own mux instead.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	llmgateway "github.com/sundarshahi/llm-gateway"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := llmgateway.FromEnv()
	if err != nil {
		logger.Error("config error", "err", err)
		os.Exit(1)
	}
	cfg.Logger = logger

	gw, err := llmgateway.New(cfg)
	if err != nil {
		logger.Error("gateway init failed", "err", err)
		os.Exit(1)
	}

	host := env("LLM_HOST", "127.0.0.1")
	port := env("LLM_PORT", "8787")
	handler := llmgateway.NewHandler(gw, llmgateway.HandlerOptions{
		AuthToken:    os.Getenv("LLM_AUTH_TOKEN"),
		MaxBodyBytes: int64(envInt("LLM_MAX_BODY_BYTES", 25*1024*1024)),
		BasePath:     env("LLM_BASE_PATH", "/v1"),
	})

	srv := &http.Server{Addr: host + ":" + port, Handler: handler}
	go func() {
		logger.Info("llm-gateway listening", "addr", "http://"+host+":"+port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("listen error", "err", err)
			os.Exit(1)
		}
	}()

	// Graceful drain on SIGTERM/SIGINT: stop accepting, let in-flight finish.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	s := <-sig
	timeoutMS := envInt("LLM_TIMEOUT_MS", 600000)
	logger.Info("draining", "signal", fmt.Sprint(s))
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMS+15000)*time.Millisecond)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("drain timeout — forcing exit")
		os.Exit(0)
	}
	logger.Info("drained cleanly")
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
