package modelrouter

import (
	"context"
	"fmt"
	"time"

	"github.com/modelbus/one-api-pro/model"
)

// GetCandidates returns models available in the group, filtered by pricing and capabilities.
func GetCandidates(ctx context.Context, group string, features *RequestFeatures, profileProvider ModelProfileProvider) ([]string, map[string]*ModelProfile, error) {
	started := time.Now()

	models, err := model.CacheGetGroupModels(ctx, group)
	if err != nil {
		return nil, nil, fmt.Errorf("get group models: %w", err)
	}
	if len(models) == 0 {
		return nil, nil, fmt.Errorf("no available models for group %s", group)
	}

	models = filterModelsWithPricing(ctx, models)
	if len(models) == 0 {
		return nil, nil, fmt.Errorf("no models with pricing found for group %s", group)
	}

	profiles := profileProvider.GetProfiles(ctx, models)

	if features != nil {
		models = filterByCapabilities(models, profiles, features)
	}

	_ = started
	return models, profiles, nil
}
