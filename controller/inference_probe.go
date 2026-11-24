// inference_probe.go 按操作执行管理员渠道能力探测
// Uses operation-specific requests and requires explicit confirmation for billable generation.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay"
	"github.com/modelbus/one-api-pro/relay/adaptor"
	"github.com/modelbus/one-api-pro/relay/adaptor/openai"
	"github.com/modelbus/one-api-pro/relay/meta"
	"github.com/modelbus/one-api-pro/relay/registry"
	"github.com/modelbus/one-api-pro/relay/relaymode"
	schema "github.com/modelbus/one-api-pro/relay/schema"
)

// OperationProbeSpec describes an available administrative probe, not upstream health.
// 版本: v0.0.28
// 日期: 2026-11-23
type OperationProbeSpec struct {
	RequiresInput bool `json:"requires_input"`
}

// ProbeOperations exposes only operation-specific probe implementations.
// 版本: v0.0.28
// 日期: 2026-11-23
func ProbeOperations(provider, modelName string) map[string]OperationProbeSpec {
	result := map[string]OperationProbeSpec{}
	a := relay.GetAdaptorByChannelID(provider)
	for operation, modes := range relay.RuntimeOperationModes(provider, modelName) {
		if !containsModelCapability(modes, "sync") {
			continue
		}
		switch operation {
		case "chat.generate":
			if !isEmbeddingTestModel(modelName) {
				result[operation] = OperationProbeSpec{}
			}
		case "embedding.create":
			if isEmbeddingTestModel(modelName) {
				result[operation] = OperationProbeSpec{}
			}
		case "rerank.score":
			if _, ok := a.(adaptor.Reranker); ok {
				result[operation] = OperationProbeSpec{}
			}
		case "audio.transcribe":
			if typedTranscriptionModel(provider, modelName) {
				result[operation] = OperationProbeSpec{RequiresInput: true}
			}
		case "image.edit":
			if _, ok := a.(adaptor.ImageEditor); ok {
				result[operation] = OperationProbeSpec{RequiresInput: true}
			}
		case "audio.speech":
			// 语音合成探测：文本输入 + 音色，回传音频字节供页面直接播放。
			// 需要适配器实现原生 TTS 转换与响应归一化接口。
			if _, ok := a.(adaptor.AudioSpeechRequester); ok {
				if _, handlerOK := a.(adaptor.AudioSpeechHandler); handlerOK {
					result[operation] = OperationProbeSpec{RequiresInput: true}
				}
			}
		case "image.generate":
			// 文生图探测：提示词输入，回传图片供页面直接预览。
			// 外层循环已用 RuntimeOperationModes 过滤，仅真正放行的 provider/model 才会到达这里。
			result[operation] = OperationProbeSpec{RequiresInput: true}
		}
	}
	return result
}

func initOperationProbeAdaptor(a adaptor.Adaptor, ch *model.Channel, provider, requested, actual string) (*meta.Meta, error) {
	config, err := ch.LoadConfig()
	if err != nil {
		return nil, err
	}
	m := &meta.Meta{ChannelID: provider, ChannelType: ch.Type, ChannelId: ch.Id, APIKey: ch.Key, BaseURL: ch.GetBaseURL(), OriginModelName: requested, ActualModelName: actual, Config: config, StartTime: time.Now()}
	if m.BaseURL == "" {
		m.BaseURL = registry.GetDefaultBaseURL(provider)
	}
	a.Init(m)
	return m, nil
}

func validProbeMediaURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil && u.Fragment == ""
}

// probeMedia is a generated media artifact returned inline to the admin UI so
// the operator can preview it (audio player / image tag) without a second fetch.
// 版本: v0.0.28
// 日期: 2026-11-24
type probeMedia struct {
	Kind        string `json:"kind"` // audio | image
	MimeType    string `json:"mime_type"`
	Base64      string `json:"base64"`
	Filename    string `json:"filename,omitempty"`
	Description string `json:"description,omitempty"`
}

// encodeAudioMedia turns normalized speech bytes into an inline preview artifact.
// 版本: v0.0.28
// 日期: 2026-11-24
func encodeAudioMedia(result *schema.AudioSpeechResponse, defaultMime string) (*probeMedia, error) {
	if result == nil || len(result.Body) == 0 {
		return nil, errors.New("speech adapter returned no audio bytes")
	}
	mime := strings.TrimSpace(result.ContentType)
	if mime == "" || strings.HasPrefix(mime, "application/octet-stream") {
		mime = defaultMime
	}
	ext := "bin"
	if strings.Contains(mime, "mpeg") || strings.Contains(mime, "mp3") {
		ext = "mp3"
	} else if strings.Contains(mime, "wav") {
		ext = "wav"
	} else if strings.Contains(mime, "ogg") {
		ext = "ogg"
	}
	return &probeMedia{
		Kind:     "audio",
		MimeType: mime,
		Base64:   base64.StdEncoding.EncodeToString(result.Body),
		Filename: "probe-speech." + ext,
	}, nil
}

