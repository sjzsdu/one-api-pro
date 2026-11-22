// user_model_alias.go 当前用户的模型别名管理接口
// Exposes account-scoped alias settings and available model metadata.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/model"
)

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
	c.JSON(200, gin.H{"success": true, "message": "", "data": gin.H{"models": names, "metadata": metadata}})
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
	c.JSON(200, gin.H{"success": true, "data": gin.H{"models": names, "metadata": metadata}})
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
