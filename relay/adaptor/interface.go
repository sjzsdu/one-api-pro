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

// AudioSpeechRequester is an optional capability implemented by adaptors that
// speak a native TTS protocol (e.g. DashScope SpeechSynthesizer) instead of the
// OpenAI /v1/audio/speech JSON body. The audio handler type-asserts before
// calling it; adaptors that don't implement it keep the OpenAI pass-through.
//
// 可选能力：把 OpenAI speech 请求转换为上游原生 TTS 请求体。
// 未实现的渠道沿用 OpenAI 协议透传，行为零变化。
// 版本: v0.0.28
// 日期: 2026-11-23
type AudioSpeechRequester interface {
	ConvertAudioSpeechRequest(request *model.AudioSpeechRequest) (any, error)
}

// AudioSpeechHandler normalizes native speech before any client response or settlement.
// 版本: v0.0.28
// 日期: 2026-11-23
type AudioSpeechHandler interface {
	AudioSpeechResponse(c *gin.Context, resp *http.Response, meta *meta.Meta) (*model.AudioSpeechResponse, *model.ErrorWithStatusCode)
}

// ImageRequestRewriter is an optional capability implemented by adaptors that
// need to rewrite the OpenAI image request into a provider-specific shape.
// The image handler replaced its hard-coded channel-ID whitelist with this
// assertion, so adding a new image provider no longer requires touching the
// handler. Adaptors that don't implement it pass the OpenAI body through.
//
// 可选能力：把 OpenAI 图片请求重写为上游原生结构。
// image handler 已用该断言取代硬编码渠道白名单，新增图片 provider 无需改 handler。
// 版本: v0.0.28
// 日期: 2026-11-23
type ImageRequestRewriter interface {
	RewriteImageRequest(request *model.ImageRequest) (any, error)
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
