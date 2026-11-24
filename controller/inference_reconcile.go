// inference_reconcile.go 未知任务的显式管理员对账
// Attaches externally verified tasks or refunds only after explicit no-charge confirmation.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/model"
	schema "github.com/modelbus/one-api-pro/relay/schema"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

// ReconcileMediaJob never generates again; administrative decisions are durable and auditable.
// 版本: v0.0.28
// 日期: 2026-11-23
func ReconcileMediaJob(c *gin.Context) {
	var r struct {
		Action            string               `json:"action"`
		ProviderTaskID    string               `json:"provider_task_id"`
		ConfirmedNoCharge bool                 `json:"confirmed_no_charge"`
		Usage             []schema.UsageMetric `json:"usage"`
		Note              string               `json:"note"`
	}
	if err := common.UnmarshalBodyReusable(c, &r); err != nil || strings.TrimSpace(r.Note) == "" || len(r.Note) > 2000 {
		c.JSON(200, gin.H{"success": false, "message": "action and external evidence note are required"})
		return
	}
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		var j model.InferenceJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&j, "id = ?", c.Param("id")).Error; err != nil {
			return err
		}
		if j.SettlementStatus == "settled" || j.SettlementStatus == "refunded" {
			return errors.New("job already finalized")
		}
		if j.LeaseUntil > time.Now().Unix() {
			return errors.New("job currently leased")
		}
		updates := map[string]any{"next_poll_at": time.Now().Unix(), "updated_at": time.Now().Unix(), "error_code": ""}
		switch r.Action {
		case "attach_task":
			if j.Status != "unknown" || r.ProviderTaskID == "" || len(r.ProviderTaskID) > 191 {
				return errors.New("unknown job and verified provider_task_id required")
			}
			updates["upstream_task_id"] = r.ProviderTaskID
			updates["status"] = "submitted"
			updates["settlement_status"] = "pending"
		case "confirm_no_charge":
			if !r.ConfirmedNoCharge || j.Status != "unknown" {
				return errors.New("unknown outcome requires explicit no-charge confirmation")
			}
			updates["status"] = "failed"
			updates["settlement_status"] = "pending"
			updates["asset_status"] = "unavailable"
		case "record_usage":
			if j.AssetStatus != "ready" || len(r.Usage) == 0 {
				return errors.New("delivered asset and verified usage required")
			}
			for _, m := range r.Usage {
				if m.Quantity < 0 || m.Scale < 0 || m.Unit != "second" || m.Metric != "video.generated_duration" {
					return errors.New("invalid video usage metric")
				}
			}
			usage, _ := json.Marshal(r.Usage)
			updates["usage_json"] = string(usage)
			updates["status"] = "succeeded"
			updates["settlement_status"] = "pending"
		default:
			return errors.New("unsupported reconciliation action")
		}
		if err := tx.Create(&model.InferenceJobReconciliation{JobID: j.ID, AdminID: c.GetInt(ctxkey.Id), Action: r.Action, Note: r.Note, CreatedAt: time.Now().Unix()}).Error; err != nil {
			return err
		}
		return tx.Model(&j).Updates(updates).Error
	})
	if err != nil {
		c.JSON(200, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "message": "reconciliation queued; no generation was submitted"})
}
