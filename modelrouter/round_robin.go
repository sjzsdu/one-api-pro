package modelrouter

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/modelbus/one-api-pro/model"
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

func (r *RoundRobinModelRouter) SelectModel(ctx context.Context, group string, userID int, _ *ModelSelectRequest) (string, error) {
	started := time.Now()
	models, err := model.CacheGetGroupModels(ctx, group)
	if err != nil || len(models) == 0 {
		routeErr := fmt.Errorf("no available models for group %s", group)
		RecordRoutingDecision(ctx, RoutingDecision{
			Strategy: r.Name(), Group: group, UserID: userID,
			Reason: "no candidates available", Error: routeErr.Error(), LatencyMs: time.Since(started).Milliseconds(),
		})
		return "", routeErr
	}
	models = filterModelsWithPricing(ctx, models)
	if len(models) == 0 {
		routeErr := fmt.Errorf("no models with pricing found for group %s", group)
		RecordRoutingDecision(ctx, RoutingDecision{
			Strategy: r.Name(), Group: group, UserID: userID,
			Reason: "no priced models available", Error: routeErr.Error(), LatencyMs: time.Since(started).Milliseconds(),
		})
		return "", routeErr
	}
	idx := atomic.AddUint64(&r.counter, 1)
	selected := models[idx%uint64(len(models))]
	RecordRoutingDecision(ctx, RoutingDecision{
		Model: selected, Strategy: r.Name(), Group: group, UserID: userID,
		Candidates: models, Reason: "round-robin selection", LatencyMs: time.Since(started).Milliseconds(),
	})
	return selected, nil
}
