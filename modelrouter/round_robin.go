package modelrouter

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

func init() {
	Register("round_robin", func() ModelRouter {
		return &RoundRobinModelRouter{}
	})
}

type RoundRobinModelRouter struct {
	counter uint64
}

func (r *RoundRobinModelRouter) Name() string {
	return "round_robin"
}

func (r *RoundRobinModelRouter) SelectModel(ctx context.Context, group string, userID int, req *ModelSelectRequest) (string, error) {
	started := time.Now()
	candidates, err := ResolveCandidates(ctx, group, requestFeatures(req))
	if err != nil || len(candidates.Models) == 0 {
		if err == nil {
			err = fmt.Errorf("no compatible models for group %s", group)
		}
		RecordRoutingDecision(ctx, RoutingDecision{
			Strategy: r.Name(), Group: group, UserID: userID,
			Reason: "no candidates available", Error: err.Error(), LatencyMs: time.Since(started).Milliseconds(),
		})
		return "", err
	}
	models := candidates.Models
	idx := atomic.AddUint64(&r.counter, 1)
	selected := models[idx%uint64(len(models))]
	RecordRoutingDecision(ctx, RoutingDecision{
		Model: selected, Strategy: r.Name(), Group: group, UserID: userID,
		Candidates: models, FilteredOut: candidates.FilteredOut, FilterReasons: candidates.FilterReasons,
		Reason: "round-robin selection", LatencyMs: time.Since(started).Milliseconds(),
	})
	return selected, nil
}
