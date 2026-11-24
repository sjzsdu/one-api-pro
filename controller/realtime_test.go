// realtime_test.go Qwen 实时会话鉴权、转发及计费集成回归
// Exercises fake WebSockets with real authentication, fixed routing and durable metering.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/gorilla/websocket"
	"github.com/modelbus/one-api-pro/channelrouter"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/config"
	api "github.com/modelbus/one-api-pro/controller"
	"github.com/modelbus/one-api-pro/middleware"
	"github.com/modelbus/one-api-pro/model"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const realtimeTestModel = "qwen3.5-omni-flash-realtime"

var realtimeUserSequence atomic.Int64

func realtimeFixture(t *testing.T, upstream string) (*httptest.Server, int, int) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err = db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.UserModelAlias{}, &model.ModelCatalog{}, &model.UserPlan{}, &model.Plan{}, &model.PlanUsage{}, &model.InferenceSettlement{}, &model.InferenceSession{}, &model.InferenceSessionTurn{}); err != nil {
		t.Fatal(err)
	}
	oldDB, oldLog, oldRedis, oldSQLite, oldConcurrency, oldRouter := model.DB, model.LOG_DB, common.RedisEnabled, common.UsingSQLite, config.ChannelConcurrencyEnabled, channelrouter.DefaultRouter
	model.DB, model.LOG_DB, common.RedisEnabled, common.UsingSQLite, config.ChannelConcurrencyEnabled = db, db, false, true, true
	channelrouter.DefaultRouter = channelrouter.NewChannelRouter()
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled, common.UsingSQLite, config.ChannelConcurrencyEnabled, channelrouter.DefaultRouter = oldDB, oldLog, oldRedis, oldSQLite, oldConcurrency, oldRouter
		sqlDB.Close()
	})
	uid := 991000 + int(realtimeUserSequence.Add(1))
	user := model.User{Id: uid, Username: fmt.Sprint("rt", uid), Status: model.UserStatusEnabled, Group: "default", Quota: 10000000, AffCode: fmt.Sprint(uid)}
	if err = db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	allowed := realtimeTestModel
	token := model.Token{UserId: uid, Key: "realtimefixture", Models: &allowed, Status: model.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 10000000}
	if err = db.Create(&token).Error; err != nil {
		t.Fatal(err)
	}
	max := 1
	ch := model.Channel{Id: uid, Name: "realtime", Type: 17, Key: "upstream-secret", Status: model.ChannelStatusEnabled, Models: realtimeTestModel, Group: "default", BaseURL: &upstream, MaxConcurrency: &max}
	if err = ch.Insert(); err != nil {
		t.Fatal(err)
	}
	caps := `{"primary_category":"audio","operations":{"chat.generate":{"modes":["realtime"],"input_modalities":["audio","text","image"],"output_modalities":["audio","text"]}},"source":"test"}`
	if err = db.Create(&model.ModelCatalog{Provider: "ali", ModelName: realtimeTestModel, Capabilities: caps}).Error; err != nil {
		t.Fatal(err)
	}
	model.InitChannelCache()
	cache := model.GetModelPriceCacheForTest()
	cache.Lock()
	model.SetModelPriceMapForTest(map[string]*model.ModelPrice{realtimeTestModel: {ModelName: realtimeTestModel, InputPrice: 1, OutputPrice: 2, BillingType: model.BillingTypeToken, Enabled: true}})
	cache.Unlock()
	t.Cleanup(func() { cache.Lock(); model.SetModelPriceMapForTest(nil); cache.Unlock() })
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/realtime", middleware.TokenAuth(), middleware.PlanQuotaCheck(), middleware.Distribute(), api.RelayRealtime)
	r.GET("/v1/realtime/sessions/:id", middleware.TokenAuth(), api.GetRealtimeSession)
	var handlers sync.WaitGroup
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		r.ServeHTTP(w, req)
	}))
	// httptest.Close does not wait for hijacked WebSocket handlers. Wait for the
	// middleware's concurrency-release defer before restoring process globals.
	t.Cleanup(func() { srv.Close(); handlers.Wait() })
	return srv, uid, token.Id
}

