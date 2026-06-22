package llmgateway

import (
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Tuner resolves per-stage configuration that isn't set explicitly on a Job.
// All methods take the stage's prompt_name. Job-level fields always win over
// whatever a Tuner returns; the Tuner only fills the gaps.
type Tuner interface {
	// ModelName returns the model id for a stage, or "" for the provider default.
	ModelName(promptName string) string
	// Samples returns the self-consistency sample count for a stage (>=1).
	Samples(promptName string) int
	// Thinking returns the MAX_THINKING_TOKENS for a stage; ok=false → inherit.
	Thinking(promptName string) (value string, ok bool)
}

var nonAlnum = regexp.MustCompile(`[^A-Z0-9]+`)

func envKeySuffix(promptName string) string {
	return nonAlnum.ReplaceAllString(strings.ToUpper(promptName), "_")
}

// EnvTuner reads per-stage tuning from the environment, preserving the original
// wrapper's scheme:
//
//	model:    LLM_MODEL_<NAME>  → DefaultModel
//	samples:  LLM_VOTE_<NAME>   → 1
//	thinking: LLM_THINK_<NAME>  → LLM_THINK_DEFAULT
//
// <NAME> is the upper-cased prompt name with non-alphanumerics collapsed to "_".
type EnvTuner struct {
	// DefaultModel is the fallback when no per-stage LLM_MODEL_<NAME> is set.
	// Defaults to the LLM_DEFAULT_MODEL env var when constructed via FromEnv.
	DefaultModel string
}

func (t EnvTuner) ModelName(promptName string) string {
	if promptName != "" {
		if v := os.Getenv("LLM_MODEL_" + envKeySuffix(promptName)); v != "" {
			return v
		}
	}
	return t.DefaultModel
}

func (t EnvTuner) Samples(promptName string) int {
	if promptName != "" {
		if v := os.Getenv("LLM_VOTE_" + envKeySuffix(promptName)); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				return n
			}
		}
	}
	return 1
}

func (t EnvTuner) Thinking(promptName string) (string, bool) {
	if promptName != "" {
		if v := os.Getenv("LLM_THINK_" + envKeySuffix(promptName)); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				return strconv.Itoa(n), true
			}
		}
	}
	if v := os.Getenv("LLM_THINK_DEFAULT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return strconv.Itoa(n), true
		}
	}
	return "", false
}
