// inference_reconcile_test.go 普通推理对账权限与幂等回归
// Tests explicit reconciliation without bypassing dedicated job or session state machines.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package model

import (
	"context"
	"testing"
)

func TestInferenceReconcile_PermissionIdempotencyAndScope(t *testing.T) {
	inferenceFixture(t)
	if err := DB.AutoMigrate(&InferenceReconciliation{}); err != nil {
		t.Fatal(err)
	}
	DB.Create(&User{Id: 2, Username: "reconcile-admin", Role: RoleAdminUser, Status: UserStatusEnabled})
	ctx := context.Background()
	for _, key := range []string{"text:one", "request:refund", "job:blocked", "session:blocked", "text:budget"} {
		err := ReserveInference(ctx, InferenceReservation{Key: key, UserID: 1, TokenID: 1, Quota: 100, BudgetOnly: key == "text:budget"})
		if err != nil {
			t.Fatal(err)
		}
		if err = RecordInferencePending(ctx, key, `{"observed_usage":{}}`, "", "usage_missing"); err != nil {
			t.Fatal(err)
		}
	}
	if err := ReconcileInference(ctx, "text:one", 1, "settle", 40, false, "verified upstream bill"); err == nil {
		t.Fatal("nonadmin accepted")
	}
	for i := 0; i < 2; i++ {
		if err := ReconcileInference(ctx, "text:one", 2, "settle", 40, false, "verified upstream bill"); err != nil {
			t.Fatal(err)
		}
	}
	if err := ReconcileInference(ctx, "text:one", 2, "settle", 41, false, "verified upstream bill"); err == nil {
		t.Fatal("conflicting charge accepted")
	}
	if err := ReconcileInference(ctx, "request:refund", 2, "refund", 0, false, "no charge"); err == nil {
		t.Fatal("unconfirmed refund accepted")
	}
	if err := ReconcileInference(ctx, "request:refund", 2, "refund", 0, true, "provider confirms no charge"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"job:blocked", "session:blocked", "text:budget"} {
		if err := ReconcileInference(ctx, key, 2, "refund", 0, true, "verified"); err == nil {
			t.Fatalf("dedicated record accepted: %s", key)
		}
	}
	var u User
	DB.First(&u, 1)
	if u.Quota != 660 || u.UsedQuota != 40 {
		t.Fatalf("unexpected duplicate charge: quota=%d used=%d", u.Quota, u.UsedQuota)
	}
}

func TestInferenceReconcile_PendingClaimAndTokenUsage(t *testing.T) {
	inferenceFixture(t)
	if err := DB.AutoMigrate(&InferenceReconciliation{}); err != nil {
		t.Fatal(err)
	}
	DB.Create(&User{Id: 2, Username: "token-admin", Role: RoleAdminUser, Status: UserStatusEnabled})
	seedInferencePlan(t, BillingTypeToken, 100, 1000)
	ctx := context.Background()
	key := "text:plan-pending"
	if err := ReserveInference(ctx, InferenceReservation{Key: key, UserID: 1, TokenID: 1, PlanID: 1, ModelName: "test", Quota: 100}); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileInference(ctx, key, 2, "settle", 40, false, "verified"); err == nil {
		t.Fatal("live reservation accepted")
	}
	if err := RecordInferencePending(ctx, key, `{"state":"settlement_pending","observed_usage":null}`, "", "usage_missing"); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileInference(ctx, key, 2, "settle", 40, false, "verified"); err == nil {
		t.Fatal("missing token-plan usage accepted")
	}
	for i := 0; i < 2; i++ {
		if err := ReconcileInference(ctx, key, 2, "settle", 40, false, "verified", `{"prompt_tokens":10,"completion_tokens":5}`); err != nil {
			t.Fatal(err)
		}
	}
	var records []PlanUsage
	DB.Where("user_plan_id = ?", 1).Find(&records)
	if len(records) != 3 {
		t.Fatalf("plan windows=%d", len(records))
	}
	for _, r := range records {
		if r.Requests != 1 || r.PromptTokens != 10 || r.CompletionTokens != 5 {
			t.Fatalf("lost or duplicated usage: %+v", r)
		}
	}
}
