// refund_cache_test.go 退款后余额缓存失效与失败边界回归
// Verifies refunds never rebuild missing quota caches from a refund delta.
// 版本: v0.0.28
// 日期: 2026-11-22
// 作者: Codex
package billing

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/model"
	"gorm.io/gorm"
)

type refundCacheRedis struct {
	redis.Cmdable
	values             map[string]string
	delError           error
	expireBeforeIncrBy bool
}

func (r *refundCacheRedis) Get(ctx context.Context, key string) *redis.StringCmd {
	if value, ok := r.values[key]; ok {
		return redis.NewStringResult(value, nil)
	}
	return redis.NewStringResult("", redis.Nil)
}

func (r *refundCacheRedis) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) *redis.StatusCmd {
	r.values[key] = fmt.Sprint(value)
	return redis.NewStatusResult("OK", nil)
}

func (r *refundCacheRedis) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	if r.delError != nil {
		return redis.NewIntResult(0, r.delError)
	}
	var deleted int64
	for _, key := range keys {
		if _, ok := r.values[key]; ok {
			delete(r.values, key)
			deleted++
		}
	}
	return redis.NewIntResult(deleted, nil)
}

func (r *refundCacheRedis) IncrBy(ctx context.Context, key string, amount int64) *redis.IntCmd {
	if r.expireBeforeIncrBy {
		delete(r.values, key)
	}
	value, _ := strconv.ParseInt(r.values[key], 10, 64)
	value += amount
	r.values[key] = strconv.FormatInt(value, 10)
	return redis.NewIntResult(value, nil)
}

func setupRefundCache(t *testing.T) (*gorm.DB, *refundCacheRedis) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.AutoMigrate(&model.User{}, &model.Token{}); err != nil {
		t.Fatal(err)
	}
	user := model.User{Id: 1, Username: "refund-cache", Quota: 440000, AccessToken: "refund", AffCode: "refund"}
	if err = db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	token := model.Token{Id: 1, UserId: 1, Key: "refund", RemainQuota: 440000, UsedQuota: 60000}
	if err = db.Create(&token).Error; err != nil {
		t.Fatal(err)
	}
	oldDB, oldRedis, oldEnabled, oldBatch := model.DB, common.RDB, common.RedisEnabled, config.BatchUpdateEnabled
	r := &refundCacheRedis{values: map[string]string{}}
	model.DB, common.RDB, common.RedisEnabled, config.BatchUpdateEnabled = db, r, true, false
	t.Cleanup(func() {
		model.DB, common.RDB, common.RedisEnabled, config.BatchUpdateEnabled = oldDB, oldRedis, oldEnabled, oldBatch
		sqlDB, _ := db.DB()
		sqlDB.Close()
	})
	return db, r
}

func assertRefundBalances(t *testing.T, db *gorm.DB, wantUser, wantToken int64) {
	t.Helper()
	var user model.User
	var token model.Token
	if err := db.First(&user, 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&token, 1).Error; err != nil {
		t.Fatal(err)
	}
	if user.Quota != wantUser || token.RemainQuota != wantToken {
		t.Fatalf("user/token balances = %d/%d, want %d/%d", user.Quota, token.RemainQuota, wantUser, wantToken)
	}
}

// TestReturnPreConsumedQuotaSync_InvalidateCache prevents both missing-key overcredit and expiry-race undercredit.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestReturnPreConsumedQuotaSync_InvalidateCache(t *testing.T) {
	for _, scenario := range []string{"missing", "present", "expires-before-increment"} {
		t.Run(scenario, func(t *testing.T) {
			db, r := setupRefundCache(t)
			if scenario != "missing" {
				r.values["user_quota:1"] = "440000"
			}
			r.expireBeforeIncrBy = scenario == "expires-before-increment"
			// 60,000 exceeds the existing low-water mark, so a delta-only cache would survive reads.
			if err := ReturnPreConsumedQuotaSync(context.Background(), 60000, 1, 1); err != nil {
				t.Fatal(err)
			}
			assertRefundBalances(t, db, 500000, 500000)
			_, existedAfterRefund := r.values["user_quota:1"]
			quota, err := model.CacheGetUserQuota(context.Background(), 1)
			if err != nil || quota != 500000 || r.values["user_quota:1"] != "500000" {
				t.Fatalf("refill = %d, %v; cache = %q", quota, err, r.values["user_quota:1"])
			}
			if existedAfterRefund {
				t.Fatal("refund left a cached balance instead of invalidating it")
			}
		})
	}
}

