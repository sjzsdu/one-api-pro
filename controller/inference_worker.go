// inference_worker.go SQL租约驱动的视频任务执行与恢复
// Recovers durable jobs without duplicating unknown submissions and settles delivered assets once.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/common/random"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay"
	"github.com/modelbus/one-api-pro/relay/adaptor"
	"github.com/modelbus/one-api-pro/relay/adaptor/provider/ali"
	handler "github.com/modelbus/one-api-pro/relay/handler"
	"github.com/modelbus/one-api-pro/relay/meta"
	schema "github.com/modelbus/one-api-pro/relay/schema"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// StartInferenceWorker polls durable job leases; every node may run it safely.
// 版本: v0.0.28
// 日期: 2026-11-23
func StartInferenceWorker(ctx context.Context) {
	if os.Getenv("INFERENCE_WORKER_ENABLED") == "false" {
		return
	}
	owner := random.GetUUID()
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := RunInferenceWorkerOnce(ctx, owner); err != nil {
					logger.Errorf(ctx, "inference worker: %v", err)
				}
			}
		}
	}()
}

// RunInferenceWorkerOnce executes a bounded batch and can be used by separately deployed workers.
// 版本: v0.0.28
// 日期: 2026-11-23
func RunInferenceWorkerOnce(ctx context.Context, owner string) error {
	now := time.Now().Unix()
	settlementErr := model.RetryPendingInferenceSettlements(ctx, 4)
	if err := model.MarkStaleInferenceSessions(ctx); err != nil {
		return err
	}
	if err := model.RecoverInferenceSubmissions(ctx, now); err != nil {
		return err
	}
	for i := 0; i < 4; i++ {
		j, err := model.ClaimInferenceJob(ctx, owner, time.Now().Unix(), 120)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			break
		}
		if err != nil {
			return err
		}
		processInferenceJob(ctx, j)
	}
	var expired []model.InferenceAsset
	if model.DB.Where("expires_at <= ?", now).Limit(20).Find(&expired).Error == nil {
		for _, asset := range expired {
			if filepath.Base(asset.StorageKey) == asset.StorageKey {
				_ = os.Remove(filepath.Join(mediaAssetDirectory(), asset.StorageKey))
			}
			model.DB.Delete(&asset)
		}
	}
	return settlementErr
}

func saveInferenceJob(j *model.InferenceJob, updates map[string]any) error {
	updates["lease_until"] = 0
	updates["lease_owner"] = ""
	if _, ok := updates["next_poll_at"]; !ok {
		updates["next_poll_at"] = time.Now().Unix() + 5
	}
	if _, ok := updates["retry_count"]; !ok {
		updates["retry_count"] = 0
	}
	updates["updated_at"] = time.Now().Unix()
	r := model.DB.Model(&model.InferenceJob{}).Where("id = ? AND lease_owner = ?", j.ID, j.LeaseOwner).Updates(updates)
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return errors.New("job lease lost")
	}
	return nil
}

func retryInferenceJob(j *model.InferenceJob, updates map[string]any) error {
	attempt := j.RetryCount + 1
	if attempt > 32 {
		attempt = 32
	}
	delay := int64(60)
	if attempt < 5 {
		delay = 5 << uint(attempt-1)
	}
	updates["retry_count"] = attempt
	updates["next_poll_at"] = time.Now().Unix() + delay
	return saveInferenceJob(j, updates)
}

