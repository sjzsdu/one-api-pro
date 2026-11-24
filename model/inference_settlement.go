// inference_settlement.go 持久化推理额度预留与幂等结算
// Atomically reserves, settles and refunds inference charges in the primary database.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package model

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/modelbus/one-api-pro/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormschema "gorm.io/gorm/schema"
	"time"
)

// InferenceReservation identifies the account and maximum initially reserved charge.
// 版本: v0.0.28
// 日期: 2026-11-23
type InferenceReservation struct {
	Key                                string
	UserID, TokenID, PlanID, ChannelID int
	ModelName                          string
	Quota                              int64
	BudgetOnly                         bool
	RequestHash                        string
	PriceSnapshot                      string
}

// InferenceSettlement is the durable monetary record; one intent has one terminal settlement.
// 版本: v0.0.28
// 日期: 2026-11-23
type InferenceSettlement struct {
	Key           string                 `gorm:"primaryKey;size:191" json:"id"`
	UserID        int                    `gorm:"index" json:"-"`
	TokenID       int                    `json:"-"`
	PlanID        int                    `json:"-"`
	ChannelID     int                    `json:"-"`
	ModelName     string                 `gorm:"size:255" json:"model"`
	BudgetOnly    bool                   `json:"-"`
	RequestHash   string                 `gorm:"size:64" json:"-"`
	ReservedQuota int64                  `json:"reserved_quota"`
	TotalQuota    int64                  `json:"total_quota"`
	Status        string                 `gorm:"size:20;index;index:idx_inference_settlement_retry,priority:1" json:"status"`
	UsageJSON     string                 `gorm:"type:text" json:"-"`
	ResultJSON    InferenceResultPayload `json:"-"`
	PriceSnapshot string                 `gorm:"type:text" json:"-"`
	PlanSnapshot  string                 `gorm:"type:text" json:"-"`
	PendingQuota  *int64                 `json:"-"`
	PendingUsage  string                 `gorm:"type:text" json:"-"`
	RetryAfter    int64                  `gorm:"index:idx_inference_settlement_retry,priority:2" json:"-"`
	PendingReason string                 `gorm:"size:128" json:"pending_reason,omitempty"`
	CreatedAt     int64                  `json:"created_at"`
	UpdatedAt     int64                  `json:"updated_at"`
}

type inferencePlanSnapshot struct {
	ModelLimits   map[string]ModelLimitRule `json:"model_limits"`
	BillingType   string                    `json:"billing_type"`
	VirtualAmount int64                     `json:"virtual_amount"`
	StartTime     int64                     `json:"start_time"`
}

// ErrInferenceFinalizedConflict means a terminal ledger entry cannot accept a different finalization.
// 版本: v0.0.28
// 日期: 2026-11-23
var ErrInferenceFinalizedConflict = errors.New("inference already finalized differently")

// InferenceResultPayload preserves bounded replay results across SQL dialects.
// 版本: v0.0.28
// 日期: 2026-11-23
type InferenceResultPayload string

// GormDataType declares the logical text storage type.
// 版本: v0.0.28
// 日期: 2026-11-23
func (InferenceResultPayload) GormDataType() string { return "string" }

// GormDBDataType uses LONGTEXT for MySQL and portable TEXT elsewhere.
// 版本: v0.0.28
// 日期: 2026-11-23
func (InferenceResultPayload) GormDBDataType(db *gorm.DB, _ *gormschema.Field) string {
	if db.Dialector.Name() == "mysql" {
		return "LONGTEXT"
	}
	return "TEXT"
}

// RecordInferencePending preserves observed usage and a bounded result when a terminal charge cannot be committed.
// 版本: v0.0.28
// 日期: 2026-11-23
func RecordInferencePending(ctx context.Context, key, usageJSON, resultJSON, reason string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if len(usageJSON) > 64<<10 || len(resultJSON) > 64<<20 || len(reason) > 128 {
		return errors.New("pending observation exceeds limit")
	}
	updates := map[string]any{"result_json": resultJSON, "pending_reason": reason, "updated_at": time.Now().Unix()}
	if usageJSON != "" {
		updates["usage_json"] = usageJSON
	}
	r := DB.WithContext(ctx).Model(&InferenceSettlement{}).Where(map[string]any{"key": key, "status": "reserved"}).Updates(updates)
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		var count int64
		if err := DB.WithContext(ctx).Model(&InferenceSettlement{}).Where(map[string]any{"key": key, "status": "reserved"}).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return errors.New("reservation is not pending")
		}
	}
	return nil
}

