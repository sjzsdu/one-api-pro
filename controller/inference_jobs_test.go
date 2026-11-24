// inference_jobs_test.go 视频任务执行、交付及幂等结算回归
// Exercises fake upstream submissions, crash outcomes, private assets and ledger charges.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/client"
	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/model"
	"gorm.io/gorm"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type jobRoundTrip func(*http.Request) (*http.Response, error)

func (f jobRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func setupJobFixture(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	oldApprox := config.ApproximateTokenEnabled
	config.ApproximateTokenEnabled = true
	oldDB, oldLog, oldRedis, oldClient, oldMemory := model.DB, model.LOG_DB, common.RedisEnabled, client.HTTPClient, config.MemoryCacheEnabled
	model.DB = db
	model.LOG_DB = db
	common.RedisEnabled = false
	config.MemoryCacheEnabled = false
	t.Cleanup(func() {
		config.ApproximateTokenEnabled = oldApprox
		model.DB = oldDB
		model.LOG_DB = oldLog
		common.RedisEnabled = oldRedis
		client.HTTPClient = oldClient
		config.MemoryCacheEnabled = oldMemory
		sqlDB.Close()
	})
	if err = db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.UserPlan{}, &model.Plan{}, &model.PlanUsage{}, &model.InferenceSettlement{}, &model.InferenceJob{}, &model.InferenceAttempt{}, &model.InferenceAsset{}, &model.InferenceSession{}, &model.InferenceSessionTurn{}, &model.InferenceJobReconciliation{}, &model.ModelCatalog{}); err != nil {
		t.Fatal(err)
	}
	db.Create(&model.User{Id: 1, Username: "job-test", Quota: 10000})
	db.Create(&model.Token{Id: 1, UserId: 1, RemainQuota: 10000})
	one := 1
	base := "https://workspace.example"
	db.Create(&model.Channel{Id: 1, Name: "test", Type: 17, Status: 1, Key: "provider-secret", BaseURL: &base, MaxConcurrency: &one})
	cache := model.GetModelPriceCacheForTest()
	cache.Lock()
	model.SetModelPriceMapForTest(map[string]*model.ModelPrice{"wan2.6-t2v": {ModelName: "wan2.6-t2v", PerRequestPrice: .001, BillingType: model.BillingTypePerRequest, Enabled: true}})
	cache.Unlock()
	t.Cleanup(func() { cache.Lock(); model.SetModelPriceMapForTest(nil); cache.Unlock() })
	t.Setenv("INFERENCE_ASSET_DIR", t.TempDir())
}
func jobContext(body string, token int) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/media/jobs", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Idempotency-Key", "intent-key")
	c.Request.Header.Set("Authorization", "Bearer provider-secret")
	c.Set(ctxkey.Id, 1)
	c.Set(ctxkey.TokenId, token)
	c.Set(ctxkey.ChannelId, 1)
	c.Set(ctxkey.ChannelID, "ali")
	c.Set(ctxkey.RequestModel, "wan2.6-t2v")
	c.Set(ctxkey.BaseURL, "https://workspace.example")
	c.Set(ctxkey.Group, "default")
	c.Set(ctxkey.Config, model.ChannelConfig{})
	return c, w
}
func fetchTestJob(t *testing.T, id string) model.InferenceJob {
	t.Helper()
	var j model.InferenceJob
	if err := model.DB.First(&j, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	return j
}
func advanceJob(t *testing.T, id string) {
	t.Helper()
	if err := model.DB.Model(&model.InferenceJob{}).Where("id = ?", id).Update("next_poll_at", 0).Error; err != nil {
		t.Fatal(err)
	}
	if err := RunInferenceWorkerOnce(context.Background(), "test-worker"); err != nil {
		t.Fatal(err)
	}
}

func TestMediaJob_ExecutionAssetAndSingleSettlement(t *testing.T) {
	setupJobFixture(t)
	submissions := 0
	client.HTTPClient = &http.Client{Transport: jobRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "workspace.example" || r.Header.Get("Authorization") != "Bearer provider-secret" {
			t.Fatal("binding changed")
		}
		body := ""
		if strings.Contains(r.URL.Path, "video-synthesis") {
			submissions++
			body = `{"output":{"task_id":"upstream-one","task_status":"PENDING"}}`
		} else {
			body = `{"output":{"task_status":"SUCCEEDED","video_url":"https://cdn.example/video.mp4"},"usage":{"duration":5}}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	oldDownload := downloadInferenceVideo
	downloadInferenceVideo = func(ctx context.Context, url, dir, key string) (int64, string, error) {
		if url != "https://cdn.example/video.mp4" {
			t.Fatal(url)
		}
		if err := os.WriteFile(filepath.Join(dir, key), []byte("video-bytes"), 0600); err != nil {
			return 0, "", err
		}
		return 11, "video/mp4", nil
	}
	t.Cleanup(func() { downloadInferenceVideo = oldDownload })
	c, w := jobContext(`{"model":"wan2.6-t2v","prompt":"sunrise","duration":5}`, 1)
	CreateMediaJob(c)
	if w.Code != 202 {
		t.Fatalf("submit=%d %s", w.Code, w.Body.String())
	}
	var j model.InferenceJob
	json.Unmarshal(w.Body.Bytes(), &j)
	replay, rw := jobContext(`{"model":"wan2.6-t2v","prompt":"sunrise","duration":5}`, 1)
	CreateMediaJob(replay)
	if rw.Code != 202 || !strings.Contains(rw.Body.String(), j.ID) {
		t.Fatalf("replay %s", rw.Body.String())
	}
	advanceJob(t, j.ID)
	advanceJob(t, j.ID)
	advanceJob(t, j.ID)
	j = fetchTestJob(t, j.ID)
	if j.Status != "succeeded" || j.AssetStatus != "ready" || j.SettlementStatus != "settled" || submissions != 1 {
		t.Fatalf("job=%+v submits=%d", j, submissions)
	}
	var u model.User
	model.DB.First(&u, 1)
	if u.Quota != 9000 || u.RequestCount != 1 {
		t.Fatalf("account %+v", u)
	}
	c, aw := jobContext("", 1)
	c.Request = httptest.NewRequest("GET", "/v1/media/assets/"+j.AssetID, nil)
	c.Params = gin.Params{{Key: "id", Value: j.AssetID}}
	GetMediaAsset(c)
	if aw.Code != 200 || aw.Body.String() != "video-bytes" {
		t.Fatalf("asset=%d %s", aw.Code, aw.Body.String())
	}
	if aw.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(aw.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatal("asset missing safe download headers")
	}
	c, aw = jobContext("", 2)
	c.Params = gin.Params{{Key: "id", Value: j.AssetID}}
	GetMediaAsset(c)
	if aw.Code != 404 {
		t.Fatal("different token read asset")
	}
	advanceJob(t, j.ID)
	model.DB.First(&u, 1)
	if u.Quota != 9000 || submissions != 1 {
		t.Fatal("terminal job repeated side effects")
	}
}

func TestMediaJob_ConcurrentWorkersRespectChannelSlot(t *testing.T) {
	setupJobFixture(t)
	var calls atomic.Int32
	client.HTTPClient = &http.Client{Transport: jobRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"output":{"task_id":"up-one","task_status":"PENDING"}}`))}, nil
	})}
	for _, key := range []string{"a", "b"} {
		c, w := jobContext(`{"model":"wan2.6-t2v","prompt":"scene"}`, 1)
		c.Request.Header.Set("Idempotency-Key", key)
		CreateMediaJob(c)
		if w.Code != 202 {
			t.Fatal(w.Body.String())
		}
	}
	var wg sync.WaitGroup
	for _, owner := range []string{"one", "two"} {
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			if err := RunInferenceWorkerOnce(context.Background(), owner); err != nil {
				t.Error(err)
			}
		}(owner)
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("channel max=1 submitted %d jobs", calls.Load())
	}
}

