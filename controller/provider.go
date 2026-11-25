// provider.go 用户侧供应商（渠道商）用量统计
// User-facing provider usage statistics.
// 版本: v0.0.28
// 日期: 2026-11-25
// 作者: MCode
//
// 用户侧原本靠「模型名猜供应商」，导致 cosyvoice-v3-flash、wan2.7-image
// 这类模型归属为空。这里改为按真实渠道归属统计：
// logs.channel_id → channels.type → relay/registry 的 provider ID。
package controller

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay/registry"
)

// providerIDUnknown 用于归类没有注册到 registry 的历史渠道类型。
// providerIDUnknown groups channel types that are not registered in the registry.
const providerIDUnknown = "unknown"

// userProviderUsageDTO 单个供应商的用户侧用量聚合：quota 以「元」为单位。
// Per-provider usage aggregate for one user: quota expressed in CNY yuan.
type userProviderUsageDTO struct {
	ProviderID       string   `json:"provider_id"`
	ProviderName     string   `json:"provider_name"`
	RequestCount     int      `json:"request_count"`
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	TotalTokens      int      `json:"total_tokens"`
	Quota            int64    `json:"quota"`
	Amount           float64  `json:"amount"`
	ModelCount       int      `json:"model_count"`
	Models           []string `json:"models"`
}

// GetUserProviderUsage 处理 GET /api/user/provider-usage。
// 返回当前用户按供应商拆分的用量：请求数、token 与费用。
// Query: start / end 为 Unix 秒时间戳，缺省为近 7 天。
// GetUserProviderUsage handles GET /api/user/provider-usage.
func GetUserProviderUsage(c *gin.Context) {
	id := c.GetInt(ctxkey.Id)
	now := time.Now()
	startOfDay := now.Truncate(24*time.Hour).AddDate(0, 0, -6).Unix()
	endOfDay := now.Truncate(24 * time.Hour).Add(24*time.Hour - time.Second).Unix()

	startParam, errStart := strconv.ParseInt(c.Query("start"), 10, 64)
	endParam, errEnd := strconv.ParseInt(c.Query("end"), 10, 64)
	if errStart == nil && errEnd == nil && startParam > 0 && endParam > startParam {
		startOfDay = startParam
		endOfDay = endParam
	}

	rows, err := model.SumUsageByChannelIdAndModel(id, startOfDay, endOfDay)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "无法获取统计信息",
			"data":    nil,
		})
		return
	}

	providerByChannel, err := providerIDByChannel()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
			"data":    nil,
		})
		return
	}

	type aggregate struct {
		requestCount int
		prompt       int
		completion   int
		quota        int64
		models       map[string]bool
	}
	byProvider := map[string]*aggregate{}
	for _, row := range rows {
		if row == nil {
			continue
		}
		providerID := providerByChannel[row.ChannelId]
		if providerID == "" {
			providerID = providerIDUnknown
		}
		item, ok := byProvider[providerID]
		if !ok {
			item = &aggregate{models: map[string]bool{}}
			byProvider[providerID] = item
		}
		item.requestCount += row.RequestCount
		item.prompt += row.PromptTokens
		item.completion += row.CompletionTokens
		item.quota += int64(row.Quota)
		item.models[row.ModelName] = true
	}

	out := make([]*userProviderUsageDTO, 0, len(byProvider))
	for providerID, item := range byProvider {
		models := make([]string, 0, len(item.models))
		for name := range item.models {
			models = append(models, name)
		}
		sort.Strings(models)
		out = append(out, &userProviderUsageDTO{
			ProviderID:       providerID,
			ProviderName:     providerDisplayNameForUser(providerID),
			RequestCount:     item.requestCount,
			PromptTokens:     item.prompt,
			CompletionTokens: item.completion,
			TotalTokens:      item.prompt + item.completion,
			Quota:            item.quota,
			Amount:           quotaToYuan(item.quota),
			ModelCount:       len(models),
			Models:           models,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Quota != out[j].Quota {
			return out[i].Quota > out[j].Quota
		}
		return out[i].ProviderID < out[j].ProviderID
	})

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    out,
	})
}

// providerIDByChannel 返回 channel_id → provider ID 的映射。
// Returns the channel ID to provider ID mapping.
func providerIDByChannel() (map[int]string, error) {
	channels, err := model.GetAllChannels(0, 0, "all")
	if err != nil {
		return nil, err
	}
	out := make(map[int]string, len(channels))
	for _, channel := range channels {
		if channel == nil {
			continue
		}
		providerID := registry.IDByLegacyType(channel.Type)
		if providerID == "" {
			providerID = providerIDUnknown
		}
		out[channel.Id] = providerID
	}
	return out, nil
}

// providerDisplayNameForUser 返回供应商展示名（registry 名称）。
// Returns the provider display name from the registry.
func providerDisplayNameForUser(providerID string) string {
	if meta, ok := registry.GetChannelMeta(providerID); ok {
		return meta.Name
	}
	if providerID == providerIDUnknown {
		return "未知渠道商"
	}
	return providerID
}
