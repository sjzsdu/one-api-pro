// text.go 既有文本协议中继与持久化额度结算
// Relays legacy text protocols with one durable reservation per provider attempt.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/common/random"
	dbmodel "github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay"
	"github.com/modelbus/one-api-pro/relay/adaptor"
	"github.com/modelbus/one-api-pro/relay/adaptor/openai"
	billingratio "github.com/modelbus/one-api-pro/relay/billing/ratio"
	"github.com/modelbus/one-api-pro/relay/meta"
	"github.com/modelbus/one-api-pro/relay/schema"
)

func RelayTextHelper(c *gin.Context) *model.ErrorWithStatusCode {
	ctx := c.Request.Context()
	meta := meta.GetByContext(c)
	textRequest, err := getAndValidateTextRequest(c, meta.Mode)
	if err != nil {
		logger.Errorf(ctx, "getAndValidateTextRequest failed: %s", err.Error())
		return openai.ErrorWrapper(err, "invalid_text_request", http.StatusBadRequest)
	}
	meta.IsStream = textRequest.Stream

	meta.OriginModelName = textRequest.Model
	// If the model router resolved "auto" to a concrete model, the resolved
	// name is already in ctxkey.RequestModel (set by the middleware). Use it
	// so that pricing and relay use the real model, not the literal "auto".
	if resolved := c.GetString(ctxkey.RequestModel); resolved != "" && resolved != textRequest.Model {
		textRequest.Model = resolved
	}
	textRequest.Model, _ = getMappedModelName(textRequest.Model, meta.ModelMapping)
	meta.ActualModelName = textRequest.Model
	systemPromptReset := setSystemPrompt(ctx, textRequest, meta.ForcedSystemPrompt)

	priceResult, err := billingratio.GetModelPrice(textRequest.Model, meta.OriginModelName)
	if err != nil {
		if meta.OriginModelName == "auto" {
			logger.SysLogf("auto route: no pricing for %s, using default", textRequest.Model)
			priceResult = &billingratio.PriceResult{
				InputPrice:  0,
				OutputPrice: 0,
				BillingType: dbmodel.BillingTypeToken,
				Found:       false,
			}
		} else {
			return openai.ErrorWrapper(err, "model_price_not_found", http.StatusUnprocessableEntity)
		}
	}
	groupDiscount := billingratio.GetGroupDiscount(meta.Group, textRequest.Model, meta.OriginModelName)

	// Resolve the adaptor before pre-consuming quota: when a channel type has no
	// registered adaptor (e.g. a legacy type-43 row left behind after the proxy
	// channel removal), returning here must not leave a pre-consumed charge.
	adaptor := relay.GetAdaptorByChannelID(meta.ChannelID)
	if adaptor == nil {
		return openai.ErrorWrapper(fmt.Errorf("invalid channel type: %s", meta.ChannelID), "invalid_channel_type", http.StatusBadRequest)
	}

	// Pre-consume amount differs by billing type:
	//   - per_request: the cost is a flat, fully-known price per call, so
	//     pre-consume the exact amount. Settlement then computes the same
	//     amount and the delta is 0 — the balance can never be drained into
	//     the negative by the "estimate now, settle later" mismatch.
	//   - token: the cost depends on the upstream usage, so pre-consume an
	//     estimate and settle the difference afterwards.
	//
	// 预扣口径按计费方式区分：per_request 请求前即可确定精确费用，按按次价格
	// 精确预扣（结算 delta 恒为 0，不会透支）；token 计费则按估算预扣、结算补差。
	meta.ReservationKey = "text:" + random.GetUUID()
	promptTokens := getPromptTokens(textRequest, meta.Mode)
	meta.PromptTokens = promptTokens
	var preConsumedQuota int64
	var bizErr *model.ErrorWithStatusCode
	if priceResult.BillingType == dbmodel.BillingTypePerRequest {
		exactQuota := billingratio.CalculatePerRequestQuota(priceResult.PerRequestPrice, 1, 1, groupDiscount)
		preConsumedQuota, bizErr = preConsumeExactQuota(ctx, exactQuota, meta)
	} else if priceResult.InputPrice == 0 && priceResult.OutputPrice == 0 && priceResult.CachedPrice == 0 {
		preConsumedQuota, bizErr = preConsumeExactQuota(ctx, 0, meta)
	} else {
		ratio := (priceResult.InputPrice + priceResult.OutputPrice) / 2.0 / billingratio.Million * config.QuotaPerUnit
		if ratio == 0 {
			ratio = 1.0
		}
		preConsumedQuota, bizErr = preConsumeQuota(ctx, textRequest, promptTokens, ratio, meta)
	}
	if bizErr != nil {
		logger.Warnf(ctx, "pre-consume quota failed: %+v", *bizErr)
		return bizErr
	}

	c.Header("X-Inference-Id", meta.ReservationKey)
	c.Header("x-should-retry", "false")
	adaptor.Init(meta)

	requestBody, err := getRequestBody(c, meta, textRequest, adaptor)
	if err != nil {
		if refundErr := refundRequestQuota(ctx, meta, preConsumedQuota); refundErr != nil {
			return openai.ErrorWrapper(refundErr, "quota_rollback_failed", http.StatusInternalServerError)
		}
		return openai.ErrorWrapper(err, "convert_request_failed", http.StatusInternalServerError)
	}

	resp, err := adaptor.DoRequest(c, meta, requestBody)
	if err != nil {
		logger.Errorf(ctx, "DoRequest failed: %s", err.Error())
		return pendingInferenceError(c, meta.ReservationKey, err, "text_transport_outcome_unknown")
	}
	if isErrorHappened(meta, resp) {
		if resp == nil || !model.RejectedHTTPStatus(resp.StatusCode) {
			return pendingInferenceError(c, meta.ReservationKey, errors.New("provider did not confirm rejection"), "text_outcome_unknown")
		}
		if refundErr := refundRequestQuota(ctx, meta, preConsumedQuota); refundErr != nil {
			return openai.ErrorWrapper(refundErr, "quota_rollback_failed", http.StatusInternalServerError)
		}
		return RelayErrorHandler(resp)
	}

	originalWriter := c.Writer
	var buffered *bufferedInferenceResponse
	if !meta.IsStream {
		buffered = newBufferedInferenceResponse(originalWriter)
		c.Writer = buffered
	}
	usage, respErr := adaptor.DoResponse(c, resp, meta)
	c.Writer = originalWriter
	if respErr != nil {
		logger.Errorf(ctx, "respErr is not nil: %+v", respErr)
		if meta.IsStream && c.Writer.Written() {
			meta.PartialResponse = true
			if usage == nil || usage.PromptTokens+usage.CompletionTokens == 0 {
				if priceResult.BillingType != dbmodel.BillingTypePerRequest {
					_ = recordPendingTextUsage(ctx, meta, usage, priceResult, "interrupted_stream_usage_missing")
					return openai.ErrorWrapper(errors.New("partial stream usage awaiting reconciliation"), "settlement_pending", http.StatusServiceUnavailable)
				}
			}
			if err := postConsumeQuota(context.WithoutCancel(ctx), usage, meta, textRequest, preConsumedQuota, priceResult, groupDiscount, systemPromptReset); err != nil {
				_ = recordPendingTextUsage(ctx, meta, usage, priceResult, "partial_stream_settlement_failed")
				return openai.ErrorWrapper(errors.New("partial stream billing awaiting reconciliation"), "settlement_pending", http.StatusServiceUnavailable)
			}
			return respErr
		}
		pendingErr := pendingInferenceError(c, meta.ReservationKey, errors.New(respErr.Message), "text_response_outcome_unknown")
		if model.ExecutionMayHaveSucceeded(respErr) {
			return respErr
		}
		return pendingErr
	}
	if err := postConsumeQuota(context.WithoutCancel(ctx), usage, meta, textRequest, preConsumedQuota, priceResult, groupDiscount, systemPromptReset); err != nil {
		if pendingErr := recordPendingTextUsage(ctx, meta, usage, priceResult, "response_settlement_failed"); pendingErr != nil {
			logger.Errorf(ctx, "pending text audit failed: %v", pendingErr)
		}
		if buffered != nil {
			recordPendingResult(ctx, meta.ReservationKey, "", buffered.body.String(), "settlement_pending")
		}
		c.Header("X-Inference-Status", "settlement_pending")
		// The successful generation is still usable. Only the exact monetary
		// transaction is queued for retry, never another provider generation.
	}
	if buffered != nil {
		for k, v := range buffered.Header() {
			c.Writer.Header()[k] = v
		}
		c.Data(buffered.status, buffered.Header().Get("Content-Type"), buffered.body.Bytes())
	}
	return nil
}

func getRequestBody(c *gin.Context, meta *meta.Meta, textRequest *model.GeneralOpenAIRequest, adaptor adaptor.Adaptor) (io.Reader, error) {
	needsConv := false
	if checker, ok := adaptor.(interface{ NeedsRequestBodyConversion() bool }); ok {
		needsConv = checker.NeedsRequestBodyConversion()
	}
	if !config.EnforceIncludeUsage &&
		meta.ChannelID == "openai" &&
		meta.OriginModelName == meta.ActualModelName &&
		!needsConv &&
		meta.ForcedSystemPrompt == "" {
		return c.Request.Body, nil
	}

	var requestBody io.Reader
	convertedRequest, err := adaptor.ConvertRequest(c, meta.Mode, textRequest)
	if err != nil {
		logger.Debugf(c.Request.Context(), "converted request failed: %s\n", err.Error())
		return nil, err
	}
	jsonData, err := json.Marshal(convertedRequest)
	if err != nil {
		logger.Debugf(c.Request.Context(), "converted request json_marshal_failed: %s\n", err.Error())
		return nil, err
	}
	logger.Debugf(c.Request.Context(), "converted request: \n%s", string(jsonData))
	requestBody = bytes.NewBuffer(jsonData)
	return requestBody, nil
}