func TestMediaJob_IdempotentReplayBypassesNewAdmission(t *testing.T) {
	setupJobFixture(t)
	body := `{"model":"wan2.6-t2v","prompt":"scene"}`
	c, w := jobContext(body, 1)
	CreateMediaJob(c)
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	var old model.InferenceJob
	json.Unmarshal(w.Body.Bytes(), &old)
	for i := 0; i < 19; i++ {
		j := model.InferenceJob{ID: fmt.Sprintf("other-%d", i), UserID: 1, TokenID: 1, IdempotencyKey: fmt.Sprintf("key-%d", i), RequestHash: "other", Status: "queued"}
		if err := model.DB.Create(&j).Error; err != nil {
			t.Fatal(err)
		}
	}
	c, w = jobContext(body, 1)
	CreateMediaJob(c)
	if w.Code != 202 || !strings.Contains(w.Body.String(), old.ID) {
		t.Fatalf("replay blocked by limit: %d %s", w.Code, w.Body.String())
	}
	r := gin.New()
	r.POST("/v1/media/jobs", func(c *gin.Context) { c.Set(ctxkey.Id, 1); c.Set(ctxkey.TokenId, 1) }, ReplayMediaJob, func(c *gin.Context) { t.Error("replay attempted new admission") })
	req := httptest.NewRequest("POST", "/v1/media/jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "intent-key")
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != 202 {
		t.Fatalf("early replay=%d", rw.Code)
	}
}

func TestMediaJob_UnknownSubmissionNeverRetries(t *testing.T) {
	setupJobFixture(t)
	calls := 0
	client.HTTPClient = &http.Client{Transport: jobRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("timeout after accepted")
	})}
	c, w := jobContext(`{"model":"wan2.6-t2v","prompt":"scene"}`, 1)
	CreateMediaJob(c)
	var j model.InferenceJob
	json.Unmarshal(w.Body.Bytes(), &j)
	advanceJob(t, j.ID)
	advanceJob(t, j.ID)
	j = fetchTestJob(t, j.ID)
	if j.Status != "unknown" || calls != 1 {
		t.Fatalf("unknown retried: %+v %d", j, calls)
	}
	var u model.User
	model.DB.First(&u, 1)
	if u.Quota != 9000 {
		t.Fatal("unknown charge was blindly refunded")
	}
}

