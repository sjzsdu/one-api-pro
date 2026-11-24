// inference_session_reconcile.go 管理员核实实时用量后的幂等对账
// Audits explicit administrator resolutions and resumes partially completed settlement safely.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package model

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ReconcileInferenceSession resolves uncertain work only after an administrator checks upstream records.
// The first resolution claims the session; interrupted retries must preserve its action, amount and note.
// 版本: v0.0.28
// 日期: 2026-11-23
func ReconcileInferenceSession(ctx context.Context, id string, actorID int, action string, total int64, note string) error {
	note = strings.TrimSpace(note)
	if (action != "settle" && action != "refund") || total < 0 || note == "" || len(note) > 2000 || action == "refund" && total != 0 {
		return errors.New("resolution requires settle/refund, a nonnegative quota and an audit note")
	}
	var s InferenceSession
	finished := false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var actor User
		if err := tx.First(&actor, "id = ? AND role >= ? AND status = ?", actorID, RoleAdminUser, UserStatusEnabled).Error; err != nil {
			return errors.New("administrator authorization required")
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&s, "id = ?", id).Error; err != nil {
			return err
		}
		if s.Status == "closed" || s.Status == "reconciling" {
			if s.ResolutionAction != action || s.ResolutionQuota != total || s.ResolutionNote != note {
				return errors.New("session already claimed by a different reconciliation")
			}
			finished = s.Status == "closed"
			return nil
		}
		if s.Status != "pending_reconciliation" {
			return errors.New("only interrupted pending sessions can be reconciled")
		}
		if action == "refund" && (s.TurnCount > 0 || s.TotalQuota > 0) {
			return errors.New("completed turns require settlement of verified usage, not a full refund")
		}
		result := tx.Model(&InferenceSession{}).Where("id = ? AND status = ?", id, "pending_reconciliation").Updates(map[string]any{"status": "reconciling", "resolution_actor_id": actorID, "resolution_action": action, "resolution_quota": total, "resolution_note": note, "updated_at": time.Now().Unix()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("session reconciliation already claimed")
		}
		s.ResolutionActorID = actorID
		s.ResolutionAction = action
		s.ResolutionQuota = total
		s.ResolutionNote = note
		return nil
	})
	if err != nil || finished {
		return err
	}
	audit, _ := json.Marshal(map[string]any{"resolution": action, "actor_id": s.ResolutionActorID, "verified_quota": total, "note": note})
	var children []InferenceSettlement
	if err = DB.WithContext(ctx).Where("user_id = ? AND token_id = ?", s.UserID, s.TokenID).Where(clause.Like{Column: clause.Column{Name: "key"}, Value: id + ":turn:%"}).Find(&children).Error; err != nil {
		return err
	}
	for _, child := range children {
		if !strings.HasPrefix(child.Key, id+":turn:") || child.Status != "reserved" {
			continue
		}
		if action == "refund" {
			err = RefundInference(ctx, child.Key)
		} else {
			err = SettleInference(ctx, child.Key, 0, string(audit))
		}
		if err != nil {
			return err
		}
	}
	if action == "refund" {
		err = RefundInference(ctx, id)
	} else {
		err = SettleInference(ctx, id, total, string(audit))
	}
	if err != nil {
		return err
	}
	return DB.WithContext(ctx).Model(&InferenceSession{}).Where("id = ? AND status = ?", id, "reconciling").Updates(map[string]any{"status": "closed", "close_reason": "admin_" + action, "total_quota": total, "pending_response": false, "pending_reservation": "", "updated_at": time.Now().Unix()}).Error
}
