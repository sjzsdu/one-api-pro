package modelrouter

import (
	"context"
	"fmt"
	"sync/atomic"

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

func (r *RoundRobinModelRouter) SelectModel(_ context.Context, group string, _ int, _ *ModelSelectRequest) (string, error) {
	models, err := model.CacheGetGroupModels(context.Background(), group)
	if err != nil || len(models) == 0 {
		return "", fmt.Errorf("no available models for group %s", group)
	}
	idx := atomic.AddUint64(&r.counter, 1)
	return models[idx%uint64(len(models))], nil
}
