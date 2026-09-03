package controller

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/client"
	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/helper"
	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/common/openaioauth"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay/channeltype"
	"github.com/modelbus/one-api-pro/relay/registry"
)

type refreshChannelModelsRequest struct {
	Id      int    `json:"id"`
	Type    int    `json:"type"`
	Key     string `json:"key"`
	BaseURL string `json:"base_url"`
}

type upstreamModelsResponse struct {
	Data   []upstreamModel `json:"data"`
	Models []upstreamModel `json:"models"`
	Error  any             `json:"error"`
}

type upstreamModel struct {
	Id    string `json:"id"`
	Name  string `json:"name"`
	Model string `json:"model"`
	Slug  string `json:"slug"`
}

const codexModelListUnavailableMessage = "无法自动获取 Codex OAuth 模型列表：上游未提供可枚举的模型目录。请在模型框中直接输入模型名并回车添加。"

var openRouterModelCache = struct {
	sync.RWMutex
	models    []string
	sourceURL string
	expiresAt time.Time
}{}

func normalizeModelNames(names []string) []string {
	seen := make(map[string]bool, len(names))
	result := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" && !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	return result
}

func copyModelNames(names []string) []string {
	result := make([]string, len(names))
	copy(result, names)
	return result
}

func resolveChannelBaseURL(channelType int, baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL != "" {
		return baseURL
	}
	return strings.TrimRight(registry.GetDefaultBaseURL(registry.IDByLegacyType(channelType)), "/")
}

func uniqueURLs(urls ...string) []string {
	seen := make(map[string]bool, len(urls))
	result := make([]string, 0, len(urls))
	for _, url := range urls {
		if url = strings.TrimSpace(url); url != "" && !seen[url] {
			seen[url] = true
			result = append(result, url)
		}
	}
	return result
}

func buildOpenRouterModelURLs(baseURL string) []string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api"
	}
	urls := []string{baseURL + "/v1/models", baseURL + "/models"}
	if strings.HasSuffix(baseURL, "/v1") {
		urls = []string{baseURL + "/models", strings.TrimSuffix(baseURL, "/v1") + "/v1/models"}
	}
	urls = append(urls, "https://openrouter.ai/api/v1/models")
	return uniqueURLs(urls...)
}

func parseUpstreamModels(resp *http.Response) ([]string, string, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, "", err
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return nil, "上游模型列表接口返回空响应", fmt.Errorf("upstream returned an empty model list")
	}
	var decoded upstreamModelsResponse
	if strings.HasPrefix(trimmed, "[") {
		err = json.Unmarshal(body, &decoded.Data)
	} else {
		err = json.Unmarshal(body, &decoded)
	}
	if err != nil {
		return nil, "解析上游模型列表 JSON 失败", err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Sprintf("上游返回状态码 %d", resp.StatusCode), fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}
	items := decoded.Data
	if len(items) == 0 {
		items = decoded.Models
	}
	models := make([]string, 0, len(items))
	for _, item := range items {
		switch {
		case item.Id != "":
			models = append(models, item.Id)
		case item.Name != "":
			models = append(models, item.Name)
		case item.Model != "":
			models = append(models, item.Model)
		case item.Slug != "":
			models = append(models, item.Slug)
		}
	}
	return normalizeModelNames(models), "", nil
}

func requestUpstreamModels(url, key string) ([]string, string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	httpClient := client.ImpatientHTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	return parseUpstreamModels(resp)
}