// RecordInferenceSettlementRetry durably records an exact charge without changing balances.
// 版本: v0.0.28
// 日期: 2026-11-23
func RecordInferenceSettlementRetry(ctx context.Context, key string, quota int64, usage string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if quota < 0 || len(usage) > 64<<10 {
		return errors.New("invalid pending settlement")
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var s InferenceSettlement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(map[string]any{"key": key}).First(&s).Error; err != nil {
			return err
		}
		if s.Status == "settled" && s.TotalQuota == quota && s.UsageJSON == usage {
			return nil
		}
		if s.Status != "reserved" {
			return errors.New("reservation cannot queue settlement")
		}
		if s.PendingQuota != nil && (*s.PendingQuota != quota || s.PendingUsage != usage) {
			return errors.New("pending settlement intent conflict")
		}
		return tx.Model(&InferenceSettlement{}).Where(map[string]any{"key": key, "status": "reserved"}).Updates(map[string]any{"pending_quota": quota, "pending_usage": usage, "retry_after": time.Now().Unix(), "pending_reason": "settlement_pending"}).Error
	})
}

// RetryPendingInferenceSettlements retries only exact ledger charges, never provider execution.
// 版本: v0.0.28
// 日期: 2026-11-23
func RetryPendingInferenceSettlements(ctx context.Context, limit int) error {
	if limit < 1 || limit > 100 {
		limit = 10
	}
	var rows []InferenceSettlement
	if err := DB.WithContext(ctx).Where("status = ? AND pending_quota IS NOT NULL AND retry_after > 0 AND retry_after <= ?", "reserved", time.Now().Unix()).Order("retry_after ASC").Limit(limit).Find(&rows).Error; err != nil {
		return err
	}
	var failure error
	for _, s := range rows {
		if err := SettleInference(ctx, s.Key, *s.PendingQuota, s.PendingUsage); err != nil {
			failure = errors.Join(failure, err)
			if updateErr := DB.WithContext(ctx).Model(&InferenceSettlement{}).Where(map[string]any{"key": s.Key, "status": "reserved"}).UpdateColumn("retry_after", time.Now().Unix()+30).Error; updateErr != nil {
				failure = errors.Join(failure, updateErr)
			}
		}
	}
	return failure
}

// ReserveInference atomically claims an idempotency key and debits available quota.
// 版本: v0.0.28
// 日期: 2026-11-23
func ReserveInference(ctx context.Context, r InferenceReservation) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if r.Key == "" || r.UserID <= 0 || r.TokenID <= 0 || r.Quota < 0 {
		return errors.New("invalid inference reservation")
	}
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing InferenceSettlement
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(map[string]any{"key": r.Key}).First(&existing).Error
		if err == nil {
			if existing.UserID != r.UserID || existing.TokenID != r.TokenID || existing.PlanID != r.PlanID || existing.ChannelID != r.ChannelID || existing.BudgetOnly != r.BudgetOnly || existing.RequestHash != r.RequestHash || existing.ModelName != r.ModelName || existing.ReservedQuota != r.Quota {
				return errors.New("reservation key reused with different intent")
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		now := time.Now().Unix()
		s := InferenceSettlement{Key: r.Key, UserID: r.UserID, TokenID: r.TokenID, PlanID: r.PlanID, ChannelID: r.ChannelID, ModelName: r.ModelName, ReservedQuota: r.Quota, BudgetOnly: r.BudgetOnly, RequestHash: r.RequestHash, PriceSnapshot: r.PriceSnapshot, Status: "reserved", CreatedAt: now, UpdatedAt: now}
		if err = tx.Create(&s).Error; err != nil {
			return err
		}
		if r.PlanID > 0 {
			up, err := inferencePlan(tx, r.PlanID, r.UserID)
			if err != nil {
				return err
			}
			var held int64
			if err = tx.Model(&InferenceSettlement{}).Select("COALESCE(SUM(reserved_quota),0)").Where("plan_id = ? AND status = ?", r.PlanID, "reserved").Scan(&held).Error; err != nil {
				return err
			}
			if cap := up.EffectiveVirtualAmount(); cap > 0 && up.UsedAmount+held > cap {
				return errors.New("insufficient plan quota")
			}
			limits, err := up.EffectiveModelLimits()
			if err != nil {
				return err
			}
			if _, ok := FindLimit(limits, r.ModelName); !ok {
				return errors.New("model not covered by plan")
			}
			// An accepted long-running operation keeps its authorization even if
			// the subscription expires or an administrator changes its rules.
			snapshot, err := json.Marshal(inferencePlanSnapshot{ModelLimits: limits, BillingType: up.EffectiveBillingType(), VirtualAmount: up.EffectiveVirtualAmount(), StartTime: up.StartTime})
			if err != nil {
				return err
			}
			if err = tx.Model(&InferenceSettlement{}).Where(map[string]any{"key": r.Key}).UpdateColumn("plan_snapshot", string(snapshot)).Error; err != nil {
				return err
			}
			if !r.BudgetOnly {
				return checkInferenceWindows(tx, up, limits)
			}
			return nil
		}
		return adjustInferenceBalance(tx, r.UserID, r.TokenID, r.Quota)
	})
	if err == nil {
		_ = CacheInvalidateUserQuota(r.UserID)
	}
	return err
}

