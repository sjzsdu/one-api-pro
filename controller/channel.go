package controller

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/client"
	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/helper"
	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/common/openaioauth"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay/adaptor/openaicodex"
	"github.com/modelbus/one-api-pro/relay/channeltype"
	"github.com/modelbus/one-api-pro/relay/registry"
)

type refreshChannelModelsRequest struct {
	Id      int    `json:"id"`
	Type    int    `json:"type"`
	Key     string `json:"key"`
	BaseURL string `json:"base_url"`
}

type validateChannelModelsRequest struct {
	Id           int    `json:"id"`
	Type         int    `json:"type"`
	Key          string `json:"key"`
	BaseURL      string `json:"base_url"`
	Models       string `json:"models"`
	ModelMapping string `json:"model_mapping"`
}

type upstreamModelsResponse struct {
	Data   []upstreamModel `json:"data"`
	Models []upstreamModel `json:"models"`
	Error  any             `json:"error"`
}

type upstreamModel struct {
	Id                  string   `json:"id"`
	Name                string   `json:"name"`
	Model               string   `json:"model"`
	Slug                string   `json:"slug"`
	Description         string   `json:"description"`
	ContextLength       int      `json:"context_length"`
	SupportedParameters []string `json:"supported_parameters"`
	Architecture        struct {
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
		Modality         string   `json:"modality"`
	} `json:"architecture"`
	Pricing struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
	} `json:"pricing"`
}

// modelCatalogItem is display metadata from an upstream catalogue. Pricing is
// deliberately expressed as USD per million tokens and is not used by the
// gateway's local billing tables.
type modelCatalogItem struct {
	Id                    string   `json:"id"`
	Name                  string   `json:"name"`
	Description           string   `json:"description,omitempty"`
	ContextLength         int      `json:"context_length,omitempty"`
	InputModalities       []string `json:"input_modalities,omitempty"`
	OutputModalities      []string `json:"output_modalities,omitempty"`
	SupportedParameters   []string `json:"supported_parameters,omitempty"`
	InputPricePerMillion  *float64 `json:"input_price_usd_per_million,omitempty"`
	OutputPricePerMillion *float64 `json:"output_price_usd_per_million,omitempty"`
}

const (
	codexModelListUnavailableMessage = "无法自动获取 Codex OAuth 模型列表：上游未提供可枚举的模型目录。请在模型框中直接输入模型名并回车添加。"
	codexBuiltinModelListSource      = "builtin://openaicodex"
)

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
	// Do not fall back to OpenRouter's public catalogue when a channel has a
	// custom endpoint. A proxy, regional gateway, or account-scoped endpoint
	// can expose a smaller roster; mixing in the public list makes the editor
	// advertise models that this channel cannot serve.
	return uniqueURLs(urls...)
}

