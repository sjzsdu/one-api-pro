// inference_probe.go 按操作执行管理员渠道能力探测
// Uses operation-specific requests and requires explicit confirmation for billable generation.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay"
	"github.com/modelbus/one-api-pro/relay/adaptor"
	"github.com/modelbus/one-api-pro/relay/meta"
	"github.com/modelbus/one-api-pro/relay/registry"
	schema "github.com/modelbus/one-api-pro/relay/schema"
	"net/url"
	"strconv"
	"strings"
	"time"
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
	c.JSON(200, gin.H{"success": true, "message": "operation probe succeeded", "data": gin.H{"usage": result.Usage}})
}
