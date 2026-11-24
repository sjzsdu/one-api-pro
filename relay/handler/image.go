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

	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/common/random"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay"
	adaptorpkg "github.com/modelbus/one-api-pro/relay/adaptor"
	"github.com/modelbus/one-api-pro/relay/adaptor/openai"
	billingratio "github.com/modelbus/one-api-pro/relay/billing/ratio"
	"github.com/modelbus/one-api-pro/relay/meta"
	relaymodel "github.com/modelbus/one-api-pro/relay/schema"
)

func getImageRequest(c *gin.Context, _ int) (*relaymodel.ImageRequest, error) {
	imageRequest := &relaymodel.ImageRequest{}
	err := common.UnmarshalBodyReusable(c, imageRequest)
	if err != nil {
		return nil, err
	}
	if imageRequest.N == 0 {
		imageRequest.N = 1
	}
	if imageRequest.Size == "" {
		imageRequest.Size = "1024x1024"
	}
	if imageRequest.Model == "" {
		imageRequest.Model = "dall-e-2"
	}
	return imageRequest, nil
}

func isValidImageSize(model string, size string) bool {
	if model == "cogview-3" || billingratio.ImageSizeRatios[model] == nil {
		return true
	}
	_, ok := billingratio.ImageSizeRatios[model][size]
	return ok
}

func isValidImagePromptLength(model string, promptLength int) bool {
	maxPromptLength, ok := billingratio.ImagePromptLengthLimitations[model]
	return !ok || promptLength <= maxPromptLength
}

func isWithinRange(element string, value int) bool {
	amounts, ok := billingratio.ImageGenerationAmounts[element]
	return !ok || (value >= amounts[0] && value <= amounts[1])
}

func getImageSizeRatio(model string, size string) float64 {
	if ratio, ok := billingratio.ImageSizeRatios[model][size]; ok {
		return ratio
	}
	return 1
}

func validateImageRequest(imageRequest *relaymodel.ImageRequest, _ *meta.Meta) *relaymodel.ErrorWithStatusCode {
	if imageRequest.Prompt == "" {
		return openai.ErrorWrapper(errors.New("prompt is required"), "prompt_missing", http.StatusBadRequest)
	}

	if !isValidImageSize(imageRequest.Model, imageRequest.Size) {
		return openai.ErrorWrapper(errors.New("size not supported for this image model"), "size_not_supported", http.StatusBadRequest)
	}

	if !isValidImagePromptLength(imageRequest.Model, len(imageRequest.Prompt)) {
		return openai.ErrorWrapper(errors.New("prompt is too long"), "prompt_too_long", http.StatusBadRequest)
	}

	if !isWithinRange(imageRequest.Model, imageRequest.N) {
		return openai.ErrorWrapper(errors.New("invalid value of n"), "n_not_within_range", http.StatusBadRequest)
	}
	return nil
}

func getImageCostRatio(imageRequest *relaymodel.ImageRequest) (float64, error) {
	if imageRequest == nil {
		return 0, errors.New("imageRequest is nil")
	}
	imageCostRatio := getImageSizeRatio(imageRequest.Model, imageRequest.Size)
	if imageRequest.Quality == "hd" && imageRequest.Model == "dall-e-3" {
		if imageRequest.Size == "1024x1024" {
			imageCostRatio *= 2
		} else {
			imageCostRatio *= 1.5
		}
	}
	return imageCostRatio, nil
}