func transientVideoPollError(err error) bool {
	var status interface{ HTTPStatusCode() int }
	if errors.As(err, &status) {
		code := status.HTTPStatusCode()
		return code == 408 || code == 425 || code == 429 || code >= 500 && code <= 599
	}
	// The current native client deliberately strips credential-bearing transport errors.
	switch err.Error() {
	case "upstream request failed", "upstream body read failed":
		return true
	case "upstream error Throttling", "upstream error TooManyRequests", "upstream error InternalError", "upstream error ServiceUnavailable":
		return true
	}
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

func permanentInferenceLedgerError(err error) bool {
	for _, target := range []error{model.ErrInferenceFinalizedConflict, gorm.ErrRecordNotFound, gorm.ErrInvalidData, gorm.ErrInvalidValue, gorm.ErrInvalidField} {
		if errors.Is(err, target) {
			return true
		}
	}
	var retryable interface{ Retryable() bool }
	if errors.As(err, &retryable) {
		return !retryable.Retryable()
	}
	var syntax *json.SyntaxError
	var shape *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &shape) {
		return true
	}
	switch err.Error() {
	case "negative inference charge", "model not covered by plan", "reserved model no longer covered", "invalid pending settlement", "pending settlement intent conflict", "reservation cannot queue settlement":
		return true
	}
	return false
}

