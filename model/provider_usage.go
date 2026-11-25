// provider_usage.go 渠道维度的用量聚合
// Channel-level usage aggregation over the consumption log.
// 版本: v0.0.28
// 日期: 2026-11-25
// 作者: MCode
//
// 本层只按「渠道 + 模型」聚合，不认识 provider 概念：
// provider ID 的映射需要 relay/registry，而 relay/registry 依赖 model，
// model 反向依赖会形成 import 环。因此「channel_id → 渠道类型 → provider」
// 的翻译留给 controller 层完成。
//
// 之所以不在 SQL 里 JOIN channels：日志表可能单独部署在 LOG_SQL_DSN 指向的
// 另一个库（见 model.InitLogDB），跨库 JOIN 在 PostgreSQL 上不成立。
package model

// ChannelUsageRow 是「渠道 + 模型」的一行用量聚合。
// ChannelUsageRow is one usage aggregate for a (channel, model) pair.
//
// 为什么按 channel_id 而不是 channel_type 聚合：channels 表与 logs 表可能
// 不在同一个库，无法在 SQL 里 JOIN；因此这里保留原始 channel_id，
// 由调用方在自己的库里翻译成渠道类型 / provider。
// Why channel_id and not channel_type: the channels table may live in another
// database than logs, so a SQL JOIN is not always possible. The raw channel_id
// is kept here and translated by the caller.
type ChannelUsageRow struct {
	ChannelId        int    `gorm:"column:channel_id" json:"channel_id"`
	ModelName        string `gorm:"column:model_name" json:"model_name"`
	RequestCount     int    `gorm:"column:request_count" json:"request_count"`
	PromptTokens     int    `gorm:"column:prompt_tokens" json:"prompt_tokens"`
	CompletionTokens int    `gorm:"column:completion_tokens" json:"completion_tokens"`
	CachedTokens     int    `gorm:"column:cached_tokens" json:"cached_tokens"`
	Quota            int    `gorm:"column:quota" json:"quota"`
}

// ChannelDailyUsageRow 是「日 + 渠道 + 模型」的一行用量聚合，
// 用于供应商详情页的趋势图。
// ChannelDailyUsageRow is one daily aggregate for a (day, channel, model).
type ChannelDailyUsageRow struct {
	Day              string `gorm:"column:day" json:"day"`
	ChannelId        int    `gorm:"column:channel_id" json:"channel_id"`
	ModelName        string `gorm:"column:model_name" json:"model_name"`
	RequestCount     int    `gorm:"column:request_count" json:"request_count"`
	PromptTokens     int    `gorm:"column:prompt_tokens" json:"prompt_tokens"`
	CompletionTokens int    `gorm:"column:completion_tokens" json:"completion_tokens"`
	CachedTokens     int    `gorm:"column:cached_tokens" json:"cached_tokens"`
	Quota            int    `gorm:"column:quota" json:"quota"`
}

// SumUsageByChannelIdAndModel 按「渠道 + 模型」聚合消费日志。
// userId 为 0 时统计全站，否则只统计该用户。
//
// 只统计 type=2（消费）日志。
// Aggregates consumption logs by channel and model.
// userId == 0 means the whole site; otherwise only that user's logs.
func SumUsageByChannelIdAndModel(userId int, start, end int64) ([]*ChannelUsageRow, error) {
	query := `
		SELECT channel_id,
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
	query += ` GROUP BY channel_id, model_name ORDER BY quota desc`

	var out []*ChannelUsageRow
	if err := LOG_DB.Raw(query, args...).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// SumUsageByChannelIdAndModelByDay 按「日 + 渠道 + 模型」聚合消费日志，
// 用于供应商详情页的趋势图。userId 为 0 时统计全站。
// Aggregates consumption logs by day, channel and model.
func SumUsageByChannelIdAndModelByDay(userId int, start, end int64) ([]*ChannelDailyUsageRow, error) {
	dayExpr := logDayExpression()
	query := `
		SELECT ` + dayExpr + ` as day,
		channel_id,
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
	query += ` GROUP BY day, channel_id, model_name ORDER BY day asc`

	var out []*ChannelDailyUsageRow
	if err := LOG_DB.Raw(query, args...).Scan(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
