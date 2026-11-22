// alias_pool_plan_test.go 跨模型切换重新计算套餐的回归测试
// Ensures failover never carries the previous model's subscription into a new attempt.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/model"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestAliasPool_RechecksPlan verifies the model-pool behavior described by this test.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestAliasPool_RechecksPlan(t *testing.T) {
	if e := model.DB.AutoMigrate(&model.Plan{}, &model.UserPlan{}, &model.PlanUsage{}); e != nil {
		t.Fatal(e)
	}
	id, _ := seedUser(t, "alias-plan", model.RoleCommonUser, "aliasplankey", true)
	plan := model.Plan{Name: "pool-plan-b", BillingType: model.BillingTypeRequest, ModelLimits: `{"plan-b":{"request_month":100}}`}
	if e := model.DB.Create(&plan).Error; e != nil {
		t.Fatal(e)
	}
	up := model.UserPlan{UserId: id, PlanId: int(plan.Id), StartTime: time.Now().Unix() - 10, EndTime: time.Now().Unix() + 3600, Status: 1, BillingType: model.BillingTypeRequest}
	if e := model.DB.Create(&up).Error; e != nil {
		t.Fatal(e)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"plan-a"}`))
	c.Set(ctxkey.PlanId, 9999)
	c.Set(ctxkey.BillingType, model.BillingTypeToken)
	enabled := true
	c.Set(aliasPoolContextKey, &AliasPoolState{Alias: model.UserModelAlias{Alias: "plan-pool", SessionTTLSeconds: 3600, FailoverEnabled: &enabled}, Targets: []model.UserModelAliasTarget{{Model: "plan-a", Weight: 1}, {Model: "plan-b", Weight: 1}}, UserID: id, Group: "default"})
	ok, e := NextAliasPoolTarget(c)
	if e != nil || !ok || c.GetInt(ctxkey.PlanId) != int(up.Id) || c.GetString(ctxkey.BillingType) != model.BillingTypeRequest {
		t.Fatalf("stale plan retained: %v %v plan=%d billing=%s", ok, e, c.GetInt(ctxkey.PlanId), c.GetString(ctxkey.BillingType))
	}
}
