// multimodal_embedding.go 通义原生多模态向量中继：请求/响应字节透传并复用现有计费与重试机制
// Native DashScope multimodal embedding relay with byte-exact passthrough and shared billing/retry.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: opencode
//
// 设计约束：
//   - 请求体不做「反序列化→再序列化」；只解析出 model 用于路由/定价/鉴权，
//     上游收到的是与客户端完全相同的字节。
//   - 响应体不改写为 OpenAI embedding 结构；调用方按 DashScope 原生结构解析。
//   - 不复用 RelayTextHelper：其入参 GeneralOpenAIRequest 装不下 input.contents /
//     parameters，硬套会丢字段。
//   - 失败重试交给 controller.Relay 已有的 error interceptor 链，不另写一套。
package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/common/random"
	dbmodel "github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay"
	relayadaptor "github.com/modelbus/one-api-pro/relay/adaptor"
	"github.com/modelbus/one-api-pro/relay/adaptor/openai"
	billingratio "github.com/modelbus/one-api-pro/relay/billing/ratio"
	"github.com/modelbus/one-api-pro/relay/meta"
	relaymodel "github.com/modelbus/one-api-pro/relay/schema"
)

const (
	multimodalEmbeddingPath = "/v1/multimodal/embeddings"

	// imageTokensPer512px is the documented token cost of one 512px image for
	// qwen3-vl-embedding (256px -> 64 tokens, 512px -> 256 tokens). We cannot
	// inspect the actual pixel size without decoding base64, so pre-consume
	// uses the index-time worst case of 256.
	imageTokensPer512px = 256
)

// multimodalEmbeddingModelOnly is deliberately minimal: only `model` is read
// from the request body. All other fields stay as raw bytes and are forwarded
// untouched.
type multimodalEmbeddingModelOnly struct {
	Model string `json:"model"`
}

