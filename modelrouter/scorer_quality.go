package modelrouter

type QualityScorer struct{}

func (s *QualityScorer) Name() string { return "quality" }

func (s *QualityScorer) Score(model string, features *RequestFeatures, ctx *RoutingContext) float64 {
	caps := GetModelCaps(model)
	return caps.QualityRank
}
