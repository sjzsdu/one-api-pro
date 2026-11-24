// inference_job.go 持久化异步推理任务、尝试和资产
// Stores idempotent intents, recoverable leases and ownership-scoped media assets.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package model

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

// InferenceJob persists an immutable provider binding and independent execution, asset and billing states.
// 版本: v0.0.28
// 日期: 2026-11-23
type InferenceJob struct {
	ID               string `gorm:"primaryKey;size:64" json:"id"`
	UserID           int    `gorm:"uniqueIndex:idx_inference_intent;index" json:"-"`
	TokenID          int    `json:"-"`
	PlanID           int    `json:"-"`
	ChannelID        int    `json:"-"`
	IdempotencyKey   string `gorm:"size:191;uniqueIndex:idx_inference_intent" json:"-"`
	RequestHash      string `gorm:"size:64" json:"-"`
	ModelName        string `gorm:"size:255" json:"model"`
	UpstreamModel    string `gorm:"size:255" json:"-"`
	ProviderID       string `gorm:"size:64" json:"-"`
	BaseURL          string `gorm:"type:text" json:"-"`
	Operation        string `gorm:"size:64" json:"operation"`
	RequestJSON      string `gorm:"type:text" json:"-"`
	PriceSnapshot    string `gorm:"type:text" json:"-"`
	ReservedQuota    int64  `json:"reserved_quota"`
	Status           string `gorm:"size:24;index" json:"status"`
	UpstreamTaskID   string `gorm:"size:191" json:"-"`
	LeaseOwner       string `gorm:"size:64" json:"-"`
	LeaseUntil       int64  `gorm:"index" json:"-"`
	NextPollAt       int64  `gorm:"index" json:"-"`
	RetryCount       int    `json:"-"`
	PollCount        int    `json:"-"`
	ProviderState    string `gorm:"size:24" json:"-"`
	CancelRequested  bool   `json:"cancel_requested"`
	AssetStatus      string `gorm:"size:24" json:"asset_status"`
	AssetID          string `gorm:"size:64" json:"asset_id,omitempty"`
	SourceURL        string `gorm:"type:text" json:"-"`
	SettlementStatus string `gorm:"size:24" json:"settlement_status"`
	UsageJSON        string `gorm:"type:text" json:"-"`
	ErrorCode        string `gorm:"size:128" json:"error_code,omitempty"`
	CreatedAt        int64  `json:"created_at"`
	UpdatedAt        int64  `json:"updated_at"`
}

// InferenceAttempt records submission before the external side effect; unknown attempts never auto-resubmit.
// 版本: v0.0.28
// 日期: 2026-11-23
type InferenceAttempt struct {
	ID             uint   `gorm:"primaryKey"`
	JobID          string `gorm:"size:64;uniqueIndex"`
	ChannelID      int
	State          string `gorm:"size:24"`
	ProviderTaskID string `gorm:"size:191"`
	CreatedAt      int64
	UpdatedAt      int64
}

// InferenceAsset binds a server-generated storage key to the creating account and token.
// 版本: v0.0.28
// 日期: 2026-11-23
type InferenceAsset struct {
	ID          string `gorm:"primaryKey;size:64" json:"id"`
	JobID       string `gorm:"size:64;uniqueIndex" json:"job_id"`
	UserID      int    `json:"-"`
	TokenID     int    `json:"-"`
	StorageKey  string `gorm:"size:128" json:"-"`
	ContentType string `gorm:"size:100" json:"content_type"`
	Size        int64  `json:"size"`
	ExpiresAt   int64  `json:"expires_at"`
	CreatedAt   int64  `json:"created_at"`
}

// InferenceJobReconciliation records the authenticated administrator's explicit external evidence.
// 版本: v0.0.28
// 日期: 2026-11-23
type InferenceJobReconciliation struct {
	ID        uint   `gorm:"primaryKey"`
	JobID     string `gorm:"size:64;index"`
	AdminID   int
	Action    string `gorm:"size:32"`
	Note      string `gorm:"type:text"`
	CreatedAt int64
}

