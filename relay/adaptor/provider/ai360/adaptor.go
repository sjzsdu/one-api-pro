package ai360

import oa "github.com/modelbus/one-api-pro/relay/adaptor/openai"

type Adaptor struct {
	*oa.Adaptor
}

var ModelList = []string{
	"360GPT_S2_V9", "360GPT_S2_V9.4", "360GPT_Pro", "360GPT-Turbo", "360GPT-Turbo-Responsibility-8K",
	"embedding-bert-512-v1", "embedding_s1_v1", "semantic_similarity_s1_v1",
}

func (a *Adaptor) GetModelList() []string  { return ModelList }
