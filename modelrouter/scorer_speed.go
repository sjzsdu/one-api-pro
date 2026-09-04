package modelrouter

import "math"

type SpeedScorer struct{}

func (s *SpeedScorer) Name() string { return "speed" }

func (s *SpeedScorer) Score(model string, features *RequestFeatures, ctx *RoutingContext) float64 {
	caps := GetModelCaps(model)
	if caps.AvgLatencyMs <= 0 {
		return 0.5
	}

	score := 1.0 - math.Min(float64(caps.AvgLatencyMs)/10000.0, 1.0)
	return math.Max(0, math.Min(1, score))
}
