// audio.go 百炼原生语音请求转换与响应归一化
// Converts speech requests and downloads validated audio before settlement.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package ali

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/relay/adaptor/openai"
	"github.com/modelbus/one-api-pro/relay/meta"
	schema "github.com/modelbus/one-api-pro/relay/schema"
)

// ConvertAudioSpeechRequest preserves the requested codec and numeric speaking rate.
// 版本: v0.0.28
// 日期: 2026-11-23
func (a *Adaptor) ConvertAudioSpeechRequest(request *schema.AudioSpeechRequest) (any, error) {
	if request == nil || strings.TrimSpace(request.Input) == "" || request.Model == "" || request.Voice == "" {
		return nil, errors.New("model, input and voice are required")
	}
	format := strings.ToLower(request.ResponseFormat)
	if format == "" {
		format = "wav"
	}
	switch format {
	case "mp3", "pcm", "wav", "opus":
	default:
		return nil, fmt.Errorf("unsupported speech format %q", format)
	}
	if request.Speed != nil && (math.IsNaN(*request.Speed) || math.IsInf(*request.Speed, 0) || *request.Speed < 0.5 || *request.Speed > 2) {
		return nil, errors.New("speed must be between 0.5 and 2")
	}
	var native NativeTTSRequest
	native.Model = request.Model
	native.Input.Text = request.Input
	native.Input.Voice = request.Voice
	native.Input.Format = format
	native.Input.SampleRate = 24000
	native.Input.Rate = request.Speed
	return &native, nil
}

// AudioSpeechResponse returns buffered media and explicit usage without writing HTTP output.
// 版本: v0.0.28
// 日期: 2026-11-23
func (a *Adaptor) AudioSpeechResponse(c *gin.Context, resp *http.Response, m *meta.Meta) (*schema.AudioSpeechResponse, *schema.ErrorWithStatusCode) {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, openai.ErrorWrapper(errors.New("cannot read speech response"), "read_response_body_failed", http.StatusBadGateway)
	}
	var native NativeTTSResponse
	if err = json.Unmarshal(body, &native); err != nil {
		return nil, openai.ErrorWrapper(errors.New("invalid upstream speech JSON"), "unmarshal_response_body_failed", http.StatusBadGateway)
	}
	if native.Code != "" && native.Code != "Success" {
		return nil, openai.ErrorWrapper(fmt.Errorf("speech upstream error: %s", native.Code), "tts_upstream_error", http.StatusBadGateway)
	}
	if native.Output.Audio.URL == "" {
		return nil, openai.ErrorWrapper(errors.New("missing audio URL"), "tts_audio_url_missing", http.StatusBadGateway)
	}
	result := &schema.AudioSpeechResponse{Characters: native.Usage.Characters}
	if m.ActualModelName == "qwen-audio-3.1-tts-flash" {
		if native.Usage.InputTokens == nil || native.Usage.OutputTokens == nil || *native.Usage.InputTokens < 0 || *native.Usage.OutputTokens < 0 || *native.Usage.InputTokens+*native.Usage.OutputTokens == 0 {
			return nil, openai.ErrorWrapper(errors.New("missing or invalid speech token usage"), "tts_usage_missing", http.StatusBadGateway)
		}
		result.TokenUsage = &schema.Usage{PromptTokens: *native.Usage.InputTokens, CompletionTokens: *native.Usage.OutputTokens, TotalTokens: *native.Usage.InputTokens + *native.Usage.OutputTokens}
	}
	if result.Characters != nil && *result.Characters < 0 {
		return nil, openai.ErrorWrapper(errors.New("invalid character usage"), "tts_usage_invalid", http.StatusBadGateway)
	}
	result.Body, result.ContentType, err = downloadMedia(c.Request.Context(), native.Output.Audio.URL)
	if err != nil {
		return nil, openai.ErrorWrapper(err, "tts_download_failed", http.StatusBadGateway)
	}
	if result.ContentType == "" {
		result.ContentType = "application/octet-stream"
	}
	return result, nil
}
