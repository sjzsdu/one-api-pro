package minimax

import (
	"fmt"

	"github.com/modelbus/one-api-pro/relay/meta"
	"github.com/modelbus/one-api-pro/relay/relaymode"
	oa "github.com/modelbus/one-api-pro/relay/adaptor/openai"
)

type Adaptor struct {
	*oa.Adaptor
}

var ModelList = []string{
	"MiniMax-M1", "MiniMax-Text-01", "MiniMax-VL-01", "abab6.5-chat", "abab6.5s-chat", "abab6-chat", "abab5.5-chat", "abab5.5s-chat",
}

func (a *Adaptor) GetRequestURL(meta *meta.Meta) (string, error) {
	if meta.Mode == relaymode.ChatCompletions {
		return fmt.Sprintf("%s/v1/text/chatcompletion_v2", meta.BaseURL), nil
	}
	return "", fmt.Errorf("unsupported relay mode %d for minimax", meta.Mode)
}

func (a *Adaptor) GetModelList() []string  { return ModelList }
