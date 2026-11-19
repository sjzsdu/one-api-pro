package adaptor

import (
	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/relay/meta"
	"github.com/modelbus/one-api-pro/relay/schema"
	"io"
	"net/http"
)

// MultimodalEmbeddingURLer is an optional capability implemented only by
// adaptors that can serve the native DashScope multimodal embedding endpoint.
// Adaptors that do not implement it must not receive this relay mode: the
// controller type-asserts before sending anything, so an unsupported channel
// fails with 400 instead of silently hitting a wrong (compatible-mode) URL.
//
// MultimodalEmbeddingURLer 是可选能力接口，仅支持原生多模态向量端点的适配器实现。
// 控制器通过类型断言识别；未实现的渠道直接返回 400，不会静默转发到错误 URL。
type MultimodalEmbeddingURLer interface {
	GetMultimodalEmbeddingURL(meta *meta.Meta) (string, error)
}

type Adaptor interface {
	Init(meta *meta.Meta)
	GetRequestURL(meta *meta.Meta) (string, error)
	SetupRequestHeader(c *gin.Context, req *http.Request, meta *meta.Meta) error
	ConvertRequest(c *gin.Context, relayMode int, request *model.GeneralOpenAIRequest) (any, error)
	ConvertImageRequest(request *model.ImageRequest) (any, error)
	DoRequest(c *gin.Context, meta *meta.Meta, requestBody io.Reader) (*http.Response, error)
	DoResponse(c *gin.Context, resp *http.Response, meta *meta.Meta) (usage *model.Usage, err *model.ErrorWithStatusCode)
	GetModelList() []string
}