// probeImageBuffer captures an adapter's image response body without writing to
// the real client, mirroring the relay handler's buffered writer contract.
// 版本: v0.0.28
// 日期: 2026-11-24
type probeImageBuffer struct {
	gin.ResponseWriter
	body   bytes.Buffer
	header http.Header
	status int
}

func newProbeImageBuffer(w gin.ResponseWriter) *probeImageBuffer {
	return &probeImageBuffer{ResponseWriter: w, header: w.Header().Clone(), status: http.StatusOK}
}

func (b *probeImageBuffer) Header() http.Header  { return b.header }
func (b *probeImageBuffer) WriteHeader(code int) { b.status = code }
func (b *probeImageBuffer) WriteHeaderNow()      {}
func (b *probeImageBuffer) Written() bool        { return b.body.Len() > 0 }
func (b *probeImageBuffer) Size() int            { return b.body.Len() }
func (b *probeImageBuffer) Flush()               {}
func (b *probeImageBuffer) Write(p []byte) (int, error) {
	if b.body.Len()+len(p) > 32<<20 {
		return 0, io.ErrShortBuffer
	}
	return b.body.Write(p)
}
func (b *probeImageBuffer) WriteString(s string) (int, error) { return b.Write([]byte(s)) }

// runImageProbe drives the adapter's native image generation pipeline and
// returns normalized, inline-previewable images (base64 or public URL).
// 版本: v0.0.28
// 日期: 2026-11-24
func runImageProbe(c *gin.Context, a adaptor.Adaptor, m *meta.Meta, actual string, input json.RawMessage) ([]probeMedia, error) {
	var req schema.ImageRequest
	if len(input) > 0 {
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, errors.New("invalid image input")
		}
	}
	req.Model = actual
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, errors.New("image input requires a prompt")
	}
	if req.N == 0 {
		req.N = 1
	}
	if req.N < 1 || req.N > 4 {
		return nil, errors.New("image count n must be between 1 and 4")
	}
	if req.Size == "" {
		req.Size = "1024x1024"
	}
	// b64_json lets the adapter inline the bytes; fall back to public URL if the
	// provider cannot embed them.
	req.ResponseFormat = "b64_json"

	requestBody, err := buildProbeImageBody(a, &req)
	if err != nil {
		return nil, err
	}

	m.Mode = relaymode.ImagesGenerations
	m.ActualModelName = actual
	resp, err := a.DoRequest(c, m, bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("image adapter returned no response")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("image upstream returned status %d", resp.StatusCode)
	}

	originalWriter := c.Writer
	buffered := newProbeImageBuffer(originalWriter)
	c.Set("response_format", "b64_json")
	c.Writer = buffered
	_, respErr := a.DoResponse(c, resp, m)
	c.Writer = originalWriter
	if respErr != nil {
		return nil, errors.New(respErr.Message)
	}

	var output openai.ImageResponse
	if err := json.Unmarshal(buffered.body.Bytes(), &output); err != nil || len(output.Data) == 0 {
		return nil, errors.New("image adapter returned no usable image")
	}
	media := make([]probeMedia, 0, len(output.Data))
	for _, item := range output.Data {
		if strings.TrimSpace(item.B64Json) != "" {
			media = append(media, probeMedia{Kind: "image", MimeType: "image/png", Base64: item.B64Json, Filename: "probe-image.png"})
			continue
		}
		if validProbeMediaURL(item.Url) {
			media = append(media, probeMedia{Kind: "image", MimeType: "image/*", Description: item.Url})
		}
	}
	if len(media) == 0 {
		return nil, errors.New("image adapter returned no previewable image")
	}
	return media, nil
}

// buildProbeImageBody uses the adapter's rewriter when available, otherwise the
// standard OpenAI image request body is passed through.
// 版本: v0.0.28
// 日期: 2026-11-24
func buildProbeImageBody(a adaptor.Adaptor, req *schema.ImageRequest) ([]byte, error) {
	var payload any = req
	if rewriter, ok := a.(adaptor.ImageRequestRewriter); ok {
		native, err := rewriter.RewriteImageRequest(req)
		if err != nil {
			return nil, err
		}
		payload = native
	}
	return json.Marshal(payload)
}

