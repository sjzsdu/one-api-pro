package modelrouter

import "math"

type CostScorer struct{}

func (s *CostScorer) Name() string { return "cost" }

func (s *CostScorer) Score(model string, features *RequestFeatures, ctx *RoutingContext) float64 {
	caps := GetModelCaps(model)
	totalPrice := caps.InputPricePer1K + caps.OutputPricePer1K
	if totalPrice <= 0 {
		return 0.5
	}

	logPrice := math.Log1p(totalPrice*1000) / math.Log1p(100)
	score := 1.0 - math.Min(logPrice, 1.0)
	return math.Max(0, math.Min(1, score))
}
