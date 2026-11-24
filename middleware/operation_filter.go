// operation_filter.go 渠道能力与参数约束过滤
// Rejects incompatible operation bindings before invoking upstream models.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package middleware

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay"
	"github.com/modelbus/one-api-pro/relay/registry"
	"strings"
)

func channelOperationError(c *gin.Context, ch *model.Channel, name string) error {
	operation, mode := relay.OperationForPath(c.Request.URL.Path)
	req := operationRequest(c)
	if req.ReadError != nil {
		return req.ReadError
	}
	if mode == "sync" && req.Stream {
		mode = "stream"
	}
	actual := name
	if mapped := ch.GetModelMapping()[name]; mapped != "" {
		actual = mapped
	}
	provider := registry.IDByLegacyType(ch.Type)
	if provider == "ali" && operation == "chat.generate" && mode != "realtime" {
		if req.HasMedia {
			return errors.New("native Ali chat adapter does not implement multimodal message input")
		}
	}
	declared, err := model.GetModelCapabilities(provider, actual)
	if err != nil {
		declared = model.ResolveModelCapabilities(provider, actual, nil)
	}
	_, known := declared.Operations[operation]
	legacy := mode != "realtime" && mode != "async" && operation != "rerank.score" && operation != "image.edit" && operation != "image.variation"
	// Existing custom explicit bindings retain their legacy wire path. Guessed
	// metadata never authorizes a new native operation or automatic model choice.
	if legacy && (declared.Source == "name_inference" || declared.PrimaryCategory == "unknown") && c.GetString(ctxkey.OriginalRequestModel) != "auto" {
		return nil
	}
	if !known {
		return errors.New("model does not declare the requested operation")
	}
	modes := relay.RuntimeOperationModes(provider, actual)[operation]
	supported := false
	for _, m := range modes {
		if m == mode {
			supported = true
		}
	}
	if !supported {
		if operation == "chat.generate" && containsOperationMode(modes, "realtime") {
			return errors.New("model_operation_unsupported: this model only implements realtime; use GET /v1/realtime WebSocket")
		}
		return errors.New("model_operation_unsupported: channel adapter does not implement the requested operation mode")
	}
	cap := declared.Operations[operation]
	supported = false
	for _, m := range cap.Modes {
		if m == mode {
			supported = true
		}
	}
	if !supported {
		return errors.New("model does not support requested execution mode")
	}

	allowed := func(values []string, value string) bool {
		if value == "" || len(values) == 0 {
			return true
		}
		for _, v := range values {
			if strings.EqualFold(v, value) {
				return true
			}
		}
		return false
	}
	if !allowed(cap.Limits.Formats, req.ResponseFormat) || !allowed(cap.Limits.Voices, req.Voice) || !allowed(cap.Limits.Sizes, req.Size) {
		return errors.New("requested format, voice or size is not supported")
	}
	if cap.Limits.MaxItems > 0 && req.N > cap.Limits.MaxItems {
		return errors.New("requested item count exceeds model limit")
	}
	if req.Speed != nil && (cap.Limits.MinSpeed > 0 && *req.Speed < cap.Limits.MinSpeed || cap.Limits.MaxSpeed > 0 && *req.Speed > cap.Limits.MaxSpeed) {
		return errors.New("requested speed exceeds model limits")
	}
	if req.Duration != 0 && (cap.Limits.MinDurationSeconds > 0 && req.Duration < cap.Limits.MinDurationSeconds || cap.Limits.MaxDurationSeconds > 0 && req.Duration > cap.Limits.MaxDurationSeconds) {
		return errors.New("requested duration exceeds model limits")
	}
	if req.Dimensions != 0 && len(cap.Limits.Dimensions) > 0 {
		found := false
		for _, d := range cap.Limits.Dimensions {
			if d == req.Dimensions {
				found = true
			}
		}
		if !found {
			return errors.New("requested embedding dimension is not supported")
		}
	}
	return nil
}

// OperationChannelCandidates applies the same hard operation constraints to first attempts and retries.
// 版本: v0.0.28
// 日期: 2026-11-23
func OperationChannelCandidates(c *gin.Context, group, name string) []*model.Channel {
	c.Set("gateway.operation_candidate_error", nil)
	candidates := model.GetChannelCandidates(group, name)
	if len(candidates) == 0 && config.MemoryCacheEnabled {
		candidates = fallbackOperationCandidates(c, group, name)
	}
	result := make([]*model.Channel, 0, len(candidates))
	var rejection error
	for _, ch := range candidates {
		if !ch.ContainsGroup(group) || !ch.ContainsModel(name) {
			continue
		}
		if err := channelOperationError(c, ch, name); err == nil {
			result = append(result, ch)
		} else if rejection == nil {
			rejection = err
		}
	}
	if len(result) == 0 && rejection != nil {
		c.Set("gateway.operation_candidate_error", &operationSelectionError{reason: rejection})
	}
	return result
}

type operationSelectionError struct{ reason error }

func (e *operationSelectionError) Error() string { return e.reason.Error() }

type operationRequestInput struct {
	Stream         bool     `json:"stream"`
	ResponseFormat string   `json:"response_format"`
	Voice          string   `json:"voice"`
	Size           string   `json:"size"`
	N              int      `json:"n"`
	Speed          *float64 `json:"speed"`
	Duration       float64  `json:"duration"`
	Dimensions     int      `json:"dimensions"`
	Messages       []any    `json:"messages"`
	Input          any      `json:"input"`
	HasMedia       bool     `json:"-"`
	ReadError      error    `json:"-"`
}

func operationRequest(c *gin.Context) *operationRequestInput {
	const key = "gateway.operation_request"
	if cached, ok := c.Get(key); ok {
		return cached.(*operationRequestInput)
	}
	req := &operationRequestInput{}
	raw, err := common.GetRequestBody(c)
	req.ReadError = err
	if err == nil && json.Unmarshal(raw, req) == nil {
		req.HasMedia = hasMediaChatValue(req.Messages) || hasMediaChatValue(req.Input)
	}
	// Retain only the parsed constraints and media flag, not another object copy
	// of potentially large data URIs across every candidate and retry.
	req.Messages = nil
	req.Input = nil
	c.Set(key, req)
	return req
}

func hasMediaChatInput(raw []byte) bool {
	var root any
	if json.Unmarshal(raw, &root) != nil {
		return false
	}
	return hasMediaChatValue(root)
}

func hasMediaChatValue(root any) bool {
	var walk func(any) bool
	walk = func(v any) bool {
		switch value := v.(type) {
		case []any:
			for _, item := range value {
				if walk(item) {
					return true
				}
			}
		case map[string]any:
			if typ, ok := value["type"].(string); ok {
				switch typ {
				case "image_url", "input_image", "image", "input_audio", "audio_url", "video_url", "input_video", "video":
					return true
				}
			}
			for _, item := range value {
				if walk(item) {
					return true
				}
			}
		}
		return false
	}
	return walk(root)
}

func containsOperationMode(modes []string, mode string) bool {
	for _, value := range modes {
		if value == mode {
			return true
		}
	}
	return false
}