// runSpeechProbe drives native or OpenAI-compatible TTS and returns the audio
// bytes inline for playback in the admin UI.
// 版本: v0.0.28
// 日期: 2026-11-24
func runSpeechProbe(c *gin.Context, a adaptor.Adaptor, m *meta.Meta, actual string, input json.RawMessage) (*probeMedia, error) {
	var req schema.AudioSpeechRequest
	if len(input) > 0 {
		if err := json.Unmarshal(input, &req); err != nil {
			return nil, errors.New("invalid speech input")
		}
	}
	req.Model = actual
	if strings.TrimSpace(req.Input) == "" {
		return nil, errors.New("speech input requires text")
	}
	if strings.TrimSpace(req.Voice) == "" {
		return nil, errors.New("speech input requires a voice")
	}
	if req.ResponseFormat == "" {
		req.ResponseFormat = "wav"
	}

	var requestBody []byte
	if requester, ok := a.(adaptor.AudioSpeechRequester); ok {
		native, err := requester.ConvertAudioSpeechRequest(&req)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(native)
		if err != nil {
			return nil, err
		}
		requestBody = encoded
	} else {
		encoded, err := json.Marshal(&req)
		if err != nil {
			return nil, err
		}
		requestBody = encoded
	}

	m.Mode = relaymode.AudioSpeech
	m.ActualModelName = actual
	resp, err := a.DoRequest(c, m, bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("speech adapter returned no response")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("speech upstream returned status %d", resp.StatusCode)
	}

	var result *schema.AudioSpeechResponse
	var respErr *schema.ErrorWithStatusCode
	if handler, ok := a.(adaptor.AudioSpeechHandler); ok {
		result, respErr = handler.AudioSpeechResponse(c, resp, m)
		if respErr != nil {
			return nil, errors.New(respErr.Message)
		}
	} else {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		if err != nil || len(body) == 0 {
			return nil, errors.New("cannot read speech response")
		}
		result = &schema.AudioSpeechResponse{Body: body, ContentType: resp.Header.Get("Content-Type")}
	}
	mime := req.ResponseFormat
	if mime == "b64_json" {
		mime = "wav"
	}
	return encodeAudioMedia(result, mimeFromFormat(mime))
}

func mimeFromFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "mp3":
		return "audio/mpeg"
	case "opus":
		return "audio/ogg"
	case "pcm":
		return "audio/pcm"
	default:
		return "audio/wav"
	}
}

