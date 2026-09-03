package geminiv2

import (
	"fmt"
	"strings"

	"github.com/modelbus/one-api-pro/relay/meta"
	oa "github.com/modelbus/one-api-pro/relay/adaptor/openai"
)

type Adaptor struct {
	*oa.Adaptor
}

var ModelList = []string{
	"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.5-flash-lite",
	"gemini-2.0-flash", "gemini-2.0-flash-001", "gemini-2.0-flash-lite", "gemini-2.0-flash-lite-preview-02-05",
	"gemini-2.0-flash-exp", "gemini-2.0-flash-thinking-exp-01-21", "gemini-2.0-pro-exp-02-05",
	"gemini-1.5-pro", "gemini-1.5-pro-001", "gemini-1.5-pro-002", "gemini-1.5-pro-latest", "gemini-1.5-pro-experimental",
	"gemini-1.5-flash", "gemini-1.5-flash-001", "gemini-1.5-flash-002", "gemini-1.5-flash-latest",
	"gemini-1.5-flash-8b", "gemini-1.5-flash-8b-001", "gemini-1.5-flash-8b-latest",
	"gemini-pro", "gemini-1.0-pro",
	"gemma-3-27b-it", "gemma-3-12b-it", "gemma-3-4b-it", "gemma-3-1b-it", "gemma-2-27b-it", "gemma-2-9b-it", "gemma-2-2b-it",
	"text-embedding-004", "embedding-001", "aqa",
}

func (a *Adaptor) GetRequestURL(meta *meta.Meta) (string, error) {
	baseURL := strings.TrimSuffix(meta.BaseURL, "/")
	requestPath := strings.TrimPrefix(meta.RequestURLPath, "/v1")
	return fmt.Sprintf("%s%s", baseURL, requestPath), nil
}

func (a *Adaptor) GetModelList() []string  { return ModelList }
