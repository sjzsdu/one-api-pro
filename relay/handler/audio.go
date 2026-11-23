// audio.go 音频中继、原生语音适配与成功后计费
// Relays audio and settles native speech only after validated media delivery.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/client"
	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/common/helper"
	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay"
	adaptorpkg "github.com/modelbus/one-api-pro/relay/adaptor"
	"github.com/modelbus/one-api-pro/relay/adaptor/openai"
	"github.com/modelbus/one-api-pro/relay/billing"
	billingratio "github.com/modelbus/one-api-pro/relay/billing/ratio"
	"github.com/modelbus/one-api-pro/relay/meta"
	"github.com/modelbus/one-api-pro/relay/relaymode"
	relaymodel "github.com/modelbus/one-api-pro/relay/schema"
)

// RelayAudioHelper relays audio with failure refunds and explicit speech usage.
// 版本: v0.0.28
// 日期: 2026-11-23
func RelayAudioHelper(c *gin.Context, relayMode int) *relaymodel.ErrorWithStatusCode {
	ctx := c.Request.Context()
	meta := meta.GetByContext(c)
	audioModel := "whisper-1"

	tokenId := c.GetInt(ctxkey.TokenId)
	channelId := c.GetInt(ctxkey.ChannelId)
	userId := c.GetInt(ctxkey.Id)
	group := c.GetString(ctxkey.Group)
	tokenName := c.GetString(ctxkey.TokenName)

	var ttsRequest relaymodel.AudioSpeechRequest
	if relayMode == relaymode.AudioSpeech {
		err := common.UnmarshalBodyReusable(c, &ttsRequest)
		if err != nil {
			return openai.ErrorWrapper(err, "invalid_json", http.StatusBadRequest)
		}
		audioModel = ttsRequest.Model
		if utf8.RuneCountInString(ttsRequest.Input) > 4096 {
			return openai.ErrorWrapper(errors.New("input is too long (over 4096 characters)"), "text_too_long", http.StatusBadRequest)
		}
	}

	originAudioModel := audioModel
	modelMapping := c.GetStringMapString(ctxkey.ModelMapping)
	if modelMapping != nil && modelMapping[audioModel] != "" {
		audioModel = modelMapping[audioModel]
	}

	// 发送请求有两条链路：
	//   1) 原生协议链：渠道适配器实现 AudioSpeechRequester，负责把 OpenAI speech
	//      请求转换为上游原生请求体，URL 与 header 也由适配器决定。新增 TTS
	//      provider 只需实现可选接口，无需改动本 handler（开放封闭）。
	//   2) OpenAI 兼容透传（默认，保持历史行为）：baseURL + /v1/audio/speech 原样转发。
	// 转写 / 翻译两种模式不接适配器，始终走透传。
	var speechAdaptor adaptorpkg.Adaptor
	useNativeSpeech := false
	if relayMode == relaymode.AudioSpeech {
		if candidate := relay.GetAdaptorByChannelID(meta.ChannelID); candidate != nil {
			if _, ok := candidate.(adaptorpkg.AudioSpeechRequester); ok {
				speechAdaptor = candidate
				speechAdaptor.Init(meta)
				useNativeSpeech = true
			}
		}
	}

	var nativeBody []byte
	if useNativeSpeech {
		nativeRequest := ttsRequest
		nativeRequest.Model = audioModel
		converted, convErr := speechAdaptor.(adaptorpkg.AudioSpeechRequester).ConvertAudioSpeechRequest(&nativeRequest)
		if convErr != nil {
			return openai.ErrorWrapper(convErr, "convert_audio_speech_request_failed", http.StatusBadRequest)
		}
		var marshalErr error
		nativeBody, marshalErr = json.Marshal(converted)
		if marshalErr != nil {
			return openai.ErrorWrapper(marshalErr, "marshal_audio_speech_request_failed", http.StatusInternalServerError)
		}
	}

	meta.ActualModelName = audioModel

	priceResult, err := billingratio.GetModelPrice(audioModel, originAudioModel)
	if err != nil {
		return openai.ErrorWrapper(err, "model_price_not_found", http.StatusUnprocessableEntity)
	}
	groupDiscount := billingratio.GetGroupDiscount(group, audioModel, originAudioModel)

	var quota int64
	var preConsumedQuota int64
	var bizErr *relaymodel.ErrorWithStatusCode
	if priceResult.BillingType == model.BillingTypePerRequest {
		// per_request：按次计费，费用在请求前已完全确定，精确预扣。
		// 旧实现按 token 占比估算预扣、却按次全额结算，单次即可透支余额。
		// per_request: flat price known upfront — pre-consume the exact amount
		// so the settlement delta is 0 and the balance cannot go negative.
		quota = billingratio.CalculatePerRequestQuota(priceResult.PerRequestPrice, 1, 1, groupDiscount)
		preConsumedQuota, bizErr = preConsumeExactQuota(ctx, quota, meta)
	} else {
		switch relayMode {
		case relaymode.AudioSpeech:
			quota = speechCharacterQuota(utf8.RuneCountInString(ttsRequest.Input), priceResult.InputPrice, groupDiscount)
			reservation := quota
			if useNativeSpeech && (priceResult.InputPrice > 0 || priceResult.OutputPrice > 0) {
				reservation = speechTokenQuota(openai.CountTokenText(ttsRequest.Input, audioModel), 0, priceResult.InputPrice, priceResult.OutputPrice, groupDiscount) + config.PreConsumedQuota
			}
			preConsumedQuota, bizErr = preConsumeAmount(ctx, reservation, meta)
		default:
			// PreConsumedQuota 是「额外预留的额度（微元）」，不是 token 数，
			// 不再乘 ratio（历史实现把它当 token 数会放大 ratio 倍）。
			// PreConsumedQuota is an extra reserved quota in micro-quota, not a token count.
			preConsumedQuota, bizErr = preConsumeAmount(ctx, config.PreConsumedQuota, meta)
		}
	}
	if bizErr != nil {
		return bizErr
	}
	succeed := false
	defer func() {
		if succeed {
			return
		}
		if preConsumedQuota > 0 {
			if refundErr := billing.ReturnPreConsumedQuotaSync(ctx, preConsumedQuota, tokenId, userId); refundErr != nil {
				logger.Errorf(ctx, "error rollback pre-consumed audio quota: %v", refundErr)
			}
		}
	}()

	baseURL := meta.BaseURL
	requestURL := c.Request.URL.String()
	if c.GetString(ctxkey.BaseURL) != "" {
		baseURL = c.GetString(ctxkey.BaseURL)
	}

	requestBody := &bytes.Buffer{}
	_, err = io.Copy(requestBody, c.Request.Body)
	if err != nil {
		return openai.ErrorWrapper(err, "new_request_body_failed", http.StatusInternalServerError)
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(requestBody.Bytes()))
	responseFormat := c.DefaultPostForm("response_format", "json")

	var resp *http.Response
	if useNativeSpeech {
		resp, err = speechAdaptor.DoRequest(c, meta, bytes.NewReader(nativeBody))
		if err != nil {
			logger.Errorf(ctx, "native TTS DoRequest failed: %s", err.Error())
			return openai.ErrorWrapper(err, "do_request_failed", http.StatusInternalServerError)
		}
	} else {
		fullRequestURL := openai.GetFullRequestURL(baseURL, requestURL, meta.ChannelID)
		if meta.ChannelID == "azure" {
			apiVersion := meta.Config.APIVersion
			if relayMode == relaymode.AudioTranscription {
				fullRequestURL = fmt.Sprintf("%s/openai/deployments/%s/audio/transcriptions?api-version=%s", baseURL, audioModel, apiVersion)
			} else if relayMode == relaymode.AudioSpeech {
				fullRequestURL = fmt.Sprintf("%s/openai/deployments/%s/audio/speech?api-version=%s", baseURL, audioModel, apiVersion)
			}
		}

		req, reqErr := http.NewRequest(c.Request.Method, fullRequestURL, requestBody)
		if reqErr != nil {
			return openai.ErrorWrapper(reqErr, "new_request_failed", http.StatusInternalServerError)
		}

		if (relayMode == relaymode.AudioTranscription || relayMode == relaymode.AudioSpeech) && meta.ChannelID == "azure" {
			apiKey := c.Request.Header.Get("Authorization")
			apiKey = strings.TrimPrefix(apiKey, "Bearer ")
			req.Header.Set("api-key", apiKey)
			req.ContentLength = c.Request.ContentLength
		} else {
			req.Header.Set("Authorization", c.Request.Header.Get("Authorization"))
		}
		req.Header.Set("Content-Type", c.Request.Header.Get("Content-Type"))
		req.Header.Set("Accept", c.Request.Header.Get("Accept"))

		resp, err = client.HTTPClient.Do(req)
		if err != nil {
			return openai.ErrorWrapper(err, "do_request_failed", http.StatusInternalServerError)
		}

		if closeErr := req.Body.Close(); closeErr != nil {
			return openai.ErrorWrapper(closeErr, "close_request_body_failed", http.StatusInternalServerError)
		}
		if closeErr := c.Request.Body.Close(); closeErr != nil {
			return openai.ErrorWrapper(closeErr, "close_request_body_failed", http.StatusInternalServerError)
		}
	}

	if relayMode != relaymode.AudioSpeech {
		responseBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return openai.ErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		}
		err = resp.Body.Close()
		if err != nil {
			return openai.ErrorWrapper(err, "close_response_body_failed", http.StatusInternalServerError)
		}

		var openAIErr openai.SlimTextResponse
		if err = json.Unmarshal(responseBody, &openAIErr); err == nil {
			if openAIErr.Error.Message != "" {
				return openai.ErrorWrapper(fmt.Errorf("type %s, code %v, message %s", openAIErr.Error.Type, openAIErr.Error.Code, openAIErr.Error.Message), "request_error", http.StatusInternalServerError)
			}
		}

		var text string
		switch responseFormat {
		case "json":
			text, err = getTextFromJSON(responseBody)
		case "text":
			text, err = getTextFromText(responseBody)
		case "srt":
			text, err = getTextFromSRT(responseBody)
		case "verbose_json":
			text, err = getTextFromVerboseJSON(responseBody)
		case "vtt":
			text, err = getTextFromVTT(responseBody)
		default:
			return openai.ErrorWrapper(errors.New("unexpected_response_format"), "unexpected_response_format", http.StatusInternalServerError)
		}
		if err != nil {
			return openai.ErrorWrapper(err, "get_text_from_body_err", http.StatusInternalServerError)
		}
		// per_request：费用为每次固定价，与转写文本长度无关，结算时
		// 必须沿用按次价，否则会又变回按 token 扣费。
		// per_request: flat per-call price, independent of transcript length.
		if priceResult.BillingType == model.BillingTypePerRequest {
			quota = billingratio.CalculatePerRequestQuota(priceResult.PerRequestPrice, 1, 1, groupDiscount)
		} else {
			// 与 CalculateTokenQuota 保持一致：math.Round 取整 + 最小 1 微元兜底，
			// 避免极短转写文本（或空文本）导致 0 扣费。
			tokens := openai.CountTokenText(text, audioModel)
			quota = int64(math.Round(float64(tokens) * priceResult.InputPrice / billingratio.Million * config.QuotaPerUnit * groupDiscount))
			if quota <= 0 && tokens > 0 {
				quota = 1
			}
		}
		resp.Body = io.NopCloser(bytes.NewBuffer(responseBody))
	}
	if resp.StatusCode != http.StatusOK {
		return RelayErrorHandler(resp)
	}

	var billedInput, billedOutput int
	billingUnit := ""
	if relayMode == relaymode.AudioSpeech {
		billedInput = utf8.RuneCountInString(ttsRequest.Input)
		billingUnit = "characters"
	}
	if useNativeSpeech {
		if speechHandler, ok := speechAdaptor.(adaptorpkg.AudioSpeechHandler); ok {
			result, respErr := speechHandler.AudioSpeechResponse(c, resp, meta)
			if respErr != nil {
				return respErr
			}
			if result == nil || len(result.Body) == 0 {
				return openai.ErrorWrapper(errors.New("empty normalized speech response"), "tts_audio_empty", http.StatusBadGateway)
			}
			if result.TokenUsage != nil {
				billedInput, billedOutput = result.TokenUsage.PromptTokens, result.TokenUsage.CompletionTokens
				billingUnit = "tokens"
			} else if result.Characters != nil {
				billedInput = *result.Characters
			}
			if priceResult.BillingType != model.BillingTypePerRequest {
				if result.TokenUsage != nil {
					quota = speechTokenQuota(billedInput, billedOutput, priceResult.InputPrice, priceResult.OutputPrice, groupDiscount)
				} else {
					quota = speechCharacterQuota(billedInput, priceResult.InputPrice, groupDiscount)
				}
			}
			resp.Body = io.NopCloser(bytes.NewReader(result.Body))
			resp.Header = make(http.Header)
			resp.Header.Set("Content-Type", result.ContentType)
			resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(result.Body)))
		}
	}

	for k, v := range resp.Header {
		c.Writer.Header().Set(k, v[0])
	}
	c.Writer.WriteHeader(resp.StatusCode)

	_, err = io.Copy(c.Writer, resp.Body)
	if err != nil {
		return openai.ErrorWrapper(err, "copy_response_body_failed", http.StatusInternalServerError)
	}
	err = resp.Body.Close()
	if err != nil {
		return openai.ErrorWrapper(err, "close_response_body_failed", http.StatusInternalServerError)
	}
	succeed = true
	quotaDelta := quota - preConsumedQuota
	defer func(ctx context.Context) {
		// 订阅用户：消耗套餐额度而非全局余额。历史实现让音频请求完全绕过
		// 订阅链路，导致订阅用户的余额被真实扣减。
		// Subscription users burn plan quota instead of account balance.
		if meta.PlanId > 0 {
			applyAudioSubscriptionConsume(ctx, meta, quota, originAudioModel, tokenName)
			return
		}
		billing.PostConsumeQuota(ctx, &billing.ConsumeQuotaParams{
			TokenId:          tokenId,
			UserId:           userId,
			ChannelId:        channelId,
			QuotaDelta:       quotaDelta,
			TotalQuota:       quota,
			ModelName:        originAudioModel,
			TokenName:        tokenName,
			InputPrice:       priceResult.InputPrice,
			OutputPrice:      priceResult.OutputPrice,
			CachedPrice:      priceResult.CachedPrice,
			PerRequestPrice:  priceResult.PerRequestPrice,
			GroupDiscount:    groupDiscount,
			PromptTokens:     billedInput,
			CompletionTokens: billedOutput,
			BillingUnit:      billingUnit,
			BillingType:      priceResult.BillingType,
		})
	}(c.Request.Context())

	return nil
}

