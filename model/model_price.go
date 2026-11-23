package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/modelbus/one-api-pro/common"
	"github.com/modelbus/one-api-pro/common/helper"
	"github.com/modelbus/one-api-pro/common/logger"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ModelPrice struct {
	Id              uint    `gorm:"primaryKey;autoIncrement" json:"id"`
	ModelName       string  `gorm:"type:varchar(100);uniqueIndex;not null" json:"model_name"`
	InputPrice      float64 `gorm:"type:decimal(16,6);default:0;not null" json:"input_price"`
	OutputPrice     float64 `gorm:"type:decimal(16,6);default:0;not null" json:"output_price"`
	CachedPrice     float64 `gorm:"type:decimal(16,6);default:0;not null" json:"cached_price"`
	PerRequestPrice float64 `gorm:"type:decimal(16,6);default:0;not null" json:"per_request_price"`
	BillingType     string  `gorm:"type:varchar(20);default:'token';not null" json:"billing_type"`
	Enabled         bool    `gorm:"default:true;not null" json:"enabled"`
	CreatedAt       int64   `json:"created_at" gorm:"bigint;default:0"`
	UpdatedAt       int64   `json:"updated_at" gorm:"bigint;default:0"`
}

type GroupPrice struct {
	Id        uint    `gorm:"primaryKey;autoIncrement" json:"id"`
	GroupName string  `gorm:"type:varchar(32);uniqueIndex:idx_group_model;not null" json:"group_name"`
	ModelName string  `gorm:"type:varchar(100);uniqueIndex:idx_group_model;default:'';not null" json:"model_name"`
	Discount  float64 `gorm:"type:decimal(10,4);default:1;not null" json:"discount"`
	CreatedAt int64   `json:"created_at" gorm:"bigint;default:0"`
	UpdatedAt int64   `json:"updated_at" gorm:"bigint;default:0"`
}

var modelPriceCache sync.RWMutex
var modelPriceMap map[string]*ModelPrice
var groupPriceCache sync.RWMutex
var groupPriceMap map[string]map[string]float64

var ModelPriceCacheSeconds = 300

// FallbackModelName 是「兜底价」在 model_price 表中的保留模型名。
// 任何在表中找不到精确/通配匹配的模型，最终都按这条记录的单价计费，
// 避免新增模型因未登记价格而直接 422 拒绝。管理员可在「模型定价」页编辑它。
// 版本: v0.0.28
// 日期: 2026-11-19
const FallbackModelName = "*"

// fallbackPrice 是兜底价的初始值。本系统暂不对外开放，价格合理性不作要求，
// 故默认 0 价——新增模型无需任何配置即可直接调用。管理员可在「定价 → 兜底价格」
// 里随时改成非零单价。该值仅在首次插入时生效，之后的修改不会被启动流程覆盖。
// 版本: v0.0.28
// 日期: 2026-11-19
var fallbackPrice = ModelPrice{
	ModelName:   FallbackModelName,
	InputPrice:  0,
	OutputPrice: 0,
	CachedPrice: 0,
	BillingType: BillingTypeToken,
	Enabled:     true,
}

func InitModelPriceCache() {
	modelPriceCache.Lock()
	defer modelPriceCache.Unlock()
	var prices []*ModelPrice
	if err := DB.Where("enabled = ?", true).Find(&prices).Error; err != nil {
		logger.SysError("failed to load model prices: " + err.Error())
		return
	}
	modelPriceMap = make(map[string]*ModelPrice)
	for _, p := range prices {
		modelPriceMap[p.ModelName] = p
	}
}

func InitGroupPriceCache() {
	groupPriceCache.Lock()
	defer groupPriceCache.Unlock()
	var prices []*GroupPrice
	if err := DB.Find(&prices).Error; err != nil {
		logger.SysError("failed to load group prices: " + err.Error())
		return
	}
	groupPriceMap = make(map[string]map[string]float64)
	for _, p := range prices {
		if groupPriceMap[p.GroupName] == nil {
			groupPriceMap[p.GroupName] = make(map[string]float64)
		}
		groupPriceMap[p.GroupName][p.ModelName] = p.Discount
	}
}

func SyncModelPriceCache(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		logger.SysLog("syncing model prices from database")
		InitModelPriceCache()
	}
}

func SyncGroupPriceCache(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		logger.SysLog("syncing group prices from database")
		InitGroupPriceCache()
	}
}

func GetModelPrice(modelName string) (*ModelPrice, bool) {
	modelPriceCache.RLock()
	defer modelPriceCache.RUnlock()
	if modelPriceMap == nil {
		return nil, false
	}
	p, ok := modelPriceMap[modelName]
	return p, ok
}

