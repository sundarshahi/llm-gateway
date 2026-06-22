package llmgateway

// Event is a single completed (or failed) job observation for metrics.
type Event struct {
	PromptName string
	Model      string
	ModelName  string
	DurationMS int64
	CacheHit   bool
	Attempts   int
	Voted      bool
	OK         bool
}

// Metrics receives one Event per finished job. Implementations must be safe for
// concurrent use and must not block.
type Metrics interface {
	Observe(Event)
}

// nopMetrics is the default no-op sink.
type nopMetrics struct{}

func (nopMetrics) Observe(Event) {}
