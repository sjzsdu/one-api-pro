// provider_test.go 用户侧供应商用量统计单测
// Unit tests for the user-facing provider usage statistics.
// 版本: v0.0.28
// 日期: 2026-11-25
// 作者: MCode
package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/model"
)

// providerUsageResponse 供应商用量接口的响应结构。
// Response shape of the provider usage endpoint.
type providerUsageResponse struct {
	Success bool                   `json:"success"`
	Message string                 `json:"message"`
	Data    []userProviderUsageDTO `json:"data"`
}

// TestGetUserProviderUsage_AttributesByChannel 锁定归属口径：
// 用量必须按真实渠道归属到供应商，而不是按模型名猜测。
// Locks the attribution contract: usage is attributed by the real channel,
// never guessed from the model name.
func TestGetUserProviderUsage_AttributesByChannel(t *testing.T) {
	// controller 包的 TestMain 只迁移了用户/日志相关表，这里补上 channels。
	// The package TestMain only migrates user/log tables, so channels is added here.
	if err := model.DB.AutoMigrate(&model.Channel{}); err != nil {
		t.Fatalf("migrate channels: %v", err)
	}
	t.Cleanup(func() {
		model.DB.Where("id IN ?", []int{4, 7}).Delete(&model.Channel{})
	})

	now := int64(1795610000)
	// 两条 alibailian 渠道（49）与一条 zhipu 渠道（16）。
	if err := model.DB.Create(&model.Channel{Id: 4, Name: "bailian-a", Type: 49,
		Status: model.ChannelStatusEnabled, Models: "qwen-plus", Group: "default"}).Error; err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	if err := model.DB.Create(&model.Channel{Id: 7, Name: "zhipu", Type: 16,
		Status: model.ChannelStatusEnabled, Models: "glm-5.2", Group: "default"}).Error; err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	// 同一个模型名挂在两个不同供应商下，必须分开归属。
	model.LOG_DB.Create(&model.Log{UserId: 1, Type: model.LogTypeConsume, ModelName: "shared-model",
		ChannelId: 4, Quota: 100, PromptTokens: 10, CompletionTokens: 5, CreatedAt: now})
	model.LOG_DB.Create(&model.Log{UserId: 1, Type: model.LogTypeConsume, ModelName: "shared-model",
		ChannelId: 7, Quota: 40, PromptTokens: 4, CompletionTokens: 2, CreatedAt: now})
	// 另一个用户的日志不得计入。
	model.LOG_DB.Create(&model.Log{UserId: 2, Type: model.LogTypeConsume, ModelName: "qwen-plus",
		ChannelId: 4, Quota: 999, CreatedAt: now})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/provider-usage", strings.NewReader(""))
	c.Set(ctxkey.Id, 1)
	GetUserProviderUsage(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp providerUsageResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, w.Body.String())
	}
	if !resp.Success {
		t.Fatalf("success = false, message=%s", resp.Message)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("providers = %d, want 2 (got %+v)", len(resp.Data), resp.Data)
	}
	byID := map[string]userProviderUsageDTO{}
	for _, item := range resp.Data {
		byID[item.ProviderID] = item
	}
	bailian, ok := byID["alibailian"]
	if !ok {
		t.Fatalf("alibailian missing from %+v", resp.Data)
	}
	if bailian.Quota != 100 || bailian.RequestCount != 1 {
		t.Fatalf("alibailian usage = %+v, want quota=100 count=1", bailian)
	}
	zhipu, ok := byID["zhipu"]
	if !ok {
		t.Fatalf("zhipu missing from %+v", resp.Data)
	}
	if zhipu.Quota != 40 || zhipu.RequestCount != 1 {
		t.Fatalf("zhipu usage = %+v, want quota=40 count=1", zhipu)
	}
	// 同名模型在两个供应商下各归各的。
	if len(bailian.Models) != 1 || bailian.Models[0] != "shared-model" {
		t.Fatalf("alibailian models = %v, want [shared-model]", bailian.Models)
	}
	if zhipu.ProviderName == "" {
		t.Fatalf("zhipu provider name should come from the registry, got %q", zhipu.ProviderName)
	}
}
