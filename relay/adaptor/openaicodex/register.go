package openaicodex

import (
	"github.com/modelbus/one-api-pro/relay/adaptor"
	"github.com/modelbus/one-api-pro/relay/registry"
)

func init() {
	registry.Register(registry.ChannelMeta{
		ID:             "openaicodex",
		Name:           "OpenAI OAuth (Codex)",
		DefaultBaseURL: "https://chatgpt.com/backend-api/codex",
		LegacyType:     52,
	}, func() adaptor.Adaptor {
		return &Adaptor{}
	})
}
