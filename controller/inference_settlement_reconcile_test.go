// inference_settlement_reconcile_test.go 普通推理查询权限回归
// Verifies owner-token isolation, private field filtering and settled-only replay.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	api "github.com/modelbus/one-api-pro/controller"
	"github.com/modelbus/one-api-pro/model"
)

func TestInferenceQuery_OwnerTokenAndSafeReplay(t *testing.T) {
	_, uid, tid := realtimeFixture(t, "http://unused.example")
	row := model.InferenceSettlement{Key: "text:inspect", UserID: uid, TokenID: tid, ChannelID: 42, Status: "reserved", ModelName: "public-model", PriceSnapshot: "private-price", UsageJSON: "private-observation", ResultJSON: model.InferenceResultPayload(`{"answer":"result"}`)}
	if err := model.DB.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	call := func(user, token int) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Set("id", user)
		c.Set("token_id", token)
		c.Params = gin.Params{{Key: "id", Value: row.Key}}
		api.GetInference(c)
		return w
	}
	for _, pair := range [][2]int{{uid + 1, tid}, {uid, tid + 1}} {
		if w := call(pair[0], pair[1]); w.Code != 404 {
			t.Fatalf("other owner read %d: %s", w.Code, w.Body)
		}
	}
	w := call(uid, tid)
	if w.Code != 200 || strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "answer") || strings.Contains(w.Body.String(), "channel") {
		t.Fatalf("unsafe pending view: %d %s", w.Code, w.Body)
	}
	model.DB.Model(&row).Update("status", "settled")
	w = call(uid, tid)
	var body map[string]any
	if json.Unmarshal(w.Body.Bytes(), &body) != nil || body["result"] == nil {
		t.Fatalf("missing settled result: %s", w.Body)
	}
}