func GetGroupDiscount(groupName string, modelName string) float64 {
	groupPriceCache.RLock()
	defer groupPriceCache.RUnlock()
	if groupPriceMap == nil {
		return 1.0
	}
	groupModels, ok := groupPriceMap[groupName]
	if !ok {
		return 1.0
	}
	if discount, ok := groupModels[modelName]; ok {
		return discount
	}
	if discount, ok := groupModels[""]; ok {
		return discount
	}
	return 1.0
}

func GetAllModelPrices() ([]*ModelPrice, error) {
	var prices []*ModelPrice
	err := DB.Order("model_name asc").Find(&prices).Error
	return prices, err
}

func GetAllGroupPrices() ([]*GroupPrice, error) {
	var prices []*GroupPrice
	err := DB.Order("group_name asc, model_name asc").Find(&prices).Error
	return prices, err
}

func (p *ModelPrice) Insert() error {
	p.CreatedAt = helper.GetTimestamp()
	p.UpdatedAt = helper.GetTimestamp()
	// Insert 是「新增」语义：无论调用方是否在 struct 上预置了 Id，都强制走主键自增。
	// 防御性兜底：避免前端/其他调用方把编辑态的旧 id 误传到 Insert() 时撞主键冲突。
	p.Id = 0
	return DB.Create(p).Error
}

func (p *ModelPrice) Update() error {
	p.UpdatedAt = helper.GetTimestamp()
	return DB.Model(p).Select("input_price", "output_price", "cached_price",
		"per_request_price", "billing_type", "enabled", "updated_at").Updates(p).Error
}

func DeleteModelPriceById(id int) error {
	// 先查询再删除，确保 GORM 回调能取到正确的 Id
	var mp ModelPrice
	if err := DB.First(&mp, "id = ?", id).Error; err != nil {
		return err
	}
	return DB.Delete(&mp).Error
}

func (p *GroupPrice) Insert() error {
	p.CreatedAt = helper.GetTimestamp()
	p.UpdatedAt = helper.GetTimestamp()
	// Insert 是「新增」语义：无论调用方是否在 struct 上预置了 Id，都强制走主键自增。
	// 防御性兜底：避免前端/其他调用方把编辑态的旧 id 误传到 Insert() 时撞主键冲突。
	p.Id = 0
	return DB.Create(p).Error
}

func (p *GroupPrice) Update() error {
	p.UpdatedAt = helper.GetTimestamp()
	return DB.Model(p).Select("discount", "updated_at").Updates(p).Error
}

func DeleteGroupPriceById(id int) error {
	// 先查询再删除，确保 GORM 回调能取到正确的 Id
	var gp GroupPrice
	if err := DB.First(&gp, "id = ?", id).Error; err != nil {
		return err
	}
	return DB.Delete(&gp).Error
}

func GetGroupNames() []string {
	groupPriceCache.RLock()
	defer groupPriceCache.RUnlock()
	names := make([]string, 0, len(groupPriceMap))
	for name := range groupPriceMap {
		names = append(names, name)
	}
	return names
}