func inferencePlan(tx *gorm.DB, id, userID int) (*UserPlan, error) {
	return loadInferencePlan(tx, id, userID, true)
}

func loadInferencePlan(tx *gorm.DB, id, userID int, requireActive bool) (*UserPlan, error) {
	var up UserPlan
	q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", id, userID)
	if requireActive {
		q = q.Where("status = ? AND end_time > ?", UserPlanStatusActive, time.Now().Unix())
	}
	if err := q.First(&up).Error; err != nil {
		return nil, err
	}
	var p Plan
	if err := tx.First(&p, up.PlanId).Error; err == nil {
		up.Plan = &p
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return &up, nil
}

func adjustInferenceBalance(tx *gorm.DB, userID, tokenID int, delta int64) error {
	var tok Token
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&tok, "id = ? AND user_id = ?", tokenID, userID).Error; err != nil {
		return err
	}
	if delta == 0 {
		var user User
		return tx.Select("id").First(&user, "id = ?", userID).Error
	}
	q := tx.Model(&User{}).Where("id = ?", userID)
	if delta > 0 {
		q = q.Where("quota >= ?", delta)
	}
	result := q.UpdateColumn("quota", gorm.Expr("quota - ?", delta))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("insufficient user quota")
	}
	if !tok.UnlimitedQuota {
		q := tx.Model(&Token{}).Where("id = ?", tokenID)
		if delta > 0 {
			q = q.Where("remain_quota >= ?", delta)
		}
		result = q.UpdateColumn("remain_quota", gorm.Expr("remain_quota - ?", delta))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("insufficient token quota")
		}
	}
	return nil
}

// SettleInference records actual metered cost once, releasing unused reservations.
// 版本: v0.0.28
// 日期: 2026-11-23
func SettleInference(ctx context.Context, key string, totalQuota int64, usageJSON string) error {
	return finalizeInference(ctx, key, totalQuota, usageJSON, false)
}

// RefundInference releases a failed intent exactly once without recording consumption.
// 版本: v0.0.28
// 日期: 2026-11-23
func RefundInference(ctx context.Context, key string) error {
	return finalizeInference(ctx, key, 0, "", true)
}

