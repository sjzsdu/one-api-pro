package ali

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/relay/adaptor"
	"github.com/modelbus/one-api-pro/relay/meta"
	"github.com/modelbus/one-api-pro/relay/relaymode"
	"github.com/modelbus/one-api-pro/relay/schema"
	"io"
	"net/http"
)

// https://help.aliyun.com/zh/dashscope/developer-reference/api-details

type Adaptor struct {
	meta *meta.Meta
}

func (a *Adaptor) Init(meta *meta.Meta) {
	a.meta = meta
}

// GetRequestURL selects the native endpoint for the requested model and modality.
// 版本: v0.0.28
// 日期: 2026-11-23
func (a *Adaptor) GetRequestURL(meta *meta.Meta) (string, error) {
	fullRequestURL := ""
	switch meta.Mode {
	case relaymode.Embeddings:
		fullRequestURL = fmt.Sprintf("%s/api/v1/services/embeddings/text-embedding/text-embedding", meta.BaseURL)
	case relaymode.ImagesGenerations:
		if isWan27(meta.ActualModelName) {
			return fmt.Sprintf("%s/api/v1/services/aigc/multimodal-generation/generation", meta.BaseURL), nil
		}
		fullRequestURL = fmt.Sprintf("%s/api/v1/services/aigc/text2image/image-synthesis", meta.BaseURL)
	case relaymode.AudioSpeech:
		// 百炼原生非实时语音合成。适配器已将 OpenAI speech 请求转换为
		// DashScope input.{text,voice,format,sample_rate} 结构。
		// BaseURL 形如 https://dashscope.aliyuncs.com，WorkspaceId 端点由运维
		// 在渠道 base_url 里配置（如 https://<ws>.cn-beijing.maas.aliyuncs.com）。
		fullRequestURL = fmt.Sprintf("%s/api/v1/services/audio/tts/SpeechSynthesizer", meta.BaseURL)
	default:
		fullRequestURL = fmt.Sprintf("%s/api/v1/services/aigc/text-generation/generation", meta.BaseURL)
	}

	return fullRequestURL, nil
}

// SetupRequestHeader sets native JSON headers and enables async only for legacy images.
// 版本: v0.0.28
// 日期: 2026-11-23
func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Request, meta *meta.Meta) error {
	adaptor.SetupCommonRequestHeader(c, req, meta)
	if meta.Mode == relaymode.AudioSpeech || meta.Mode == relaymode.ImagesGenerations {
		req.Header.Set("Content-Type", "application/json")
	}
	if meta.IsStream {
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("X-DashScope-SSE", "enable")
	}
	req.Header.Set("Authorization", "Bearer "+meta.APIKey)

	if meta.Mode == relaymode.ImagesGenerations && !isWan27(meta.ActualModelName) {
		req.Header.Set("X-DashScope-Async", "enable")
	}
	if a.meta.Config.Plugin != "" {
		req.Header.Set("X-DashScope-Plugin", a.meta.Config.Plugin)
	}
	return nil
}

func (a *Adaptor) ConvertRequest(c *gin.Context, relayMode int, request *model.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	switch relayMode {
	case relaymode.Embeddings:
		aliEmbeddingRequest := ConvertEmbeddingRequest(*request)
		return aliEmbeddingRequest, nil
	default:
		aliRequest := ConvertRequest(*request)
		return aliRequest, nil
	}
}

// ConvertImageRequest chooses the image request protocol for the model family.
// 版本: v0.0.28
// 日期: 2026-11-23
func (a *Adaptor) ConvertImageRequest(request *model.ImageRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}

	if isWan27(request.Model) {
		return convertWanImageRequest(request)
	}
	aliRequest := ConvertImageRequest(*request)
	return aliRequest, nil
}

// RewriteImageRequest implements adaptor.ImageRequestRewriter.
// 版本: v0.0.28
// 日期: 2026-11-23
func (a *Adaptor) RewriteImageRequest(request *model.ImageRequest) (any, error) {
	return a.ConvertImageRequest(request)
}

func (a *Adaptor) DoRequest(c *gin.Context, meta *meta.Meta, requestBody io.Reader) (*http.Response, error) {
	return adaptor.DoRequestHelper(a, c, meta, requestBody)
}

// DoResponse normalizes responses using the selected modality and image protocol.
// 版本: v0.0.28
// 日期: 2026-11-23
func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, meta *meta.Meta) (usage *model.Usage, err *model.ErrorWithStatusCode) {
	if meta.IsStream {
		err, usage = StreamHandler(c, resp)
	} else {
		switch meta.Mode {
		case relaymode.Embeddings:
			err, usage = EmbeddingHandler(c, resp)
		case relaymode.ImagesGenerations:
			if isWan27(meta.ActualModelName) {
				err, usage = wanImageHandler(c, resp)
			} else {
				err, usage = ImageHandler(c, resp)
			}
		default:
			err, usage = Handler(c, resp)
		}
	}
	return
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}
