package modelrouter

import (
	"context"
	"strings"

	"github.com/modelbus/one-api-pro/model"
)

// filterModelsWithPricing returns only models that have a pricing entry.
// It also excludes batch-only models (`:batch` suffix) since they are not
// usable for real-time requests.
// If ALL remaining models lack pricing, it returns them all instead of
// returning an empty list.
func filterModelsWithPricing(ctx context.Context, models []string) []string {
	var filtered []string
	for _, m := range models {
		if strings.HasSuffix(m, ":batch") {
			continue
		}
		if _, ok := model.GetModelPrice(m); ok {
			filtered = append(filtered, m)
		}
	}
	if len(filtered) == 0 {
		var nonBatch []string
		for _, m := range models {
			if !strings.HasSuffix(m, ":batch") {
				nonBatch = append(nonBatch, m)
			}
		}
		if len(nonBatch) > 0 {
			return nonBatch
		}
		return models
	}
	return filtered
}
