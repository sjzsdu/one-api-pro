// inference_job_test.go 异步推理幂等与租约恢复回归
// Tests job identity, tenant isolation and crash-safe submission claims.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package model

import (
	"context"
	"errors"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestInferenceJob_IdempotencyAndLeaseRecovery(t *testing.T) {
	inferenceFixture(t)
	if err := DB.AutoMigrate(&InferenceJob{}, &InferenceAttempt{}, &InferenceAsset{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	j := InferenceJob{ID: "job-one", UserID: 1, TokenID: 1, IdempotencyKey: "same", RequestHash: "hash", Status: "queued"}
	got, created, err := CreateInferenceJob(ctx, j)
	if err != nil || !created {
		t.Fatalf("create %v", err)
	}
	j.ID = "other"
	again, created, err := CreateInferenceJob(ctx, j)
	if err != nil || created || again.ID != got.ID {
		t.Fatalf("duplicate %v %+v", err, again)
	}
	j.RequestHash = "different"
	if _, _, err := CreateInferenceJob(ctx, j); err == nil {
		t.Fatal("accepted mismatched idempotency payload")
	}
	claimed, err := ClaimInferenceJob(ctx, "worker-one", time.Now().Unix(), 60)
	if err != nil || claimed.ID != "job-one" {
		t.Fatalf("claim %+v %v", claimed, err)
	}
	if _, err := ClaimInferenceJob(ctx, "worker-two", time.Now().Unix(), 60); err == nil {
		t.Fatal("concurrent lease accepted")
	}
	DB.Model(&InferenceJob{}).Where("id = ?", claimed.ID).Updates(map[string]any{"status": "submitting", "lease_until": time.Now().Unix() - 1})
	if err := RecoverInferenceSubmissions(ctx, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	DB.First(&got, "id = ?", claimed.ID)
	if got.Status != "unknown" {
		t.Fatalf("ambiguous submission retried: %s", got.Status)
	}
	if _, err := GetInferenceJob(ctx, claimed.ID, 1, 2); err == nil {
		t.Fatal("another token read job")
	}
}

func TestInferenceJob_ReconciliationIsNeverClaimed(t *testing.T) {
	inferenceFixture(t)
	if err := DB.AutoMigrate(&InferenceJob{}); err != nil {
		t.Fatal(err)
	}
	if err := DB.Create(&InferenceJob{ID: "awaiting-review", Status: "succeeded", AssetStatus: "pending", SettlementStatus: "pending_reconciliation"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimInferenceJob(context.Background(), "worker", time.Now().Unix(), 120); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("reconciliation re-entered polling: %v", err)
	}
}