func TestMediaJob_EmptyQueueStillExpiresAssets(t *testing.T) {
	setupJobFixture(t)
	key := "expired.mp4"
	os.WriteFile(filepath.Join(mediaAssetDirectory(), key), []byte("old"), 0600)
	model.DB.Create(&model.InferenceAsset{ID: "expired", JobID: "old", UserID: 1, TokenID: 1, StorageKey: key, ExpiresAt: time.Now().Unix() - 1})
	if err := RunInferenceWorkerOnce(context.Background(), "worker"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(mediaAssetDirectory(), key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("expired asset remains")
	}
}

func TestMediaWorker_PollBackoffAndPermanentFailure(t *testing.T) {
	setupJobFixture(t)
	status := http.StatusServiceUnavailable
	calls := 0
	client.HTTPClient = &http.Client{Transport: jobRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"output":{"task_status":"RUNNING"}}`)), Header: make(http.Header)}, nil
	})}
	j := model.InferenceJob{ID: "poll-retry", UserID: 1, TokenID: 1, ChannelID: 1, ProviderID: "ali", BaseURL: "https://workspace.example", Status: "running", UpstreamTaskID: "upstream-task", CreatedAt: time.Now().Unix(), SettlementStatus: "pending"}
	model.DB.Create(&j)
	for _, delay := range []int64{5, 10, 20, 40, 60, 60} {
		before := time.Now().Unix()
		advanceJob(t, j.ID)
		row := fetchTestJob(t, j.ID)
		if row.NextPollAt-before < delay || row.NextPollAt-before > delay+1 {
			t.Fatalf("retry delay want=%d got=%d", delay, row.NextPollAt-before)
		}
	}
	status = http.StatusOK
	advanceJob(t, j.ID)
	status = http.StatusTooManyRequests
	before := time.Now().Unix()
	advanceJob(t, j.ID)
	if row := fetchTestJob(t, j.ID); row.NextPollAt-before > 6 {
		t.Fatalf("successful poll did not reset backoff: %+v", row)
	}
	status = http.StatusUnauthorized
	advanceJob(t, j.ID)
	row := fetchTestJob(t, j.ID)
	if row.Status != "unknown" || row.SettlementStatus != "pending_reconciliation" {
		t.Fatalf("permanent error kept polling: %+v", row)
	}
	prior := calls
	advanceJob(t, j.ID)
	if calls != prior {
		t.Fatal("pending reconciliation called upstream again")
	}
}

func TestMediaCancel_ConcurrentTerminalTransition(t *testing.T) {
	setupJobFixture(t)
	model.DB.Create(&model.InferenceJob{ID: "cancel-race", UserID: 1, TokenID: 1, Status: "running"})
	name := "test:finish-before-cancel"
	model.DB.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "inference_jobs" {
			tx.Session(&gorm.Session{NewDB: true}).Exec("UPDATE inference_jobs SET status = ? WHERE id = ?", "succeeded", "cancel-race")
		}
	})
	t.Cleanup(func() { model.DB.Callback().Query().Remove(name) })
	c, w := jobContext("{}", 1)
	c.Params = gin.Params{{Key: "id", Value: "cancel-race"}}
	CancelMediaJob(c)
	if w.Code != 409 {
		t.Fatalf("false cancel accepted: %d %s", w.Code, w.Body)
	}
}

func TestMediaCreate_ReservationStorageFailureIsRecoverable(t *testing.T) {
	setupJobFixture(t)
	name := "test:reservation-storage-failure"
	model.DB.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "inference_settlements" {
			tx.AddError(errors.New("temporary database failure"))
		}
	})
	t.Cleanup(func() { model.DB.Callback().Create().Remove(name) })
	c, w := jobContext(`{"model":"wan2.6-t2v","prompt":"test"}`, 1)
	CreateMediaJob(c)
	if w.Code != 503 || !strings.Contains(w.Body.String(), "reservation_unavailable") {
		t.Fatalf("storage error misclassified: %d %s", w.Code, w.Body)
	}
	var j model.InferenceJob
	model.DB.First(&j)
	if j.Status != "reserving" {
		t.Fatalf("recoverable reservation permanently failed: %+v", j)
	}
}

func TestMediaWorker_UnchangedRunningBackoffAndStateProgress(t *testing.T) {
	setupJobFixture(t)
	state := "PENDING"
	client.HTTPClient = &http.Client{Transport: jobRoundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"output":{"task_status":%q}}`, state))), Header: make(http.Header)}, nil
	})}
	j := model.InferenceJob{ID: "poll-progress", UserID: 1, TokenID: 1, ChannelID: 1, ProviderID: "ali", BaseURL: "https://workspace.example", Status: "submitted", UpstreamTaskID: "upstream-task", CreatedAt: time.Now().Unix(), SettlementStatus: "pending"}
	model.DB.Create(&j)
	for _, delay := range []int64{5, 10, 20, 40, 60, 60} {
		before := time.Now().Unix()
		advanceJob(t, j.ID)
		row := fetchTestJob(t, j.ID)
		if row.NextPollAt-before < delay || row.NextPollAt-before > delay+1 {
			t.Fatalf("unchanged task delay want=%d got=%d", delay, row.NextPollAt-before)
		}
	}
	state = "RUNNING"
	for _, delay := range []int64{5, 10, 20} {
		before := time.Now().Unix()
		advanceJob(t, j.ID)
		row := fetchTestJob(t, j.ID)
		if row.NextPollAt-before < delay || row.NextPollAt-before > delay+1 {
			t.Fatalf("progress reset delay want=%d got=%d", delay, row.NextPollAt-before)
		}
	}
}