// CreateInferenceJob atomically claims the user idempotency key and validates repeated intent.
// 版本: v0.0.28
// 日期: 2026-11-23
func CreateInferenceJob(ctx context.Context, j InferenceJob) (*InferenceJob, bool, error) {
	if j.ID == "" || j.IdempotencyKey == "" || j.RequestHash == "" || j.UserID <= 0 || j.TokenID <= 0 {
		return nil, false, errors.New("invalid inference job")
	}
	j.CreatedAt = time.Now().Unix()
	j.UpdatedAt = j.CreatedAt
	created := false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&account, "id = ?", j.UserID).Error; err != nil {
			return err
		}
		lock := tx.Model(&User{}).Where("id = ?", j.UserID).UpdateColumn("updated_at", gorm.Expr("updated_at"))
		if lock.Error != nil {
			return lock.Error
		}
		var old InferenceJob
		err := tx.Where("user_id = ? AND idempotency_key = ?", j.UserID, j.IdempotencyKey).First(&old).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var active int64
		if err = tx.Model(&InferenceJob{}).Where("user_id = ? AND status IN ?", j.UserID, []string{"reserving", "queued", "submitting", "submitted", "running"}).Count(&active).Error; err != nil {
			return err
		}
		if active >= 20 {
			return errors.New("too many active jobs")
		}
		result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "idempotency_key"}}, DoNothing: true}).Create(&j)
		created = result.RowsAffected == 1
		return result.Error
	})
	if err != nil {
		return nil, false, err
	}
	if created {
		return &j, true, nil
	}

	var old InferenceJob
	if err := DB.WithContext(ctx).Where("user_id = ? AND idempotency_key = ?", j.UserID, j.IdempotencyKey).First(&old).Error; err != nil {
		return nil, false, err
	}
	if old.TokenID != j.TokenID || old.RequestHash != j.RequestHash {
		return nil, false, errors.New("idempotency key reused with a different request or token")
	}
	return &old, false, nil
}

// GetInferenceJob prevents a same-account token with narrower permissions from accessing another token's jobs.
// 版本: v0.0.28
// 日期: 2026-11-23
func GetInferenceJob(ctx context.Context, id string, userID, tokenID int) (*InferenceJob, error) {
	var j InferenceJob
	err := DB.WithContext(ctx).Where("id = ? AND user_id = ? AND token_id = ?", id, userID, tokenID).First(&j).Error
	return &j, err
}

// ClaimInferenceJob uses an atomic lease comparison for cross-process at-least-once worker delivery.
// 版本: v0.0.28
// 日期: 2026-11-23
func ClaimInferenceJob(ctx context.Context, owner string, now, leaseSeconds int64) (*InferenceJob, error) {
	var candidates []InferenceJob
	err := DB.WithContext(ctx).Where("status IN ? AND lease_until <= ? AND next_poll_at <= ?", []string{"reserving", "queued", "submitted", "running", "succeeded", "failed", "canceled"}, now, now).Where("status NOT IN ? OR asset_status = ? OR settlement_status = ?", []string{"succeeded", "failed", "canceled"}, "pending", "pending").Where("settlement_status <> ?", "pending_reconciliation").Order("next_poll_at ASC, created_at ASC").Limit(20).Find(&candidates).Error
	if err != nil {
		return nil, err
	}
	for _, j := range candidates {
		r := DB.WithContext(ctx).Model(&InferenceJob{}).Where("id = ? AND lease_until = ? AND status = ?", j.ID, j.LeaseUntil, j.Status).Updates(map[string]any{"lease_owner": owner, "lease_until": now + leaseSeconds, "updated_at": now})
		if r.Error != nil {
			return nil, r.Error
		}
		if r.RowsAffected == 1 {
			j.LeaseOwner = owner
			j.LeaseUntil = now + leaseSeconds
			return &j, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// RecoverInferenceSubmissions preserves ambiguous externally-submitted effects after worker crashes.
// 版本: v0.0.28
// 日期: 2026-11-23
func RecoverInferenceSubmissions(ctx context.Context, now int64) error {
	return DB.WithContext(ctx).Model(&InferenceJob{}).Where("status = ? AND lease_until <= ?", "submitting", now).Updates(map[string]any{"status": "unknown", "error_code": "submission_outcome_unknown", "settlement_status": "pending_reconciliation", "lease_owner": "", "lease_until": 0, "updated_at": now}).Error
}
