package modelrouter

import (
	"context"
	"math/rand"
)

type RandomModelRouter struct{}

func NewRandomModelRouter() *RandomModelRouter {
	return &RandomModelRouter{}
}

func (r *RandomModelRouter) Name() string {
	return "random"
}

func (r *RandomModelRouter) SelectModel(_ context.Context, _ string, _ int, request *ModelSelectRequest) (string, error) {
	candidates, err := availableCandidates(request)
	if err != nil {
		return "", err
	}
	return candidates[rand.Intn(len(candidates))], nil
}
