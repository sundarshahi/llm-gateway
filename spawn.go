package llmgateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// errCLITimeout marks a deadline-exceeded spawn so the retry loop skips retrying
// it (re-running a timeout just burns another full Timeout).
var errCLITimeout = errors.New("cli timeout")

// errCLIUsageLimit marks a spawn that failed because the provider account hit a
// usage/rate limit. Retrying can't succeed until the limit resets, so the retry
// loop returns immediately with a 429 instead of burning attempts on a 502.
var errCLIUsageLimit = errors.New("cli usage limit")

// usageLimitMarkers are substrings the CLIs print (to stdout) when an account is
// out of quota. Matched case-insensitively against combined CLI output.
var usageLimitMarkers = []string{
	"weekly limit",
	"usage limit",
	"rate limit",
	"hit your limit",
	"reached your",
	"quota",
	"too many requests",
}

func isUsageLimit(s string) bool {
	l := strings.ToLower(s)
	for _, m := range usageLimitMarkers {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

// runCLI acquires a concurrency slot, then spawns the provider's command. The
// parent ctx is honored: if the caller (e.g. a disconnected HTTP client) cancels
// it, the spawn is killed and the slot freed. A per-spawn timeout is layered on
// top of the parent deadline.
func (g *Gateway) runCLI(ctx context.Context, p Provider, r SpawnRequest) (string, error) {
	select {
	case g.sem <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-g.sem }()
	return g.spawn(ctx, p, r)
}

func (g *Gateway) spawn(ctx context.Context, p Provider, r SpawnRequest) (string, error) {
	inv, err := p.Invocation(r)
	if err != nil {
		return "", err
	}

	cctx, cancel := context.WithTimeout(ctx, g.cfg.Timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, inv.Cmd, inv.Args...)
	if len(inv.Env) > 0 {
		cmd.Env = append(os.Environ(), inv.Env...)
	}
	cmd.Stdin = strings.NewReader(inv.Stdin)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb

	err = cmd.Run()
	// Distinguish our per-spawn timeout from a parent cancellation (client gone).
	if cctx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
		return "", fmt.Errorf("%s timed out after %v: %w", inv.Cmd, g.cfg.Timeout, errCLITimeout)
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			// The Claude/Gemini CLIs print fatal messages (e.g. "You've hit your
			// weekly limit") to stdout, not stderr. Fall back to stdout so the real
			// cause surfaces in logs and the HTTP error instead of "(no stderr)".
			detail := strings.TrimSpace(errb.String())
			if detail == "" {
				detail = strings.TrimSpace(out.String())
			}
			if detail == "" {
				detail = "(no output)"
			}
			wrapped := fmt.Errorf("%s exited %d: %s", inv.Cmd, ee.ExitCode(), detail)
			if isUsageLimit(detail) {
				return "", fmt.Errorf("%w: %w", errCLIUsageLimit, wrapped)
			}
			return "", wrapped
		}
		return "", fmt.Errorf("failed to spawn %s: %s (installed + on PATH + authenticated?)", inv.Cmd, err.Error())
	}
	return strings.TrimSpace(out.String()), nil
}

func head(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func nowMS(t time.Time) int64 { return time.Since(t).Milliseconds() }