func processInferenceJob(parent context.Context, j *model.InferenceJob) {
	if time.Now().Unix()-j.CreatedAt > inferenceJobDeadlineSeconds() && j.Status != "failed" && j.Status != "canceled" {
		_ = saveInferenceJob(j, map[string]any{"status": "unknown", "settlement_status": "pending_reconciliation", "error_code": "job_deadline_exceeded"})
		return
	}
	ctx, cancel := context.WithTimeout(parent, 100*time.Second)
	defer cancel()
	if j.Status == "reserving" {
		if err := reserveMediaJob(ctx, j); err != nil {
			_, code, terminal := mediaReservationFailure(err)
			if terminal {
				_ = saveInferenceJob(j, map[string]any{"status": "failed", "asset_status": "unavailable", "settlement_status": "not_reserved", "error_code": code})
			} else {
				_ = retryInferenceJob(j, map[string]any{"error_code": code})
			}
		} else {
			_ = saveInferenceJob(j, map[string]any{"status": "queued"})
		}
		return
	}
	if j.Status == "failed" || j.Status == "canceled" {
		if err := model.RefundInference(ctx, "job:"+j.ID); err != nil {
			if permanentInferenceLedgerError(err) {
				_ = saveInferenceJob(j, map[string]any{"settlement_status": "pending_reconciliation", "error_code": "refund_reconciliation_required"})
			} else {
				_ = retryInferenceJob(j, map[string]any{"settlement_status": "pending", "error_code": "refund_temporarily_unavailable"})
			}
			return
		}
		_ = saveInferenceJob(j, map[string]any{"settlement_status": "refunded", "asset_status": "unavailable"})
		return
	}
	if j.Status == "succeeded" {
		finalizeMediaJob(ctx, j)
		return
	}
	channel, err := model.GetChannelById(j.ChannelID, true)
	if err != nil {
		_ = saveInferenceJob(j, map[string]any{"status": "unknown", "settlement_status": "pending_reconciliation", "error_code": "channel_unavailable"})
		return
	}
	a := relay.GetAdaptorByChannelID(j.ProviderID)
	p, ok := a.(adaptor.VideoJobProvider)
	if !ok {
		_ = saveInferenceJob(j, map[string]any{"status": "unknown", "settlement_status": "pending_reconciliation", "error_code": "provider_unavailable"})
		return
	}
	m := &meta.Meta{BaseURL: j.BaseURL, APIKey: channel.Key, ChannelId: j.ChannelID, ChannelID: j.ProviderID, ActualModelName: j.UpstreamModel, OriginModelName: j.ModelName}
	a.Init(m)
	if j.Status == "queued" {
		if j.CancelRequested {
			_ = saveInferenceJob(j, map[string]any{"status": "canceled", "asset_status": "unavailable"})
			return
		}
		attempt := model.InferenceAttempt{JobID: j.ID, ChannelID: j.ChannelID, State: "submitting", CreatedAt: time.Now().Unix(), UpdatedAt: time.Now().Unix()}
		err = model.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&model.Channel{}).Where("id = ?", j.ChannelID).UpdateColumn("updated_at", gorm.Expr("updated_at")).Error; err != nil {
				return err
			}
			var locked model.Channel
			if err := tx.First(&locked, j.ChannelID).Error; err != nil {
				return err
			}
			if limit := locked.GetMaxConcurrency(); limit > 0 {
				var active int64
				if err := tx.Model(&model.InferenceJob{}).Where("channel_id = ? AND status IN ?", j.ChannelID, []string{"submitting", "submitted", "running", "unknown"}).Where("created_at > ?", time.Now().Unix()-inferenceJobDeadlineSeconds()).Count(&active).Error; err != nil {
					return err
				}
				if active >= int64(limit) {
					return errors.New("video channel concurrency full")
				}
			}
			if err := tx.Create(&attempt).Error; err != nil {
				return err
			}
			r := tx.Model(&model.InferenceJob{}).Where("id = ? AND lease_owner = ? AND status = ?", j.ID, j.LeaseOwner, "queued").Update("status", "submitting")
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return errors.New("job changed before submission")
			}
			return nil
		})
		if err != nil {
			_ = retryInferenceJob(j, map[string]any{"error_code": "submission_claim_failed"})
			return
		}
		var req schema.VideoRequest
		if json.Unmarshal([]byte(j.RequestJSON), &req) != nil {
			_ = saveInferenceJob(j, map[string]any{"status": "failed", "asset_status": "unavailable", "error_code": "invalid_persisted_request"})
			return
		}
		task, err := p.SubmitVideo(ctx, m, &req)
		if err != nil {
			var uncertain *schema.SubmissionUnknownError
			if errors.As(err, &uncertain) {
				_ = saveInferenceJob(j, map[string]any{"status": "unknown", "settlement_status": "pending_reconciliation", "error_code": "submission_outcome_unknown"})
				model.DB.Model(&attempt).Update("state", "unknown")
			} else {
				_ = saveInferenceJob(j, map[string]any{"status": "failed", "asset_status": "unavailable", "error_code": "submission_failed"})
				model.DB.Model(&attempt).Update("state", "failed")
			}
			return
		}
		if err = saveInferenceJob(j, map[string]any{"status": "submitted", "upstream_task_id": task.ID}); err == nil {
			model.DB.Model(&attempt).Updates(map[string]any{"state": "submitted", "provider_task_id": task.ID, "updated_at": time.Now().Unix()})
		}
		return
	}
	if j.CancelRequested {
		_ = p.CancelVideo(ctx, m, j.UpstreamTaskID)
	}
	task, err := p.PollVideo(ctx, m, j.UpstreamTaskID)
	if err != nil {
		if transientVideoPollError(err) {
			_ = retryInferenceJob(j, map[string]any{"error_code": "poll_temporarily_unavailable"})
		} else {
			_ = saveInferenceJob(j, map[string]any{"status": "unknown", "settlement_status": "pending_reconciliation", "error_code": "poll_permanently_unavailable"})
		}
		return
	}
	switch task.State {
	case "PENDING", "RUNNING":
		count := j.PollCount + 1
		if j.ProviderState != task.State {
			count = 1
		}
		if count > 32 {
			count = 32
		}
		delay := int64(60)
		if count < 5 {
			delay = 5 << uint(count-1)
		}
		_ = saveInferenceJob(j, map[string]any{"status": "running", "error_code": "", "poll_count": count, "provider_state": task.State, "next_poll_at": time.Now().Unix() + delay})
	case "FAILED", "CANCELED":
		status := "failed"
		if task.State == "CANCELED" {
			status = "canceled"
		}
		_ = saveInferenceJob(j, map[string]any{"status": status, "asset_status": "unavailable", "error_code": task.ErrorCode})
	case "UNKNOWN":
		_ = saveInferenceJob(j, map[string]any{"status": "unknown", "settlement_status": "pending_reconciliation", "error_code": "provider_task_unknown"})
	case "SUCCEEDED":
		usage, _ := json.Marshal(task.Usage)
		_ = saveInferenceJob(j, map[string]any{"status": "succeeded", "source_url": task.URL, "usage_json": string(usage), "asset_status": "pending", "settlement_status": "pending", "error_code": ""})
	default:
		_ = saveInferenceJob(j, map[string]any{"status": "unknown", "settlement_status": "pending_reconciliation", "error_code": "unexpected_provider_state"})
	}
}

