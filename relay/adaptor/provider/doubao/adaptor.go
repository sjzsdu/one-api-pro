package doubao

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
	"doubao-seed-1-6", "doubao-seed-1-6-thinking", "doubao-seed-1-6-flash", "doubao-seed-1-6-vision",
	"doubao-1-5-pro-32k", "doubao-1-5-pro-256k", "doubao-1-5-lite-32k", "doubao-1-5-thinking-pro", "doubao-1-5-vision-pro",
	"Doubao-pro-128k", "Doubao-pro-32k", "Doubao-pro-4k", "Doubao-lite-128k", "Doubao-lite-32k", "Doubao-lite-4k",
	"Doubao-embedding", "doubao-embedding", "doubao-embedding-large", "doubao-embedding-text-240715",
	"deepseek-r1", "deepseek-v3", "deepseek-r1-distill-qwen-32b",
}

func (a *Adaptor) GetRequestURL(meta *meta.Meta) (string, error) {
	switch meta.Mode {
	case relaymode.ChatCompletions:
		return fmt.Sprintf("%s/api/v3/chat/completions", meta.BaseURL), nil
	case relaymode.Embeddings:
		return fmt.Sprintf("%s/api/v3/embeddings", meta.BaseURL), nil
	default:
	}
	return "", fmt.Errorf("unsupported relay mode %d for doubao", meta.Mode)
}

func (a *Adaptor) GetModelList() []string  { return ModelList }
