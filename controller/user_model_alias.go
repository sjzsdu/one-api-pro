// user_model_alias.go 当前用户的模型别名管理接口
// Exposes account-scoped alias settings and available model metadata.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package controller

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay/registry"
)

// aliasTargetProviders derives model-name → provider labels from enabled channels.
// Only provider identifiers are exposed; channel name/id/topology is never returned to account users.
func aliasTargetProviders() map[string][]string {
	result := map[string][]string{}
	var channels []*model.Channel
	if err := model.DB.Where("status = ?", model.ChannelStatusEnabled).Find(&channels).Error; err != nil {
		return result
	}
	add := func(modelName, provider string) {
		if modelName == "" || provider == "" {
			return
		}
		for _, existing := range result[modelName] {
			if existing == provider {
				return
			}
		}
		result[modelName] = append(result[modelName], provider)
	}
	for _, channel := range channels {
		provider := registry.IDByLegacyType(channel.Type)
		if provider == "" {
			continue
		}
		mapping := channel.GetModelMapping()
		for _, raw := range strings.Split(channel.Models, ",") {
			name := strings.TrimSpace(raw)
			if name == "" || name == "*" || name == "auto" {
				continue
			}
			add(name, provider)
			if actual := strings.TrimSpace(mapping[name]); actual != "" && actual != name {
				add(actual, provider)
			}
		}
	}
	return result
}

// enrichAliasTargetMetadata fills provider labels for alias candidates from live channels.
// Catalog rows keep their richer metadata; channels only supply the missing provider label.
func enrichAliasTargetMetadata(names []string, metadata []model.ModelCatalog) []model.ModelCatalog {
	providers := aliasTargetProviders()
	seen := map[string]bool{}
	for _, item := range metadata {
		seen[item.ModelName+"|"+item.Provider] = true
	}
	result := append([]model.ModelCatalog{}, metadata...)
	for _, name := range names {
		for _, provider := range providers[name] {
			key := name + "|" + provider
			if seen[key] {
				continue
			}
			seen[key] = true
			result = append(result, model.ModelCatalog{ModelName: name, Provider: provider})
		}
	}
	return result
}

// GetUserModelAliases lists the authenticated user's saved mappings.
// 版本: v0.0.28
// 日期: 2026-11-22
func GetUserModelAliases(c *gin.Context) {
	items, err := model.GetUserModelAliases(c.GetInt(ctxkey.Id))
	if err != nil {
		c.JSON(200, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "message": "", "data": items})
}

// UpdateUserModelAliases replaces only the authenticated user's mappings.
// 版本: v0.0.28
// 日期: 2026-11-22
func UpdateUserModelAliases(c *gin.Context) {
	var req struct {
		Aliases []model.UserModelAlias `json:"aliases"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Aliases == nil {
		c.JSON(200, gin.H{"success": false, "message": "无效的模型别名配置"})
		return
	}
	if err := model.SaveUserModelAliases(c.Request.Context(), c.GetInt(ctxkey.Id), req.Aliases); err != nil {
		c.JSON(200, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "message": "", "data": req.Aliases})
}

// GetUserAliasTargets returns concrete group models with optional discovery capabilities.
// 版本: v0.0.28
// 日期: 2026-11-22
func GetUserAliasTargets(c *gin.Context) {
	names, metadata, err := model.GetUserAliasTargets(c.Request.Context(), c.GetInt(ctxkey.Id))
	if err != nil {
		c.JSON(200, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "message": "", "data": gin.H{"models": names, "metadata": enrichAliasTargetMetadata(names, metadata)}})
}

// GetGlobalModelAliases exposes explicitly published aliases to root administrators.
// 版本: v0.0.28
// 日期: 2026-11-22
func GetGlobalModelAliases(c *gin.Context) {
	items, err := model.GetUserModelAliases(0)
	if err != nil {
		c.JSON(200, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "data": items})
}

// UpdateGlobalModelAliases replaces the root-managed global namespace.
// 版本: v0.0.28
// 日期: 2026-11-22
func UpdateGlobalModelAliases(c *gin.Context) {
	var req struct {
		Aliases []model.UserModelAlias `json:"aliases"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Aliases == nil {
		c.JSON(200, gin.H{"success": false, "message": "无效的模型池配置"})
		return
	}
	if err := model.SaveUserModelAliases(c.Request.Context(), 0, req.Aliases); err != nil {
		c.JSON(200, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "data": req.Aliases})
}

// GetGlobalAliasTargets exposes enabled models across groups for root configuration.
// 版本: v0.0.28
// 日期: 2026-11-22
func GetGlobalAliasTargets(c *gin.Context) {
	names, metadata, err := model.GetGlobalAliasTargets(c.Request.Context())
	if err != nil {
		c.JSON(200, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "data": gin.H{"models": names, "metadata": enrichAliasTargetMetadata(names, metadata)}})
}

// GetEffectiveModelAliases returns only caller-visible candidates with their source.
// 版本: v0.0.28
// 日期: 2026-11-22
func GetEffectiveModelAliases(c *gin.Context) {
	items, available, err := model.GetUserModelAliasesWithGroupModels(c.Request.Context(), c.GetInt(ctxkey.Id))
	if err != nil {
		c.JSON(200, gin.H{"success": false, "message": err.Error()})
		return
	}
	result := []model.UserModelAlias{}
	for _, item := range items {
		if available[item.Alias] {
			continue
		}
		targets := []model.UserModelAliasTarget{}
		for _, target := range item.Targets {
			if available[target.Model] {
				targets = append(targets, target)
			}
		}
		if len(targets) > 0 {
			item.Targets = targets
			item.TargetModel = targets[0].Model
			result = append(result, item)
		}
	}
	c.JSON(200, gin.H{"success": true, "data": result})
}
