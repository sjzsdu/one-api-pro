// operations.go 百炼重排序、语音识别与图片编辑原生操作
// Implements typed native operations and validates usage before delivery.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package ali

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/modelbus/one-api-pro/common/client"
	dbmodel "github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay/meta"
	schema "github.com/modelbus/one-api-pro/relay/schema"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func nativeJSON(ctx context.Context, m *meta.Meta, method, path string, body any, async bool, out any) error {
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return &schema.UpstreamRejectedError{Err: err}
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(m.BaseURL, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return &schema.UpstreamRejectedError{Err: errors.New("invalid upstream endpoint")}
	}
	req.Header.Set("Authorization", "Bearer "+m.APIKey)
	req.Header.Set("Content-Type", "application/json")
	if async {
		req.Header.Set("X-DashScope-Async", "enable")
	}
	resp, err := client.HTTPClient.Do(req)
	if err != nil {
		return errors.New("upstream request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &nativeStatusError{Status: resp.StatusCode}
	}
	raw, err = io.ReadAll(io.LimitReader(resp.Body, (64<<20)+1))
	if err != nil {
		return errors.New("upstream body read failed")
	}
	if len(raw) > 64<<20 {
		return errors.New("upstream body exceeds response limit")
	}
	var envelope struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return errors.New("invalid upstream JSON")
	}
	if envelope.Code != "" && envelope.Code != "Success" {
		return fmt.Errorf("upstream error %s", envelope.Code)
	}
	if err = json.Unmarshal(raw, out); err != nil {
		return errors.New("invalid upstream result")
	}
	return nil
}

// Rerank maps flat rerank inputs to DashScope's input/parameters protocol.
// 版本: v0.0.28
// 日期: 2026-11-23
func (a *Adaptor) Rerank(ctx context.Context, m *meta.Meta, r *schema.RerankRequest) (*schema.OperationResult, error) {
	if dbmodel.AliOperationModelFamily(r.Model) != "gte-rerank-v2" || strings.TrimSpace(r.Query) == "" || len(r.Documents) == 0 || len(r.Documents) > 500 || r.TopN < 0 || r.TopN > len(r.Documents) {
		return nil, &schema.UpstreamRejectedError{Err: errors.New("invalid rerank model, query, documents or top_n")}
	}
	for _, doc := range r.Documents {
		if strings.TrimSpace(doc) == "" {
			return nil, &schema.UpstreamRejectedError{Err: errors.New("empty rerank document")}
		}
	}
	var v struct {
		Output struct {
			Results []struct {
				Index    int     `json:"index"`
				Score    float64 `json:"relevance_score"`
				Document any     `json:"document,omitempty"`
			} `json:"results"`
		} `json:"output"`
		Usage struct {
			Tokens *int64 `json:"total_tokens"`
		} `json:"usage"`
	}
	body := map[string]any{"model": r.Model, "input": map[string]any{"query": r.Query, "documents": r.Documents}, "parameters": map[string]any{"top_n": r.TopN, "return_documents": r.ReturnDocuments}}
	if err := nativeJSON(ctx, m, "POST", "/api/v1/services/rerank/text-rerank/text-rerank", body, false, &v); err != nil {
		return nil, err
	}
	if v.Usage.Tokens == nil || *v.Usage.Tokens < 0 || len(v.Output.Results) == 0 {
		return nil, errors.New("missing rerank result or usage")
	}
	for _, item := range v.Output.Results {
		if item.Index < 0 || item.Index >= len(r.Documents) {
			return nil, errors.New("invalid rerank document index")
		}
	}
	result, _ := json.Marshal(map[string]any{"model": r.Model, "results": v.Output.Results, "usage": map[string]any{"total_tokens": *v.Usage.Tokens}})
	return &schema.OperationResult{Body: result, Usage: []schema.UsageMetric{{Metric: "text.input_tokens", Quantity: *v.Usage.Tokens, Unit: "token", Scale: 1}}}, nil
}

func mediaInput(raw, prefix string) error {
	if strings.HasPrefix(raw, "data:"+prefix+"/") {
		comma := strings.Index(raw, ",")
		if comma < 0 || !strings.HasSuffix(raw[:comma], ";base64") {
			return errors.New("invalid media data URI")
		}
		if len(raw) > 15<<20 {
			return errors.New("media exceeds input limit")
		}
		if _, err := base64.StdEncoding.DecodeString(raw[comma+1:]); err != nil {
			return errors.New("invalid media base64")
		}
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("invalid media URL")
	}
	return validateMediaURL(u)
}

