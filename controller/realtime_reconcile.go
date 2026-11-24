// realtime_reconcile.go 实时会话管理员对账接口
// Exposes audited explicit settlement for closed, uncertain realtime sessions.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/model"
)

// InspectRealtimeSession exposes price snapshots and metering records to authenticated administrators.
// 版本: v0.0.28
// 日期: 2026-11-23
func InspectRealtimeSession(c *gin.Context) {
	if c.GetInt("role") < model.RoleAdminUser {
		c.JSON(200, gin.H{"success": false, "message": "administrator authorization required"})
		return
	}
	var s model.InferenceSession
	if err := model.DB.WithContext(c.Request.Context()).First(&s, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(200, gin.H{"success": false, "message": "session not found"})
		return
	}
	var turns []model.InferenceSessionTurn
	if err := model.DB.WithContext(c.Request.Context()).Where("session_id = ?", s.ID).Order("created_at, response_id").Find(&turns).Error; err != nil {
		c.JSON(200, gin.H{"success": false, "message": "cannot read session usage"})
		return
	}
	var ledger model.InferenceSettlement
	if err := model.DB.WithContext(c.Request.Context()).Where(map[string]any{"key": s.ID}).First(&ledger).Error; err != nil {
		c.JSON(200, gin.H{"success": false, "message": "cannot read session reservation"})
		return
	}
	records := make([]gin.H, 0, len(turns))
	for _, turn := range turns {
		var usage any
		_ = json.Unmarshal([]byte(turn.UsageJSON), &usage)
		records = append(records, gin.H{"response_id": turn.ResponseID, "input_tokens": turn.InputTokens, "output_tokens": turn.OutputTokens, "quota": turn.Quota, "usage": usage, "created_at": turn.CreatedAt})
	}
	var price any
	_ = json.Unmarshal([]byte(s.PriceSnapshot), &price)
	c.JSON(200, gin.H{"success": true, "data": gin.H{"session": s, "price_snapshot": price, "turns": records, "settlement": ledger, "resolution": gin.H{"action": s.ResolutionAction, "total_quota": s.ResolutionQuota, "actor_id": s.ResolutionActorID, "note": s.ResolutionNote}}})
}

// ReconcileRealtimeSession requires AdminAuth and verified upstream usage before changing held funds.
// 版本: v0.0.28
// 日期: 2026-11-23
func ReconcileRealtimeSession(c *gin.Context) {
	if c.GetInt("role") < model.RoleAdminUser {
		c.JSON(200, gin.H{"success": false, "message": "administrator authorization required"})
		return
	}
	var input struct {
		Action     string `json:"action"`
		TotalQuota *int64 `json:"total_quota"`
		Note       string `json:"note"`
	}
	if c.ShouldBindJSON(&input) != nil || input.TotalQuota == nil {
		c.JSON(200, gin.H{"success": false, "message": "action, total_quota and verified-usage note are required"})
		return
	}
	if err := model.ReconcileInferenceSession(c.Request.Context(), c.Param("id"), c.GetInt("id"), input.Action, *input.TotalQuota, input.Note); err != nil {
		c.JSON(200, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "message": "session accounting reconciled"})
}
