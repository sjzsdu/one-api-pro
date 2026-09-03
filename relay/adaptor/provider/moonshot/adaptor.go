package moonshot

import oa "github.com/modelbus/one-api-pro/relay/adaptor/openai"

type Adaptor struct {
	*oa.Adaptor
}

var ModelList = []string{
	"moonshot-v1-8k-vision-preview",
	"moonshot-v1-32k-vision-preview",
	"kimi-k2.6",
	"kimi-k2.7-code",
	"moonshot-v1-8k",
	"moonshot-v1-32k",
	"moonshot-v1-auto",
	"kimi-k2.5",
	"kimi-k2.7-code-highspeed",
	"moonshot-v1-128k-vision-preview",
	"moonshot-v1-128k",
}

func (a *Adaptor) GetModelList() []string  { return ModelList }
