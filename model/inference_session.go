// inference_session.go 实时会话预付预算与逐轮用量
// Persists unique response usage and retains uncertain reservations for reconciliation.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package model

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// InferenceSession records a fixed upstream channel and a prepaid Qwen session budget.
// 版本: v0.0.28
// 日期: 2026-11-23
type InferenceSession struct {
	ID                 string `gorm:"primaryKey;size:191" json:"id"`
	UserID             int    `gorm:"index" json:"-"`
	TokenID            int    `json:"-"`
	ChannelID          int    `json:"-"`
	ModelName          string `gorm:"size:255" json:"model"`
	UpstreamModel      string `gorm:"size:255" json:"-"`
	Protocol           string `gorm:"size:40" json:"protocol"`
	Status             string `gorm:"size:32;index" json:"status"`
	CloseReason        string `gorm:"size:64" json:"close_reason,omitempty"`
	PriceSnapshot      string `gorm:"type:text" json:"-"`
	BudgetQuota        int64  `json:"budget_quota"`
	TotalQuota         int64  `json:"total_quota"`
	MaxTurns           int    `json:"max_turns"`
	TurnCount          int    `json:"turn_count"`
	PendingResponse    bool   `json:"pending_response"`
	PendingReservation string `gorm:"size:191" json:"-"`
	ResolutionActorID  int    `json:"-"`
	ResolutionAction   string `gorm:"size:16" json:"-"`
	ResolutionQuota    int64  `json:"-"`
	ResolutionNote     string `gorm:"type:text" json:"-"`
	CreatedAt          int64  `json:"created_at"`
	UpdatedAt          int64  `gorm:"index" json:"updated_at"`
}

// InferenceSessionTurn is one immutable response-id metering record, not a cumulative session delta.
// 版本: v0.0.28
// 日期: 2026-11-23
type InferenceSessionTurn struct {
	SessionID      string `gorm:"primaryKey;size:191" json:"session_id"`
	ResponseID     string `gorm:"primaryKey;size:255" json:"response_id"`
	ReservationKey string `gorm:"size:191" json:"-"`
	InputTokens    int64  `json:"input_tokens"`
	OutputTokens   int64  `json:"output_tokens"`
	Quota          int64  `json:"quota"`
	UsageJSON      string `gorm:"type:text" json:"-"`
	CreatedAt      int64  `json:"created_at"`
}

// RecordInferenceSessionTurn atomically records a unique response without exceeding prepaid limits.
// Replayed ids must contain the same usage; conflicting replays require reconciliation.
// 版本: v0.0.28
// 日期: 2026-11-23
func RecordInferenceSessionTurn(ctx context.Context, id, responseID string, input, output, quota int64, usageJSON string) (bool, error) {
	if responseID == "" || len(responseID) > 255 || input < 0 || output < 0 || quota < 0 {
		return false, errors.New("invalid realtime turn")
	}
	added := false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var s InferenceSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&s, "id = ?", id).Error; err != nil {
			return err
		}
		var old InferenceSessionTurn
		if err := tx.First(&old, "session_id = ? AND response_id = ?", id, responseID).Error; err == nil {
			var a, b any
			if json.Unmarshal([]byte(old.UsageJSON), &a) != nil || json.Unmarshal([]byte(usageJSON), &b) != nil || old.InputTokens != input || old.OutputTokens != output || old.Quota != quota || !reflect.DeepEqual(a, b) {
				return errors.New("conflicting realtime response usage")
			}
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if s.Status != "active" || s.TurnCount >= s.MaxTurns || quota > s.BudgetQuota-s.TotalQuota {
			return errors.New("realtime prepaid budget exhausted")
		}
		now := time.Now().Unix()
		turn := InferenceSessionTurn{SessionID: id, ResponseID: responseID, ReservationKey: s.PendingReservation, InputTokens: input, OutputTokens: output, Quota: quota, UsageJSON: usageJSON, CreatedAt: now}
		if err := tx.Create(&turn).Error; err != nil {
			return err
		}
		result := tx.Model(&InferenceSession{}).Where("id = ? AND status = ? AND total_quota = ? AND turn_count = ?", id, "active", s.TotalQuota, s.TurnCount).Updates(map[string]any{"total_quota": s.TotalQuota + quota, "turn_count": s.TurnCount + 1, "pending_response": false, "pending_reservation": "", "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("realtime session changed concurrently")
		}
		added = true
		return nil
	})
	return added, err
}

// FinishInferenceSession settles known usage once or retains uncertain funds for reconciliation.
// 版本: v0.0.28
// 日期: 2026-11-23
func FinishInferenceSession(ctx context.Context, id string, uncertain bool, reason string) error {
	var s InferenceSession
	if err := DB.WithContext(ctx).First(&s, "id = ?", id).Error; err != nil {
		return err
	}
	if s.Status == "closed" {
		return nil
	}
	var turns []InferenceSessionTurn
	if err := DB.WithContext(ctx).Where("session_id = ?", id).Order("created_at, response_id").Find(&turns).Error; err != nil {
		return err
	}
	for _, turn := range turns {
		// A recorded response has known usage even if a later response was interrupted.
		// Recover its window accounting without releasing the unknown response's request slot.
		if turn.ReservationKey != "" {
			if err := SettleInference(ctx, turn.ReservationKey, 0, turn.UsageJSON); err != nil {
				return err
			}
		}
	}
	if uncertain || s.PendingResponse || s.Status == "pending_reconciliation" {
		return DB.WithContext(ctx).Model(&InferenceSession{}).Where("id = ? AND status <> ?", id, "closed").Updates(map[string]any{"status": "pending_reconciliation", "close_reason": reason, "updated_at": time.Now().Unix()}).Error
	}
	usage, _ := json.Marshal(turns)
	if err := SettleInference(ctx, id, s.TotalQuota, string(usage)); err != nil {
		_ = DB.WithContext(ctx).Model(&InferenceSession{}).Where("id = ?", id).Updates(map[string]any{"status": "pending_reconciliation", "close_reason": "settlement_failed", "updated_at": time.Now().Unix()}).Error
		return err
	}
	return DB.WithContext(ctx).Model(&InferenceSession{}).Where("id = ?", id).Updates(map[string]any{"status": "closed", "close_reason": reason, "updated_at": time.Now().Unix()}).Error
}

// MarkStaleInferenceSessions exposes interrupted sessions for reconciliation without refunding unknown work.
// 版本: v0.0.28
// 日期: 2026-11-23
func MarkStaleInferenceSessions(ctx context.Context) error {
	return DB.WithContext(ctx).Model(&InferenceSession{}).Where("status = ? AND updated_at < ?", "active", time.Now().Add(-5*time.Minute).Unix()).Updates(map[string]any{"status": "pending_reconciliation", "close_reason": "process_interrupted"}).Error
}
