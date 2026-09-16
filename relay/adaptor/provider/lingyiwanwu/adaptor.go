package lingyiwanwu

import oa "github.com/modelbus/one-api-pro/relay/adaptor/openai"

type Adaptor struct {
	*oa.Adaptor
}

var ModelList = []string{
	"yi-large", "yi-large-turbo", "yi-large-preview", "yi-medium", "yi-medium-200k", "yi-spark", "yi-lightning", "yi-vision",
	"yi-34b-chat-0205", "yi-34b-chat-200k", "yi-vl-plus",
}

func (a *Adaptor) GetModelList() []string  { return ModelList }
