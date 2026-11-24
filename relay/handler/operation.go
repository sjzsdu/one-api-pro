// operation.go 强类型同步操作执行、计量与持久结算
// Executes native operations with bounded inputs and durable idempotent accounting.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/common/random"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay"
	"github.com/modelbus/one-api-pro/relay/adaptor"
	"github.com/modelbus/one-api-pro/relay/adaptor/openai"
	ratio "github.com/modelbus/one-api-pro/relay/billing/ratio"
	"github.com/modelbus/one-api-pro/relay/meta"
	schema "github.com/modelbus/one-api-pro/relay/schema"
	"io"
	"math/big"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

// MeteredQuota calculates fixed-point local prices using the operation's explicit units.
// Token/character/duration unit prices retain the existing per-million configuration;
// per-request prices apply to each image, otherwise to the single operation.
// 版本: v0.0.28
// 日期: 2026-11-23
func MeteredQuota(price *ratio.PriceResult, discount float64, metrics []schema.UsageMetric) (int64, error) {
	number := func(v float64) (*big.Rat, error) {
		r, ok := new(big.Rat).SetString(strconv.FormatFloat(v, 'f', -1, 64))
		if !ok || r.Sign() < 0 {
			return nil, errors.New("invalid price")
		}
		return r, nil
	}
	total := new(big.Rat)
	d, err := number(discount)
	if err != nil {
		return 0, err
	}
	if price.BillingType == model.BillingTypePerRequest {
		total, err = number(price.PerRequestPrice)
		if err != nil {
			return 0, err
		}
		for _, m := range metrics {
			if m.Unit == "image" {
				total.Mul(total, big.NewRat(m.Quantity, 1))
				break
			}
		}
	} else {
		for _, m := range metrics {
			if m.Quantity < 0 {
				return 0, errors.New("negative usage")
			}
			scale := m.Scale
			if scale <= 0 {
				scale = 1
			}
			p := price.InputPrice
			if strings.Contains(m.Metric, "output") {
				p = price.OutputPrice
			}
			r, e := number(p)
			if e != nil {
				return 0, e
			}
			r.Mul(r, big.NewRat(m.Quantity, scale))
			r.Quo(r, big.NewRat(1000000, 1))
			total.Add(total, r)
		}
	}
	unit, err := number(config.QuotaPerUnit)
	if err != nil {
		return 0, err
	}
	total.Mul(total, d)
	total.Mul(total, unit)
	if total.Sign() == 0 {
		return 0, nil
	}
	total.Add(total, big.NewRat(1, 2))
	v := new(big.Int).Quo(total.Num(), total.Denom())
	if !v.IsInt64() {
		return 0, errors.New("charge overflow")
	}
	q := v.Int64()
	if q == 0 {
		q = 1
	}
	return q, nil
}

