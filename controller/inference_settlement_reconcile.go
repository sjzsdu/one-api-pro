// inference_settlement_reconcile.go 普通推理的安全查询与管理员对账
// Exposes private-token status tracking and audited ordinary-request settlement operations.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/model"
)

// GetInference returns an ordinary request only to its creating account and exact token.
// 版本: v0.0.28
// 日期: 2026-11-23
func GetInference(c *gin.Context) {
	var s model.InferenceSettlement
	if err := model.DB.WithContext(c.Request.Context()).Where(map[string]any{"key": c.Param("id"), "user_id": c.GetInt(ctxkey.Id), "token_id": c.GetInt(ctxkey.TokenId)}).First(&s).Error; err != nil || !model.IsOrdinaryInference(s) {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "inference not found"}})
		return
	}
	result := gin.H{"id": s.Key, "model": s.ModelName, "status": s.Status, "reserved_quota": s.ReservedQuota, "total_quota": s.TotalQuota, "pending_reason": s.PendingReason, "created_at": s.CreatedAt, "updated_at": s.UpdatedAt}
	if s.Status == "settled" && json.Valid([]byte(s.ResultJSON)) {
		result["result"] = json.RawMessage(s.ResultJSON)
	}
	c.JSON(http.StatusOK, result)
}

// InspectInferenceSettlement exposes ordinary settlement observations and resolution audit to administrators.
// 版本: v0.0.28
// 日期: 2026-11-23
func InspectInferenceSettlement(c *gin.Context) {
	if c.GetInt("role") < model.RoleAdminUser {
		c.JSON(200, gin.H{"success": false, "message": "administrator authorization required"})
		return
	}
	var s model.InferenceSettlement
	if err := model.DB.WithContext(c.Request.Context()).Where(map[string]any{"key": c.Param("id")}).First(&s).Error; err != nil || !model.IsOrdinaryInference(s) {
		c.JSON(200, gin.H{"success": false, "message": "ordinary inference not found"})
		return
	}
	var audit model.InferenceReconciliation
	model.DB.WithContext(c.Request.Context()).First(&audit, "settlement_key = ?", s.Key)
	var usage, price any
	_ = json.Unmarshal([]byte(s.UsageJSON), &usage)
	_ = json.Unmarshal([]byte(s.PriceSnapshot), &price)
	c.JSON(200, gin.H{"success": true, "data": gin.H{"settlement": s, "usage": usage, "price_snapshot": price, "resolution": audit}})
}

// ReconcileInferenceSettlement requires an explicit, immutable, administrator-verified accounting decision.
// 版本: v0.0.28
// 日期: 2026-11-23
func ReconcileInferenceSettlement(c *gin.Context) {
	if c.GetInt("role") < model.RoleAdminUser {
		c.JSON(200, gin.H{"success": false, "message": "administrator authorization required"})
		return
	}
	var input struct {
		Action            string          `json:"action"`
		TotalQuota        *int64          `json:"total_quota"`
		ConfirmedNoCharge bool            `json:"confirmed_no_charge"`
		Note              string          `json:"note"`
		Usage             json.RawMessage `json:"usage"`
	}
	if c.ShouldBindJSON(&input) != nil || input.TotalQuota == nil {
		c.JSON(200, gin.H{"success": false, "message": "action, total_quota and evidence note are required"})
		return
	}
	if err := model.ReconcileInference(c.Request.Context(), c.Param("id"), c.GetInt(ctxkey.Id), input.Action, *input.TotalQuota, input.ConfirmedNoCharge, input.Note, string(input.Usage)); err != nil {
		c.JSON(200, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "message": "inference accounting reconciled"})
}