func dialRealtime(t *testing.T, base, query string) *websocket.Conn {
	t.Helper()
	header := http.Header{"Authorization": []string{"Bearer sk-realtimefixture"}}
	conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/v1/realtime?model="+realtimeTestModel+query, header)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("dial realtime: %v (%d)", err, status)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	t.Cleanup(func() { conn.Close() })
	return conn
}

func awaitRealtimeStatus(t *testing.T, uid int, status string) model.InferenceSession {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var s model.InferenceSession
		err := model.DB.Where("user_id = ?", uid).First(&s).Error
		if err == nil && s.Status == status {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("session did not reach %s: %+v %v", status, s, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRealtime_AuthenticationAndPermissions(t *testing.T) {
	srv, _, _ := realtimeFixture(t, "http://127.0.0.1:1")
	for _, tc := range []struct {
		auth, model string
		want        int
	}{{"", realtimeTestModel, 401}, {"Bearer sk-realtimefixture", "unauthorized-model", 403}} {
		req, _ := http.NewRequest("GET", srv.URL+"/v1/realtime?model="+tc.model, nil)
		req.Header.Set("Authorization", tc.auth)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Fatalf("auth status=%d want=%d", resp.StatusCode, tc.want)
		}
	}
}

func TestRealtime_FunctionFramesAndDuplicateUsage(t *testing.T) {
	var calls atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api-ws/v1/realtime" || r.Header.Get("Authorization") != "Bearer upstream-secret" || r.URL.Query().Get("model") != realtimeTestModel {
			t.Errorf("wrong upstream request: %s auth=%s", r.URL, r.Header.Get("Authorization"))
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, init, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if !strings.Contains(string(init), `"turn_detection":null`) {
			t.Errorf("session not initialized manually: %s", init)
			return
		}
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var event struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(data, &event)
			if event.Type == "conversation.item.create" {
				_ = conn.WriteMessage(websocket.TextMessage, data)
				continue
			}
			if event.Type == "response.create" {
				n := calls.Add(1)
				done := fmt.Sprintf(`{"type":"response.done","response":{"id":"r%d","usage":{"input_tokens":%d,"output_tokens":5,"input_tokens_details":{"audio_tokens":%d}}}}`, n, n*10, n*10)
				_ = conn.WriteMessage(websocket.TextMessage, []byte(done))
				_ = conn.WriteMessage(websocket.TextMessage, []byte(done))
			}
		}
	}))
	defer up.Close()
	srv, uid, _ := realtimeFixture(t, up.URL)
	conn := dialRealtime(t, srv.URL, "&max_turns=3")
	tool := `{"type":"conversation.item.create","item":{"type":"function_call_output","call_id":"opaque-call-id","output":"tool result"}}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(tool)); err != nil {
		t.Fatal(err)
	}
	_, got, err := conn.ReadMessage()
	if err != nil || string(got) != tool {
		t.Fatalf("tool frame changed: %s %v", got, err)
	}
	for turn := 0; turn < 2; turn++ {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create"}`))
		for i := 0; i < 2; i++ {
			_, raw, e := conn.ReadMessage()
			if e != nil || !strings.Contains(string(raw), `"response.done"`) {
				t.Fatalf("completion: %s %v", raw, e)
			}
		}
	}
	conn.Close()
	s := awaitRealtimeStatus(t, uid, "closed")
	if s.TurnCount != 2 || s.TotalQuota != 50 {
		t.Fatalf("duplicate or delta metering: %+v", s)
	}
	var user model.User
	model.DB.First(&user, uid)
	if user.Quota != 10000000-50 {
		t.Fatalf("wrong actual charge: %d", user.Quota)
	}
	if channelrouter.DefaultRouter.Concurrency.GetActiveCount(uid) != 0 {
		t.Fatal("session concurrency leaked")
	}
	read := func(key string) int {
		req, _ := http.NewRequest("GET", srv.URL+"/v1/realtime/sessions/"+s.ID, nil)
		req.Header.Set("Authorization", "Bearer sk-"+key)
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if status := read("realtimefixture"); status != 200 {
		t.Fatalf("owner status query=%d", status)
	}
	other := model.User{Id: uid + 1000, Username: fmt.Sprint("other", uid), AccessToken: fmt.Sprint("other", uid), AffCode: fmt.Sprint("other", uid), Status: model.UserStatusEnabled, Group: "default"}
	if err = model.DB.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if err = model.DB.Create(&model.Token{UserId: other.Id, Key: "otherrealtime", Status: model.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 10000000}).Error; err != nil {
		t.Fatal(err)
	}
	if status := read("otherrealtime"); status != 404 {
		t.Fatalf("session leaked to another owner: %d", status)
	}
}