// RelayOperationHelper invokes an opt-in typed operation on the selected channel.
// 版本: v0.0.28
// 日期: 2026-11-23
func RelayOperationHelper(c *gin.Context, operation string) *schema.ErrorWithStatusCode {
	ctx := c.Request.Context()
	m := meta.GetByContext(c)
	name := c.GetString(ctxkey.RequestModel)
	actual := name
	if mapped := m.ModelMapping[name]; mapped != "" {
		actual = mapped
	}
	m.OriginModelName = name
	m.ActualModelName = actual
	a := relay.GetAdaptorByChannelID(m.ChannelID)
	if a == nil {
		return openai.ErrorWrapper(errors.New("invalid channel"), "invalid_channel", 400)
	}
	a.Init(m)
	invoke, validationErr := operationInvocation(c, operation, actual, a, m)
	if validationErr != nil {
		return openai.ErrorWrapper(validationErr, "invalid_operation_request", 400)
	}
	price, err := ratio.GetModelPrice(actual, name)
	if err != nil {
		return openai.ErrorWrapper(err, "model_price_not_found", 422)
	}
	discount := ratio.GetGroupDiscount(m.Group, actual, name)
	estimate := operationReserveMetrics(c, operation)
	reserve, err := MeteredQuota(price, discount, estimate)
	if err != nil {
		return openai.ErrorWrapper(err, "invalid_price", 422)
	}
	if reserve > 0 && price.BillingType != model.BillingTypePerRequest {
		reserve += config.PreConsumedQuota
	}
	snapshot, _ := json.Marshal(map[string]any{"price": price, "discount": discount})
	key := "request:" + random.GetUUID()
	if err = model.ReserveInference(ctx, model.InferenceReservation{Key: key, UserID: m.UserId, TokenID: m.TokenId, PlanID: m.PlanId, ChannelID: m.ChannelId, ModelName: name, Quota: reserve, PriceSnapshot: string(snapshot)}); err != nil {
		return openai.ErrorWrapper(err, "insufficient_quota", 403)
	}
	c.Header("X-Inference-Id", key)
	result, err := invoke(ctx)
	if err != nil || result == nil || len(result.Body) == 0 {
		if err == nil {
			err = errors.New("empty operation result")
		}
		if !schema.ExecutionWasRejected(err) {
			return pendingInferenceError(c, key, err, "operation_outcome_unknown")
		}
		if refundErr := model.RefundInference(context.WithoutCancel(ctx), key); refundErr != nil {
			return pendingInferenceError(c, key, refundErr, "rejection_refund_failed")
		}
		return openai.ErrorWrapper(err, "operation_upstream_failed", 502)
	}
	amount, err := MeteredQuota(price, discount, result.Usage)
	if err != nil {
		raw, _ := json.Marshal(result.Usage)
		recordPendingResult(ctx, key, string(raw), string(result.Body), "invalid_usage")
		c.Header("x-should-retry", "false")
		return openai.ErrorWrapper(err, "usage_pending", 502)
	}
	usage, _ := json.Marshal(result.Usage)
	if err = model.SettleInference(context.WithoutCancel(ctx), key, amount, string(usage)); err != nil {
		if pendingErr := model.RecordInferenceSettlementRetry(context.WithoutCancel(ctx), key, amount, string(usage)); pendingErr != nil {
			logger.Errorf(ctx, "exact pending charge persistence failed: %v", pendingErr)
		}
		recordPendingResult(ctx, key, string(usage), string(result.Body), "settlement_pending")
		c.Header("X-Inference-Status", "settlement_pending")
		c.Header("x-should-retry", "false")
		c.Data(200, "application/json", result.Body)
		return nil
	}
	if amount != 0 {
		input, output := model.InferenceUsageTokens(string(usage))
		model.RecordConsumeLog(ctx, &model.Log{UserId: m.UserId, ChannelId: m.ChannelId, ModelName: name, ResponseModelName: actual, TokenName: m.TokenName, Quota: int(amount), PromptTokens: int(input), CompletionTokens: int(output), Content: "推理计量 " + key + " | " + string(usage), PlanId: m.PlanId})
	}
	c.Header("X-Inference-Id", key)
	c.Data(200, "application/json", result.Body)
	return nil
}

func operationInvocation(c *gin.Context, op, name string, a adaptor.Adaptor, m *meta.Meta) (func(context.Context) (*schema.OperationResult, error), error) {
	switch op {
	case "rerank.score":
		p, ok := a.(adaptor.Reranker)
		if !ok {
			return nil, errors.New("channel does not support rerank")
		}
		var r schema.RerankRequest
		if err := common.UnmarshalBodyReusable(c, &r); err != nil {
			return nil, err
		}
		r.Model = name
		if r.Query == "" || len(r.Documents) == 0 || len(r.Documents) > 500 || r.TopN < 0 || r.TopN > len(r.Documents) {
			return nil, errors.New("invalid rerank query/documents/top_n")
		}
		return func(ctx context.Context) (*schema.OperationResult, error) { return p.Rerank(ctx, m, &r) }, nil
	case "audio.transcribe":
		p, ok := a.(adaptor.Transcriber)
		if !ok {
			return nil, errors.New("channel does not support native transcription")
		}
		var r schema.TranscriptionRequest
		if err := common.UnmarshalBodyReusable(c, &r); err != nil {
			return nil, err
		}
		r.Model = name
		if strings.HasPrefix(c.ContentType(), "multipart/") {
			raw, err := uploadedMedia(c, "file", 10<<20, "audio")
			if err != nil {
				return nil, err
			}
			r.Audio = raw
		}
		if r.Audio == "" || (r.ResponseFormat != "" && r.ResponseFormat != "json") {
			return nil, errors.New("audio is required; response_format must be json")
		}
		return func(ctx context.Context) (*schema.OperationResult, error) { return p.Transcribe(ctx, m, &r) }, nil
	case "image.edit":
		p, ok := a.(adaptor.ImageEditor)
		if !ok {
			return nil, errors.New("channel does not support image editing")
		}
		var r schema.ImageEditRequest
		if err := common.UnmarshalBodyReusable(c, &r); err != nil {
			return nil, err
		}
		r.Model = name
		if strings.HasPrefix(c.ContentType(), "multipart/") {
			if _, err := c.FormFile("mask"); err == nil {
				return nil, errors.New("this image editor does not support masks")
			}
			raw, err := uploadedMedia(c, "image", 10<<20, "image")
			if err != nil {
				return nil, err
			}
			r.Images = []string{raw}
		}
		if r.Prompt == "" || len(r.Images) == 0 || len(r.Images) > 9 || r.Mask != "" || r.N < 0 || r.N > 4 {
			return nil, errors.New("invalid image edit prompt, references, mask or n")
		}
		return func(ctx context.Context) (*schema.OperationResult, error) { return p.EditImage(ctx, m, &r) }, nil
	}
	return nil, errors.New("operation is not implemented")
}

