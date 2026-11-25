package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/common/random"
	"gorm.io/gorm"
)

var (
	TokenCacheSeconds         = config.SyncFrequency
	UserId2GroupCacheSeconds  = config.SyncFrequency
	UserId2QuotaCacheSeconds  = config.SyncFrequency
	UserId2StatusCacheSeconds = config.SyncFrequency
	GroupModelsCacheSeconds   = config.SyncFrequency
)

func CacheGetTokenByKey(key string) (*Token, error) {
	// key 是保留字，列名引用必须按方言生成（PostgreSQL 不接受反引号）。
	keyCol := common.QuoteIdentifier("key")
	var token Token
	if !common.RedisEnabled {
		err := DB.Where(keyCol+" = ?", key).First(&token).Error
		return &token, err
	}
	tokenObjectString, err := common.RedisGet(fmt.Sprintf("token:%s", key))
	if err != nil {
		err := DB.Where(keyCol+" = ?", key).First(&token).Error
		if err != nil {
			return nil, err
		}
		jsonBytes, err := json.Marshal(token)
		if err != nil {
			return nil, err
		}
		err = common.RedisSet(fmt.Sprintf("token:%s", key), string(jsonBytes), time.Duration(TokenCacheSeconds)*time.Second)
		if err != nil {
			logger.SysError("Redis set token error: " + err.Error())
		}
		return &token, nil
	}
	err = json.Unmarshal([]byte(tokenObjectString), &token)
	return &token, err
}

func CacheGetUserGroup(id int) (group string, err error) {
	if !common.RedisEnabled {
		return GetUserGroup(id)
	}
	group, err = common.RedisGet(fmt.Sprintf("user_group:%d", id))
	if err != nil {
		group, err = GetUserGroup(id)
		if err != nil {
			return "", err
		}
		err = common.RedisSet(fmt.Sprintf("user_group:%d", id), group, time.Duration(UserId2GroupCacheSeconds)*time.Second)
		if err != nil {
			logger.SysError("Redis set user group error: " + err.Error())
		}
	}
	return group, err
}

func fetchAndUpdateUserQuota(ctx context.Context, id int) (quota int64, err error) {
	quota, err = GetUserQuota(id)
	if err != nil {
		return 0, err
	}
	err = common.RedisSet(fmt.Sprintf("user_quota:%d", id), fmt.Sprintf("%d", quota), time.Duration(UserId2QuotaCacheSeconds)*time.Second)
	if err != nil {
		logger.Error(ctx, "Redis set user quota error: "+err.Error())
	}
	return
}

// userQuotaLowWaterMark is the cache freshness threshold for user_quota:<id>.
// When the cached value drops to or below this mark, the next read will refresh
// from DB to avoid serving a stale value that no longer reflects users.quota.
//
// Why 50_000 (instead of config.PreConsumedQuota = 500):
//   - Single pre-consume for high-price models (e.g. minimax-m3 ¥8.40/1M tokens)
//     can be ~80,000 quota, far above 500.
//   - With the old threshold, the cache could drift into the dead zone
//     [501, 80_000) — large enough to look "has quota" but too small to cover
//     the next pre-consume — and stay there indefinitely until it dropped
//     further to ≤500, producing spurious 403 "insufficient_user_quota".
//   - 50_000 keeps the dead zone narrow (~30K wide vs ~80K before) and well
//     below typical single-request pre-consume amounts.
//
// userQuotaLowWaterMark 是 user_quota:<id> 的缓存新鲜度阈值。
// 缓存值跌至或低于该阈值时，下次读取会回源 DB 刷新，避免返回已偏离
// users.quota 的陈旧值。
//
// 设为 50_000（而非 config.PreConsumedQuota = 500）的原因：
//   - 高单价模型（如 minimax-m3 ¥8.40/百万 tokens）单次预扣可达 ~80,000 quota，
//     远超 500。
//   - 旧阈值下，缓存可能漂移到 [501, 80_000) 死区——看似还有钱但已不够
//     下一次预扣——并长期停留，直至进一步跌至 ≤500 才触发刷新，造成
//     误报 403 insufficient_user_quota。
//   - 50_000 让死区收窄到 ~30K（原来 ~80K），且远低于典型单次预扣。
const userQuotaLowWaterMark int64 = 50_000

