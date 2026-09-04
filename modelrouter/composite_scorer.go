package modelrouter

import "github.com/modelbus/one-api-pro/common/config"

type CompositeScorer struct {
	Scorers []ModelScorer
	Weights []float64
}

func NewCompositeScorer() *CompositeScorer {
	return &CompositeScorer{
		Scorers: []ModelScorer{
			&QualityScorer{},
			&CostScorer{},
			&SpeedScorer{},
			&IntentScorer{},
		},
		Weights: []float64{
			config.ScorerQualityWeight,
			config.ScorerCostWeight,
			config.ScorerSpeedWeight,
			config.ScorerIntentWeight,
		},
	}
}

func (cs *CompositeScorer) Score(model string, features *RequestFeatures, ctx *RoutingContext) float64 {
	var totalWeight float64
	var totalScore float64

	for i, scorer := range cs.Scorers {
		if i >= len(cs.Weights) {
			break
		}
		w := cs.Weights[i]
		s := scorer.Score(model, features, ctx)
		totalScore += w * s
		totalWeight += w
	}

	if totalWeight == 0 {
		return 0
	}
	return totalScore / totalWeight
}

func (cs *CompositeScorer) ScoreWithDetails(model string, features *RequestFeatures, ctx *RoutingContext) *ScoreResult {
	result := &ScoreResult{
		Model:   model,
		Details: make(map[string]float64),
	}

	var totalWeight float64
	var totalScore float64

	for i, scorer := range cs.Scorers {
		if i >= len(cs.Weights) {
			break
		}
		w := cs.Weights[i]
		s := scorer.Score(model, features, ctx)
		totalScore += w * s
		totalWeight += w
		result.Details[scorer.Name()] = s
	}

	if totalWeight > 0 {
		result.Score = totalScore / totalWeight
	}
	return result
}

func (cs *CompositeScorer) BestModel(models []string, features *RequestFeatures, ctx *RoutingContext) *ScoreResult {
	if len(models) == 0 {
		return nil
	}

	var best *ScoreResult
	for _, m := range models {
		r := cs.ScoreWithDetails(m, features, ctx)
		if best == nil || r.Score > best.Score {
			best = r
		}
	}
	return best
}