var defaultModelPrices = []ModelPrice{
	{ModelName: "gpt-4o", InputPrice: 2.5, OutputPrice: 10, CachedPrice: 1.25, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "gpt-4o-mini", InputPrice: 0.15, OutputPrice: 0.6, CachedPrice: 0.075, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "gpt-4-turbo", InputPrice: 10, OutputPrice: 30, CachedPrice: 5, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "gpt-4", InputPrice: 30, OutputPrice: 60, CachedPrice: 30, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "gpt-3.5-turbo", InputPrice: 0.5, OutputPrice: 1.5, CachedPrice: 0.5, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "o1", InputPrice: 15, OutputPrice: 60, CachedPrice: 7.5, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "o1-mini", InputPrice: 3, OutputPrice: 12, CachedPrice: 1.5, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "o3-mini", InputPrice: 1.1, OutputPrice: 4.4, CachedPrice: 0.55, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "claude-3.5-sonnet", InputPrice: 3, OutputPrice: 15, CachedPrice: 0.3, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "claude-3.5-haiku", InputPrice: 1, OutputPrice: 5, CachedPrice: 0.1, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "claude-3-opus", InputPrice: 15, OutputPrice: 75, CachedPrice: 1.5, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "claude-3-haiku", InputPrice: 0.25, OutputPrice: 1.25, CachedPrice: 0.025, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "claude-3-sonnet", InputPrice: 3, OutputPrice: 15, CachedPrice: 0.3, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "deepseek-chat", InputPrice: 0.14, OutputPrice: 0.28, CachedPrice: 0.014, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "deepseek-reasoner", InputPrice: 0.55, OutputPrice: 2.19, CachedPrice: 0.14, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "dall-e-2", PerRequestPrice: 0.016, BillingType: BillingTypePerRequest, Enabled: true},
	{ModelName: "dall-e-3", PerRequestPrice: 0.04, BillingType: BillingTypePerRequest, Enabled: true},
	// 文生图：百炼 wan2.7 系列（支持自定义 size，按张计费）。
	{ModelName: "wan2.7-image", PerRequestPrice: 0.04, BillingType: BillingTypePerRequest, Enabled: true},
	{ModelName: "wan2.7-image-pro", PerRequestPrice: 0.08, BillingType: BillingTypePerRequest, Enabled: true},
	{ModelName: "whisper-1", InputPrice: 0.006, OutputPrice: 0, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "tts-1", InputPrice: 0.015, OutputPrice: 0, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "tts-1-hd", InputPrice: 0.03, OutputPrice: 0, BillingType: BillingTypeToken, Enabled: true},
	// 字符型 TTS：InputPrice 是每百万 Unicode 字符的网关售价（元）。
	// Qwen 3.1 Flash 使用上游 input/output tokens；其 InputPrice/OutputPrice 为每百万 token 售价。
	// 日期: 2026-11-23
	{ModelName: "cosyvoice-v3-flash", InputPrice: 0.8, OutputPrice: 0, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "cosyvoice-v3-plus", InputPrice: 1.6, OutputPrice: 0, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "qwen-audio-3.1-tts-flash", InputPrice: 1.2, OutputPrice: 0, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "qwen-audio-3.1-tts-plus", InputPrice: 2.4, OutputPrice: 0, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "text-embedding-3-small", InputPrice: 0.02, OutputPrice: 0, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "text-embedding-3-large", InputPrice: 0.13, OutputPrice: 0, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "gemini-1.5-pro", InputPrice: 1.25, OutputPrice: 5, CachedPrice: 0.3125, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "gemini-1.5-flash", InputPrice: 0.075, OutputPrice: 0.3, CachedPrice: 0.01875, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "gemini-2.0-flash", InputPrice: 0.1, OutputPrice: 0.4, CachedPrice: 0.025, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "gemini-2.0-flash-lite", InputPrice: 0.025, OutputPrice: 0.1, CachedPrice: 0.00625, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "qwen-max", InputPrice: 2.4, OutputPrice: 2.4, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "qwen-plus", InputPrice: 0.8, OutputPrice: 0.8, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "qwen-turbo", InputPrice: 0.3, OutputPrice: 0.3, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "glm-4-plus", InputPrice: 50, OutputPrice: 50, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "glm-4-flash", InputPrice: 0.1, OutputPrice: 0.1, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "moonshot-v1-8k", InputPrice: 12, OutputPrice: 12, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "moonshot-v1-32k", InputPrice: 24, OutputPrice: 24, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "moonshot-v1-128k", InputPrice: 60, OutputPrice: 60, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "hunyuan-turbo", InputPrice: 15, OutputPrice: 15, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "step-2-16k", InputPrice: 38, OutputPrice: 38, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "SparkDesk", InputPrice: 18, OutputPrice: 18, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "mistral-large-latest", InputPrice: 2, OutputPrice: 6, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "mistral-small-latest", InputPrice: 0.2, OutputPrice: 0.6, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "command-r-plus", InputPrice: 2.5, OutputPrice: 10, BillingType: BillingTypeToken, Enabled: true},
	{ModelName: "command-r", InputPrice: 0.5, OutputPrice: 1.5, BillingType: BillingTypeToken, Enabled: true},
}

var defaultGroupPrices = []GroupPrice{
	{GroupName: "default", ModelName: "", Discount: 1.0},
	{GroupName: "vip", ModelName: "", Discount: 1.0},
	{GroupName: "svip", ModelName: "", Discount: 1.0},
}

func InitDefaultPrices() error {
	var count int64
	DB.Model(&ModelPrice{}).Count(&count)
	if count > 0 {
		return nil
	}
	logger.SysLog("initializing default model prices...")
	for i := range defaultModelPrices {
		p := defaultModelPrices[i]
		p.CreatedAt = helper.GetTimestamp()
		p.UpdatedAt = helper.GetTimestamp()
		p.Enabled = true
		if err := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&p).Error; err != nil {
			logger.SysError("failed to insert default model price for " + p.ModelName + ": " + err.Error())
		}
	}
	var groupCount int64
	DB.Model(&GroupPrice{}).Count(&groupCount)
	if groupCount > 0 {
		return nil
	}
	logger.SysLog("initializing default group prices...")
	for i := range defaultGroupPrices {
		p := defaultGroupPrices[i]
		p.CreatedAt = helper.GetTimestamp()
		p.UpdatedAt = helper.GetTimestamp()
		if err := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&p).Error; err != nil {
			logger.SysError("failed to insert default group price: " + err.Error())
		}
	}
	return nil
}