// RelayImageHelper settles images only after successful response normalization.
// 版本: v0.0.28
// 日期: 2026-11-23
func RelayImageHelper(c *gin.Context, relayMode int) (outErr *relaymodel.ErrorWithStatusCode) {
	ctx := c.Request.Context()
	meta := meta.GetByContext(c)
	imageRequest, err := getImageRequest(c, meta.Mode)
	if err != nil {
		logger.Errorf(ctx, "getImageRequest failed: %s", err.Error())
		return openai.ErrorWrapper(err, "invalid_image_request", http.StatusBadRequest)
	}

	var isModelMapped bool
	meta.OriginModelName = imageRequest.Model
	imageRequest.Model, isModelMapped = getMappedModelName(imageRequest.Model, meta.ModelMapping)
	meta.ActualModelName = imageRequest.Model

	bizErr := validateImageRequest(imageRequest, meta)
	if bizErr != nil {
		return bizErr
	}

	imageCostRatio, err := getImageCostRatio(imageRequest)
	if err != nil {
		return openai.ErrorWrapper(err, "get_image_cost_ratio_failed", http.StatusInternalServerError)
	}

	imageModel := imageRequest.Model
	imageRequest.Model, _ = getMappedModelName(imageRequest.Model, billingratio.ImageOriginModelName)
	c.Set("response_format", imageRequest.ResponseFormat)

	priceResult, priceErr := billingratio.GetModelPrice(imageModel, meta.OriginModelName)
	if priceErr != nil {
		return openai.ErrorWrapper(priceErr, "model_price_not_found", http.StatusUnprocessableEntity)
	}
	groupDiscount := billingratio.GetGroupDiscount(meta.Group, imageModel, meta.OriginModelName)

	var quota int64
	if priceResult.BillingType == model.BillingTypePerRequest {
		quota = billingratio.CalculatePerRequestQuota(priceResult.PerRequestPrice, imageCostRatio, imageRequest.N, groupDiscount)
	} else {
		quota = billingratio.CalculateTokenQuota(
			priceResult.InputPrice, priceResult.OutputPrice, priceResult.CachedPrice,
			1, 0, 0,
			groupDiscount,
		)
		quota = int64(float64(quota) * imageCostRatio * float64(imageRequest.N))
	}

	if priceResult.InputPrice == 0 && priceResult.OutputPrice == 0 && priceResult.PerRequestPrice == 0 {
		quota = 0
	}
	snapshot, _ := json.Marshal(map[string]any{"price": priceResult, "discount": groupDiscount})
	reservationKey := "image:" + random.GetUUID()
	if reserveErr := model.ReserveInference(ctx, model.InferenceReservation{Key: reservationKey, UserID: meta.UserId, TokenID: meta.TokenId, PlanID: meta.PlanId, ChannelID: meta.ChannelId, ModelName: meta.OriginModelName, Quota: quota, PriceSnapshot: string(snapshot)}); reserveErr != nil {
		return openai.ErrorWrapper(reserveErr, "insufficient_quota", 403)
	}
	c.Header("X-Inference-Id", reservationKey)
	c.Header("x-should-retry", "false")
	finalized := false
	submitted, rejected := false, false
	defer func() {
		if !finalized {
			if submitted && !rejected {
				cause := errors.New("image result could not be confirmed")
				if outErr != nil {
					cause = errors.New(outErr.Message)
				}
				outErr = pendingInferenceError(c, reservationKey, cause, "image_outcome_unknown")
				return
			}
			if err := model.RefundInference(context.WithoutCancel(ctx), reservationKey); err != nil {
				logger.Errorf(ctx, "image refund pending: %v", err)
			}
		}
	}()

	var requestBody io.Reader
	if isModelMapped || meta.ChannelID == "azure" {
		jsonStr, err := json.Marshal(imageRequest)
		if err != nil {
			return openai.ErrorWrapper(err, "marshal_image_request_failed", http.StatusInternalServerError)
		}
		requestBody = bytes.NewBuffer(jsonStr)
	} else {
		requestBody = c.Request.Body
	}

	adaptor := relay.GetAdaptorByChannelID(meta.ChannelID)
	if adaptor == nil {
		return openai.ErrorWrapper(fmt.Errorf("invalid channel type: %s", meta.ChannelID), "invalid_channel_type", http.StatusBadRequest)
	}
	adaptor.Init(meta)

	// 协议转换改为能力探测：实现 ImageRequestRewriter 的适配器声明自己需要
	// 把 OpenAI 图片请求重写为上游原生结构。历史实现在这里硬编码了
	// zhipu/ali/replicate/baidu 四个渠道 ID，新增 provider 必须改 handler；
	// 现在把决定权交给适配器自身，符合开放封闭原则。
	if rewriter, ok := adaptor.(adaptorpkg.ImageRequestRewriter); ok {
		finalRequest, err := rewriter.RewriteImageRequest(imageRequest)
		if err != nil {
			return openai.ErrorWrapper(err, "convert_image_request_failed", http.StatusBadRequest)
		}
		jsonStr, err := json.Marshal(finalRequest)
		if err != nil {
			return openai.ErrorWrapper(err, "marshal_image_request_failed", http.StatusInternalServerError)
		}
		requestBody = bytes.NewBuffer(jsonStr)
	}

	submitted = true
	resp, err := adaptor.DoRequest(c, meta, requestBody)
	if err != nil {
		logger.Errorf(ctx, "DoRequest failed: %s", err.Error())
		return openai.ErrorWrapper(err, "do_request_failed", http.StatusInternalServerError)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		rejected = relaymodel.RejectedHTTPStatus(resp.StatusCode)
		return RelayErrorHandler(resp)
	}
	originalWriter := c.Writer
	buffered := newBufferedInferenceResponse(originalWriter)
	c.Writer = buffered
	_, respErr := adaptor.DoResponse(c, resp, meta)
	c.Writer = originalWriter
	if respErr != nil {
		return respErr
	}
	var output openai.ImageResponse
	if json.Unmarshal(buffered.body.Bytes(), &output) != nil || len(output.Data) == 0 {
		return openai.ErrorWrapper(errors.New("invalid image response or result count"), "invalid_image_response", 502)
	}
	if priceResult.BillingType == model.BillingTypePerRequest {
		quota = billingratio.CalculatePerRequestQuota(priceResult.PerRequestPrice, imageCostRatio, len(output.Data), groupDiscount)
	} else if imageRequest.N > 0 {
		quota = quota / int64(imageRequest.N) * int64(len(output.Data))
	}
	if priceResult.InputPrice == 0 && priceResult.OutputPrice == 0 && priceResult.PerRequestPrice == 0 {
		quota = 0
	}
	metrics := []relaymodel.UsageMetric{{Metric: "image.generated", Quantity: int64(len(output.Data)), Scale: 1, Unit: "image"}}
	usage, _ := json.Marshal(metrics)
	finalized = true
	if settleErr := model.SettleInference(context.WithoutCancel(ctx), reservationKey, quota, string(usage)); settleErr != nil {
		if pendingErr := model.RecordInferenceSettlementRetry(context.WithoutCancel(ctx), reservationKey, quota, string(usage)); pendingErr != nil {
			logger.Errorf(ctx, "exact image charge persistence failed: %v", pendingErr)
		}
		recordPendingResult(ctx, reservationKey, string(usage), buffered.body.String(), "settlement_pending")
		c.Header("X-Inference-Id", reservationKey)
		c.Header("X-Inference-Status", "settlement_pending")
		c.Header("x-should-retry", "false")
	}
	if quota != 0 {
		model.RecordConsumeLog(ctx, &model.Log{UserId: meta.UserId, ChannelId: meta.ChannelId, ModelName: meta.OriginModelName, ResponseModelName: meta.ActualModelName, TokenName: c.GetString(ctxkey.TokenName), Quota: int(quota), Content: fmt.Sprintf("图片计费：%d张 × 分组折扣%.2f", len(output.Data), groupDiscount), PlanId: meta.PlanId})
	}
	for k, v := range buffered.Header() {
		for _, value := range v {
			c.Writer.Header().Add(k, value)
		}
	}
	c.Header("X-Inference-Id", reservationKey)
	c.Data(buffered.status, "application/json", buffered.body.Bytes())

	return nil
}
