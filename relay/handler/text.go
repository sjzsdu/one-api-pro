package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/common/logger"
	dbmodel "github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay"
	"github.com/modelbus/one-api-pro/relay/adaptor"
	"github.com/modelbus/one-api-pro/relay/adaptor/openai"
	"github.com/modelbus/one-api-pro/relay/billing"
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

	if meta.DefaultModel != "" {
		textRequest.Model = meta.DefaultModel
	}
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

	ratio := 1.0
	if priceResult.BillingType == dbmodel.BillingTypeToken {
		ratio = (priceResult.InputPrice + priceResult.OutputPrice) / 2.0 / billingratio.Million * config.QuotaPerUnit
		if ratio == 0 {
			ratio = 1.0
		}
	}

	promptTokens := getPromptTokens(textRequest, meta.Mode)
	meta.PromptTokens = promptTokens
	preConsumedQuota, bizErr := preConsumeQuota(ctx, textRequest, promptTokens, ratio, meta)
	if bizErr != nil {
		logger.Warnf(ctx, "preConsumeQuota failed: %+v", *bizErr)
		return bizErr
	}

	adaptor := relay.GetAdaptorByChannelID(meta.ChannelID)
	if adaptor == nil {
		return openai.ErrorWrapper(fmt.Errorf("invalid channel type: %s", meta.ChannelID), "invalid_channel_type", http.StatusBadRequest)
	}
	adaptor.Init(meta)

	requestBody, err := getRequestBody(c, meta, textRequest, adaptor)
	if err != nil {
		return openai.ErrorWrapper(err, "convert_request_failed", http.StatusInternalServerError)
	}

	resp, err := adaptor.DoRequest(c, meta, requestBody)
	if err != nil {
		logger.Errorf(ctx, "DoRequest failed: %s", err.Error())
		return openai.ErrorWrapper(err, "do_request_failed", http.StatusInternalServerError)
	}
	if isErrorHappened(meta, resp) {
		billing.ReturnPreConsumedQuota(ctx, preConsumedQuota, meta.TokenId)
		return RelayErrorHandler(resp)
	}

	usage, respErr := adaptor.DoResponse(c, resp, meta)
	if respErr != nil {
		logger.Errorf(ctx, "respErr is not nil: %+v", respErr)
		billing.ReturnPreConsumedQuota(ctx, preConsumedQuota, meta.TokenId)
		return respErr
	}
	go postConsumeQuota(ctx, usage, meta, textRequest, preConsumedQuota, priceResult, groupDiscount, systemPromptReset)
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