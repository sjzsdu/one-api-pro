package modelrouter

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/modelbus/one-api-pro/model"
)

func init() {
	Register("random", func() ModelRouter {
		return &RandomModelRouter{}
	})
}

type RandomModelRouter struct{}

func (r *RandomModelRouter) Name() string {
	return "random"
}

func (r *RandomModelRouter) SelectModel(ctx context.Context, group string, userID int, _ *ModelSelectRequest) (string, error) {
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
	selected := models[rand.Intn(len(models))]
	RecordRoutingDecision(ctx, RoutingDecision{
		Model: selected, Strategy: r.Name(), Group: group, UserID: userID,
		Candidates: models, Reason: "uniform random selection", LatencyMs: time.Since(started).Milliseconds(),
	})
	return selected, nil
}
