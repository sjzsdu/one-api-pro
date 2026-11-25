// provider.go 供应商（渠道商）统计与目录 HTTP handler
// Provider statistics and catalogue HTTP handlers.
// 版本: v0.0.28
// 日期: 2026-11-25
// 作者: MCode
//
// 路由由 router/api.go 在 /api/admin/providers 与 /api/admin/free-models 下以 AdminAuth 注册。
//
// 本层负责把 model 层的「渠道类型」聚合翻译成 provider 概念：
// provider ID 来自 relay/registry，而 model 层不能依赖 relay/registry（import 环），
// 因此映射必须留在这里。
package admin

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay/registry"
)

// providerIDUnknown 用于归类没有注册到 registry 的历史渠道类型。
// providerIDUnknown groups channel types that are not registered in the registry.
const providerIDUnknown = "unknown"

// providerUsageDTO 单个供应商的用量聚合输出：quota 以「元」为单位。
// Per-provider usage aggregate: quota expressed in CNY yuan.
type providerUsageDTO struct {
	ProviderID       string   `json:"provider_id"`
	ProviderName     string   `json:"provider_name"`
	RequestCount     int      `json:"request_count"`
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	CachedTokens     int      `json:"cached_tokens"`
	TotalTokens      int      `json:"total_tokens"`
	Quota            int64    `json:"quota"`
	Amount           float64  `json:"amount"`
	ModelCount       int      `json:"model_count"`
	ChannelCount     int      `json:"channel_count"`
	EnabledChannels  int      `json:"enabled_channels"`
	Share            float64  `json:"share"`
	Models           []string `json:"models"`
	FreeModels       []string `json:"free_models"`
	UnpricedModels   []string `json:"unpriced_models"`
	Title            string   `json:"title"`
	Category         string   `json:"category"`
	Summary          string   `json:"summary"`
	BaseURL          string   `json:"base_url"`
	DefaultBaseURL   string   `json:"default_base_url"`
	SignupURL        string   `json:"signup_url"`
	ConsoleURL       string   `json:"console_url"`
	DocURL           string   `json:"doc_url"`
	FreePolicy       string   `json:"free_policy"`
	Notes            string   `json:"notes"`
	HasProfile       bool     `json:"has_profile"`
}

// providerModelUsageDTO 单个模型在某供应商下的用量聚合。
// Per-model usage aggregate within one provider.
type providerModelUsageDTO struct {
	ModelName        string  `json:"model_name"`
	DisplayName      string  `json:"display_name"`
	RequestCount     int     `json:"request_count"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CachedTokens     int     `json:"cached_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	Quota            int64   `json:"quota"`
	Amount           float64 `json:"amount"`
	IsFree           bool    `json:"is_free"`
	FreeType         string  `json:"free_type"`
	HasPrice         bool    `json:"has_price"`
	InputPrice       float64 `json:"input_price"`
	OutputPrice      float64 `json:"output_price"`
	BillingType      string  `json:"billing_type"`
	ChannelIds       []int   `json:"channel_ids"`
}

