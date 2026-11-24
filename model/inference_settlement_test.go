// inference_settlement_test.go 推理预留与结算原子性回归
// Verifies durable idempotent reservations, settlement and refunds.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package model

import (
	"context"
	"fmt"
	"github.com/glebarez/sqlite"
	"github.com/modelbus/one-api-pro/common"
	"gorm.io/gorm"
	"testing"
	"time"
)

func inferenceFixture(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	oldRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedis })
	old := DB
	DB = db
	t.Cleanup(func() { DB = old; s, _ := db.DB(); s.Close() })
	if err = db.AutoMigrate(&User{}, &Token{}, &Channel{}, &UserPlan{}, &Plan{}, &PlanUsage{}, &InferenceSettlement{}); err != nil {
		t.Fatal(err)
	}
	db.Create(&User{Id: 1, Username: "ledger-test", AccessToken: "ledger-test-access", AffCode: "ledger-test-aff", Quota: 1000})
	db.Create(&Token{Id: 1, UserId: 1, RemainQuota: 1000})
}

func seedInferencePlan(t *testing.T, billing string, requests, tokens int64) {
	t.Helper()
	rule := fmt.Sprintf(`{"test":{"period_h":1,"request_period":%d,"request_week":%d,"request_month":%d,"token_period":%d,"token_week":%d,"token_month":%d}}`, requests, requests, requests, tokens, tokens, tokens)
	if err := DB.Create(&Plan{Id: 1, Name: "inference", Status: 1, BillingType: billing, ModelLimits: rule, VirtualAmount: 500}).Error; err != nil {
		t.Fatal(err)
	}
	if err := DB.Create(&UserPlan{Id: 1, UserId: 1, PlanId: 1, Status: UserPlanStatusActive, StartTime: time.Now().Unix() - 60, EndTime: time.Now().Unix() + 3600, BillingType: billing, ModelLimits: rule, VirtualAmount: 500}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestInferenceSettlement_PlanTurnsAndHeldBudget(t *testing.T) {
	inferenceFixture(t)
	seedInferencePlan(t, BillingTypeRequest, 1, 0)
	ctx := context.Background()
	budget := InferenceReservation{Key: "budget", UserID: 1, TokenID: 1, PlanID: 1, ModelName: "test", Quota: 400, BudgetOnly: true}
	if err := ReserveInference(ctx, budget); err != nil {
		t.Fatal(err)
	}
	turn := InferenceReservation{Key: "turn-one", UserID: 1, TokenID: 1, PlanID: 1, ModelName: "test", Quota: 0}
	if err := ReserveInference(ctx, turn); err != nil {
		t.Fatal(err)
	}
	turn.Key = "turn-two"
	if err := ReserveInference(ctx, turn); err == nil {
		t.Fatal("concurrent pending turn bypassed request window")
	}
	if err := SettleInference(ctx, "turn-one", 0, `[]`); err != nil {
		t.Fatal(err)
	}
	if err := SettleInference(ctx, "budget", 400, `[]`); err != nil {
		t.Fatal(err)
	}
	var up UserPlan
	DB.First(&up, 1)
	var u User
	DB.First(&u, 1)
	if up.UsedAmount != 400 || u.Quota != 1000 || u.RequestCount != 1 {
		t.Fatalf("subscription accounting %+v %+v", up, u)
	}
}

func TestInferenceSettlement_PlanCannotSpendAnotherReservation(t *testing.T) {
	inferenceFixture(t)
	seedInferencePlan(t, BillingTypeRequest, 10, 0)
	ctx := context.Background()
	a := InferenceReservation{Key: "a", UserID: 1, TokenID: 1, PlanID: 1, ModelName: "test", Quota: 300}
	if err := ReserveInference(ctx, a); err != nil {
		t.Fatal(err)
	}
	a.Key = "b"
	a.Quota = 200
	if err := ReserveInference(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := SettleInference(ctx, "a", 350, `[]`); err == nil {
		t.Fatal("settlement stole a different held reservation")
	}
	if err := SettleInference(ctx, "a", 250, `[]`); err != nil {
		t.Fatal(err)
	}
}

func TestInferenceSettlement_PlanTokenWindowsAccumulate(t *testing.T) {
	inferenceFixture(t)
	seedInferencePlan(t, BillingTypeToken, 10, 5)
	ctx := context.Background()
	r := InferenceReservation{Key: "tokens", UserID: 1, TokenID: 1, PlanID: 1, ModelName: "test", Quota: 0}
	if err := ReserveInference(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := SettleInference(ctx, r.Key, 0, `{"prompt_tokens":3,"completion_tokens":2}`); err != nil {
		t.Fatal(err)
	}
	var rows []PlanUsage
	DB.Find(&rows)
	if len(rows) != 3 || rows[0].PromptTokens != 3 || rows[0].CompletionTokens != 2 {
		t.Fatalf("token windows=%+v", rows)
	}
	r.Key = "tokens-next"
	if err := ReserveInference(ctx, r); err == nil {
		t.Fatal("exhausted token window admitted another call")
	}
}

func TestInferenceSettlement_IdempotentAndRefund(t *testing.T) {
	inferenceFixture(t)
	ctx := context.Background()
	r := InferenceReservation{Key: "one", UserID: 1, TokenID: 1, Quota: 100, ModelName: "test"}
	if err := ReserveInference(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := ReserveInference(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := SettleInference(ctx, "one", 60, `[]`); err != nil {
		t.Fatal(err)
	}
	if err := SettleInference(ctx, "one", 60, `[]`); err != nil {
		t.Fatal(err)
	}
	var u User
	DB.First(&u, 1)
	if u.Quota != 940 || u.RequestCount != 1 || u.UsedQuota != 60 {
		t.Fatalf("user=%+v", u)
	}
	r.Key = "two"
	if err := ReserveInference(ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := RefundInference(ctx, "two"); err != nil {
		t.Fatal(err)
	}
	if err := RefundInference(ctx, "two"); err != nil {
		t.Fatal(err)
	}
	DB.First(&u, 1)
	if u.Quota != 940 {
		t.Fatalf("refund balance=%d", u.Quota)
	}
	if err := SettleInference(ctx, "two", 10, `[]`); err == nil {
		t.Fatal("refunded reservation was charged")
	}
}

func TestInferenceSettlement_InsufficientIsAtomic(t *testing.T) {
	inferenceFixture(t)
	err := ReserveInference(context.Background(), InferenceReservation{Key: "large", UserID: 1, TokenID: 1, Quota: 1001})
	if err == nil {
		t.Fatal("accepted insufficient quota")
	}
	var u User
	var tok Token
	DB.First(&u, 1)
	DB.First(&tok, 1)
	if u.Quota != 1000 || tok.RemainQuota != 1000 {
		t.Fatal("partial debit")
	}
	var n int64
	DB.Model(&InferenceSettlement{}).Count(&n)
	if n != 0 {
		t.Fatal("failed reserve persisted")
	}
}

func TestInferenceSettlement_ExpiredPlanFinishesAcceptedReservation(t *testing.T) {
	inferenceFixture(t)
	seedInferencePlan(t, BillingTypeToken, 10, 20)
	ctx := context.Background()
	r := InferenceReservation{Key: "accepted-plan", UserID: 1, TokenID: 1, PlanID: 1, ModelName: "test", Quota: 100}
	if err := ReserveInference(ctx, r); err != nil {
		t.Fatal(err)
	}
	var accepted InferenceSettlement
	if err := DB.Where(map[string]any{"key": r.Key}).First(&accepted).Error; err != nil {
		t.Fatal(err)
	}
	var original UserPlan
	DB.First(&original, 1)
	if err := DB.Model(&UserPlan{}).Where("id = ?", 1).Updates(map[string]any{"end_time": time.Now().Unix() - 1, "status": UserPlanStatusExpired}).Error; err != nil {
		t.Fatal(err)
	}
	// Configuration changes after acceptance cannot revoke this operation's
	// original model coverage or change its billing windows.
	if err := DB.Model(&Plan{}).Where("id = ?", 1).Updates(map[string]any{"model_limits": `{"replacement":{"period_h":24,"request_period":1}}`, "billing_type": BillingTypeRequest, "virtual_amount": 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := DB.Create(&UserPlan{Id: 2, UserId: 1, PlanId: 1, Status: UserPlanStatusActive, StartTime: time.Now().Unix(), EndTime: time.Now().Unix() + 3600, BillingType: BillingTypeRequest, ModelLimits: `{"replacement":{"request_period":1}}`}).Error; err != nil {
		t.Fatal(err)
	}
	usage := `{"prompt_tokens":3,"completion_tokens":2}`
	if err := SettleInference(ctx, r.Key, 75, usage); err != nil {
		t.Fatal(err)
	}
	if err := SettleInference(ctx, r.Key, 75, usage); err != nil {
		t.Fatal(err)
	}
	var oldPlan, newPlan UserPlan
	DB.First(&oldPlan, 1)
	DB.First(&newPlan, 2)
	if oldPlan.UsedAmount != 75 || newPlan.UsedAmount != 0 {
		t.Fatalf("charge moved plans: old=%d new=%d", oldPlan.UsedAmount, newPlan.UsedAmount)
	}
	var rows []PlanUsage
	DB.Find(&rows)
	if len(rows) != 3 {
		t.Fatalf("expected original three windows: %+v", rows)
	}
	for _, row := range rows {
		if row.UserPlanId != 1 || row.Model != "test" || row.Requests != 1 || row.PromptTokens != 3 || row.CompletionTokens != 2 || row.WindowIndex != CalcWindowIndex(accepted.CreatedAt, original.StartTime, row.WindowType, 1) {
			t.Fatalf("wrong accepted window: %+v", row)
		}
	}
	r.Key = "new-after-expiry"
	if err := ReserveInference(ctx, r); err == nil {
		t.Fatal("expired plan admitted a new reservation")
	}
}

func TestInferenceSettlement_ExpiredPlanRefundIsIdempotent(t *testing.T) {
	inferenceFixture(t)
	seedInferencePlan(t, BillingTypeRequest, 5, 0)
	ctx := context.Background()
	r := InferenceReservation{Key: "cancel-expired", UserID: 1, TokenID: 1, PlanID: 1, ModelName: "test", Quota: 100}
	if err := ReserveInference(ctx, r); err != nil {
		t.Fatal(err)
	}
	DB.Model(&UserPlan{}).Where("id = ?", 1).Updates(map[string]any{"status": UserPlanStatusExpired, "end_time": time.Now().Unix() - 1})
	if err := RefundInference(ctx, r.Key); err != nil {
		t.Fatal(err)
	}
	if err := RefundInference(ctx, r.Key); err != nil {
		t.Fatal(err)
	}
	var up UserPlan
	DB.First(&up, 1)
	if up.UsedAmount != 0 {
		t.Fatal("refund consumed plan")
	}
}
