package modelrouter

import (
	"context"

	schema "github.com/modelbus/one-api-pro/relay/schema"
)

type ModelRouter interface {
	Name() string
	SelectModel(ctx context.Context, group string, userId int, request *ModelSelectRequest) (string, error)
}

type ModelSelectRequest struct {
	Model             string           `json:"model"`
	Messages          []schema.Message `json:"messages"`
	Tools             []schema.Tool    `json:"tools,omitempty"`
	MaxTokens         int              `json:"max_tokens,omitempty"`
	Features          *RequestFeatures `json:"-"`
	DisableSessionPin bool             `json:"-"`
}
