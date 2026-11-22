// alias_pool_distributor_test.go 初次选路无渠道时的池内切换测试
// Covers failover before the relay handler is reached.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package middleware

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/model"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestAliasPool_DistributeMissingChannel switches a stale advertised model before returning 503.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestAliasPool_DistributeMissingChannel(t *testing.T) {
	if e := model.DB.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.UserModelAlias{}, &model.UserPlan{}); e != nil {
		t.Fatal(e)
	}
	id, _ := seedUser(t, "pool-nochannel", model.RoleCommonUser, "poolnochannel", true)
	first := model.Channel{Status: 1, Type: 1, Key: "a", Models: "nochannel-a", Group: "default"}
	second := model.Channel{Status: 1, Type: 1, Key: "b", Models: "nochannel-b", Group: "default"}
	if e := first.Insert(); e != nil {
		t.Fatal(e)
	}
	if e := second.Insert(); e != nil {
		t.Fatal(e)
	}
	defer func() {
		model.DB.Where("channel_id IN ?", []int{first.Id, second.Id}).Delete(&model.Ability{})
		model.DB.Where("id IN ?", []int{first.Id, second.Id}).Delete(&model.Channel{})
	}()
	items := []model.UserModelAlias{{Alias: "nochannel-pool", Targets: []model.UserModelAliasTarget{{Model: "nochannel-a", Weight: 1}, {Model: "nochannel-b", Weight: 1}}}}
	if e := model.SaveUserModelAliases(context.Background(), 0, items); e != nil {
		t.Fatal(e)
	}
	defer model.SaveUserModelAliases(context.Background(), 0, nil)
	model.DB.Model(&model.Channel{}).Where("id = ?", first.Id).Update("status", 0)
	model.ClaimAliasSession(model.AliasPoolKey(id, items[0], "missing"), model.AliasSessionBinding{Model: "nochannel-a"}, time.Hour)
	r := gin.New()
	r.POST("/v1/chat/completions", TokenAuth(), PlanQuotaCheck(), Distribute(), func(c *gin.Context) {
		if c.GetString(ctxkey.RequestModel) != "nochannel-b" || c.GetInt(ctxkey.ChannelId) != second.Id {
			t.Error("did not switch unavailable first candidate")
		}
		c.JSON(200, gin.H{"ok": true})
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"nochannel-pool","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-poolnochannel")
	req.Header.Set("X-Session-ID", "missing")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