// speechCharacterQuota bills Unicode characters at the configured input price.
// 版本: v0.0.28
// 日期: 2026-11-23
func speechCharacterQuota(characters int, inputPrice, discount float64) int64 {
	return speechTokenQuota(characters, 0, inputPrice, 0, discount)
}

func speechTokenQuota(input, output int, inputPrice, outputPrice, discount float64) int64 {
	cost := (float64(input)*inputPrice + float64(output)*outputPrice) * discount / billingratio.Million * config.QuotaPerUnit
	if cost <= 0 {
		return 0
	}
	quota := int64(math.Round(cost))
	if quota == 0 {
		return 1
	}
	return quota
}

// applyAudioSubscriptionConsume 音频请求的订阅结算：累加套餐窗口用量与虚拟余额，
// 并把消费计入用户 used_quota / request_count（不扣用户全局余额）。
//
// 版本: v0.0.25
// 日期: 2026-10-05
// 作者: opencode
func applyAudioSubscriptionConsume(ctx context.Context, m *meta.Meta, quota int64, modelName string, tokenName string) {
	now := helper.GetTimestamp()
	ups, err := model.CacheGetUserActivePlans(m.UserId)
	if err != nil {
		logger.Error(ctx, "failed to get active plans for audio subscription billing: "+err.Error())
	} else {
		for _, up := range ups {
			if int(up.Id) != m.PlanId {
				continue
			}
			// 套餐配置优先取实时值，plans 行已删时回退到购买快照。
			limits, limitsErr := up.EffectiveModelLimits()
			if limitsErr != nil {
				logger.Error(ctx, "failed to parse plan model limits: "+limitsErr.Error())
				break
			}
			rule, found := model.FindLimit(limits, modelName)
			if !found {
				break
			}
			for _, windowType := range []string{model.WindowTypePeriod, model.WindowTypeWeek, model.WindowTypeMonth} {
				windowIndex := model.CalcWindowIndex(now, up.StartTime, windowType, rule.PeriodH)
				if err := model.IncrementPlanUsage(int(up.Id), modelName, windowType, windowIndex, 1, 0, 0, 0); err != nil {
					logger.Error(ctx, "failed to increment plan usage: "+err.Error())
				}
			}
			if err := model.IncrementUserPlanUsedAmount(int(up.Id), quota); err != nil {
				logger.Error(ctx, "failed to increment plan used amount: "+err.Error())
			}
			break
		}
	}
	model.UpdateUserUsedQuotaAndRequestCount(m.UserId, quota)
	model.UpdateChannelUsedQuota(m.ChannelId, quota)
	if quota != 0 {
		model.RecordConsumeLog(ctx, &model.Log{
			UserId:        m.UserId,
			ChannelId:     m.ChannelId,
			ModelName:     modelName,
			TokenName:     tokenName,
			Quota:         int(quota),
			Content:       "订阅计费 | 音频请求",
			BillingSource: 1,
			PlanId:        m.PlanId,
		})
	}
}

func getTextFromVTT(body []byte) (string, error) {
	return getTextFromSRT(body)
}
func getTextFromVerboseJSON(body []byte) (string, error) {
	var whisperResponse openai.WhisperVerboseJSONResponse
	if err := json.Unmarshal(body, &whisperResponse); err != nil {
		return "", fmt.Errorf("unmarshal_response_body_failed err :%w", err)
	}
	return whisperResponse.Text, nil
}

func getTextFromSRT(body []byte) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	var builder strings.Builder
	var textLine bool
	for scanner.Scan() {
		line := scanner.Text()
		if textLine {
			builder.WriteString(line)
			textLine = false
			continue
		} else if strings.Contains(line, "-->") {
			textLine = true
			continue
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return builder.String(), nil
}

func getTextFromText(body []byte) (string, error) {
	return strings.TrimSuffix(string(body), "\n"), nil
}

func getTextFromJSON(body []byte) (string, error) {
	var whisperResponse openai.WhisperJSONResponse
	if err := json.Unmarshal(body, &whisperResponse); err != nil {
		return "", fmt.Errorf("unmarshal_response_body_failed err :%w", err)
	}
	return whisperResponse.Text, nil
}
