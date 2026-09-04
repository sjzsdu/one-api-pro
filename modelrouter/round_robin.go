package modelrouter

import (
	"context"
	"fmt"
	"sync/atomic"
)

type RoundRobinModelRouter struct {
	counter atomic.Uint64
}

func NewRoundRobinModelRouter() *RoundRobinModelRouter {
	return &RoundRobinModelRouter{}
}

func (r *RoundRobinModelRouter) Name() string {
	return "round_robin"
}

func (r *RoundRobinModelRouter) SelectModel(_ context.Context, _ string, _ int, request *ModelSelectRequest) (string, error) {
	candidates, err := availableCandidates(request)
	if err != nil {
		return "", err
	}
	index := (r.counter.Add(1) - 1) % uint64(len(candidates))
	return candidates[index], nil
}

func availableCandidates(request *ModelSelectRequest) ([]string, error) {
	if request == nil || len(request.AvailableModels) == 0 {
		return nil, fmt.Errorf("no models available for automatic routing")
	}
	return request.AvailableModels, nil
}
