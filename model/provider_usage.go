// provider_usage.go 供应商（渠道商）维度的用量聚合
// Provider-level usage aggregation over the consumption log.
// 版本: v0.0.28
// 日期: 2026-11-25
// 作者: MCode
//
// 本层只按「渠道类型 + 模型」聚合，不认识 provider 概念：
// provider ID 的映射需要 relay/registry，而 relay/registry 依赖 model，
// model 反向依赖会形成 import 环。因此映射在 controller 层完成，
// 本层只输出可复用的原始聚合行。
package model

// ProviderUsageRow 是「渠道类型 × 模型」的一行用量聚合。
// ProviderUsageRow is one usage aggregate for a (channel type, model) pair.
//
// 为什么不直接按 provider 聚合：provider 由 relay/registry 定义，
// 而 model 不能依赖 relay/registry（会形成 import 环）。
// Why not grouped by provider directly: provider IDs come from relay/registry,
// and model must not import relay/registry (import cycle).
type ProviderUsageRow struct {
	ChannelType      int    `gorm:"column:channel_type" json:"channel_type"`
	ModelName        string `gorm:"column:model_name" json:"model_name"`
	RequestCount     int    `gorm:"column:request_count" json:"request_count"`
	PromptTokens     int    `gorm:"column:prompt_tokens" json:"prompt_tokens"`
	CompletionTokens int    `gorm:"column:completion_tokens" json:"completion_tokens"`
	CachedTokens     int    `gorm:"column:cached_tokens" json:"cached_tokens"`
	Quota            int    `gorm:"column:quota" json:"quota"`
}

// ProviderDailyUsageRow 是「日 × 渠道类型 × 模型」的一行用量聚合，
// 用于供应商详情页的趋势图。
// ProviderDailyUsageRow is one daily aggregate for a (day, channel type, model).
type ProviderDailyUsageRow struct {
	Day              string `gorm:"column:day" json:"day"`
	ChannelType      int    `gorm:"column:channel_type" json:"channel_type"`
	ModelName        string `gorm:"column:model_name" json:"model_name"`
	RequestCount     int    `gorm:"column:request_count" json:"request_count"`
	PromptTokens     int    `gorm:"column:prompt_tokens" json:"prompt_tokens"`
	CompletionTokens int    `gorm:"column:completion_tokens" json:"completion_tokens"`
	CachedTokens     int    `gorm:"column:cached_tokens" json:"cached_tokens"`
	Quota            int    `gorm:"column:quota" json:"quota"`
}

// SumUsageByChannelTypeAndModel 按「渠道类型 + 模型」聚合消费日志。
// userId 为 0 时统计全站，否则只统计该用户。
//
// 只统计 type=2（消费）日志；channel_id=0 的行会被保留，
// 由调用方决定如何归入「未知渠道」。
// Aggregates consumption logs by channel type and model.
// userId == 0 means the whole site; otherwise only that user's logs.
func SumUsageByChannelTypeAndModel(userId int, start, end int64) ([]*ProviderUsageRow, error) {
	query := `
		SELECT channel_id as channel_type,
		model_name, count(1) as request_count,
		sum(prompt_tokens) as prompt_tokens,
		sum(completion_tokens) as completion_tokens,
		sum(cached_tokens) as cached_tokens,
		sum(quota) as quota
		FROM logs
		WHERE type = ?
		AND created_at BETWEEN ? AND ?`
	args := []any{LogTypeConsume, start, end}
	if userId > 0 {
		query += ` AND user_id = ?`
		args = append(args, userId)
	}
	query += ` GROUP BY channel_type, model_name ORDER BY quota desc`

	var out []*ProviderUsageRow
	if err := LOG_DB.Raw(query, args...).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// SumUsageByChannelTypeAndModelByDay 按「日 + 渠道类型 + 模型」聚合消费日志，
// 用于供应商详情页的趋势图。userId 为 0 时统计全站。
// Aggregates consumption logs by day, channel type and model.
func SumUsageByChannelTypeAndModelByDay(userId int, start, end int64) ([]*ProviderDailyUsageRow, error) {
	dayExpr := logDayExpression()
	query := `
		SELECT ` + dayExpr + ` as day,
		channel_id as channel_type,
		model_name, count(1) as request_count,
		sum(prompt_tokens) as prompt_tokens,
		sum(completion_tokens) as completion_tokens,
		sum(cached_tokens) as cached_tokens,
		sum(quota) as quota
		FROM logs
		WHERE type = ?
		AND created_at BETWEEN ? AND ?`
	args := []any{LogTypeConsume, start, end}
	if userId > 0 {
		query += ` AND user_id = ?`
		args = append(args, userId)
	}
	query += ` GROUP BY day, channel_type, model_name ORDER BY day asc`

	var out []*ProviderDailyUsageRow
	if err := LOG_DB.Raw(query, args...).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