func parseUpstreamModels(resp *http.Response) ([]string, string, error) {
	items, message, err := parseUpstreamModelItems(resp)
	if err != nil {
		return nil, message, err
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

func parseUpstreamModelItems(resp *http.Response) ([]upstreamModel, string, error) {
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
	return items, "", nil
}

func requestUpstreamModels(url, key string) ([]string, string, error) {
	headers := http.Header{}
	if key != "" {
		headers.Set("Authorization", "Bearer "+key)
	}
	return requestUpstreamModelsWithHeaders(url, headers)
}

func requestUpstreamModelsWithHeaders(url string, headers http.Header) ([]string, string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "application/json")
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
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

func refreshOpenRouterModelCatalog(baseURL, key string) ([]upstreamModel, string, error) {
	var lastErr error
	for _, url := range buildOpenRouterModelURLs(baseURL) {
		headers := http.Header{}
		if key != "" {
			headers.Set("Authorization", "Bearer "+key)
		}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header = headers
		httpClient := client.ImpatientHTTPClient
		if httpClient == nil {
			httpClient = &http.Client{Timeout: 5 * time.Second}
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		items, _, err := parseUpstreamModelItems(resp)
		if err == nil && len(items) > 0 {
			return items, url, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", fmt.Errorf("OpenRouter 未返回模型目录")
}

func openRouterCatalogItems(models []upstreamModel) []modelCatalogItem {
	items := make([]modelCatalogItem, 0, len(models))
	for _, upstream := range models {
		id := upstream.Id
		if id == "" {
			id = upstream.Model
		}
		if id == "" {
			id = upstream.Slug
		}
		if id == "" {
			continue
		}
		item := modelCatalogItem{Id: id, Name: upstream.Name, Description: upstream.Description,
			ContextLength: upstream.ContextLength, InputModalities: upstream.Architecture.InputModalities,
			OutputModalities: upstream.Architecture.OutputModalities, SupportedParameters: upstream.SupportedParameters}
		if value, err := strconv.ParseFloat(upstream.Pricing.Prompt, 64); err == nil {
			price := value * 1_000_000
			item.InputPricePerMillion = &price
		}
		if value, err := strconv.ParseFloat(upstream.Pricing.Completion, 64); err == nil {
			price := value * 1_000_000
			item.OutputPricePerMillion = &price
		}
		items = append(items, item)
	}
	return items
}

func persistOpenRouterCatalog(items []modelCatalogItem, sourceURL string) error {
	records := make([]model.ModelCatalog, 0, len(items))
	for _, item := range items {
		records = append(records, model.ModelCatalog{
			Provider: "openrouter", ModelName: item.Id, DisplayName: item.Name,
			Description: item.Description, ContextLength: item.ContextLength,
			InputModalities:      marshalCatalogField(item.InputModalities),
			OutputModalities:     marshalCatalogField(item.OutputModalities),
			SupportedParameters:  marshalCatalogField(item.SupportedParameters),
			InputPricePerMillion: item.InputPricePerMillion, OutputPricePerMillion: item.OutputPricePerMillion,
			SourceURL: sourceURL,
		})
	}
	return model.UpsertModelCatalog(records)
}

func marshalCatalogField(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
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
	headers := http.Header{
		"Authorization": []string{"Bearer " + cred.AccessToken},
		"originator":    []string{"codex_cli_rs"},
		"OpenAI-Beta":   []string{"responses=experimental"},
	}
	if cred.AccountID != "" {
		headers.Set("Chatgpt-Account-Id", cred.AccountID)
	}
	for _, url := range uniqueURLs(strings.TrimRight(baseURL, "/")+"/models", strings.TrimRight(baseURL, "/")+"/v1/models") {
		models, _, requestErr := requestUpstreamModelsWithHeaders(url, headers)
		if requestErr == nil && len(models) > 0 {
			return models, url, nil
		}
	}

	// The private OAuth endpoint normally has no enumerable /models resource.
	// Returning the same catalogue used by the adaptor makes the UI refresh
	// action useful while keeping manual model entry available for new rollouts.
	if models := openaicodex.KnownCodexModels(); len(models) > 0 {
		return models, codexBuiltinModelListSource, nil
	}
	return nil, "", fmt.Errorf("%s", codexModelListUnavailableMessage)
}

// modelMappingKeys returns the channel-side model names of a model mapping.
// These are gateway-owned aliases that never appear in the upstream /models
// catalogue, so a catalogue cross-check must treat them as valid.
func modelMappingKeys(mapping map[string]string) []string {
	keys := make([]string, 0, len(mapping))
	for key := range mapping {
		if key = strings.TrimSpace(key); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func parseModelMappingKeys(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return nil
	}
	mapping := make(map[string]string)
	if err := json.Unmarshal([]byte(raw), &mapping); err != nil {
		return nil
	}
	return modelMappingKeys(mapping)
}

func ValidateChannelModels(c *gin.Context) {
	req := validateChannelModelsRequest{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}

	aliasModels := parseModelMappingKeys(req.ModelMapping)
	if req.Id > 0 {
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
		aliasModels = append(aliasModels, modelMappingKeys(channel.GetModelMapping())...)
	}

	models := normalizeModelNames(strings.Split(req.Models, ","))
	if len(models) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "请先填写至少一个模型名"})
		return
	}
	if req.Type == 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "请先选择渠道类型"})
		return
	}

	baseURL := resolveChannelBaseURL(req.Type, req.BaseURL)
	var listed []string
	var source string
	var err error
	verificationEnabled := true
	switch req.Type {
	case channeltype.OpenRouter:
		listed, source, err = getOpenRouterModelList(baseURL, req.Key, true)
	case channeltype.OpenAICodexOAuth:
		// Codex OAuth does not expose a reliable model catalogue. Its bundled
		// list is useful as a candidate set, but cannot prove account access.
		listed = openaicodex.KnownCodexModels()
		source = codexBuiltinModelListSource
		verificationEnabled = false
	default:
		for _, url := range uniqueURLs(baseURL+"/models", baseURL+"/v1/models") {
			listed, _, err = requestUpstreamModels(url, req.Key)
			if err == nil && len(listed) > 0 {
				source = url
				break
			}
		}
		if len(listed) == 0 {
			// A missing or unsupported catalogue is not proof that the model is
			// invalid. Keep manual entry possible and report it as unverified.
			err = nil
		}
	}

	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if len(listed) == 0 || !verificationEnabled {
		// A provider that cannot enumerate models must keep manual model entry
		// possible. Report those models as unverified instead of silently
		// claiming that they are available.
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "上游未提供模型目录，模型暂无法自动验证",
			"data": gin.H{
				"valid_models":         []string{},
				"invalid_models":       []string{},
				"unverified_models":    models,
				"verification_enabled": false,
				"source_url":           source,
			},
		})
		return
	}

	listedSet := make(map[string]struct{}, len(listed)+len(aliasModels))
	for _, name := range listed {
		listedSet[name] = struct{}{}
	}
	// Model-mapping keys are gateway-side aliases, not upstream model IDs.
	for _, name := range aliasModels {
		listedSet[name] = struct{}{}
	}
	valid := make([]string, 0, len(models))
	invalid := make([]string, 0)
	for _, name := range models {
		if _, ok := listedSet[name]; ok {
			valid = append(valid, name)
		} else {
			invalid = append(invalid, name)
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "模型验证完成",
		"data": gin.H{
			"valid_models":         valid,
			"invalid_models":       invalid,
			"unverified_models":    []string{},
			"verification_enabled": true,
			"source_url":           source,
		},
	})
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
	// used_quota 在 API 边界统一换算为「元」（见 quota_dto.go）。
	// Balance 保持上游账户原币种（USD），不做换算。
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    toChannelDTOs(channels),
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
	// used_quota 在 API 边界统一换算为「元」（见 quota_dto.go）。
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    toChannelDTOs(channels),
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
	// used_quota 在 API 边界统一换算为「元」（见 quota_dto.go）。
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    toChannelDTO(channel),
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
	var catalog []modelCatalogItem
	var sourceURL string
	var err error
	switch req.Type {
	case channeltype.OpenRouter:
		var upstream []upstreamModel
		upstream, sourceURL, err = refreshOpenRouterModelCatalog(baseURL, req.Key)
		if err == nil {
			catalog = openRouterCatalogItems(upstream)
			if persistErr := persistOpenRouterCatalog(catalog, sourceURL); persistErr != nil {
				logger.SysError("persist OpenRouter model catalog: " + persistErr.Error())
			}
			models = make([]string, 0, len(catalog))
			for _, item := range catalog {
				models = append(models, item.Id)
			}
		}
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
			"catalog":    catalog,
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
	if channel.Type == channeltype.Proxy {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Proxy 渠道类型已移除",
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
	if channel.Type == channeltype.Proxy {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "Proxy 渠道类型已移除",
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
	// used_quota 在 API 边界统一换算为「元」（见 quota_dto.go）。
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    toChannelDTO(&channel),
	})
	return
}