func CacheGetUserQuota(ctx context.Context, id int) (quota int64, err error) {
	if !common.RedisEnabled {
		return GetUserQuota(id)
	}
	quotaString, err := common.RedisGet(fmt.Sprintf("user_quota:%d", id))
	if err != nil {
		return fetchAndUpdateUserQuota(ctx, id)
	}
	quota, err = strconv.ParseInt(quotaString, 10, 64)
	if err != nil {
		return 0, nil
	}
	if quota <= userQuotaLowWaterMark { // when user's cached quota is at or below the low-water mark, we need to fetch from db
		logger.Infof(ctx, "user %d's cached quota is too low: %d, refreshing from db", quota, id)
		return fetchAndUpdateUserQuota(ctx, id)
	}
	return quota, nil
}

func CacheUpdateUserQuota(ctx context.Context, id int) error {
	if !common.RedisEnabled {
		return nil
	}
	quota, err := CacheGetUserQuota(ctx, id)
	if err != nil {
		return err
	}
	err = common.RedisSet(fmt.Sprintf("user_quota:%d", id), fmt.Sprintf("%d", quota), time.Duration(UserId2QuotaCacheSeconds)*time.Second)
	return err
}

// CacheInvalidateUserQuota forces the next balance read to load the committed database value.
// 版本: v0.0.28
// 日期: 2026-11-22
func CacheInvalidateUserQuota(id int) error {
	if !common.RedisEnabled {
		return nil
	}
	if common.RDB == nil {
		return errors.New("Redis client is not initialized")
	}
	return common.RedisDel(fmt.Sprintf("user_quota:%d", id))
}

func CacheDecreaseUserQuota(id int, quota int64) error {
	if !common.RedisEnabled {
		return nil
	}
	err := common.RedisDecrease(fmt.Sprintf("user_quota:%d", id), int64(quota))
	return err
}

// CacheIncreaseUserQuota rolls back a previous CacheDecreaseUserQuota when the
// matching DB-side write fails (e.g. PreConsumeTokenQuota rejected the token).
// Without this rollback, the Redis cache would drift further from users.quota
// every time the post-decrease DB write fails, eventually landing in the dead
// zone [lowWaterMark, preConsumedQuota) and causing spurious 403.
//
// CacheIncreaseUserQuota 在对应的 DB 写入失败（如 PreConsumeTokenQuota 拒绝该
// token）时，回滚此前 CacheDecreaseUserQuota 的扣减。若不回滚，每次 DB 写入
// 失败都会让 Redis 缓存进一步偏离 users.quota，最终落入死区
// [lowWaterMark, preConsumedQuota)，导致误报 403。
func CacheIncreaseUserQuota(id int, quota int64) error {
	if !common.RedisEnabled {
		return nil
	}
	err := common.RedisIncrease(fmt.Sprintf("user_quota:%d", id), int64(quota))
	return err
}

func CacheIsUserEnabled(userId int) (bool, error) {
	if !common.RedisEnabled {
		return IsUserEnabled(userId)
	}
	enabled, err := common.RedisGet(fmt.Sprintf("user_enabled:%d", userId))
	if err == nil {
		return enabled == "1", nil
	}

	userEnabled, err := IsUserEnabled(userId)
	if err != nil {
		return false, err
	}
	enabled = "0"
	if userEnabled {
		enabled = "1"
	}
	err = common.RedisSet(fmt.Sprintf("user_enabled:%d", userId), enabled, time.Duration(UserId2StatusCacheSeconds)*time.Second)
	if err != nil {
		logger.SysError("Redis set user enabled error: " + err.Error())
	}
	return userEnabled, err
}

