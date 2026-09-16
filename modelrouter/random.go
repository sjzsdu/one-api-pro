package modelrouter

import (
	"context"
	"fmt"
	"math/rand"
	"time"
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

func (r *RandomModelRouter) SelectModel(ctx context.Context, group string, userID int, req *ModelSelectRequest) (string, error) {
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
	selected := models[rand.Intn(len(models))]
	RecordRoutingDecision(ctx, RoutingDecision{
		Model: selected, Strategy: r.Name(), Group: group, UserID: userID,
		Candidates: models, FilteredOut: candidates.FilteredOut, FilterReasons: candidates.FilterReasons,
		Reason: "uniform random selection", LatencyMs: time.Since(started).Milliseconds(),
	})
	return selected, nil
}
