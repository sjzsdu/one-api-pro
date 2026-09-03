package xai

import oa "github.com/modelbus/one-api-pro/relay/adaptor/openai"

type Adaptor struct {
	*oa.Adaptor
}

var ModelList = []string{
	"grok-4.3",
	"grok-4.20",
	"grok-4.20-multi-agent",
	"grok-4",
	"grok-4-0709",
	"grok-4-fast",
	"grok-4-fast-reasoning",
	"grok-4-fast-non-reasoning",
	"grok-3",
	"grok-3-latest",
	"grok-3-fast",
	"grok-3-fast-latest",
	"grok-3-mini",
	"grok-3-mini-latest",
	"grok-3-mini-fast",
	"grok-3-mini-fast-latest",
	"grok-2",
	"grok-2-latest",
	"grok-2-1212",
	"grok-beta",
	"grok-2-vision",
	"grok-2-vision-latest",
	"grok-2-vision-1212",
	"grok-vision-beta",
}

func (a *Adaptor) GetModelList() []string  { return ModelList }