func refreshOpenRouterModels(baseURL, key string) ([]string, string, error) {
	var lastErr error
	for _, url := range buildOpenRouterModelURLs(baseURL) {
		models, _, err := requestUpstreamModels(url, key)
		if err == nil && len(models) > 0 {
			return models, url, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", fmt.Errorf("OpenRouter 未返回模型列表")
}

func getOpenRouterModelList(baseURL, key string, force bool) ([]string, string, error) {
	now := time.Now()
	openRouterModelCache.RLock()
	if !force && len(openRouterModelCache.models) > 0 && now.Before(openRouterModelCache.expiresAt) {
		models := copyModelNames(openRouterModelCache.models)
		source := openRouterModelCache.sourceURL
		openRouterModelCache.RUnlock()
		return models, source, nil
	}
	openRouterModelCache.RUnlock()

	openRouterModelCache.Lock()
	defer openRouterModelCache.Unlock()
	if !force && len(openRouterModelCache.models) > 0 && time.Now().Before(openRouterModelCache.expiresAt) {
		return copyModelNames(openRouterModelCache.models), openRouterModelCache.sourceURL, nil
	}
	models, source, err := refreshOpenRouterModels(baseURL, key)
	if err != nil {
		if len(openRouterModelCache.models) > 0 {
			return copyModelNames(openRouterModelCache.models), openRouterModelCache.sourceURL, nil
		}
		return nil, "", err
	}
	openRouterModelCache.models = copyModelNames(models)
	openRouterModelCache.sourceURL = source
	openRouterModelCache.expiresAt = time.Now().Add(6 * time.Hour)
	return copyModelNames(models), source, nil
}

func refreshOpenAICodexModels(channelID int, baseURL, key string) ([]string, string, error) {
	cred, err := openaioauth.ParseCredentialKey(key)
	if err != nil {
		return nil, "", err
	}
	if cred.NeedsRefresh() && cred.RefreshToken != "" {
		cred, err = openaioauth.RefreshAccessToken(cred, openaioauth.DefaultConfig())
		if err != nil {
			return nil, "", err
		}
		encoded, encodeErr := openaioauth.EncodeCredentialKey(cred)
		if encodeErr != nil {
			return nil, "", encodeErr
		}
		if channelID > 0 {
			if updateErr := model.UpdateChannelKeyById(channelID, encoded); updateErr != nil {
				logger.SysError(fmt.Sprintf("failed to persist refreshed Codex credential: %s", updateErr.Error()))
			}
		}
	}
	for _, url := range uniqueURLs(strings.TrimRight(baseURL, "/")+"/models", strings.TrimRight(baseURL, "/")+"/v1/models") {
		models, _, requestErr := requestUpstreamModels(url, cred.AccessToken)
		if requestErr == nil && len(models) > 0 {
			return models, url, nil
		}
	}
	return nil, "", fmt.Errorf("%s", codexModelListUnavailableMessage)
}

func GetAllChannels(c *gin.Context) {
	p, _ := strconv.Atoi(c.Query("p"))
	if p < 0 {
		p = 0
	}
	channels, err := model.GetAllChannels(p*config.ItemsPerPage, config.ItemsPerPage, "limited")
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
		"data":    channels,
	})
	return
}

func SearchChannels(c *gin.Context) {
	keyword := c.Query("keyword")
	channels, err := model.SearchChannels(keyword)
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
		"data":    channels,
	})
	return
}

func GetChannel(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	channel, err := model.GetChannelById(id, false)
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
		"data":    channel,
	})
	return
}

// RefreshChannelModels fetches a provider's current model catalogue. The
// result is returned to the editor; it is intentionally not written to the
// channel until the administrator saves the form.
func RefreshChannelModels(c *gin.Context) {
	var req refreshChannelModelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if req.Id != 0 {
		channel, err := model.GetChannelById(req.Id, true)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
			return
		}
		if req.Type == 0 {
			req.Type = channel.Type
		}
		if req.Key == "" {
			req.Key = channel.Key
		}
		if req.BaseURL == "" {
			req.BaseURL = channel.GetBaseURL()
		}
	}
	req.Key = strings.TrimSpace(req.Key)
	if req.Type == 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "请先选择渠道类型"})
		return
	}
	if req.Type != channeltype.OpenRouter && req.Key == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "请先填写渠道密钥"})
		return
	}
	baseURL := resolveChannelBaseURL(req.Type, req.BaseURL)
	var models []string
	var sourceURL string
	var err error
	switch req.Type {
	case channeltype.OpenRouter:
		models, sourceURL, err = getOpenRouterModelList(baseURL, req.Key, true)
	case channeltype.OpenAICodexOAuth:
		models, sourceURL, err = refreshOpenAICodexModels(req.Id, baseURL, req.Key)
	default:
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "当前渠道类型暂不支持自动刷新模型列表"})
		return
	}
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"models":     models,
			"source_url": sourceURL,
		},
	})
}

func AddChannel(c *gin.Context) {
	channel := model.Channel{}
	err := c.ShouldBindJSON(&channel)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	channel.CreatedTime = helper.GetTimestamp()
	keys := strings.Split(channel.Key, "\n")
	channels := make([]model.Channel, 0, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		localChannel := channel
		localChannel.Key = key
		channels = append(channels, localChannel)
	}
	err = model.BatchInsertChannels(channels)
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

func DeleteChannel(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	channel := model.Channel{Id: id}
	err := channel.Delete()
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

func DeleteDisabledChannel(c *gin.Context) {
	rows, err := model.DeleteDisabledChannel()
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
		"data":    rows,
	})
	return
}

func UpdateChannel(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	channel := model.Channel{}
	if err := json.Unmarshal(body, &channel); err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	// Determine which JSON keys were actually present in the payload so that
	// Update() only touches those columns. A partial request like {id, status}
	// must not wipe the channel's other fields to empty.
	provided := map[string]bool{}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err == nil {
		for key := range raw {
			provided[key] = true
		}
	}
	err = channel.Update(provided)
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
		"data":    channel,
	})
	return
}