func TestRealtime_MissingUsageRetainsPrepaidFunds(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage()
		_, _, _ = conn.ReadMessage()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.done","response":{"id":"missing"}}`))
	}))
	defer up.Close()
	srv, uid, _ := realtimeFixture(t, up.URL)
	conn := dialRealtime(t, srv.URL, "&max_turns=1")
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create"}`))
	_, _, _ = conn.ReadMessage()
	conn.Close()
	s := awaitRealtimeStatus(t, uid, "pending_reconciliation")
	var ledger model.InferenceSettlement
	model.DB.First(&ledger, "key = ?", s.ID)
	var user model.User
	model.DB.First(&user, uid)
	if ledger.Status != "reserved" || user.Quota != 10000000-s.BudgetQuota {
		t.Fatalf("unknown usage was refunded: ledger=%s balance=%d budget=%d", ledger.Status, user.Quota, s.BudgetQuota)
	}
	if channelrouter.DefaultRouter.Concurrency.GetActiveCount(uid) != 0 {
		t.Fatal("concurrency leaked on missing usage")
	}
}

func TestRealtime_TurnLimitStopsUpstreamConsumption(t *testing.T) {
	var calls atomic.Int64
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage()
		for {
			_, raw, e := conn.ReadMessage()
			if e != nil {
				return
			}
			if strings.Contains(string(raw), "response.create") {
				calls.Add(1)
				_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.done","response":{"id":"only","usage":{"input_tokens":10,"output_tokens":5}}}`))
			}
		}
	}))
	defer up.Close()
	srv, uid, _ := realtimeFixture(t, up.URL)
	conn := dialRealtime(t, srv.URL, "&max_turns=1")
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create"}`))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create"}`))
	_, raw, err := conn.ReadMessage()
	if err != nil || !strings.Contains(string(raw), "response_budget_exhausted") {
		t.Fatalf("limit response %s %v", raw, err)
	}
	conn.Close()
	s := awaitRealtimeStatus(t, uid, "closed")
	if calls.Load() != 1 || s.TurnCount != 1 || s.TotalQuota != 20 {
		t.Fatalf("unbounded responses: calls=%d session=%+v", calls.Load(), s)
	}
}

func TestRealtime_DisconnectDuringResponseRequiresReconciliation(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage()
		_, _, _ = conn.ReadMessage()
	}))
	defer up.Close()
	srv, uid, _ := realtimeFixture(t, up.URL)
	conn := dialRealtime(t, srv.URL, "&max_turns=1")
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create"}`))
	_, _, _ = conn.ReadMessage()
	conn.Close()
	s := awaitRealtimeStatus(t, uid, "pending_reconciliation")
	var ledger model.InferenceSettlement
	model.DB.First(&ledger, "key = ?", s.ID)
	if ledger.Status != "reserved" {
		t.Fatalf("unknown upstream work refunded: %s", ledger.Status)
	}
	if channelrouter.DefaultRouter.Concurrency.GetActiveCount(uid) != 0 {
		t.Fatal("disconnected session retained concurrency")
	}
}
