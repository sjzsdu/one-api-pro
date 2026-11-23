package billing

import (
	"context"
	"fmt"

	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/model"
)

func ReturnPreConsumedQuota(ctx context.Context, preConsumedQuota int64, tokenId int) {
	if preConsumedQuota != 0 {
		go func(ctx context.Context) {
			err := model.PostConsumeTokenQuota(tokenId, -preConsumedQuota)
			if err != nil {
				logger.Error(ctx, "error return pre-consumed quota: "+err.Error())
			}
		}(ctx)
	}
}

type ConsumeQuotaParams struct {
	TokenId          int
	UserId           int
	ChannelId        int
	QuotaDelta       int64
	TotalQuota       int64
	ModelName        string
	TokenName        string
	InputPrice       float64
	OutputPrice      float64
	CachedPrice      float64
	PerRequestPrice  float64
	GroupDiscount    float64
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	BillingType      string
	// BillingUnit optionally labels character-priced speech in consumption logs.
	// 版本: v0.0.28
	// 日期: 2026-11-23
	BillingUnit string
}

// PostConsumeQuota settles balance and records explicit billing units; zero-price calls are valid.
// 版本: v0.0.28
// 日期: 2026-11-23
func PostConsumeQuota(ctx context.Context, params *ConsumeQuotaParams) {
	err := model.PostConsumeTokenQuota(params.TokenId, params.QuotaDelta)
	if err != nil {
		logger.SysError("error consuming token remain quota: " + err.Error())
	}
	err = model.CacheUpdateUserQuota(ctx, params.UserId)
	if err != nil {
		logger.SysError("error update user quota cache: " + err.Error())
	}
	if params.TotalQuota != 0 {
		logContent := fmt.Sprintf("定价：输入¥%.6f/百万tokens × %d + 输出¥%.6f/百万tokens × %d",
			params.InputPrice, params.PromptTokens, params.OutputPrice, params.CompletionTokens)
		if params.BillingUnit == "characters" {
			logContent = fmt.Sprintf("定价：输入¥%.6f/百万字符 × %d", params.InputPrice, params.PromptTokens)
		}
		if params.CachedTokens > 0 {
			logContent += fmt.Sprintf(" + 缓存¥%.6f/百万tokens × %d", params.CachedPrice, params.CachedTokens)
		}
		if params.GroupDiscount != 1.0 {
			logContent += fmt.Sprintf(" × 分组折扣%.2f", params.GroupDiscount)
		}
		if params.BillingType == "per_request" {
			logContent = fmt.Sprintf("按次计费：¥%.6f × 分组折扣%.2f", params.PerRequestPrice, params.GroupDiscount)
		}
		model.RecordConsumeLog(ctx, &model.Log{
			UserId:           params.UserId,
			ChannelId:        params.ChannelId,
			PromptTokens:     params.PromptTokens,
			CompletionTokens: params.CompletionTokens,
			CachedTokens:     params.CachedTokens,
			ModelName:        params.ModelName,
			TokenName:        params.TokenName,
			Quota:            int(params.TotalQuota),
			Content:          logContent,
		})
		model.UpdateUserUsedQuotaAndRequestCount(params.UserId, params.TotalQuota)
		model.UpdateChannelUsedQuota(params.ChannelId, params.TotalQuota)
	}
	if params.TotalQuota < 0 {
		logger.Error(ctx, fmt.Sprintf("totalQuota consumed is %d, something is wrong", params.TotalQuota))
	}
}

// ReturnPreConsumedQuotaSync refunds a failed attempt and invalidates its balance cache before retrying.
// 版本: v0.0.28
// 日期: 2026-11-22
func ReturnPreConsumedQuotaSync(ctx context.Context, quota int64, tokenID, userID int) error {
	if quota == 0 {
		return nil
	}
	if err := model.PostConsumeTokenQuota(tokenID, -quota); err != nil {
		return err
	}
	// A refund delta is not a balance: INCRBY could recreate an expired key with
	// only that delta, or add it twice after IncreaseUserQuota already refilled it.
	if err := model.CacheInvalidateUserQuota(userID); err != nil {
		// The DB refund is committed. Returning an error would invite another refund.
		logger.Warnf(ctx, "refund completed for user %d, but quota cache invalidation failed: %v", userID, err)
	}
	return nil
}
