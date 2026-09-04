package modelrouter

import "context"

// Message is the subset of a chat message needed to make a routing decision.
type Message struct {
	Role    string `json:"role,omitempty"`
	Content any    `json:"content,omitempty"`
}

// ModelSelectRequest contains the request data and the models the caller is
// allowed to use. AvailableModels is populated by the middleware after
// applying group and token restrictions.
type ModelSelectRequest struct {
	Model           string    `json:"model,omitempty"`
	Messages        []Message `json:"messages,omitempty"`
	Prompt          any       `json:"prompt,omitempty"`
	Input           any       `json:"input,omitempty"`
	AvailableModels []string  `json:"-"`
}

// ModelRouter selects one model from request.AvailableModels.
type ModelRouter interface {
	Name() string
	SelectModel(ctx context.Context, group string, userId int, request *ModelSelectRequest) (string, error)
}

// ModelScorer is the extension point for classifiers used by
// ScoringModelRouter. Implementations return a score for any candidate they
// recognize; candidates with a score less than or equal to zero are ignored.
type ModelScorer interface {
	Score(ctx context.Context, request *ModelSelectRequest, candidates []string) map[string]float64
}