func finalizeInference(ctx context.Context, key string, total int64, usage string, refund bool) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if total < 0 {
		return errors.New("negative inference charge")
	}
	var userID int
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var s InferenceSettlement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(map[string]any{"key": key}).First(&s).Error; err != nil {
			return err
		}
		userID = s.UserID
		status := "settled"
		if refund {
			status = "refunded"
		}
		if s.Status != "reserved" {
			if s.Status == status && s.TotalQuota == total && (refund || s.UsageJSON == usage) {
				return nil
			}
			return ErrInferenceFinalizedConflict
		}
		if s.PlanID > 0 {
			up, err := loadInferencePlan(tx, s.PlanID, s.UserID, false)
			if err != nil {
				return err
			}
			if !refund {
				if s.PlanSnapshot != "" {
					var snapshot inferencePlanSnapshot
					if err = json.Unmarshal([]byte(s.PlanSnapshot), &snapshot); err != nil {
						return err
					}
					limitsJSON, err := json.Marshal(snapshot.ModelLimits)
					if err != nil {
						return err
					}
					up.Plan = nil
					up.ModelLimits, up.BillingType = string(limitsJSON), snapshot.BillingType
					up.VirtualAmount, up.StartTime = snapshot.VirtualAmount, snapshot.StartTime
				}
				var held int64
				if err = tx.Model(&InferenceSettlement{}).Select("COALESCE(SUM(reserved_quota),0)").Where("plan_id = ? AND status = ?", s.PlanID, "reserved").Scan(&held).Error; err != nil {
					return err
				}
				// Only additional consumption needs new capacity: the reserved
				// amount was already admitted and cannot be revoked afterwards.
				if cap := up.EffectiveVirtualAmount(); total > s.ReservedQuota && cap > 0 && up.UsedAmount+held-s.ReservedQuota+total > cap {
					return errors.New("insufficient plan quota")
				}
				if err = tx.Model(&UserPlan{}).Where("id = ?", s.PlanID).UpdateColumn("used_amount", gorm.Expr("used_amount + ?", total)).Error; err != nil {
					return err
				}
				limits, err := up.EffectiveModelLimits()
				if err != nil {
					return err
				}
				rule, ok := FindLimit(limits, s.ModelName)
				if !ok {
					return errors.New("model not covered by plan")
				}
				if !s.BudgetOnly {
					input, output, cached := inferenceTokenDetails(usage)
					if err = checkInferenceTokenSettlement(tx, up, limits, s.ModelName, s.CreatedAt, input, output); err != nil {
						return err
					}
					for _, window := range []string{WindowTypePeriod, WindowTypeWeek, WindowTypeMonth} {
						pu := PlanUsage{UserPlanId: s.PlanID, Model: s.ModelName, WindowType: window, WindowIndex: CalcWindowIndex(s.CreatedAt, up.StartTime, window, rule.PeriodH), Requests: 1, PromptTokens: input, CompletionTokens: output, CachedTokens: cached, UpdatedTime: time.Now().Unix()}
						if err = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_plan_id"}, {Name: "model"}, {Name: "window_type"}, {Name: "window_index"}}, DoUpdates: clause.Assignments(map[string]any{"requests": gorm.Expr("requests + 1"), "prompt_tokens": gorm.Expr("prompt_tokens + ?", input), "completion_tokens": gorm.Expr("completion_tokens + ?", output), "cached_tokens": gorm.Expr("cached_tokens + ?", cached), "updated_time": pu.UpdatedTime})}).Create(&pu).Error; err != nil {
							return err
						}
					}
				}
			}
		} else if err := adjustInferenceBalance(tx, s.UserID, s.TokenID, total-s.ReservedQuota); err != nil {
			return err
		}
		if !refund {
			userUpdates := map[string]any{"used_quota": gorm.Expr("used_quota + ?", total)}
			if !s.BudgetOnly {
				userUpdates["request_count"] = gorm.Expr("request_count + 1")
			}
			if err := tx.Model(&User{}).Where("id = ?", s.UserID).Updates(userUpdates).Error; err != nil {
				return err
			}
			if err := tx.Model(&Token{}).Where("id = ?", s.TokenID).UpdateColumn("used_quota", gorm.Expr("used_quota + ?", total)).Error; err != nil {
				return err
			}
			if err := tx.Model(&Channel{}).Where("id = ?", s.ChannelID).UpdateColumn("used_quota", gorm.Expr("used_quota + ?", total)).Error; err != nil {
				return err
			}
		}
		result := tx.Model(&InferenceSettlement{}).Where(map[string]any{"key": key, "status": "reserved"}).Updates(map[string]any{"status": status, "total_quota": total, "usage_json": usage, "pending_quota": nil, "pending_usage": "", "pending_reason": "", "updated_at": time.Now().Unix()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("settlement concurrent transition")
		}
		return nil
	})
	if err == nil {
		_ = CacheInvalidateUserQuota(userID)
		if common.RedisEnabled {
			CacheDeleteUserActivePlans(userID)
		}
	}
	return err
}

