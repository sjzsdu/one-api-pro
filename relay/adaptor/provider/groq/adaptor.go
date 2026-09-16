package groq

import oa "github.com/modelbus/one-api-pro/relay/adaptor/openai"

type Adaptor struct {
	*oa.Adaptor
}

var ModelList = []string{
	"llama-3.3-70b-versatile", "llama-3.1-8b-instant",
	"llama3-70b-8192", "llama3-8b-8192",
	"meta-llama/llama-4-scout-17b-16e-instruct", "meta-llama/llama-4-maverick-17b-128e-instruct",
	"meta-llama/llama-guard-4-12b", "llama-guard-3-8b",
	"gemma2-9b-it", "gemma-7b-it",
	"qwen/qwen3-32b", "qwen-qwq-32b",
	"deepseek-r1-distill-llama-70b", "deepseek-r1-distill-llama-70b-specdec", "deepseek-r1-distill-qwen-32b",
	"mistral-saba-24b",
	"llama-3.2-11b-vision-preview", "llama-3.2-90b-vision-preview",
	"llava-v1.5-7b-4096-preview",
	"mixtral-8x7b-32768",
	"whisper-large-v3", "whisper-large-v3-turbo", "distil-whisper-large-v3-en",
}

func (a *Adaptor) GetModelList() []string  { return ModelList }
