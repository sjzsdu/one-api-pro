package togetherai

import oa "github.com/modelbus/one-api-pro/relay/adaptor/openai"

type Adaptor struct {
	*oa.Adaptor
}

var ModelList = []string{
	"meta-llama/Llama-4-Maverick-17B-128E-Instruct-FP8", "meta-llama/Llama-4-Scout-17B-16E-Instruct",
	"meta-llama/Llama-3.3-70B-Instruct-Turbo", "meta-llama/Llama-3.2-90B-Vision-Instruct-Turbo", "meta-llama/Llama-3.2-11B-Vision-Instruct-Turbo",
	"meta-llama/Meta-Llama-3.1-405B-Instruct-Turbo", "meta-llama/Meta-Llama-3.1-70B-Instruct-Turbo", "meta-llama/Meta-Llama-3.1-8B-Instruct-Turbo",
	"meta-llama/Llama-3-70b-chat-hf", "meta-llama/Llama-3-8b-chat-hf",
	"deepseek-ai/DeepSeek-R1", "deepseek-ai/DeepSeek-V3", "deepseek-ai/deepseek-coder-33b-instruct",
	"Qwen/Qwen2.5-72B-Instruct-Turbo", "Qwen/Qwen2.5-7B-Instruct-Turbo", "Qwen/Qwen1.5-72B-Chat",
	"mistralai/Mixtral-8x22B-Instruct-v0.1", "mistralai/Mixtral-8x7B-Instruct-v0.1", "mistralai/Mistral-7B-Instruct-v0.2",
	"BAAI/bge-large-en-v1.5", "BAAI/bge-base-en-v1.5", "togethercomputer/m2-bert-80M-8k-retrieval",
}

func (a *Adaptor) GetModelList() []string  { return ModelList }
