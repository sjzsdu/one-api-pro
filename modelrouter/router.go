package modelrouter

const defaultStrategy = "random"

var DefaultRouter ModelRouter

func init() {
	Register(NewRoundRobinModelRouter())
	Register(NewRandomModelRouter())
	Register(NewScoringModelRouter(NewRandomModelRouter()))
	InitRouter(defaultStrategy)
}

// InitRouter selects a registered strategy. Invalid configuration safely
// falls back to random so model:auto never prevents the service from starting.
func InitRouter(strategy string) {
	if err := SetDefault(strategy); err != nil {
		_ = SetDefault(defaultStrategy)
	}
}