// InitFallbackPrice 幂等确保兜底价记录存在。
// 与 InitDefaultPrices 不同，本函数没有「表非空就跳过」的保护——存量库升级后
// 必须补插这条记录，否则老用户依然会遇到 model price not found。
// 使用 OnConflict DoNothing：管理员在后台改过的单价不会被重启覆盖。
// 版本: v0.0.28
// 日期: 2026-11-19
func InitFallbackPrice() error {
	var count int64
	if err := DB.Model(&ModelPrice{}).Where("model_name = ?", FallbackModelName).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	p := fallbackPrice
	p.CreatedAt = helper.GetTimestamp()
	p.UpdatedAt = helper.GetTimestamp()
	if err := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&p).Error; err != nil {
		return err
	}
	logger.SysLog("initialized fallback model price (*)")
	return nil
}

// GetFallbackPrice 读取兜底价配置（供后台「定价 → 兜底价格」页展示）。
// 直接读库而非缓存，这样即使兜底价被禁用（enabled=false，不会进缓存）也能读到当前配置。
// 若库中无记录（极端情况：被删且尚未重启），返回一个内存中的默认结构（Id=0），
// 前台仍能展示并保存，由 SaveFallbackPrice 负责落库。
// 版本: v0.0.28
// 日期: 2026-11-19
func GetFallbackPrice() (*ModelPrice, error) {
	var p ModelPrice
	err := DB.Where("model_name = ?", FallbackModelName).First(&p).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			fp := fallbackPrice
			return &fp, nil
		}
		return nil, err
	}
	return &p, nil
}

// SaveFallbackPrice 保存兜底价配置：存在则更新全部价格字段与开关，不存在则插入。
// 兜底价的 model_name 恒为保留名 *，忽略调用方传入的任何 model_name。
// 保存后刷新内存缓存，使新价格立即在计费链路生效（无需等 300s 同步周期）。
// 版本: v0.0.28
// 日期: 2026-11-19
func SaveFallbackPrice(inputPrice, outputPrice, cachedPrice, perRequestPrice float64, billingType string, enabled bool) (*ModelPrice, error) {
	if billingType == "" {
		billingType = BillingTypeToken
	}
	now := helper.GetTimestamp()
	var existing ModelPrice
	err := DB.Where("model_name = ?", FallbackModelName).First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		p := ModelPrice{
			ModelName:       FallbackModelName,
			InputPrice:      inputPrice,
			OutputPrice:     outputPrice,
			CachedPrice:     cachedPrice,
			PerRequestPrice: perRequestPrice,
			BillingType:     billingType,
			Enabled:         enabled,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if err := DB.Create(&p).Error; err != nil {
			return nil, err
		}
		InitModelPriceCache()
		return &p, nil
	}
	existing.InputPrice = inputPrice
	existing.OutputPrice = outputPrice
	existing.CachedPrice = cachedPrice
	existing.PerRequestPrice = perRequestPrice
	existing.BillingType = billingType
	existing.Enabled = enabled
	existing.UpdatedAt = now
	if err := DB.Model(&existing).Select("input_price", "output_price", "cached_price",
		"per_request_price", "billing_type", "enabled", "updated_at").Updates(&existing).Error; err != nil {
		return nil, err
	}
	InitModelPriceCache()
	return &existing, nil
}

func ModelPrice2JSONString() string {
	modelPriceCache.RLock()
	defer modelPriceCache.RUnlock()
	if modelPriceMap == nil {
		return "{}"
	}
	jsonBytes, err := json.Marshal(modelPriceMap)
	if err != nil {
		logger.SysError("error marshalling model price map: " + err.Error())
		return "{}"
	}
	return string(jsonBytes)
}

func GroupPrice2JSONString() string {
	groupPriceCache.RLock()
	defer groupPriceCache.RUnlock()
	if groupPriceMap == nil {
		return "{}"
	}
	jsonBytes, err := json.Marshal(groupPriceMap)
	if err != nil {
		logger.SysError("error marshalling group price map: " + err.Error())
		return "{}"
	}
	return string(jsonBytes)
}

func CacheGetModelPrice(modelName string) (*ModelPrice, error) {
	if common.RedisEnabled {
		return cacheGetModelPriceRedis(modelName)
	}
	return cacheGetModelPriceDB(modelName)
}