func uploadedMedia(c *gin.Context, field string, limit int64, kind string) (string, error) {
	file, err := c.FormFile(field)
	if err != nil {
		return "", err
	}
	if file.Size > limit {
		return "", errors.New("media input exceeds size limit")
	}
	f, err := file.Open()
	if err != nil {
		return "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || len(raw) == 0 || int64(len(raw)) > limit {
		return "", errors.New("invalid media upload")
	}
	typ := file.Header.Get("Content-Type")
	typ, _, _ = mime.ParseMediaType(typ)
	if typ == "" || typ == "application/octet-stream" {
		typ = http.DetectContentType(raw)
	}
	if !strings.HasPrefix(typ, kind+"/") {
		return "", fmt.Errorf("expected %s media", kind)
	}
	return "data:" + typ + ";base64," + base64.StdEncoding.EncodeToString(raw), nil
}

func operationReserveMetrics(c *gin.Context, operation string) []schema.UsageMetric {
	switch operation {
	case "image.edit":
		var r schema.ImageEditRequest
		_ = common.UnmarshalBodyReusable(c, &r)
		n := r.N
		if n == 0 {
			n = 1
		}
		return []schema.UsageMetric{{Metric: "image.generated", Quantity: int64(n), Scale: 1, Unit: "image"}}
	case "audio.transcribe":
		return []schema.UsageMetric{{Metric: "audio.input_duration", Quantity: 300, Scale: 1, Unit: "second"}}
	case "rerank.score":
		var r schema.RerankRequest
		_ = common.UnmarshalBodyReusable(c, &r)
		tokens := int64(len(r.Query)+64) * int64(len(r.Documents))
		for _, doc := range r.Documents {
			tokens += int64(len(doc))
		}
		return []schema.UsageMetric{{Metric: "text.input_tokens", Quantity: tokens, Scale: 1, Unit: "token"}}
	}
	return nil
}

func recordPendingResult(ctx context.Context, key, usage, result, reason string) {
	if err := model.RecordInferencePending(context.WithoutCancel(ctx), key, usage, result, reason); err != nil {
		logger.Errorf(ctx, "pending inference result persistence failed for %s: %v", key, err)
		if fallbackErr := model.RecordInferencePending(context.WithoutCancel(ctx), key, usage, "", "result_persistence_failed"); fallbackErr != nil {
			logger.Errorf(ctx, "pending inference usage persistence failed for %s: %v", key, fallbackErr)
		}
	}
}

func pendingInferenceError(c *gin.Context, key string, err error, reason string) *schema.ErrorWithStatusCode {
	raw, _ := json.Marshal(map[string]any{"state": "pending_reconciliation", "reason": reason, "error": err.Error()})
	recordPendingResult(c.Request.Context(), key, string(raw), "", reason)
	c.Header("X-Inference-Id", key)
	c.Header("x-should-retry", "false")
	return openai.ErrorWrapper(fmt.Errorf("upstream outcome requires reconciliation (%s): %w", key, err), "outcome_unknown", 502)
}
