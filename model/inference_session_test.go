// inference_session_test.go 实时会话逐轮用量与预付预算回归
// Verifies unique response metering, bounded budget and reconciliation without free retries.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package model

import (
	"context"
	"testing"
)

func TestInferenceSession_DuplicateCompletionAndBudget(t *testing.T) {
	inferenceFixture(t)
	if err := DB.AutoMigrate(&InferenceSession{}, &InferenceSessionTurn{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := ReserveInference(ctx, InferenceReservation{Key: "rt-one", UserID: 1, TokenID: 1, ModelName: "omni", Quota: 100}); err != nil {
		t.Fatal(err)
	}
	if err := DB.Create(&InferenceSession{ID: "rt-one", UserID: 1, TokenID: 1, ModelName: "omni", BudgetQuota: 100, MaxTurns: 2, Status: "active"}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		added, err := RecordInferenceSessionTurn(ctx, "rt-one", "response-1", 10, 5, 30, `{"input_tokens":10,"output_tokens":5}`)
		if err != nil || added != (i == 0) {
			t.Fatalf("duplicate result %v %v", added, err)
		}
	}
	if _, err := RecordInferenceSessionTurn(ctx, "rt-one", "response-1", 11, 5, 30, `{"input_tokens":11,"output_tokens":5}`); err == nil {
		t.Fatal("conflicting replay was accepted")
	}
	if _, err := RecordInferenceSessionTurn(ctx, "rt-one", "response-2", 10, 5, 80, `{}`); err == nil {
		t.Fatal("session exceeded prepaid budget")
	}
	if err := FinishInferenceSession(ctx, "rt-one", false, "client_closed"); err != nil {
		t.Fatal(err)
	}
	if err := FinishInferenceSession(ctx, "rt-one", false, "client_closed"); err != nil {
		t.Fatal(err)
	}
	var u User
	DB.First(&u, 1)
	if u.Quota != 970 || u.UsedQuota != 30 {
		t.Fatalf("duplicate charge or missing refund: %+v", u)
	}
	var s InferenceSession
	DB.First(&s, "id = ?", "rt-one")
	if s.TurnCount != 1 || s.TotalQuota != 30 || s.Status != "closed" {
		t.Fatalf("session=%+v", s)
	}
}

func TestInferenceSession_UnknownUsageRetainsReservation(t *testing.T) {
	inferenceFixture(t)
	if err := DB.AutoMigrate(&InferenceSession{}, &InferenceSessionTurn{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := ReserveInference(ctx, InferenceReservation{Key: "rt-unknown", UserID: 1, TokenID: 1, Quota: 100, ModelName: "omni"}); err != nil {
		t.Fatal(err)
	}
	DB.Create(&InferenceSession{ID: "rt-unknown", UserID: 1, TokenID: 1, BudgetQuota: 100, MaxTurns: 2, Status: "active"})
	if err := FinishInferenceSession(ctx, "rt-unknown", true, "missing_usage"); err != nil {
		t.Fatal(err)
	}
	var u User
	DB.First(&u, 1)
	var s InferenceSettlement
	DB.First(&s, "key = ?", "rt-unknown")
	if u.Quota != 900 || s.Status != "reserved" {
		t.Fatalf("unknown consumption was refunded: user=%d ledger=%s", u.Quota, s.Status)
	}
}

func TestInferenceSession_InterruptedCloseSettlesKnownTurnsAndHoldsUnknown(t *testing.T) {
	inferenceFixture(t)
	seedInferencePlan(t, BillingTypeRequest, 3, 0)
	if err := DB.AutoMigrate(&InferenceSession{}, &InferenceSessionTurn{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const id = "rt-interrupted"
	for _, r := range []InferenceReservation{
		{Key: id, UserID: 1, TokenID: 1, PlanID: 1, ModelName: "test", Quota: 100, BudgetOnly: true},
		{Key: id + ":turn:1", UserID: 1, TokenID: 1, PlanID: 1, ModelName: "test"},
		{Key: id + ":turn:2", UserID: 1, TokenID: 1, PlanID: 1, ModelName: "test"},
	} {
		if err := ReserveInference(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := DB.Create(&InferenceSession{ID: id, UserID: 1, TokenID: 1, BudgetQuota: 100, TotalQuota: 30, TurnCount: 1, MaxTurns: 3, Status: "active", PendingResponse: true, PendingReservation: id + ":turn:2"}).Error; err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after recording a completed turn but before settling its window usage.
	if err := DB.Create(&InferenceSessionTurn{SessionID: id, ResponseID: "known", ReservationKey: id + ":turn:1", InputTokens: 10, OutputTokens: 5, Quota: 30, UsageJSON: `{"input_tokens":10,"output_tokens":5}`}).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := FinishInferenceSession(ctx, id, true, "interrupted"); err != nil {
			t.Fatal(err)
		}
	}
	for key, want := range map[string]string{id: "reserved", id + ":turn:1": "settled", id + ":turn:2": "reserved"} {
		var row InferenceSettlement
		if err := DB.First(&row, "key = ?", key).Error; err != nil || row.Status != want {
			t.Fatalf("%s status=%s want=%s err=%v", key, row.Status, want, err)
		}
	}
	var usage PlanUsage
	if err := DB.Where("user_plan_id = ? AND window_type = ?", 1, WindowTypePeriod).First(&usage).Error; err != nil || usage.Requests != 1 || usage.PromptTokens+usage.CompletionTokens != 15 {
		t.Fatalf("known turn not counted exactly once: %+v err=%v", usage, err)
	}
	// Unknown zero-quota turns still represent real requests and must not free a slot on disconnect.
	r := InferenceReservation{Key: id + ":turn:3", UserID: 1, TokenID: 1, PlanID: 1, ModelName: "test"}
	if err := ReserveInference(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.Key = id + ":turn:4"
	if err := ReserveInference(ctx, r); err == nil {
		t.Fatal("disconnect freed an unresolved request slot")
	}
}

func TestInferenceSession_AdminReconciliationIsAuditedAndIdempotent(t *testing.T) {
	inferenceFixture(t)
	if err := DB.AutoMigrate(&InferenceSession{}, &InferenceSessionTurn{}); err != nil {
		t.Fatal(err)
	}
	if err := DB.Create(&User{Id: 2, Username: "realtime-admin", Role: RoleAdminUser, Status: UserStatusEnabled, AffCode: "realtimeadmin", AccessToken: "realtime-admin-test"}).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := ReserveInference(ctx, InferenceReservation{Key: "rt-reconcile", UserID: 1, TokenID: 1, Quota: 100, ModelName: "omni", BudgetOnly: true}); err != nil {
		t.Fatal(err)
	}
	DB.Create(&InferenceSession{ID: "rt-reconcile", UserID: 1, TokenID: 1, ModelName: "omni", BudgetQuota: 100, MaxTurns: 2, Status: "pending_reconciliation"})
	if err := ReconcileInferenceSession(ctx, "rt-reconcile", 1, "settle", 40, "checked upstream usage"); err == nil {
		t.Fatal("ordinary user reconciled accounting")
	}
	for i := 0; i < 2; i++ {
		if err := ReconcileInferenceSession(ctx, "rt-reconcile", 2, "settle", 40, "checked upstream usage"); err != nil {
			t.Fatal(err)
		}
	}
	if err := ReconcileInferenceSession(ctx, "rt-reconcile", 2, "settle", 41, "checked upstream usage"); err == nil {
		t.Fatal("conflicting finalization accepted")
	}
	var u User
	DB.First(&u, 1)
	var s InferenceSession
	DB.First(&s, "id = ?", "rt-reconcile")
	if u.Quota != 960 || u.UsedQuota != 40 || s.Status != "closed" || s.ResolutionActorID != 2 || s.ResolutionNote != "checked upstream usage" {
		t.Fatalf("incorrect audited reconciliation user=%+v session=%+v", u, s)
	}
	if err := ReserveInference(ctx, InferenceReservation{Key: "rt-refund", UserID: 1, TokenID: 1, Quota: 100, ModelName: "omni", BudgetOnly: true}); err != nil {
		t.Fatal(err)
	}
	DB.Create(&InferenceSession{ID: "rt-refund", UserID: 1, TokenID: 1, ModelName: "omni", BudgetQuota: 100, MaxTurns: 2, Status: "pending_reconciliation"})
	if err := ReconcileInferenceSession(ctx, "rt-refund", 2, "refund", 0, "provider confirms no execution"); err != nil {
		t.Fatal(err)
	}
	DB.First(&u, 1)
	if u.Quota != 960 || u.UsedQuota != 40 {
		t.Fatalf("confirmed non-execution refund changed prior charges: %+v", u)
	}
}
