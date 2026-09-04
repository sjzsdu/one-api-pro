package modelrouter

import (
	"context"
	"fmt"
	"math/rand"

	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/model"
	schema "github.com/modelbus/one-api-pro/relay/schema"
)

func init() {
	Register("scoring", func() ModelRouter {
		return NewEnhancedScoringRouter()
	})
}

type EnhancedScoringRouter struct {
	filters []ModelFilter
	scorer  *CompositeScorer
}

func NewEnhancedScoringRouter() *EnhancedScoringRouter {
	return &EnhancedScoringRouter{
		filters: DefaultFilters(),
		scorer:  NewCompositeScorer(),
	}
}

func (r *EnhancedScoringRouter) Name() string {
	return "scoring"
}

func (r *EnhancedScoringRouter) SelectModel(ctx context.Context, group string, userId int, req *ModelSelectRequest) (string, error) {
	models, err := model.CacheGetGroupModels(ctx, group)
	if err != nil || len(models) == 0 {
		return "", fmt.Errorf("no available models for group %s", group)
	}

	if req == nil || len(req.Messages) == 0 {
		return models[rand.Intn(len(models))], nil
	}

	features := ExtractFeatures(req.Messages, req)

	if config.ModelFilterEnabled {
		for _, f := range r.filters {
			models = f.Filter(models, features)
			if len(models) == 0 {
				models, _ = model.CacheGetGroupModels(ctx, group)
				break
			}
		}
	}

	if len(models) == 0 {
		return "", fmt.Errorf("no available models for group %s after filtering", group)
	}

	rCtx := &RoutingContext{
		Group:           group,
		UserId:          userId,
		AvailableModels: models,
	}

	best := r.scorer.BestModel(models, features, rCtx)
	if best == nil || best.Score == 0 {
		return models[rand.Intn(len(models))], nil
	}

	return best.Model, nil
}

func extractPrompt(messages []schema.Message) string {
	var prompt string
	for _, msg := range messages {
		if msg.Role == "user" {
			prompt += msg.StringContent() + " "
		}
	}
	if len(prompt) > maxPromptLength {
		prompt = prompt[:maxPromptLength]
	}
	return prompt
}