func TestMediaJob_EmptyQueueStillRetriesExactSettlement(t *testing.T) {
	setupJobFixture(t)
	ctx := context.Background()
	if err := model.ReserveInference(ctx, model.InferenceReservation{Key: "text:retry-charge", UserID: 1, TokenID: 1, Quota: 100}); err != nil {
		t.Fatal(err)
	}
	if err := model.RecordInferenceSettlementRetry(ctx, "text:retry-charge", 40, `{"prompt_tokens":10}`); err != nil {
		t.Fatal(err)
	}
	if err := RunInferenceWorkerOnce(ctx, "empty-worker"); err != nil {
		t.Fatal(err)
	}
	var ledger model.InferenceSettlement
	model.DB.Where(map[string]any{"key": "text:retry-charge"}).First(&ledger)
	if ledger.Status != "settled" || ledger.TotalQuota != 40 {
		t.Fatalf("empty queue skipped exact ledger retry: %+v", ledger)
	}
	if err := RunInferenceWorkerOnce(ctx, "empty-worker"); err != nil {
		t.Fatal(err)
	}
	var user model.User
	model.DB.First(&user, 1)
	if user.UsedQuota != 40 {
		t.Fatalf("retry billed twice: %d", user.UsedQuota)
	}
}

func TestMediaCreate_DatedModelRetainsUpstreamID(t *testing.T) {
	setupJobFixture(t)
	dated := "wan2.6-t2v-2025-09-08"
	c, w := jobContext(`{"model":"wan2.6-t2v","prompt":"test"}`, 1)
	c.Set(ctxkey.ModelMapping, map[string]string{"wan2.6-t2v": dated})
	CreateMediaJob(c)
	if w.Code != 202 {
		t.Fatalf("dated native family rejected: %d %s", w.Code, w.Body)
	}
	var j model.InferenceJob
	model.DB.First(&j)
	var body map[string]any
	json.Unmarshal([]byte(j.RequestJSON), &body)
	if j.UpstreamModel != dated || body["model"] != dated {
		t.Fatalf("wire model rewritten: %+v", j)
	}
}