// multimodalEmbeddingResponseMeta extracts billing usage and request_id. It
// never holds the embedding vectors themselves, so no base64/float payload is
// copied into logs.
type multimodalEmbeddingResponseMeta struct {
	Usage struct {
		ImageTokens int `json:"image_tokens"`
		InputTokens int `json:"input_tokens"`
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
	RequestID string `json:"request_id"`
}

// estimateMultimodalPromptTokens approximates upstream billable tokens for
// pre-consume only. Images are charged at the 512px rate; text at 4 chars per
// token. The real charge is settled from upstream usage.total_tokens.
func estimateMultimodalPromptTokens(body []byte) int {
	var req struct {
		Input struct {
			Contents []struct {
				Image json.RawMessage `json:"image"`
				Text  string          `json:"text"`
			} `json:"contents"`
		} `json:"input"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return 0
	}
	total := 0
	for _, content := range req.Input.Contents {
		if len(content.Image) > 0 && string(content.Image) != "null" && string(content.Image) != `""` {
			total += imageTokensPer512px
		}
		if content.Text != "" {
			total += (len([]rune(content.Text)) + 3) / 4
		}
	}
	return total
}

// truncateForLog keeps log lines bounded. Never pass credentials or base64
// image data here — those must not appear in logs at all.
func truncateForLog(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "..."
}

// RelayMultimodalEmbeddingHelper forwards POST /v1/multimodal/embeddings to
// the native DashScope endpoint with byte-exact request and response bodies.
func RelayMultimodalEmbeddingHelper(c *gin.Context) *relaymodel.ErrorWithStatusCode {
	ctx := c.Request.Context()
	m := meta.GetByContext(c)

	// GetRequestBody caches the bytes on the gin context, so Relay's retry loop
	// can replay the exact same payload on the next channel.
	body, err := common.GetRequestBody(c)
	if err != nil {
		return openai.ErrorWrapper(fmt.Errorf("read request body: %w", err), "invalid_request", http.StatusBadRequest)
	}
	var modelOnly multimodalEmbeddingModelOnly
	if err := json.Unmarshal(body, &modelOnly); err != nil {
		return openai.ErrorWrapper(fmt.Errorf("invalid JSON body: %w", err), "invalid_request", http.StatusBadRequest)
	}
	modelName := strings.TrimSpace(modelOnly.Model)
	if modelName == "" {
		return openai.ErrorWrapper(errors.New("missing model in request body"), "invalid_request", http.StatusBadRequest)
	}
	// Auto-routing may have resolved "auto" to a concrete model in TokenAuth.
	if resolved := c.GetString(ctxkey.RequestModel); resolved != "" && resolved != "auto" && resolved != modelName {
		modelName = resolved
	}
	m.OriginModelName = modelName
	actualModel, _ := getMappedModelName(modelName, m.ModelMapping)
	m.ActualModelName = actualModel

	// Resolve adaptor and verify the optional capability BEFORE pre-consuming:
	// an unsupported channel must never leave a pre-consumed charge behind.
	adaptorInstance := relay.GetAdaptorByChannelID(m.ChannelID)
	if adaptorInstance == nil {
		return openai.ErrorWrapper(fmt.Errorf("invalid channel type: %s", m.ChannelID), "invalid_channel_type", http.StatusBadRequest)
	}
	urler, ok := adaptorInstance.(relayadaptor.MultimodalEmbeddingURLer)
	if !ok {
		return openai.ErrorWrapper(
			fmt.Errorf("channel %s does not support multimodal embeddings", m.ChannelID),
			"unsupported_channel_for_multimodal_embedding",
			http.StatusBadRequest,
		)
	}
	fullURL, err := urler.GetMultimodalEmbeddingURL(m)
	if err != nil {
		return openai.ErrorWrapper(err, "invalid_multimodal_embedding_url", http.StatusBadRequest)
	}

	priceResult, err := billingratio.GetModelPrice(actualModel, m.OriginModelName)
	if err != nil {
		return openai.ErrorWrapper(err, "model_price_not_found", http.StatusUnprocessableEntity)
	}
	groupDiscount := billingratio.GetGroupDiscount(m.Group, actualModel, m.OriginModelName)

	promptTokens := estimateMultimodalPromptTokens(body)
	m.PromptTokens = promptTokens

	m.ReservationKey = "multimodal:" + random.GetUUID()
	var preConsumedQuota int64
	var bizErr *relaymodel.ErrorWithStatusCode
	if priceResult.BillingType == dbmodel.BillingTypePerRequest {
		exactQuota := billingratio.CalculatePerRequestQuota(priceResult.PerRequestPrice, 1, 1, groupDiscount)
		preConsumedQuota, bizErr = preConsumeExactQuota(ctx, exactQuota, m)
	} else if priceResult.InputPrice == 0 && priceResult.OutputPrice == 0 && priceResult.CachedPrice == 0 {
		preConsumedQuota, bizErr = preConsumeExactQuota(ctx, 0, m)
	} else {
		ratio := (priceResult.InputPrice + priceResult.OutputPrice) / 2.0 / billingratio.Million * config.QuotaPerUnit
		if ratio == 0 {
			ratio = 1.0
		}
		// Reuse the existing pre-consume formula verbatim. An empty request gives
		// MaxTokens=0, so the estimate is PreConsumedQuota + round(tokens*ratio).
		preConsumedQuota, bizErr = preConsumeQuota(ctx, &relaymodel.GeneralOpenAIRequest{}, promptTokens, ratio, m)
	}
	if bizErr != nil {
		logger.Warnf(ctx, "multimodal embedding pre-consume failed: %+v", *bizErr)
		return bizErr
	}

	c.Header("X-Inference-Id", m.ReservationKey)
	// Distribute/SetupContextForSelectedChannel has already replaced the client
	// Authorization header with the channel key. Build a fresh outbound request
	// with only the headers we control — client credentials are never forwarded.
	outReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(body))
	if err != nil {
		if refundErr := refundRequestQuota(ctx, m, preConsumedQuota); refundErr != nil {
			_ = recordPendingTextUsage(ctx, m, nil, priceResult, "refund_failed")
			return openai.ErrorWrapper(refundErr, "settlement_pending", http.StatusServiceUnavailable)
		}
		return openai.ErrorWrapper(err, "build_upstream_request_failed", http.StatusInternalServerError)
	}
	outReq.Header.Set("Content-Type", "application/json")
	outReq.Header.Set("Accept", "application/json")
	outReq.Header.Set("Authorization", "Bearer "+m.APIKey)

	start := time.Now()
	// Use the low-level relay HTTP helper with the URL returned by the optional
	// capability interface. Adaptor.DoRequest would call GetRequestURL (the
	// compatible-mode URL), so it must not be used for this native endpoint.
	// adaptor.DoRequest uses the shared client.HTTPClient, i.e. the global relay
	// timeout (and relay proxy when configured). No new HTTP client is created.
	resp, err := relayadaptor.DoRequest(c, outReq)
	elapsed := time.Since(start)
	if err != nil {
		if refundErr := refundRequestQuota(ctx, m, preConsumedQuota); refundErr != nil {
			_ = recordPendingTextUsage(ctx, m, nil, priceResult, "refund_failed")
			return openai.ErrorWrapper(refundErr, "settlement_pending", http.StatusServiceUnavailable)
		}
		logger.Errorf(ctx, "multimodal embedding do_request_failed: model=%s channel_id=%d elapsed=%s err=%s",
			actualModel, m.ChannelId, elapsed, err.Error())
		return openai.ErrorWrapper(err, "do_request_failed", http.StatusInternalServerError)
	}

	if isErrorHappened(m, resp) {
		if refundErr := refundRequestQuota(ctx, m, preConsumedQuota); refundErr != nil {
			_ = recordPendingTextUsage(ctx, m, nil, priceResult, "refund_failed")
			return openai.ErrorWrapper(refundErr, "settlement_pending", http.StatusServiceUnavailable)
		}
		bizErr := RelayErrorHandler(resp) // keeps the raw upstream message
		logger.Errorf(ctx, "multimodal embedding upstream error: model=%s channel_id=%d status=%d elapsed=%s message=%s",
			actualModel, m.ChannelId, bizErr.StatusCode, elapsed, truncateForLog(bizErr.Error.Message, 256))
		return bizErr
	}

	respBody, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		// A successful upstream response may already have incurred cost; preserve
		// the hold when its usage cannot be read instead of retrying the request.
		_ = recordPendingTextUsage(ctx, m, nil, priceResult, "upstream_response_read_failed")
		return openai.ErrorWrapper(err, "outcome_unknown", http.StatusServiceUnavailable)
	}

	var responseMeta multimodalEmbeddingResponseMeta
	_ = json.Unmarshal(respBody, &responseMeta)
	requestID := resp.Header.Get("X-Request-Id")
	if requestID == "" {
		requestID = responseMeta.RequestID
	}

	// Never log the request body (contains base64 images), the API key, or the
	// embedding vectors. Only routing/usage/timing metadata is logged.
	logger.Infof(ctx, "multimodal embedding ok: model=%s channel_id=%d image_tokens=%d input_tokens=%d total_tokens=%d elapsed=%s request_id=%s",
		actualModel, m.ChannelId, responseMeta.Usage.ImageTokens, responseMeta.Usage.InputTokens,
		responseMeta.Usage.TotalTokens, elapsed, truncateForLog(requestID, 32))

	// Settle before returning so a process restart cannot swallow the delta.
	if err := settleMultimodalEmbedding(ctx, m, &responseMeta, preConsumedQuota, priceResult, groupDiscount); err != nil {
		if errors.Is(err, errMultimodalUsageMissing) {
			_ = recordPendingTextUsage(ctx, m, nil, priceResult, "multimodal_usage_missing")
			return openai.ErrorWrapper(err, "usage_pending", http.StatusServiceUnavailable)
		}
		_ = recordPendingTextUsage(ctx, m, nil, priceResult, "multimodal_settlement_failed")
		return openai.ErrorWrapper(errors.New("embedding billing awaiting reconciliation"), "settlement_pending", http.StatusServiceUnavailable)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	if requestID != "" {
		c.Header("X-Request-Id", requestID)
	}
	// Byte-exact passthrough: do not re-marshal and do not translate into the
	// OpenAI embedding shape.
	c.Data(resp.StatusCode, contentType, respBody)
	c.Abort()
	return nil
}

var errMultimodalUsageMissing = errors.New("embedding usage awaiting reconciliation")

// settleMultimodalEmbedding charges observed usage or a known flat/free price.
// Missing paid token usage retains its reservation for explicit reconciliation.
func settleMultimodalEmbedding(
	ctx context.Context,
	m *meta.Meta,
	responseMeta *multimodalEmbeddingResponseMeta,
	preConsumedQuota int64,
	priceResult *billingratio.PriceResult,
	groupDiscount float64,
) error {
	inputTokens := responseMeta.Usage.InputTokens + responseMeta.Usage.ImageTokens
	totalTokens := responseMeta.Usage.TotalTokens
	if totalTokens == 0 {
		totalTokens = inputTokens
	}
	if inputTokens == 0 && totalTokens > 0 {
		inputTokens = totalTokens
	}
	usage := &relaymodel.Usage{PromptTokens: inputTokens, TotalTokens: totalTokens}

	if inputTokens == 0 && responseMeta.Usage.TotalTokens == 0 {
		if priceResult.BillingType == dbmodel.BillingTypePerRequest ||
			priceResult.InputPrice == 0 && priceResult.OutputPrice == 0 && priceResult.CachedPrice == 0 {
			// Flat and explicitly zero prices are known without token usage.
			return postConsumeQuota(context.WithoutCancel(ctx), usage, m, &relaymodel.GeneralOpenAIRequest{}, preConsumedQuota, priceResult, groupDiscount, false)
		}
		return errMultimodalUsageMissing
	}
	return postConsumeQuota(context.WithoutCancel(ctx), usage, m, &relaymodel.GeneralOpenAIRequest{}, preConsumedQuota, priceResult, groupDiscount, false)
}