// Transcribe invokes Qwen's synchronous multimodal ASR and meters audio seconds.
// 版本: v0.0.28
// 日期: 2026-11-23
func (a *Adaptor) Transcribe(ctx context.Context, m *meta.Meta, r *schema.TranscriptionRequest) (*schema.OperationResult, error) {
	if dbmodel.AliOperationModelFamily(r.Model) != "qwen3-asr-flash" || r.Audio == "" || (r.ResponseFormat != "" && r.ResponseFormat != "json") {
		return nil, &schema.UpstreamRejectedError{Err: errors.New("unsupported transcription model or response_format")}
	}
	if err := mediaInput(r.Audio, "audio"); err != nil {
		return nil, &schema.UpstreamRejectedError{Err: err}
	}
	options := map[string]any{"enable_itn": false}
	if r.Language != "" {
		options["language"] = r.Language
	}
	body := map[string]any{"model": r.Model, "input": map[string]any{"messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"audio": r.Audio}}}}}, "parameters": map[string]any{"asr_options": options}}
	var v struct {
		Output struct {
			Choices []struct {
				Message struct {
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		} `json:"output"`
		Usage struct {
			Seconds json.Number `json:"seconds"`
		} `json:"usage"`
	}
	if err := nativeJSON(ctx, m, "POST", "/api/v1/services/aigc/multimodal-generation/generation", body, false, &v); err != nil {
		return nil, err
	}
	seconds, ok := new(big.Rat).SetString(v.Usage.Seconds.String())
	if !ok || seconds.Sign() < 0 || seconds.Cmp(big.NewRat(300, 1)) > 0 || len(v.Output.Choices) == 0 {
		return nil, errors.New("missing or invalid transcription usage")
	}
	seconds.Mul(seconds, big.NewRat(1000, 1))
	ms := new(big.Int).Quo(seconds.Num(), seconds.Denom())
	if !ms.IsInt64() {
		return nil, errors.New("invalid transcription duration")
	}
	var text strings.Builder
	for _, choice := range v.Output.Choices {
		for _, part := range choice.Message.Content {
			text.WriteString(part.Text)
		}
	}
	result, _ := json.Marshal(map[string]any{"text": text.String()})
	return &schema.OperationResult{Body: result, Usage: []schema.UsageMetric{{Metric: "audio.input_duration", Quantity: ms.Int64(), Scale: 1000, Unit: "second"}}}, nil
}

// EditImage maps bounded reference images to Wan2.7's native message protocol.
// 版本: v0.0.28
// 日期: 2026-11-23
func (a *Adaptor) EditImage(ctx context.Context, m *meta.Meta, r *schema.ImageEditRequest) (*schema.OperationResult, error) {
	if !isWan27(r.Model) || strings.TrimSpace(r.Prompt) == "" || len(r.Images) < 1 || len(r.Images) > 9 || r.Mask != "" {
		return nil, &schema.UpstreamRejectedError{Err: errors.New("Wan editing requires 1-9 reference images and does not support mask")}
	}
	if r.N == 0 {
		r.N = 1
	}
	if r.N < 1 || r.N > 4 {
		return nil, &schema.UpstreamRejectedError{Err: errors.New("n must be between 1 and 4")}
	}
	if r.ResponseFormat != "" && r.ResponseFormat != "url" && r.ResponseFormat != "b64_json" {
		return nil, &schema.UpstreamRejectedError{Err: errors.New("unsupported image response_format")}
	}
	if r.Size == "4K" {
		return nil, &schema.UpstreamRejectedError{Err: errors.New("image editing supports at most 2K")}
	}
	content := make([]any, 0, len(r.Images)+1)
	for _, image := range r.Images {
		if err := mediaInput(image, "image"); err != nil {
			return nil, &schema.UpstreamRejectedError{Err: err}
		}
		content = append(content, map[string]any{"image": image})
	}
	content = append(content, map[string]any{"text": r.Prompt})
	parameters := map[string]any{"n": r.N}
	if r.Size != "" {
		parameters["size"] = strings.ReplaceAll(r.Size, "x", "*")
	}
	body := map[string]any{"model": r.Model, "input": map[string]any{"messages": []any{map[string]any{"role": "user", "content": content}}}, "parameters": parameters}
	var v struct {
		Output struct {
			Choices []struct {
				Message struct {
					Content []struct {
						Image string `json:"image"`
					} `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		} `json:"output"`
	}
	if err := nativeJSON(ctx, m, "POST", "/api/v1/services/aigc/multimodal-generation/generation", body, false, &v); err != nil {
		return nil, err
	}
	images := []map[string]string{}
	for _, choice := range v.Output.Choices {
		for _, part := range choice.Message.Content {
			if part.Image == "" {
				continue
			}
			if r.ResponseFormat == "b64_json" {
				data, _, err := downloadMedia(ctx, part.Image)
				if err != nil {
					return nil, err
				}
				images = append(images, map[string]string{"b64_json": base64.StdEncoding.EncodeToString(data)})
			} else {
				images = append(images, map[string]string{"url": part.Image})
			}
		}
	}
	if len(images) == 0 {
		return nil, errors.New("image editing returned no images")
	}
	result, _ := json.Marshal(map[string]any{"created": time.Now().Unix(), "data": images})
	return &schema.OperationResult{Body: result, Usage: []schema.UsageMetric{{Metric: "image.generated", Quantity: int64(len(images)), Scale: 1, Unit: "image"}}}, nil
}

type nativeStatusError struct{ Status int }

func (e *nativeStatusError) Error() string {
	return fmt.Sprintf("upstream returned status %d", e.Status)
}

// HTTPStatusCode exposes provider status without parsing a diagnostic string.
// 版本: v0.0.28
// 日期: 2026-11-23
func (e *nativeStatusError) HTTPStatusCode() int { return e.Status }

// ExecutionRejected identifies explicit client rejection; server failures remain uncertain.
// 版本: v0.0.28
// 日期: 2026-11-23
func (e *nativeStatusError) ExecutionRejected() bool { return schema.RejectedHTTPStatus(e.Status) }
