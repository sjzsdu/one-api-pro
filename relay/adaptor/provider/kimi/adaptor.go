package kimi

import oa "github.com/modelbus/one-api-pro/relay/adaptor/openai"

// Adaptor uses Kimi's OpenAI-compatible API.
type Adaptor struct {
	*oa.Adaptor
}

var ModelList = []string{
	"kimi-k2.7-code",
	"kimi-k2.7-code-highspeed",
	"kimi-k2.6",
	"kimi-k2.5",
	"moonshot-v1-auto",
	"moonshot-v1-8k",
	"moonshot-v1-32k",
	"moonshot-v1-128k",
	"moonshot-v1-8k-vision-preview",
	"moonshot-v1-32k-vision-preview",
	"moonshot-v1-128k-vision-preview",
}

func (a *Adaptor) GetModelList() []string { return ModelList }