var downloadInferenceVideo = ali.DownloadVideoAsset

func finalizeMediaJob(ctx context.Context, j *model.InferenceJob) {
	var asset model.InferenceAsset
	if j.AssetStatus != "ready" {
		if j.SourceURL == "" {
			_ = saveInferenceJob(j, map[string]any{"error_code": "result_url_missing"})
			return
		}
		id := "asset_" + random.GetUUID()
		key := id + ".mp4"
		size, typ, err := downloadInferenceVideo(ctx, j.SourceURL, mediaAssetDirectory(), key)
		if err != nil {
			_ = saveInferenceJob(j, map[string]any{"error_code": "asset_download_failed"})
			return
		}
		asset = model.InferenceAsset{ID: id, JobID: j.ID, UserID: j.UserID, TokenID: j.TokenID, StorageKey: key, ContentType: typ, Size: size, CreatedAt: time.Now().Unix(), ExpiresAt: time.Now().Add(7 * 24 * time.Hour).Unix()}
		if err = model.DB.Create(&asset).Error; err != nil {
			_ = os.Remove(filepath.Join(mediaAssetDirectory(), key))
			if err = model.DB.Where("job_id = ?", j.ID).First(&asset).Error; err != nil {
				_ = saveInferenceJob(j, map[string]any{"error_code": "asset_storage_failed"})
				return
			}
		}
		// Persist asset readiness before settlement so a crash never regenerates or redownloads an owned asset.
		r := model.DB.Model(&model.InferenceJob{}).Where("id = ? AND lease_owner = ?", j.ID, j.LeaseOwner).Updates(map[string]any{"asset_id": asset.ID, "asset_status": "ready"})
		if r.Error != nil || r.RowsAffected != 1 {
			return
		}
		j.AssetID = asset.ID
		j.AssetStatus = "ready"
	}
	var metrics []schema.UsageMetric
	var price inferencePriceSnapshot
	if json.Unmarshal([]byte(j.PriceSnapshot), &price) != nil || json.Unmarshal([]byte(j.UsageJSON), &metrics) != nil || len(metrics) == 0 {
		_ = saveInferenceJob(j, map[string]any{"settlement_status": "pending_reconciliation", "error_code": "usage_missing"})
		return
	}
	total, err := handler.MeteredQuota(&price.Price, price.Discount, metrics)
	if err != nil {
		_ = saveInferenceJob(j, map[string]any{"settlement_status": "pending_reconciliation", "error_code": "usage_invalid"})
		return
	}
	if err = model.SettleInference(ctx, "job:"+j.ID, total, j.UsageJSON); err != nil {
		if permanentInferenceLedgerError(err) {
			_ = saveInferenceJob(j, map[string]any{"settlement_status": "pending_reconciliation", "error_code": "settlement_reconciliation_required"})
		} else {
			_ = retryInferenceJob(j, map[string]any{"settlement_status": "pending", "error_code": "settlement_pending"})
		}
		return
	}
	_ = saveInferenceJob(j, map[string]any{"settlement_status": "settled", "error_code": ""})
}

func inferenceJobDeadlineSeconds() int64 {
	const fallback int64 = 24 * 60 * 60
	v, err := strconv.ParseInt(os.Getenv("INFERENCE_JOB_TIMEOUT_SECONDS"), 10, 64)
	if err != nil || v < 60 || v > 7*24*60*60 {
		return fallback
	}
	return v
}
