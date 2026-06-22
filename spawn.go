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
			stderr := strings.TrimSpace(errb.String())
			if stderr == "" {
				stderr = "(no stderr)"
			}
			return "", fmt.Errorf("%s exited %d: %s", inv.Cmd, ee.ExitCode(), stderr)
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
