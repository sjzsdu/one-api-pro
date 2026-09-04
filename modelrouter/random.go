package modelrouter

import (
	"context"
	"fmt"
	"math/rand"

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

func (r *RandomModelRouter) SelectModel(_ context.Context, group string, _ int, _ *ModelSelectRequest) (string, error) {
	models, err := model.CacheGetGroupModels(context.Background(), group)
	if err != nil || len(models) == 0 {
		return "", fmt.Errorf("no available models for group %s", group)
	}
	return models[rand.Intn(len(models))], nil
}
