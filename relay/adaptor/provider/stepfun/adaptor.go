package stepfun

import oa "github.com/modelbus/one-api-pro/relay/adaptor/openai"

type Adaptor struct {
	*oa.Adaptor
}

var ModelList = []string{
	"step-1-8k", "step-1-32k", "step-1-128k", "step-1-256k", "step-1-flash",
	"step-2-16k", "step-2-mini", "step-2-16k-exp", "step-2-16k-202411",
	"step-1v-8k", "step-1v-32k", "step-1o-turbo-vision", "step-1o-audio", "step-1o-vision-32k",
	"step-1x-medium", "step-1x-large",
}

func (a *Adaptor) GetModelList() []string  { return ModelList }