func cacheGetModelPriceRedis(modelName string) (*ModelPrice, error) {
	key := fmt.Sprintf("model_price:%s", modelName)
	data, err := common.RedisGet(key)
	if err == nil && data != "" {
		var p ModelPrice
		if jsonErr := json.Unmarshal([]byte(data), &p); jsonErr == nil {
			return &p, nil
		}
	}
	p, dbErr := cacheGetModelPriceDB(modelName)
	if dbErr != nil {
		return nil, dbErr
	}
	jsonBytes, _ := json.Marshal(p)
	_ = common.RedisSet(key, string(jsonBytes), time.Duration(ModelPriceCacheSeconds)*time.Second)
	return p, nil
}

func cacheGetModelPriceDB(modelName string) (*ModelPrice, error) {
	var p ModelPrice
	err := DB.Where("model_name = ? AND enabled = ?", modelName, true).First(&p).Error
	return &p, err
}

func CacheGetGroupPrice(groupName string, modelName string) (*GroupPrice, error) {
	if common.RedisEnabled {
		return cacheGetGroupPriceRedis(groupName, modelName)
	}
	return cacheGetGroupPriceDB(groupName, modelName)
}

func cacheGetGroupPriceRedis(groupName string, modelName string) (*GroupPrice, error) {
	key := fmt.Sprintf("group_price:%s:%s", groupName, modelName)
	data, err := common.RedisGet(key)
	if err == nil && data != "" {
		var p GroupPrice
		if jsonErr := json.Unmarshal([]byte(data), &p); jsonErr == nil {
			return &p, nil
		}
	}
	p, dbErr := cacheGetGroupPriceDB(groupName, modelName)
	if dbErr != nil {
		return nil, dbErr
	}
	jsonBytes, _ := json.Marshal(p)
	_ = common.RedisSet(key, string(jsonBytes), time.Duration(ModelPriceCacheSeconds)*time.Second)
	return p, nil
}

func cacheGetGroupPriceDB(groupName string, modelName string) (*GroupPrice, error) {
	var p GroupPrice
	groupCol := "`group_name`"
	modelCol := "`model_name`"
	if common.UsingPostgreSQL {
		groupCol = `"group_name"`
		modelCol = `"model_name"`
	}
	err := DB.Where(groupCol+" = ? AND "+modelCol+" = ?", groupName, modelName).First(&p).Error
	if err != nil {
		if common.UsingPostgreSQL {
			groupCol = `"group_name"`
		}
		err = DB.Where(groupCol+" = ? AND model_name = ?", groupName, "").First(&p).Error
	}
	return &p, err
}

// FindModelPriceByPattern 用「价格表里的 model_name 作为 pattern」去匹配请求模型名，
// 命中最具体的那个 pattern（最长优先）。这是为 gpt-4o-2024-08-06 这类带版本/日期
// 后缀的模型准备的：只要表里存在 gpt-4o，就能覆盖所有以它为子串的变体。
//
// 注意：Go map 遍历顺序随机，旧实现「命中第一个」会让同时匹配多个 pattern 时
// 结果不确定（例如表里同时有 gpt-4o 与 gpt-4o-mini，请求 gpt-4o-mini-2024 时
// 可能错误命中 gpt-4o）。改为最长 pattern 优先，并在同长度时按字典序取，
// 保证确定性。兜底价 FallbackModelName 是字面 "*"，不会作为子串模式命中任何
// 模型名，这里仍显式跳过以防万一。
func FindModelPriceByPattern(modelName string) *ModelPrice {
	modelPriceCache.RLock()
	defer modelPriceCache.RUnlock()
	if modelPriceMap == nil {
		return nil
	}
	if p, ok := modelPriceMap[modelName]; ok {
		return p
	}
	var bestPattern string
	var best *ModelPrice
	for pattern, p := range modelPriceMap {
		if pattern == "" || pattern == FallbackModelName {
			continue
		}
		if !strings.Contains(modelName, pattern) {
			continue
		}
		if best == nil || len(pattern) > len(bestPattern) ||
			(len(pattern) == len(bestPattern) && pattern < bestPattern) {
			bestPattern = pattern
			best = p
		}
	}
	return best
}

func GetModelPriceCacheForTest() *sync.RWMutex {
	return &modelPriceCache
}

func SetModelPriceMapForTest(m map[string]*ModelPrice) {
	modelPriceMap = m
}

func GetGroupPriceCacheForTest() *sync.RWMutex {
	return &groupPriceCache
}

func SetGroupPriceMapForTest(m map[string]map[string]float64) {
	groupPriceMap = m
}
