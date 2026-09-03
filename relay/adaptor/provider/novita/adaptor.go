package novita

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
	"meta-llama/llama-3.1-405b-instruct", "meta-llama/llama-3.1-70b-instruct", "meta-llama/llama-3.1-8b-instruct",
	"meta-llama/llama-3-70b-instruct", "meta-llama/llama-3-8b-instruct",
	"deepseek/deepseek-r1", "deepseek/deepseek-v3", "deepseek/deepseek-r1-distill-llama-70b",
	"qwen/qwen-2.5-72b-instruct", "qwen/qwen-2.5-32b-instruct", "qwen/qwen-2.5-7b-instruct",
	"mistralai/mixtral-8x7b-instruct", "mistralai/mistral-7b-instruct", "mistralai/mistral-nemo",
	"nousresearch/hermes-2-pro-llama-3-8b", "nousresearch/nous-hermes-llama2-13b",
	"cognitivecomputations/dolphin-mixtral-8x22b", "sao10k/l3-70b-euryale-v2.1", "sophosympatheia/midnight-rose-70b",
	"gryphe/mythomax-l2-13b", "Nous-Hermes-2-Mixtral-8x7B-DPO", "lzlv_70b", "teknium/openhermes-2.5-mistral-7b", "microsoft/wizardlm-2-8x22b",
}

func (a *Adaptor) GetRequestURL(meta *meta.Meta) (string, error) {
	if meta.Mode == relaymode.ChatCompletions {
		return fmt.Sprintf("%s/chat/completions", meta.BaseURL), nil
	}
	return "", fmt.Errorf("unsupported relay mode %d for novita", meta.Mode)
}

func (a *Adaptor) GetModelList() []string  { return ModelList }
