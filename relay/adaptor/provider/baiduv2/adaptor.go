package baiduv2

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
	"deepseek-ocr",
	"qianfan-ocr",
	"deepseek-r1-250528",
	"deepseek-v3.1-250821",
	"pp-structurev3",
	"glm-5.2",
	"qwen3-235b-a22b-thinking-2507",
	"qwen3-235b-a22b-instruct-2507",
	"qwen3-vl-32b-thinking",
	"qwen3-vl-32b-instruct",
	"qwen3.5-397b-a17b",
	"ernie-4.5-0.3b",
	"deepseek-v4-pro",
	"qwen3-vl-30b-a3b-thinking",
	"ernie-x1-turbo-32k-preview",
	"ernie-x1-turbo-32k",
	"internvl3-38b",
	"kimi-k2.5",
	"qianfan-check-vl",
	"qianfan-ipcharacter",
	"ernie-x1.1",
	"ernie-x1.1-preview",
	"deepseek-v3.2-think",
	"qwen3-embedding-8b",
	"qianfan-vl-70b",
	"deepseek-v4-flash",
	"qwen3-embedding-0.6b",
	"qwen3-30b-a3b-thinking-2507",
	"qwen3-30b-a3b-instruct-2507",
	"qwen3-14b",
	"qwen3-32b",
	"ernie-lite-pro-128k",
	"qwen3-embedding-4b",
	"deepseek-v3",
	"qwen3-vl-235b-a22b-thinking",
	"qwen3-vl-235b-a22b-instruct",
	"paddleocr-vl-0.9b",
	"qianfan-ocr-fast",
	"deepseek-r1-distill-qwen-32b",
	"qwen3-coder-480b-a35b-instruct",
	"deepseek-r1-distill-qwen-14b",
	"glm-5.1",
	"embedding",
	"ernie-4.5-turbo-vl",
	"ernie-4.5-turbo-vl-32k",
	"qwen3-next-80b-a3b-thinking",
	"qwen3-next-80b-a3b-instruct",
	"qwen3-vl-30b-a3b-instruct",
	"qwen3-8b",
	"qianfan-toytalk",
	"deepseek-v3.1-think-250821",
	"qwen-image",
	"minimax-m2.5",
	"bge-large-zh",
	"tao-8k",
	"flux.1-schnell",
	"deepseek-v3.2",
	"ernie-image-turbo",
	"ernie-5.1",
	"qianfan-vl-1.5-flash",
	"qwen3-coder-30b-a3b-instruct",
	"qwen3-vl-8b-instruct",
	"ernie-4.5-turbo-20260402",
	"ernie-4.5-turbo-32k",
	"ernie-4.5-turbo-128k",
	"glm-5",
	"qianfan-vl-8b",
	"ernie-5.0",
	"ernie-5.0-thinking-preview",
	"qwen3.5-35b-a3b",
	"bge-large-en",
	"qwen3-vl-8b-thinking",
	"qwen-image-edit",
	"qwen3.5-27b",
	"qwen3.5-122b-a10b",
	"ernie-speed-pro-128k",
	"kimi-k2.6",
}

func (a *Adaptor) GetRequestURL(meta *meta.Meta) (string, error) {
	switch meta.Mode {
	case relaymode.ChatCompletions:
		return fmt.Sprintf("%s/v2/chat/completions", meta.BaseURL), nil
	default:
	}
	return "", fmt.Errorf("unsupported relay mode %d for baidu v2", meta.Mode)
}

func (a *Adaptor) GetModelList() []string  { return ModelList }