func checkInferenceWindows(tx *gorm.DB, up *UserPlan, limits map[string]ModelLimitRule) error {
	var rows []*PlanUsage
	if err := tx.Where("user_plan_id = ?", up.Id).Find(&rows).Error; err != nil {
		return err
	}
	var pending []InferenceSettlement
	if err := tx.Where("plan_id = ? AND status = ? AND budget_only = ?", up.Id, "reserved", false).Find(&pending).Error; err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, p := range pending {
		rule, ok := FindLimit(limits, p.ModelName)
		if !ok {
			return errors.New("reserved model no longer covered")
		}
		for _, window := range []string{WindowTypePeriod, WindowTypeWeek, WindowTypeMonth} {
			rows = append(rows, &PlanUsage{Model: p.ModelName, WindowType: window, WindowIndex: CalcWindowIndex(p.CreatedAt, up.StartTime, window, rule.PeriodH), Requests: 1})
		}
	}
	for _, window := range []string{WindowTypePeriod, WindowTypeWeek, WindowTypeMonth} {
		if WeightedUsage(limits, rows, window, up.EffectiveBillingType(), now, up.StartTime) >= QuotaPoolCapacity && up.EffectiveBillingType() == BillingTypeToken {
			return errors.New("plan token window exhausted")
		}
		if WeightedUsage(limits, rows, window, BillingTypeRequest, now, up.StartTime) > QuotaPoolCapacity+1e-9 {
			return errors.New("plan request window exhausted")
		}
	}
	return nil
}

// InferenceUsageTokens extracts text token usage for plan windows and auditing.
// 版本: v0.0.28
// 日期: 2026-11-23
func InferenceUsageTokens(raw string) (int64, int64) {
	input, output, _ := inferenceTokenDetails(raw)
	return input, output
}
func inferenceTokenDetails(raw string) (int64, int64, int64) {
	var metrics []struct {
		Metric   string `json:"metric"`
		Quantity int64  `json:"quantity"`
	}
	if json.Unmarshal([]byte(raw), &metrics) == nil {
		var input, output, cached int64
		for _, m := range metrics {
			if m.Quantity < 0 {
				continue
			}
			switch m.Metric {
			case "text.input_tokens":
				input += m.Quantity
			case "text.output_tokens":
				output += m.Quantity
			case "text.cached_tokens":
				cached += m.Quantity
			}
		}
		return input, output, cached
	}
	var usage struct {
		Prompt     int64 `json:"prompt_tokens"`
		Completion int64 `json:"completion_tokens"`
		Input      int64 `json:"input_tokens"`
		Output     int64 `json:"output_tokens"`
		Details    struct {
			Cached int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	}
	if json.Unmarshal([]byte(raw), &usage) != nil {
		return 0, 0, 0
	}
	return max(usage.Prompt, usage.Input), max(usage.Completion, usage.Output), usage.Details.Cached
}
func checkInferenceTokenSettlement(tx *gorm.DB, up *UserPlan, limits map[string]ModelLimitRule, name string, createdAt, input, output int64) error {
	if up.EffectiveBillingType() != BillingTypeToken {
		return nil
	}
	var rows []*PlanUsage
	if err := tx.Where("user_plan_id = ?", up.Id).Find(&rows).Error; err != nil {
		return err
	}
	rule, ok := FindLimit(limits, name)
	if !ok {
		return errors.New("model not covered by plan")
	}
	for _, window := range []string{WindowTypePeriod, WindowTypeWeek, WindowTypeMonth} {
		rows = append(rows, &PlanUsage{Model: name, WindowType: window, WindowIndex: CalcWindowIndex(createdAt, up.StartTime, window, rule.PeriodH), PromptTokens: input, CompletionTokens: output})
		if WeightedUsage(limits, rows, window, BillingTypeToken, createdAt, up.StartTime) > QuotaPoolCapacity+1e-9 {
			return errors.New("actual token usage exceeds plan window")
		}
	}
	return nil
}
