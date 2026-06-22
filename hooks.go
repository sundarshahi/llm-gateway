package llmgateway

import "encoding/json"

// PostProcessFunc transforms the structured data of a JSON/schema reply before
// it is returned (and before it is used as the cached "text"). It receives the
// resolved prompt_name, the job's raw Input, and the parsed data, and returns
// the (possibly rewritten) data. It must be safe for concurrent use and should
// be a no-op for stages it doesn't handle.
//
// This is the seam that keeps application-specific reply rewriting (e.g. an SEO
// internal-linker that splices links into the input markdown) out of the core.
type PostProcessFunc func(promptName string, input json.RawMessage, data any) any