// OperationProbe validates the requested operation and never probes media through chat completion.
// 版本: v0.0.28
// 日期: 2026-11-23
func OperationProbe(c *gin.Context) {
	var r struct {
		Model       string          `json:"model"`
		Operation   string          `json:"operation"`
		ConfirmCost bool            `json:"confirm_cost"`
		Input       json.RawMessage `json:"input"`
	}
	if err := common.UnmarshalBodyReusable(c, &r); err != nil {
		c.JSON(200, gin.H{"success": false, "message": "invalid probe request"})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(200, gin.H{"success": false, "message": "invalid channel"})
		return
	}
	ch, err := model.GetChannelById(id, true)
	if err != nil || !ch.ContainsModel(r.Model) {
		c.JSON(200, gin.H{"success": false, "message": "model not configured on channel"})
		return
	}
	provider := registry.IDByLegacyType(ch.Type)
	actual := r.Model
	if mapped := ch.GetModelMapping()[r.Model]; mapped != "" {
		actual = mapped
	}
	if _, ok := ProbeOperations(provider, actual)[r.Operation]; !ok {
		c.JSON(200, gin.H{"success": false, "status": "unsupported_probe", "message": "use an inference example to verify this operation; no probe was sent"})
		return
	}
	capability, capabilityErr := model.GetModelCapabilities(provider, actual)
	if capabilityErr != nil || !containsModelCapability(capability.Operations[r.Operation].Modes, "sync") {
		c.JSON(200, gin.H{"success": false, "message": "model does not declare this synchronous operation"})
		return
	}
	if !r.ConfirmCost {
		c.JSON(200, gin.H{"success": false, "message": "confirm_cost is required before sending a billable probe"})
		return
	}
	started := time.Now()
	attempted, successful := false, false
	var result *schema.OperationResult
	defer func() {
		if !attempted {
			return
		}
		entry := map[string]any{"operation": r.Operation, "success": successful, "admin_id": c.GetInt(ctxkey.Id)}
		if result != nil {
			entry["usage"] = result.Usage
		}
		encoded, _ := json.Marshal(entry)
		model.RecordTestLog(c.Request.Context(), &model.Log{UserId: c.GetInt(ctxkey.Id), ChannelId: ch.Id, ModelName: r.Model, ResponseModelName: actual, Content: string(encoded), ElapsedTime: time.Since(started).Milliseconds()})
	}()
	modes := relay.RuntimeOperationModes(provider, actual)[r.Operation]
	if len(modes) == 0 {
		c.JSON(200, gin.H{"success": false, "message": "gateway does not implement this operation on channel"})
		return
	}
	syncMode := false
	for _, mode := range modes {
		if mode == "sync" {
			syncMode = true
		}
	}
	if !syncMode {
		c.JSON(200, gin.H{"success": false, "message": "use explicit asynchronous or realtime endpoint; no chat probe was sent"})
		return
	}
	if r.Operation == "chat.generate" || r.Operation == "embedding.create" {
		if len(r.Input) > 0 {
			c.JSON(200, gin.H{"success": false, "message": "use existing channel probe for chat and embedding"})
			return
		}
		attempted = true
		message, e, oe := testChannel(c.Request.Context(), ch, buildTestRequest(r.Model))
		if e != nil {
			message = e.Error()
		}
		if oe != nil {
			message = oe.Message
		}
		successful = e == nil && oe == nil
		c.JSON(200, gin.H{"success": successful, "message": message})
		return
	}
	a := relay.GetAdaptorByChannelID(provider)
	m, err := initOperationProbeAdaptor(a, ch, provider, r.Model, actual)
	if err != nil {
		c.JSON(200, gin.H{"success": false, "message": "invalid channel configuration"})
		return
	}
	// 部分原生适配器（如百炼异步图片轮询）从 c.Request.Header 读取上游 key。
	// 生成类探测在此把渠道 key 写入请求头，与正常 relay 的 distributor 行为一致；
	// 非生成类探测沿用原有逻辑，不受影响。
	if r.Operation == "audio.speech" || r.Operation == "image.generate" {
		c.Request.Header.Set("Authorization", "Bearer "+ch.Key)
	}
	switch r.Operation {
	case "rerank.score":
		p, ok := a.(adaptor.Reranker)
		if !ok {
			err = errors.New("unsupported rerank")
			break
		}
		req := schema.RerankRequest{Model: actual, Query: "test", Documents: []string{"test document"}, TopN: 1}
		if len(r.Input) > 0 {
			err = json.Unmarshal(r.Input, &req)
			req.Model = actual
		}
		if err == nil {
			if strings.TrimSpace(req.Query) == "" || len(req.Documents) == 0 {
				err = errors.New("rerank input requires query and documents")
			}
		}
		if err == nil {
			attempted = true
			result, err = p.Rerank(c.Request.Context(), m, &req)
		}
	case "audio.transcribe":
		p, ok := a.(adaptor.Transcriber)
		if !ok {
			err = errors.New("unsupported transcription")
			break
		}
		var req schema.TranscriptionRequest
		err = json.Unmarshal(r.Input, &req)
		req.Model = actual
		if err == nil && (!validProbeMediaURL(req.Audio) || req.ResponseFormat != "" && req.ResponseFormat != "json") {
			err = errors.New("input.audio must be an HTTP(S) audio URL and response_format must be json")
		}
		if err == nil {
			attempted = true
			result, err = p.Transcribe(c.Request.Context(), m, &req)
		}
	case "image.edit":
		p, ok := a.(adaptor.ImageEditor)
		if !ok {
			err = errors.New("unsupported editing")
			break
		}
		var req schema.ImageEditRequest
		err = json.Unmarshal(r.Input, &req)
		req.Model = actual
		if err == nil && (strings.TrimSpace(req.Prompt) == "" || len(req.Images) < 1 || len(req.Images) > 9 || req.Mask != "" || req.N < 0 || req.N > 4) {
			err = errors.New("input requires prompt and 1-9 image URLs, n between 1 and 4, without mask")
		}
		if err == nil {
			for _, image := range req.Images {
				if !validProbeMediaURL(image) {
					err = errors.New("input.images must contain HTTP(S) image URLs")
					break
				}
			}
		}
		if err == nil {
			attempted = true
			result, err = p.EditImage(c.Request.Context(), m, &req)
		}
	case "audio.speech":
		var media *probeMedia
		media, err = runSpeechProbe(c, a, m, actual, r.Input)
		if err == nil {
			attempted = true
			successful = true
			c.JSON(200, gin.H{"success": true, "message": "operation probe succeeded", "data": gin.H{"media": []probeMedia{*media}, "time": time.Since(started).Seconds()}})
			return
		}
	case "image.generate":
		var media []probeMedia
		media, err = runImageProbe(c, a, m, actual, r.Input)
		if err == nil {
			attempted = true
			successful = true
			c.JSON(200, gin.H{"success": true, "message": "operation probe succeeded", "data": gin.H{"media": media, "time": time.Since(started).Seconds()}})
			return
		}
	default:
		err = errors.New("use inference endpoint with explicit input; asynchronous and realtime generation are not bulk-probed")
	}
	if err != nil || result == nil {
		if err == nil {
			err = errors.New("probe returned no result")
		}
		c.JSON(200, gin.H{"success": false, "message": err.Error()})
		return
	}
	successful = true
	c.JSON(200, gin.H{"success": true, "message": "operation probe succeeded", "data": gin.H{"usage": result.Usage, "time": time.Since(started).Seconds()}})
}
