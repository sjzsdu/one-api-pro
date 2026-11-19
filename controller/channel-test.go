package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/common/helper"
	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/common/message"
	"github.com/modelbus/one-api-pro/middleware"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/monitor"
	"github.com/modelbus/one-api-pro/relay"
	relayadaptor "github.com/modelbus/one-api-pro/relay/adaptor"
	"github.com/modelbus/one-api-pro/relay/adaptor/openai"
	"github.com/modelbus/one-api-pro/relay/handler"
	"github.com/modelbus/one-api-pro/relay/meta"
	"github.com/modelbus/one-api-pro/relay/relaymode"
	relaymodel "github.com/modelbus/one-api-pro/relay/schema"
)

func buildTestRequest(model string) *relaymodel.GeneralOpenAIRequest {
	if model == "" {
		model = "gpt-3.5-turbo"
	}
	testRequest := &relaymodel.GeneralOpenAIRequest{
		Model: model,
		Input: config.TestPrompt,
	}
	testMessage := relaymodel.Message{
		Role:    "user",
		Content: config.TestPrompt,
	}
	testRequest.Messages = append(testRequest.Messages, testMessage)
	return testRequest
}

func isEmbeddingTestModel(modelName string) bool {
	modelName = strings.ToLower(modelName)
	return strings.Contains(modelName, "embedding") ||
		strings.Contains(modelName, "bge-") ||
		strings.Contains(modelName, "/bge") ||
		strings.Contains(modelName, "gte-") ||
		strings.Contains(modelName, "bce-embedding")
}

// isNativeMultimodalTestModel reports whether a model is served only by the
// native (non-OpenAI-compatible) multimodal embedding endpoint. Naming it
// explicitly avoids routing every *-embedding model to the native path:
// text-embedding-v3 and friends must keep using /v1/embeddings.
func isNativeMultimodalTestModel(modelName string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(modelName)), "qwen3-vl-embedding")
}

// parseNativeMultimodalTestResponse parses a DashScope native multimodal
// embedding response and mirrors parseEmbeddingTestResponse's success signal.
func parseNativeMultimodalTestResponse(resp []byte) (string, error) {
	var payload struct {
		Output struct {
			Embeddings []struct {
				Embedding []float64 `json:"embedding"`
			} `json:"embeddings"`
		} `json:"output"`
	}
	if err := json.Unmarshal(resp, &payload); err != nil {
		return "", err
	}
	if len(payload.Output.Embeddings) == 0 || len(payload.Output.Embeddings[0].Embedding) == 0 {
		return "", errors.New("multimodal embedding response has no vector data")
	}
	return fmt.Sprintf("embedding ok, dimensions: %d", len(payload.Output.Embeddings[0].Embedding)), nil
}

// testNativeMultimodalEmbedding sends a minimal native request to an
// adaptor that implements MultimodalEmbeddingURLer (currently alibailian).
// It deliberately mirrors RelayMultimodalEmbeddingHelper's URL/header choices:
// the compatible-mode /v1/embeddings path would 404 for these models.
func testNativeMultimodalEmbedding(c *gin.Context, adaptor relayadaptor.Adaptor, meta *meta.Meta, modelName string) (string, error, *relaymodel.Error) {
	urler, ok := adaptor.(relayadaptor.MultimodalEmbeddingURLer)
	if !ok {
		return "", fmt.Errorf("channel does not support native multimodal embeddings (model %s)", modelName), nil
	}
	fullURL, err := urler.GetMultimodalEmbeddingURL(meta)
	if err != nil {
		return "", err, nil
	}
	// Minimal native body: one text content. Dimension is omitted so the upstream
	// default applies; the test only asserts the endpoint is reachable.
	body, err := json.Marshal(map[string]any{
		"model": modelName,
		"input": map[string]any{
			"contents": []map[string]string{{"text": config.TestPrompt}},
		},
	})
	if err != nil {
		return "", err, nil
	}
	outReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, fullURL, bytes.NewReader(body))
	if err != nil {
		return "", err, nil
	}
	outReq.Header.Set("Content-Type", "application/json")
	outReq.Header.Set("Accept", "application/json")
	outReq.Header.Set("Authorization", "Bearer "+meta.APIKey)
	// relayadaptor.DoRequest closes c.Request.Body on success; the test context
	// created in testChannel has a nil body, so give it an inert one.
	if c.Request.Body == nil {
		c.Request.Body = io.NopCloser(bytes.NewReader(nil))
	}
	resp, err := relayadaptor.DoRequest(c, outReq)
	if err != nil {
		return "", err, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bizErr := controller.RelayErrorHandler(resp)
		msg := bizErr.Error.Message
		if msg != "" {
			msg = ", error message: " + msg
		}
		return "", fmt.Errorf("http status code: %d%s", resp.StatusCode, msg), &bizErr.Error
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err, nil
	}
	message, err := parseNativeMultimodalTestResponse(respBody)
	return message, err, nil
}