// providerTrendPoint 趋势图的一天一个点。
// One point per day on the trend chart.
type providerTrendPoint struct {
	Day              string  `json:"day"`
	RequestCount     int     `json:"request_count"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	Quota            int64   `json:"quota"`
	Amount           float64 `json:"amount"`
}

// providerDetailDTO 供应商详情输出。
// Provider detail response.
type providerDetailDTO struct {
	ProviderID     string                   `json:"provider_id"`
	ProviderName   string                   `json:"provider_name"`
	Title          string                   `json:"title"`
	Category       string                   `json:"category"`
	Summary        string                   `json:"summary"`
	BaseURL        string                   `json:"base_url"`
	DefaultBaseURL string                   `json:"default_base_url"`
	SignupURL      string                   `json:"signup_url"`
	ConsoleURL     string                   `json:"console_url"`
	DocURL         string                   `json:"doc_url"`
	FreePolicy     string                   `json:"free_policy"`
	Notes          string                   `json:"notes"`
	HasProfile     bool                     `json:"has_profile"`
	Channels       []*providerChannelDTO    `json:"channels"`
	Models         []*providerModelUsageDTO `json:"models"`
	FreeModels     []*model.ModelFreeModel  `json:"free_models"`
	Usage          *providerUsageDTO        `json:"usage"`
	Trend          []providerTrendPoint     `json:"trend"`
	Range          string                   `json:"range"`
}

// providerChannelDTO 供应商下的一个渠道。
// One channel under a provider.
type providerChannelDTO struct {
	Id            int      `json:"id"`
	Name          string   `json:"name"`
	Status        int      `json:"status"`
	BaseURL       string   `json:"base_url"`
	Models        []string `json:"models"`
	Group         string   `json:"group"`
	Priority      int64    `json:"priority"`
	UsedQuota     int64    `json:"used_quota"`
	UsedAmount    float64  `json:"used_amount"`
	LastError     string   `json:"last_error"`
	LastErrorTime int64    `json:"last_error_time"`
}

// providerChannelState 汇总一个 provider 下的渠道信息。
// providerChannelState aggregates the channels of one provider.
type providerChannelState struct {
	ChannelIds      []int
	EnabledChannels int
	Models          map[string]bool
	ChannelCount    int
	Channels        []*model.Channel
}

// providerIndex 是统计所需的渠道索引：channel_id → 渠道类型 / provider。
// providerIndex maps channel IDs to their channel type and provider.
type providerIndex struct {
	States              map[string]*providerChannelState
	TypeByChannelID     map[int]int
	ProviderByChannelID map[int]string
}

// buildProviderIndex 建立渠道索引。
// Builds the channel index.
func buildProviderIndex(channels []*model.Channel) *providerIndex {
	index := &providerIndex{
		States:              map[string]*providerChannelState{},
		TypeByChannelID:     map[int]int{},
		ProviderByChannelID: map[int]string{},
	}
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		providerID := registry.IDByLegacyType(channel.Type)
		if providerID == "" {
			providerID = providerIDUnknown
		}
		index.TypeByChannelID[channel.Id] = channel.Type
		index.ProviderByChannelID[channel.Id] = providerID

		state, ok := index.States[providerID]
		if !ok {
			state = &providerChannelState{Models: map[string]bool{}}
			index.States[providerID] = state
		}
		state.ChannelCount++
		state.ChannelIds = append(state.ChannelIds, channel.Id)
		state.Channels = append(state.Channels, channel)
		if channel.Status == model.ChannelStatusEnabled {
			state.EnabledChannels++
		}
		for _, name := range strings.Split(channel.Models, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				state.Models[name] = true
			}
		}
	}
	return index
}

// providerDisplayName 返回供应商展示名：策展标题优先，其次 registry 名称。
// Returns the provider display name: curated title first, then the registry name.
func providerDisplayName(providerID string, profile *model.ProviderProfile) string {
	if profile != nil && profile.Title != "" {
		return profile.Title
	}
	if meta, ok := registry.GetChannelMeta(providerID); ok {
		return meta.Name
	}
	if providerID == providerIDUnknown {
		return "未知渠道商"
	}
	return providerID
}

// buildProviderUsage 把 model 层的聚合行按 provider 汇总。
// Groups model-layer aggregate rows by provider.
func buildProviderUsage(rows []*model.ChannelUsageRow, index *providerIndex,
	freeModels map[string][]*model.ModelFreeModel, prices map[string]*model.ModelPrice) []*providerUsageDTO {
	profiles, _ := model.GetProviderProfiles()
	profileByID := map[string]*model.ProviderProfile{}
	for _, profile := range profiles {
		profileByID[profile.ProviderID] = profile
	}

	usageByProvider := map[string]*providerUsageDTO{}
	modelsByProvider := map[string]map[string]bool{}

	for _, row := range rows {
		if row == nil {
			continue
		}
		providerID := index.ProviderByChannelID[row.ChannelId]
		if providerID == "" {
			providerID = providerIDUnknown
		}
		item, ok := usageByProvider[providerID]
		if !ok {
			item = &providerUsageDTO{
				ProviderID:     providerID,
				Models:         []string{},
				FreeModels:     []string{},
				UnpricedModels: []string{},
			}
			usageByProvider[providerID] = item
			modelsByProvider[providerID] = map[string]bool{}
		}
		item.RequestCount += row.RequestCount
		item.PromptTokens += row.PromptTokens
		item.CompletionTokens += row.CompletionTokens
		item.CachedTokens += row.CachedTokens
		item.Quota += int64(row.Quota)
		modelsByProvider[providerID][row.ModelName] = true
	}

	totalQuota := int64(0)
	for _, item := range usageByProvider {
		totalQuota += item.Quota
	}

	out := make([]*providerUsageDTO, 0, len(usageByProvider))
	for providerID, item := range usageByProvider {
		state := index.States[providerID]
		if state != nil {
			item.ChannelCount = state.ChannelCount
			item.EnabledChannels = state.EnabledChannels
		}
		item.TotalTokens = item.PromptTokens + item.CompletionTokens + item.CachedTokens
		item.Amount = quotaToYuan(item.Quota)
		if totalQuota > 0 {
			item.Share = float64(item.Quota) / float64(totalQuota)
		}
		for name := range modelsByProvider[providerID] {
			item.Models = append(item.Models, name)
			if _, ok := prices[name]; !ok {
				item.UnpricedModels = append(item.UnpricedModels, name)
			}
		}
		sort.Strings(item.Models)
		sort.Strings(item.UnpricedModels)
		item.ModelCount = len(item.Models)
		for _, free := range freeModels[providerID] {
			item.FreeModels = append(item.FreeModels, free.ModelName)
		}
		sort.Strings(item.FreeModels)

		profile := profileByID[providerID]
		if profile != nil {
			item.HasProfile = true
			item.Title = profile.Title
			item.Category = profile.Category
			item.Summary = profile.Summary
			item.SignupURL = profile.SignupURL
			item.ConsoleURL = profile.ConsoleURL
			item.DocURL = profile.DocURL
			item.FreePolicy = profile.FreePolicy
			item.Notes = profile.Notes
		}
		item.ProviderName = providerDisplayName(providerID, profile)
		if meta, ok := registry.GetChannelMeta(providerID); ok {
			item.DefaultBaseURL = meta.DefaultBaseURL
		}
		out = append(out, item)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Quota != out[j].Quota {
			return out[i].Quota > out[j].Quota
		}
		return out[i].ProviderID < out[j].ProviderID
	})
	return out
}

// loadProviderContext 一次性加载统计所需的全部数据。
// Loads every dataset the provider statistics need.
func loadProviderContext() ([]*model.Channel, map[string][]*model.ModelFreeModel, map[string]*model.ModelPrice, error) {
	channels, err := model.GetAllChannels(0, 0, "all")
	if err != nil {
		return nil, nil, nil, err
	}
	freeModels, err := model.GetModelFreeModels("")
	if err != nil {
		return nil, nil, nil, err
	}
	prices, err := model.GetAllModelPrices()
	if err != nil {
		return nil, nil, nil, err
	}
	freeByProvider := map[string][]*model.ModelFreeModel{}
	for _, free := range freeModels {
		freeByProvider[free.ProviderID] = append(freeByProvider[free.ProviderID], free)
	}
	priceMap := map[string]*model.ModelPrice{}
	for _, price := range prices {
		priceMap[price.ModelName] = price
	}
	return channels, freeByProvider, priceMap, nil
}

// GetProviders 处理 GET /api/admin/providers。
// Query: range=today|7d|30d|all (default 7d)。
// 返回全部已注册供应商的用量、渠道与免费模型概况。
// GetProviders handles GET /api/admin/providers.
func GetProviders(c *gin.Context) {
	win := model.ParseAdminChartsRange(c.Query("range"))
	channels, freeByProvider, prices, err := loadProviderContext()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	rows, err := model.SumUsageByChannelIdAndModel(0, win.Start, win.End)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	index := buildProviderIndex(channels)
	usage := buildProviderUsage(rows, index, freeByProvider, prices)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"range": win.Key,
			"items": usage,
		},
	})
}

// GetProviderDetail 处理 GET /api/admin/providers/:id。
// 返回该供应商的渠道、模型用量、免费模型与趋势。
// GetProviderDetail handles GET /api/admin/providers/:id.
func GetProviderDetail(c *gin.Context) {
	providerID := c.Param("id")
	if providerID == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "provider id 不能为空"})
		return
	}
	win := model.ParseAdminChartsRange(c.Query("range"))
	channels, freeByProvider, prices, err := loadProviderContext()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	rows, err := model.SumUsageByChannelIdAndModel(0, win.Start, win.End)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	daily, err := model.SumUsageByChannelIdAndModelByDay(0, win.Start, win.End)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	profiles, err := model.GetProviderProfiles()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	var profile *model.ProviderProfile
	for _, item := range profiles {
		if item.ProviderID == providerID {
			profile = item
			break
		}
	}

	index := buildProviderIndex(channels)
	usageList := buildProviderUsage(rows, index, freeByProvider, prices)
	var usage *providerUsageDTO
	for _, item := range usageList {
		if item.ProviderID == providerID {
			usage = item
			break
		}
	}
	if usage == nil {
		usage = &providerUsageDTO{ProviderID: providerID, Models: []string{}, FreeModels: []string{}, UnpricedModels: []string{}}
	}

	// 只保留该供应商的模型级与趋势数据。
	providerChannelIds := map[int]bool{}
	for channelID, pid := range index.ProviderByChannelID {
		if pid == providerID {
			providerChannelIds[channelID] = true
		}
	}

	modelUsage := map[string]*providerModelUsageDTO{}
	channelIDsByModel := map[string]map[int]bool{}
	for _, row := range rows {
		if row == nil || !providerChannelIds[row.ChannelId] {
			continue
		}
		item, ok := modelUsage[row.ModelName]
		if !ok {
			item = &providerModelUsageDTO{ModelName: row.ModelName, ChannelIds: []int{}}
			modelUsage[row.ModelName] = item
			channelIDsByModel[row.ModelName] = map[int]bool{}
		}
		item.RequestCount += row.RequestCount
		item.PromptTokens += row.PromptTokens
		item.CompletionTokens += row.CompletionTokens
		item.CachedTokens += row.CachedTokens
		item.Quota += int64(row.Quota)
	}
	// 渠道归属：按渠道实际配置给模型补上 channel id。
	for _, channel := range channels {
		if !providerChannelIds[channel.Id] {
			continue
		}
		for _, name := range strings.Split(channel.Models, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			set, ok := channelIDsByModel[name]
			if !ok {
				continue
			}
			set[channel.Id] = true
		}
	}

	freeByModel := map[string]*model.ModelFreeModel{}
	for _, free := range freeByProvider[providerID] {
		freeByModel[free.ModelName] = free
	}

	modelList := make([]*providerModelUsageDTO, 0, len(modelUsage))
	for name, item := range modelUsage {
		item.TotalTokens = item.PromptTokens + item.CompletionTokens + item.CachedTokens
		item.Amount = quotaToYuan(item.Quota)
		item.DisplayName = providerID + ":" + name
		if price, ok := prices[name]; ok {
			item.HasPrice = true
			item.InputPrice = price.InputPrice
			item.OutputPrice = price.OutputPrice
			item.BillingType = price.BillingType
		}
		if free, ok := freeByModel[name]; ok {
			item.IsFree = true
			item.FreeType = free.FreeType
		}
		if set, ok := channelIDsByModel[name]; ok {
			for id := range set {
				item.ChannelIds = append(item.ChannelIds, id)
			}
			sort.Ints(item.ChannelIds)
		}
		modelList = append(modelList, item)
	}
	sort.Slice(modelList, func(i, j int) bool {
		if modelList[i].Quota != modelList[j].Quota {
			return modelList[i].Quota > modelList[j].Quota
		}
		return modelList[i].ModelName < modelList[j].ModelName
	})

	trendMap := map[string]*providerTrendPoint{}
	var trendDays []string
	for _, row := range daily {
		if row == nil || !providerChannelIds[row.ChannelId] {
			continue
		}
		point, ok := trendMap[row.Day]
		if !ok {
			point = &providerTrendPoint{Day: row.Day}
			trendMap[row.Day] = point
			trendDays = append(trendDays, row.Day)
		}
		point.RequestCount += row.RequestCount
		point.PromptTokens += row.PromptTokens
		point.CompletionTokens += row.CompletionTokens
		point.Quota += int64(row.Quota)
	}
	sort.Strings(trendDays)
	trend := make([]providerTrendPoint, 0, len(trendDays))
	for _, day := range trendDays {
		point := trendMap[day]
		point.TotalTokens = point.PromptTokens + point.CompletionTokens
		point.Amount = quotaToYuan(point.Quota)
		trend = append(trend, *point)
	}

	channelDTOs := make([]*providerChannelDTO, 0)
	for _, channel := range channels {
		if !providerChannelIds[channel.Id] {
			continue
		}
		models := []string{}
		for _, name := range strings.Split(channel.Models, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				models = append(models, name)
			}
		}
		channelDTOs = append(channelDTOs, &providerChannelDTO{
			Id:            channel.Id,
			Name:          channel.Name,
			Status:        channel.Status,
			BaseURL:       channel.GetBaseURL(),
			Models:        models,
			Group:         channel.Group,
			Priority:      channel.GetPriority(),
			UsedQuota:     channel.UsedQuota,
			UsedAmount:    quotaToYuan(channel.UsedQuota),
			LastError:     channel.LastError,
			LastErrorTime: channel.LastErrorTime,
		})
	}

	detail := &providerDetailDTO{
		ProviderID:   providerID,
		ProviderName: providerDisplayName(providerID, profile),
		Channels:     channelDTOs,
		Models:       modelList,
		FreeModels:   freeByProvider[providerID],
		Usage:        usage,
		Trend:        trend,
		Range:        win.Key,
	}
	if profile != nil {
		detail.HasProfile = true
		detail.Title = profile.Title
		detail.Category = profile.Category
		detail.Summary = profile.Summary
		detail.SignupURL = profile.SignupURL
		detail.ConsoleURL = profile.ConsoleURL
		detail.DocURL = profile.DocURL
		detail.FreePolicy = profile.FreePolicy
		detail.Notes = profile.Notes
	}
	if meta, ok := registry.GetChannelMeta(providerID); ok {
		detail.DefaultBaseURL = meta.DefaultBaseURL
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    detail,
	})
}

// SaveProviderProfile 处理 PUT /api/admin/providers/:id/profile。
// 保存供应商策展信息。
// SaveProviderProfile handles PUT /api/admin/providers/:id/profile.
func SaveProviderProfile(c *gin.Context) {
	providerID := c.Param("id")
	if providerID == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "provider id 不能为空"})
		return
	}
	var payload model.ProviderProfile
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	payload.ProviderID = providerID
	payload.Id = 0
	if err := model.UpsertProviderProfile(&payload); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": payload})
}

// ListFreeModels 处理 GET /api/admin/free-models。
// Query: provider_id 可选，按供应商过滤。
// ListFreeModels handles GET /api/admin/free-models.
func ListFreeModels(c *gin.Context) {
	items, err := model.GetModelFreeModels(c.Query("provider_id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if items == nil {
		items = []*model.ModelFreeModel{}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": items})
}

// SaveFreeModel 处理 PUT /api/admin/free-models。
// 新增或更新一条免费模型记录。
// SaveFreeModel handles PUT /api/admin/free-models.
func SaveFreeModel(c *gin.Context) {
	var payload model.ModelFreeModel
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	if payload.ProviderID == "" || payload.ModelName == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": model.ErrFreeModelInvalid.Error()})
		return
	}
	if payload.FreeType == "" {
		payload.FreeType = model.FreeTypePermanent
	}
	payload.Id = 0
	if err := model.UpsertModelFreeModel(&payload); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": payload})
}

// DeleteFreeModel 处理 DELETE /api/admin/free-models。
// Query: provider_id 与 model_name 必填。
// DeleteFreeModel handles DELETE /api/admin/free-models.
func DeleteFreeModel(c *gin.Context) {
	providerID := c.Query("provider_id")
	modelName := c.Query("model_name")
	if providerID == "" || modelName == "" {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "provider_id 与 model_name 不能为空"})
		return
	}
	if err := model.DeleteModelFreeModel(providerID, modelName); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// DetectFreeModels 处理 POST /api/admin/free-models/detect。
// 对给定模型名按后缀识别疑似免费模型，供管理员确认后入清单。
// DetectFreeModels handles POST /api/admin/free-models/detect.
func DetectFreeModels(c *gin.Context) {
	var payload struct {
		ModelNames []string `json:"model_names"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	type detected struct {
		ModelName string `json:"model_name"`
		Suffix    string `json:"suffix"`
	}
	items := make([]detected, 0, len(payload.ModelNames))
	for _, name := range payload.ModelNames {
		if ok, suffix := model.DetectFreeModelSuffix(name); ok {
			items = append(items, detected{ModelName: name, Suffix: suffix})
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": items})
}