func CacheGetGroupModels(ctx context.Context, group string) ([]string, error) {
	if !common.RedisEnabled {
		return GetGroupModels(ctx, group)
	}
	modelsStr, err := common.RedisGet(fmt.Sprintf("group_models:%s", group))
	if err == nil {
		// 空结果不写入缓存（见下方），但若历史数据已写入空串，读回时
		// strings.Split("", ",") 会得到 [""]，这里显式过滤，避免前端出现空白模型项。
		if modelsStr == "" {
			return []string{}, nil
		}
		return strings.Split(modelsStr, ","), nil
	}
	models, err := GetGroupModels(ctx, group)
	if err != nil {
		return nil, err
	}
	// 空结果不缓存，否则 abilities/channels 后续补上数据后仍会读到旧空值，
	// 最长 stale 一个 GroupModelsCacheSeconds 周期。
	if len(models) == 0 {
		return models, nil
	}
	err = common.RedisSet(fmt.Sprintf("group_models:%s", group), strings.Join(models, ","), time.Duration(GroupModelsCacheSeconds)*time.Second)
	if err != nil {
		logger.SysError("Redis set group models error: " + err.Error())
	}
	return models, nil
}

// InvalidateGroupModelsCache clears the derived model list after channel or
// ability changes so the token editor does not show stale models.
func InvalidateGroupModelsCache(groups ...string) {
	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	seen := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		if err := common.RedisDel(fmt.Sprintf("group_models:%s", group)); err != nil {
			logger.SysError(fmt.Sprintf("failed to invalidate group model cache for %s: %s", group, err.Error()))
		}
	}
}

var group2model2channels map[string]map[string][]*Channel
var channelSyncLock sync.RWMutex
var channelId2channel map[int]*Channel

func InitChannelCache() {
	newChannelId2channel := make(map[int]*Channel)
	var channels []*Channel
	DB.Where("status = ?", ChannelStatusEnabled).Find(&channels)
	for _, channel := range channels {
		newChannelId2channel[channel.Id] = channel
	}
	var abilities []*Ability
	DB.Find(&abilities)
	groups := make(map[string]bool)
	for _, ability := range abilities {
		groups[ability.Group] = true
	}
	newGroup2model2channels := make(map[string]map[string][]*Channel)
	for group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		newGroup2model2channels[group] = make(map[string][]*Channel)
	}
	type wildcardChannel struct {
		channel *Channel
		groups  []string
	}
	var wildcardChannels []wildcardChannel
	for _, channel := range channels {
		groups := uniqueTrimmedValues(strings.Split(channel.Group, ","))
		models := uniqueTrimmedValues(strings.Split(channel.Models, ","))
		isWildcard := len(models) == 1 && models[0] == "*"
		if isWildcard {
			wildcardChannels = append(wildcardChannels, wildcardChannel{channel: channel, groups: groups})
			continue
		}
		for _, group := range groups {
			model2channels, ok := newGroup2model2channels[group]
			if !ok {
				model2channels = make(map[string][]*Channel)
				newGroup2model2channels[group] = model2channels
			}
			for _, model := range models {
				if _, ok := model2channels[model]; !ok {
					model2channels[model] = make([]*Channel, 0)
				}
				model2channels[model] = append(model2channels[model], channel)
			}
		}
	}
	// Wildcard channels: map to every model that exists in their group
	for _, wc := range wildcardChannels {
		for _, group := range wc.groups {
			model2channels, ok := newGroup2model2channels[group]
			if !ok {
				model2channels = make(map[string][]*Channel)
				newGroup2model2channels[group] = model2channels
			}
			for model := range model2channels {
				model2channels[model] = append(model2channels[model], wc.channel)
			}
		}
	}

	// sort by priority
	for group, model2channels := range newGroup2model2channels {
		for model, channels := range model2channels {
			sort.Slice(channels, func(i, j int) bool {
				return channels[i].GetPriority() > channels[j].GetPriority()
			})
			newGroup2model2channels[group][model] = channels
		}
	}

	channelSyncLock.Lock()
	group2model2channels = newGroup2model2channels
	channelId2channel = newChannelId2channel
	channelSyncLock.Unlock()
	logger.SysLog("channels synced from database")
}

func SyncChannelCache(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		logger.SysLog("syncing channels from database")
		InitChannelCache()
	}
}