func parseTestResponse(resp string) (*openai.TextResponse, string, error) {
	var response openai.TextResponse
	err := json.Unmarshal([]byte(resp), &response)
	if err != nil {
		return nil, "", err
	}
	if len(response.Choices) == 0 {
		return nil, "", errors.New("response has no choices")
	}
	stringContent, ok := response.Choices[0].Content.(string)
	if !ok {
		return nil, "", errors.New("response content is not string")
	}
	return &response, stringContent, nil
}

func parseEmbeddingTestResponse(resp string) (string, error) {
	var response openai.EmbeddingResponse
	if err := json.Unmarshal([]byte(resp), &response); err != nil {
		return "", err
	}
	if len(response.Data) == 0 || len(response.Data[0].Embedding) == 0 {
		return "", errors.New("embedding response has no vector data")
	}
	return fmt.Sprintf("embedding ok, dimensions: %d", len(response.Data[0].Embedding)), nil
}

func testChannel(ctx context.Context, channel *model.Channel, request *relaymodel.GeneralOpenAIRequest) (responseMessage string, err error, openaiErr *relaymodel.Error) {
	startTime := time.Now()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	modelName := request.Model
	modelMap := channel.GetModelMapping()
	if modelName == "" || !channel.ContainsModel(modelName) {
		modelNames := strings.Split(channel.Models, ",")
		if len(modelNames) > 0 {
			modelName = strings.TrimSpace(modelNames[0])
		}
	}
	if modelMap != nil && modelMap[modelName] != "" {
		modelName = modelMap[modelName]
	}
	relayMode := relaymode.ChatCompletions
	requestPath := "/v1/chat/completions"
	if isEmbeddingTestModel(modelName) {
		relayMode = relaymode.Embeddings
		requestPath = "/v1/embeddings"
	}
	c.Request = &http.Request{
		Method: "POST",
		URL:    &url.URL{Path: requestPath},
		Body:   nil,
		Header: make(http.Header),
	}
	c.Request.Header.Set("Authorization", "Bearer "+channel.Key)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(ctxkey.Channel, channel.Type)
	c.Set(ctxkey.BaseURL, channel.GetBaseURL())
	cfg, _ := channel.LoadConfig()
	c.Set(ctxkey.Config, cfg)
	middleware.SetupContextForSelectedChannel(c, channel, "")
	meta := meta.GetByContext(c)
	adaptor := relay.GetAdaptorByChannel(channel.Type)
	if adaptor == nil {
		return "", fmt.Errorf("invalid channel type: %d, adaptor is nil", channel.Type), nil
	}
	adaptor.Init(meta)
	meta.OriginModelName, meta.ActualModelName = request.Model, modelName
	request.Model = modelName
	// Native multimodal embeddings (e.g. qwen3-vl-embedding) only exist on a
	// non-compatible-mode DashScope endpoint. Route the test the same way the
	// production relay does; otherwise /v1/embeddings returns 404 and a healthy
	// channel/model is reported as broken.
	if isNativeMultimodalTestModel(modelName) {
		if _, ok := adaptor.(relayadaptor.MultimodalEmbeddingURLer); ok {
			message, nativeErr, nativeOpenAIErr := testNativeMultimodalEmbedding(c, adaptor, meta, modelName)
			if nativeErr != nil || nativeOpenAIErr != nil {
				return message, nativeErr, nativeOpenAIErr
			}
			return message, nil, nil
		}
	}
	convertedRequest, err := adaptor.ConvertRequest(c, relayMode, request)
	if err != nil {
		return "", err, nil
	}
	jsonData, err := json.Marshal(convertedRequest)
	if err != nil {
		return "", err, nil
	}
	defer func() {
		logContent := fmt.Sprintf("渠道 %s 测试成功，响应：%s", channel.Name, responseMessage)
		if err != nil || openaiErr != nil {
			errorMessage := ""
			if err != nil {
				errorMessage = err.Error()
			} else {
				errorMessage = openaiErr.Message
			}
			logContent = fmt.Sprintf("渠道 %s 测试失败，错误：%s", channel.Name, errorMessage)
		}
		go model.RecordTestLog(ctx, &model.Log{
			ChannelId:   channel.Id,
			ModelName:   modelName,
			Content:     logContent,
			ElapsedTime: helper.CalcElapsedTime(startTime),
		})
	}()
	logger.SysLog(string(jsonData))
	requestBody := bytes.NewBuffer(jsonData)
	c.Request.Body = io.NopCloser(requestBody)
	resp, err := adaptor.DoRequest(c, meta, requestBody)
	if err != nil {
		return "", err, nil
	}
	if resp != nil && resp.StatusCode != http.StatusOK {
		err := controller.RelayErrorHandler(resp)
		errorMessage := err.Error.Message
		if errorMessage != "" {
			errorMessage = ", error message: " + errorMessage
		}
		return "", fmt.Errorf("http status code: %d%s", resp.StatusCode, errorMessage), &err.Error
	}
	usage, respErr := adaptor.DoResponse(c, resp, meta)
	if respErr != nil {
		return "", fmt.Errorf("%s", respErr.Error.Message), &respErr.Error
	}
	if usage == nil {
		return "", errors.New("usage is nil"), nil
	}
	rawResponse := w.Body.String()
	if relayMode == relaymode.Embeddings {
		responseMessage, err = parseEmbeddingTestResponse(rawResponse)
	} else {
		_, responseMessage, err = parseTestResponse(rawResponse)
	}
	if err != nil {
		logger.SysError(fmt.Sprintf("failed to parse test response: %s, \nresponse: %s", err.Error(), rawResponse))
		return "", err, nil
	}
	result := w.Result()
	// print result.Body
	respBody, err := io.ReadAll(result.Body)
	if err != nil {
		return "", err, nil
	}
	logger.SysLog(fmt.Sprintf("testing channel #%d, response: \n%s", channel.Id, string(respBody)))
	return responseMessage, nil, nil
}

