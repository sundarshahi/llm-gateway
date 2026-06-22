package llmgateway

import (
	"context"
	"errors"
	"time"
)

// runVoted runs N samples and returns the JSON majority (self-consistency). Only
// valid samples vote; ties break toward the first seen. The winning sample's raw
// stdout is cached, so the expensive vote runs once and identical re-runs are
// served from cache. Samples run sequentially through the global concurrency
// gate so a voting job can't monopolize every CLI slot.
func (g *Gateway) runVoted(ctx context.Context, j Job, r resolved, t0 time.Time) Result {
	counts := map[string]int{}
	resultByKey := map[string]Result{}
	rawByKey := map[string]string{}
	order := []string{}
	valid := 0
	last := errResult(502, j.Model, "all vote samples failed")

	for i := 0; i < r.samples; i++ {
		text, err := g.runCLI(ctx, r.provider, r.req)
		if err != nil {
			g.log.Error("vote sample cli error", "stage", labelOf(j), "sample", i+1, "of", r.samples, "err", err.Error())
			last = errResult(502, j.Model, err.Error())
			if errors.Is(err, errCLITimeout) || ctx.Err() != nil {
				break
			}
			continue
		}
		out, ok := g.parseOutput(j, r, text)
		if !ok {
			last = out
			continue
		}
		vk := voteKey(out.Data)
		if _, seen := counts[vk]; !seen {
			order = append(order, vk)
			resultByKey[vk] = out
			rawByKey[vk] = text
		}
		counts[vk]++
		valid++
	}

	if valid == 0 {
		g.log.Error("vote: no valid samples", "stage", labelOf(j), "of", r.samples, "ms", nowMS(t0))
		g.observe(j, r, nowMS(t0), Event{Voted: true, OK: false})
		return last
	}
	best := order[0]
	for _, vk := range order {
		if counts[vk] > counts[best] {
			best = vk
		}
	}
	g.log.Info("vote winner", "stage", labelOf(j), "model", orDefault(r.modelName),
		"winner", counts[best], "of", r.samples, "valid", valid, "ms", nowMS(t0))
	if g.cfg.Cache != nil {
		g.cfg.Cache.Put(r.cacheKey, rawByKey[best])
	}
	g.observe(j, r, nowMS(t0), Event{Voted: true, OK: true})
	return resultByKey[best]
}