func TestMediaWorker_PermanentRefundFailureStopsRetry(t *testing.T) {
	for _, finalized := range []bool{false, true} {
		t.Run(fmt.Sprintf("finalized=%v", finalized), func(t *testing.T) {
			setupJobFixture(t)
			id := "refund-permanent"
			ctx := context.Background()
			if finalized {
				if err := model.ReserveInference(ctx, model.InferenceReservation{Key: "job:" + id, UserID: 1, TokenID: 1, Quota: 100}); err != nil {
					t.Fatal(err)
				}
				if err := model.SettleInference(ctx, "job:"+id, 40, `[]`); err != nil {
					t.Fatal(err)
				}
			}
			model.DB.Create(&model.InferenceJob{ID: id, UserID: 1, TokenID: 1, Status: "failed", AssetStatus: "unavailable", SettlementStatus: "pending", CreatedAt: time.Now().Unix()})
			advanceJob(t, id)
			row := fetchTestJob(t, id)
			if row.SettlementStatus != "pending_reconciliation" || row.ErrorCode != "refund_reconciliation_required" {
				t.Fatalf("permanent refund retried: %+v", row)
			}
			advanceJob(t, id)
			if row = fetchTestJob(t, id); row.RetryCount != 0 || row.LeaseOwner != "" {
				t.Fatalf("manual reconciliation job was reclaimed: %+v", row)
			}
			var user model.User
			model.DB.First(&user, 1)
			if finalized && (user.UsedQuota != 40 || user.Quota != 9960) {
				t.Fatalf("conflicting refund changed balance: %+v", user)
			}
		})
	}
}

func TestMediaWorker_TransientRefundFailureBacksOff(t *testing.T) {
	setupJobFixture(t)
	ctx := context.Background()
	id := "refund-transient"
	if err := model.ReserveInference(ctx, model.InferenceReservation{Key: "job:" + id, UserID: 1, TokenID: 1, Quota: 100}); err != nil {
		t.Fatal(err)
	}
	model.DB.Create(&model.InferenceJob{ID: id, UserID: 1, TokenID: 1, Status: "canceled", AssetStatus: "unavailable", SettlementStatus: "pending", CreatedAt: time.Now().Unix()})
	name := "test:transient-ledger-read"
	model.DB.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*model.InferenceSettlement); ok {
			tx.AddError(errors.New("temporary database connection failure"))
		}
	})
	t.Cleanup(func() { model.DB.Callback().Query().Remove(name) })
	before := time.Now().Unix()
	advanceJob(t, id)
	row := fetchTestJob(t, id)
	if row.SettlementStatus != "pending" || row.RetryCount != 1 || row.NextPollAt-before < 5 {
		t.Fatalf("transient error did not back off: %+v", row)
	}
}