// TestReturnPreConsumedQuotaSync_CacheFailureDoesNotRepeatRefund keeps a committed refund successful.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestReturnPreConsumedQuotaSync_CacheFailureDoesNotRepeatRefund(t *testing.T) {
	db, r := setupRefundCache(t)
	r.values["user_quota:1"] = "440000"
	r.delError = errors.New("Redis unavailable")
	if err := ReturnPreConsumedQuotaSync(context.Background(), 60000, 1, 1); err != nil {
		t.Fatalf("committed refund was reported as failed: %v", err)
	}
	assertRefundBalances(t, db, 500000, 500000)
	if r.values["user_quota:1"] != "440000" {
		t.Fatal("cache failure unexpectedly changed the cached quota")
	}
}

// TestReturnPreConsumedQuotaSync_DatabaseFailure stops before cache invalidation or token credit.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestReturnPreConsumedQuotaSync_DatabaseFailure(t *testing.T) {
	for _, scenario := range []string{"missing-token", "user-update-fails", "unlimited-user-update-fails"} {
		t.Run(scenario, func(t *testing.T) {
			db, r := setupRefundCache(t)
			r.values["user_quota:1"] = "440000"
			tokenID := 1
			if scenario == "missing-token" {
				tokenID = 99
			} else {
				if scenario == "unlimited-user-update-fails" {
					if err := db.Model(&model.Token{}).Where("id = ?", 1).Update("unlimited_quota", true).Error; err != nil {
						t.Fatal(err)
					}
				}
				db.Callback().Update().Before("gorm:update").Register("refund-user-update-failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "users" {
						tx.AddError(errors.New("user quota update failed"))
					}
				})
			}
			if err := ReturnPreConsumedQuotaSync(context.Background(), 60000, tokenID, 1); err == nil {
				t.Fatal("database failure was reported as successful refund")
			}
			assertRefundBalances(t, db, 440000, 440000)
			if r.values["user_quota:1"] != "440000" {
				t.Fatal("database failure changed the cache")
			}
		})
	}
}

// TestReturnPreConsumedQuotaSync_NoRedisAndZero keeps disabled-cache and zero-refund paths unchanged.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestReturnPreConsumedQuotaSync_NoRedisAndZero(t *testing.T) {
	db, r := setupRefundCache(t)
	common.RedisEnabled = false
	if err := ReturnPreConsumedQuotaSync(context.Background(), 60000, 1, 1); err != nil {
		t.Fatal(err)
	}
	assertRefundBalances(t, db, 500000, 500000)
	common.RedisEnabled = true
	r.values["user_quota:1"] = "500000"
	if err := ReturnPreConsumedQuotaSync(context.Background(), 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	assertRefundBalances(t, db, 500000, 500000)
	if r.values["user_quota:1"] != "500000" {
		t.Fatal("zero refund invalidated an unrelated cache")
	}
}

// TestCacheInvalidateUserQuota_NoClient handles disabled or uninitialized Redis without a panic.
// 版本: v0.0.28
// 日期: 2026-11-22
func TestCacheInvalidateUserQuota_NoClient(t *testing.T) {
	oldEnabled, oldRedis := common.RedisEnabled, common.RDB
	t.Cleanup(func() { common.RedisEnabled, common.RDB = oldEnabled, oldRedis })
	common.RedisEnabled, common.RDB = false, nil
	if err := model.CacheInvalidateUserQuota(1); err != nil {
		t.Fatal(err)
	}
	common.RedisEnabled = true
	if err := model.CacheInvalidateUserQuota(1); err == nil {
		t.Fatal("enabled but uninitialized Redis was silently ignored")
	}
}