func CacheGetRandomSatisfiedChannel(group string, model string, ignoreFirstPriority bool) (*Channel, error) {
	if !config.MemoryCacheEnabled {
		return GetRandomSatisfiedChannel(group, model, ignoreFirstPriority)
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	channels := group2model2channels[group][model]
	if len(channels) == 0 {
		return nil, errors.New("channel not found")
	}
	endIdx := len(channels)
	// choose by priority
	firstChannel := channels[0]
	if firstChannel.GetPriority() > 0 {
		for i := range channels {
			if channels[i].GetPriority() != firstChannel.GetPriority() {
				endIdx = i
				break
			}
		}
	}
	idx := rand.Intn(endIdx)
	if ignoreFirstPriority {
		if endIdx < len(channels) { // which means there are more than one priority
			idx = random.RandRange(endIdx, len(channels))
		}
	}
	return channels[idx], nil
}

// channelCandidateQuery 构建「启用渠道 + 命中分组 + 模型名子串」的候选查询。
// Builds the candidate query (enabled channels, matching group, model substring).
//
// 单独抽出来是为了能用 PostgreSQL 方言做 DryRun 校验列名引用：
// group 是保留字，硬编码 MySQL 反引号会让线上 PostgreSQL 整条 SQL 语法错误。
//
// 版本: v0.0.28
// 日期: 2026-11-25
func channelCandidateQuery(db *gorm.DB, group, modelName string) *gorm.DB {
	return db.Where("status = ? AND (models LIKE ? OR models = '*') AND "+groupColumnRef()+" LIKE ?",
		ChannelStatusEnabled, "%"+modelName+"%", "%"+group+"%").
		Order("priority DESC")
}

func GetChannelCandidates(group string, modelName string) []*Channel {
	if !config.MemoryCacheEnabled {
		var channels []*Channel
		// 列名必须按方言引用（groupColumnRef），详见 channelCandidateQuery 注释。
		err := channelCandidateQuery(DB, group, modelName).Find(&channels).Error
		if err != nil {
			logger.SysError("failed to get channel candidates: " + err.Error())
			return nil
		}
		sort.Slice(channels, func(i, j int) bool {
			return channels[i].GetPriority() > channels[j].GetPriority()
		})
		return channels
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	channels := group2model2channels[group][modelName]
	if len(channels) == 0 {
		return nil
	}
	result := make([]*Channel, len(channels))
	copy(result, channels)
	return result
}

func CacheGetChannelById(channelId int) (*Channel, bool) {
	if !config.MemoryCacheEnabled {
		return nil, false
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	ch, ok := channelId2channel[channelId]
	return ch, ok
}

// GetFallbackChannel returns an enabled fallback channel for the given user
// group. It returns nil if no fallback channel is configured for that group.
// Among fallback channels with the same priority, the pick is random.
//
// Note: cooldown / concurrency / RPM are intentionally NOT checked here — the
// caller (controller/relay.go) acquires concurrency via channelrouter after
// picking, and a failed fallback call simply returns the error to the client.
func GetFallbackChannel(group string) (*Channel, error) {
	if group == "" {
		return nil, errors.New("group is empty")
	}
	var channels []*Channel
	// group 是保留字，列名引用必须按方言生成（PostgreSQL 不接受反引号）。
	groupCol := groupColumnRef()
	err := DB.Where("status = ? AND is_fallback = ? AND "+groupCol+" LIKE ?",
		ChannelStatusEnabled, true, "%"+group+"%").
		Order("fallback_priority DESC, id DESC").
		Find(&channels).Error
	if err != nil {
		logger.SysError("failed to query fallback channels: " + err.Error())
		return nil, err
	}
	if len(channels) == 0 {
		return nil, nil
	}

	// Walk the priority tier; skip channels that have no models configured.
	highest := channels[0].GetFallbackPriority()
	tier := make([]*Channel, 0, len(channels))
	for _, ch := range channels {
		if ch.GetFallbackPriority() != highest {
			break
		}
		if strings.TrimSpace(ch.Models) == "" {
			continue
		}
		tier = append(tier, ch)
	}
	if len(tier) == 0 {
		return nil, nil
	}
	return tier[rand.Intn(len(tier))], nil
}
