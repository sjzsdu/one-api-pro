package ratio

import (
	"fmt"
	"math"
	"strings"

	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/model"
)

const Million = 1_000_000.0

type PriceResult struct {
	InputPrice      float64
	OutputPrice     float64
	CachedPrice     float64
	PerRequestPrice float64
	BillingType     string
	Discount        float64
	Found           bool
	// IsFallback 表示价格来自兜底价（保留模型名 *），而非该模型的精确/通配定价。
	// 用于日志与可观测性：提示管理员该模型尚未单独登记定价。
	IsFallback bool
}

func GetModelPrice(modelName string, fallbackModelNames ...string) (*PriceResult, error) {
	tryModelName := modelName
	if strings.HasPrefix(tryModelName, "qwen-") && strings.HasSuffix(tryModelName, "-internet") {
		tryModelName = strings.TrimSuffix(tryModelName, "-internet")
	}
	if strings.HasPrefix(tryModelName, "command-") && strings.HasSuffix(tryModelName, "-internet") {
		tryModelName = strings.TrimSuffix(tryModelName, "-internet")
	}

	mp, found := model.GetModelPrice(tryModelName)
	if !found {
		mp = model.FindModelPriceByPattern(tryModelName)
	}
	if mp == nil {
		for _, fallback := range fallbackModelNames {
			fb := fallback
			if strings.HasPrefix(fb, "qwen-") && strings.HasSuffix(fb, "-internet") {
				fb = strings.TrimSuffix(fb, "-internet")
			}
			if strings.HasPrefix(fb, "command-") && strings.HasSuffix(fb, "-internet") {
				fb = strings.TrimSuffix(fb, "-internet")
			}
			mp, found = model.GetModelPrice(fb)
			if !found {
				mp = model.FindModelPriceByPattern(fb)
			}
			if mp != nil {
				break
			}
		}
	}
	isFallback := false
	if mp == nil {
		// 最后一道兜底：任何未登记定价的模型都按保留名 "*" 的单价计费。
		// 这样新增模型不会因缺价格直接 422 拒绝；管理员可在后台编辑 "*" 定价，
		// 需要单独计费的模型再补一条精确记录即可覆盖兜底。
		if fallback, ok := model.GetModelPrice(model.FallbackModelName); ok && fallback != nil {
			mp = fallback
			isFallback = true
			logger.SysWarnf("model %q has no explicit price, billing with fallback price (*): in=%.4f out=%.4f; add a dedicated price entry if needed",
				modelName, mp.InputPrice, mp.OutputPrice)
		}
	}
	if mp == nil {
		names := append([]string{modelName}, fallbackModelNames...)
		return nil, fmt.Errorf("model price not found for model(s): %v; no explicit price and fallback price (*) is disabled or missing; please add a price entry or enable the fallback price", names)
	}
	return &PriceResult{
		InputPrice:      mp.InputPrice,
		OutputPrice:     mp.OutputPrice,
		CachedPrice:     mp.CachedPrice,
		PerRequestPrice: mp.PerRequestPrice,
		BillingType:     mp.BillingType,
		Found:           true,
		IsFallback:      isFallback,
	}, nil
}

func GetGroupDiscount(groupName string, modelName string, fallbackModelNames ...string) float64 {
	discount := model.GetGroupDiscount(groupName, modelName)
	if discount != 1.0 {
		return discount
	}
	for _, fallback := range fallbackModelNames {
		discount = model.GetGroupDiscount(groupName, fallback)
		if discount != 1.0 {
			return discount
		}
	}
	return 1.0
}

func CalculateTokenQuota(inputPrice float64, outputPrice float64, cachedPrice float64, promptTokens int, completionTokens int, cachedTokens int, groupDiscount float64) int64 {
	if groupDiscount == 0 {
		groupDiscount = 1.0
	}
	inputTokens := promptTokens - cachedTokens
	quota := (inputPrice*float64(inputTokens) + outputPrice*float64(completionTokens) + cachedPrice*float64(cachedTokens)) * groupDiscount / Million * config.QuotaPerUnit
	// 用 math.Round 而非直接截断：前端/管理端也用 Round（model.YuanToQuota、
	// utils/quota.js），两端取整口径必须一致，否则同一笔消费会相差 1 微元。
	result := int64(math.Round(quota))
	if result <= 0 && (promptTokens+completionTokens) > 0 {
		result = 1
	}
	return result
}

func CalculatePerRequestQuota(perRequestPrice float64, sizeRatio float64, n int, groupDiscount float64) int64 {
	if groupDiscount == 0 {
		groupDiscount = 1.0
	}
	if n <= 0 {
		n = 1
	}
	quota := perRequestPrice * sizeRatio * float64(n) * groupDiscount * config.QuotaPerUnit
	// 同 CalculateTokenQuota：统一用 math.Round，与前端/管理端取整口径一致。
	result := int64(math.Round(quota))
	if result <= 0 {
		result = 1
	}
	return result
}

func QuotaToUnit(quota int64) float64 {
	return float64(quota) / config.QuotaPerUnit
}

func ModelPriceExists(modelName string) bool {
	_, err := GetModelPrice(modelName)
	return err == nil
}

// GetModelRatio remains for backward compatibility with billing.go PostConsumeQuota
func GetModelRatio(name string, channelType int) float64 {
	logger.SysError("GetModelRatio is deprecated, use GetModelPrice instead")
	return 1
}

func GetCompletionRatio(name string, channelType int) float64 {
	logger.SysError("GetCompletionRatio is deprecated, use GetModelPrice instead")
	return 1
}