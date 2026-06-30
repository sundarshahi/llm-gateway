package llmgateway

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsUsageLimit(t *testing.T) {
	hits := []string{
		"You've hit your weekly limit · resets 11am (UTC)",
		"Usage limit reached",
		"429 Too Many Requests",
		"You have reached your quota for this period",
		"Rate limit exceeded",
	}
	for _, s := range hits {
		if !isUsageLimit(s) {
			t.Errorf("expected usage-limit match for %q", s)
		}
	}

	misses := []string{
		"claude exited 1: (no output)",
		"invalid JSON output",
		"context deadline exceeded",
		"",
	}
	for _, s := range misses {
		if isUsageLimit(s) {
			t.Errorf("did not expect usage-limit match for %q", s)
		}
	}
}

func TestUsageLimitErrorIsDetectable(t *testing.T) {
	wrapped := fmt.Errorf("claude exited 1: You've hit your weekly limit")
	err := fmt.Errorf("%w: %w", errCLIUsageLimit, wrapped)
	if !errors.Is(err, errCLIUsageLimit) {
		t.Fatal("errors.Is should detect errCLIUsageLimit through the wrap")
	}
	// Original detail must remain in the message so logs/n8n see the real cause.
	if got := err.Error(); !isUsageLimit(got) {
		t.Errorf("wrapped error lost the underlying detail: %q", got)
	}
}
