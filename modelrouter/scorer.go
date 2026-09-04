package modelrouter

type ModelScorer interface {
	Name() string
	Score(model string, features *RequestFeatures, ctx *RoutingContext) float64
}

type RoutingContext struct {
	Group           string
	UserId          int
	AvailableModels []string
}

type ScoreResult struct {
	Model   string
	Score   float64
	Details map[string]float64
}
