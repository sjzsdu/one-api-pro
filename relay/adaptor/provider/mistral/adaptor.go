package mistral

import oa "github.com/modelbus/one-api-pro/relay/adaptor/openai"

type Adaptor struct {
	*oa.Adaptor
}

var ModelList = []string{
	"mistral-large-latest", "mistral-large-2411", "mistral-large-2407", "mistral-large-2402",
	"mistral-medium-latest", "mistral-small-latest", "mistral-small-2503", "mistral-small-2501", "mistral-small-2409",
	"ministral-8b-latest", "ministral-8b-2410", "ministral-3b-latest", "ministral-3b-2410",
	"open-mistral-7b", "open-mixtral-8x7b", "open-mixtral-8x22b",
	"codestral-latest", "codestral-2405", "codestral-2501",
	"pixtral-large-latest", "pixtral-large-2411", "pixtral-12b-2409",
	"mistral-embed", "mistral-moderation-latest",
}

func (a *Adaptor) GetModelList() []string  { return ModelList }