func TestChannel(c *gin.Context) {
	ctx := c.Request.Context()
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	channel, err := model.GetChannelById(id, true)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	modelName := c.Query("model")
	testRequest := buildTestRequest(modelName)
	tik := time.Now()
	responseMessage, err, _ := testChannel(ctx, channel, testRequest)
	tok := time.Now()
	milliseconds := tok.Sub(tik).Milliseconds()
	if err != nil {
		milliseconds = 0
	}
	go channel.UpdateResponseTime(milliseconds)
	consumedTime := float64(milliseconds) / 1000.0
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success":   false,
			"message":   err.Error(),
			"time":      consumedTime,
			"modelName": modelName,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"message":   responseMessage,
		"time":      consumedTime,
		"modelName": modelName,
	})
	return
}

var testAllChannelsLock sync.Mutex
var testAllChannelsRunning bool = false

func testChannels(ctx context.Context, notify bool, scope string) error {
	if config.RootUserEmail == "" {
		config.RootUserEmail = model.GetRootUserEmail()
	}
	testAllChannelsLock.Lock()
	if testAllChannelsRunning {
		testAllChannelsLock.Unlock()
		return errors.New("测试已在运行中")
	}
	testAllChannelsRunning = true
	testAllChannelsLock.Unlock()
	channels, err := model.GetAllChannels(0, 0, scope)
	if err != nil {
		return err
	}
	var disableThreshold = int64(config.ChannelDisableThreshold * 1000)
	if disableThreshold == 0 {
		disableThreshold = 10000000 // a impossible value
	}
	go func() {
		for _, channel := range channels {
			isChannelEnabled := channel.Status == model.ChannelStatusEnabled
			tik := time.Now()
			testRequest := buildTestRequest("")
			_, err, openaiErr := testChannel(ctx, channel, testRequest)
			tok := time.Now()
			milliseconds := tok.Sub(tik).Milliseconds()
			if isChannelEnabled && milliseconds > disableThreshold {
				err = fmt.Errorf("响应时间 %.2fs 超过阈值 %.2fs", float64(milliseconds)/1000.0, float64(disableThreshold)/1000.0)
				if config.AutomaticDisableChannelEnabled {
					monitor.DisableChannel(channel.Id, channel.Name, err.Error())
				} else {
					_ = message.Notify(message.ByAll, fmt.Sprintf("渠道 %s （%d）测试超时", channel.Name, channel.Id), "", err.Error())
				}
			}
			if isChannelEnabled && monitor.ShouldDisableChannel(openaiErr, -1) {
				monitor.DisableChannel(channel.Id, channel.Name, err.Error())
			}
			if !isChannelEnabled && monitor.ShouldEnableChannel(err, openaiErr) {
				monitor.EnableChannel(channel.Id, channel.Name)
			}
			channel.UpdateResponseTime(milliseconds)
			time.Sleep(config.RequestInterval)
		}
		testAllChannelsLock.Lock()
		testAllChannelsRunning = false
		testAllChannelsLock.Unlock()
		if notify {
			err := message.Notify(message.ByAll, "渠道测试完成", "", "渠道测试完成，如果没有收到禁用通知，说明所有渠道都正常")
			if err != nil {
				logger.SysError(fmt.Sprintf("failed to send email: %s", err.Error()))
			}
		}
	}()
	return nil
}

func TestChannels(c *gin.Context) {
	ctx := c.Request.Context()
	scope := c.Query("scope")
	if scope == "" {
		scope = "all"
	}
	err := testChannels(ctx, true, scope)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
	return
}

func AutomaticallyTestChannels(frequency int) {
	ctx := context.Background()
	for {
		time.Sleep(time.Duration(frequency) * time.Minute)
		logger.SysLog("testing all channels")
		_ = testChannels(ctx, false, "all")
		logger.SysLog("channel test finished")
	}
}
