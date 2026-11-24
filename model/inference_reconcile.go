// inference_reconcile.go 普通同步推理的显式审计对账
// Claims administrator resolutions durably before applying existing settlement or refund operations.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// InferenceReconciliation stores one immutable administrator decision per ordinary inference.
// 版本: v0.0.28
// 日期: 2026-11-23
type InferenceReconciliation struct {
	SettlementKey     string `gorm:"primaryKey;size:191" json:"id"`
	ActorID           int    `json:"actor_id"`
	PayloadHash       string `gorm:"size:64" json:"payload_hash"`
	UsageJSON         string `gorm:"type:text" json:"-"`
	Action            string `gorm:"size:20" json:"action"`
	TotalQuota        int64  `json:"total_quota"`
	ConfirmedNoCharge bool   `json:"confirmed_no_charge"`
	Note              string `gorm:"type:text" json:"note"`
	Status            string `gorm:"size:20" json:"status"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
}

// IsOrdinaryInference excludes dedicated asynchronous and realtime accounting state machines.
// 版本: v0.0.28
// 日期: 2026-11-23
func IsOrdinaryInference(s InferenceSettlement) bool {
	if s.BudgetOnly || strings.Contains(s.Key, ":turn:") {
		return false
	}
	for _, prefix := range []string{"text:", "request:", "multimodal:", "audio:", "image:"} {
		if strings.HasPrefix(s.Key, prefix) && len(s.Key) > len(prefix) {
			return true
		}
	}
	return false
}

// ReconcileInference records an immutable resolution, then resumes its idempotent ledger operation.
// A pending reason or stale age is required to avoid claiming a currently executing request.
// 版本: v0.0.28
// 日期: 2026-11-23
func ReconcileInference(ctx context.Context, key string, actorID int, action string, total int64, confirmed bool, note string, verifiedUsage ...string) error {
	note = strings.TrimSpace(note)
	if note == "" || len(note) > 2000 || total < 0 || (action != "settle" && action != "refund") || action == "refund" && (!confirmed || total != 0) {
		return errors.New("verified settle/refund action, quota and audit note are required")
	}
	usageOverride := ""
	if len(verifiedUsage) > 0 {
		usageOverride = verifiedUsage[0]
	}
	if len(usageOverride) > 64<<10 || usageOverride != "" && !json.Valid([]byte(usageOverride)) {
		return errors.New("invalid verified usage")
	}
	payload, _ := json.Marshal(struct {
		Actor     int
		Action    string
		Total     int64
		Confirmed bool
		Note      string
		Usage     string
	}{actorID, action, total, confirmed, note, usageOverride})
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	finished := false
	usage := ""
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var actor User
		if err := tx.First(&actor, "id = ? AND role >= ? AND status = ?", actorID, RoleAdminUser, UserStatusEnabled).Error; err != nil {
			return errors.New("administrator authorization required")
		}
		var s InferenceSettlement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(map[string]any{"key": key}).First(&s).Error; err != nil {
			return err
		}
		if !IsOrdinaryInference(s) {
			return errors.New("dedicated job/session reconciliation is required")
		}
		usage = s.UsageJSON
		var observation struct {
			State    string          `json:"state"`
			Observed json.RawMessage `json:"observed_usage"`
		}
		_ = json.Unmarshal([]byte(usage), &observation)
		if len(observation.Observed) > 0 {
			usage = string(observation.Observed)
		}
		if usageOverride != "" {
			usage = usageOverride
		}
		if action == "settle" && s.PlanID > 0 {
			var plan UserPlan
			if err := tx.First(&plan, s.PlanID).Error; err != nil {
				return err
			}
			if plan.EffectiveBillingType() == BillingTypeToken {
				input, output := InferenceUsageTokens(usage)
				if input+output == 0 {
					return errors.New("verified token usage is required for token-plan settlement")
				}
			}
		}
		var decision InferenceReconciliation
		err := tx.First(&decision, "settlement_key = ?", key).Error
		if err == nil {
			if decision.PayloadHash != hash {
				return errors.New("inference already claimed by a different reconciliation")
			}
			usage = decision.UsageJSON
			finished = decision.Status == "completed"
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if s.Status != "reserved" || s.PendingReason == "" && observation.State != "settlement_pending" && s.UpdatedAt > time.Now().Add(-5*time.Minute).Unix() {
			return errors.New("only pending or stale ordinary reservations can be reconciled")
		}
		now := time.Now().Unix()
		return tx.Create(&InferenceReconciliation{SettlementKey: key, ActorID: actorID, PayloadHash: hash, UsageJSON: usage, Action: action, TotalQuota: total, ConfirmedNoCharge: confirmed, Note: note, Status: "claimed", CreatedAt: now, UpdatedAt: now}).Error
	})
	if err != nil || finished {
		return err
	}
	if action == "refund" {
		err = RefundInference(ctx, key)
	} else {
		err = SettleInference(ctx, key, total, usage)
	}
	if err != nil {
		return err
	}
	return DB.WithContext(ctx).Model(&InferenceReconciliation{}).Where("settlement_key = ? AND payload_hash = ?", key, hash).Updates(map[string]any{"status": "completed", "updated_at": time.Now().Unix()}).Error
}
