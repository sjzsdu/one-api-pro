package channelrouter

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelbus/one-api-pro/common/config"
	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/model"
)

type ChannelRouter struct {
	filters     []ChannelFilter
	selector    ChannelSelector
	Cooldown    *CooldownManager
	Concurrency *ConcurrencyTracker
	Sticky      *StickySessionStore
	RPM         *RPMTracker
}

var DefaultRouter *ChannelRouter

func NewChannelRouter() *ChannelRouter {
	r := &ChannelRouter{
		Cooldown:    NewCooldownManager(),
		Concurrency: NewConcurrencyTracker(),
		Sticky:      NewStickySessionStore(),
		RPM:         NewRPMTracker(),
		selector:    &PriorityRandomSelector{},
	}
	r.filters = []ChannelFilter{
		&StatusFilter{},
		&FallbackFilter{},
		&ExcludedChannelFilter{},
		&CooldownFilter{cooldown: r.Cooldown},
		&ConcurrencyFilter{concurrency: r.Concurrency},
		&RPMFilter{rpmTracker: r.RPM},
		&StickySessionFilter{sticky: r.Sticky},
		&PriorityFilter{},
	}
	return r
}

func InitRouter() {
	DefaultRouter = NewChannelRouter()
	DefaultRouter.Cooldown.StartCleanupLoop(30 * time.Second)
	if config.ChannelStickySessionEnabled {
		DefaultRouter.Sticky.LoadFromLogDB()
	}
	logger.SysLog("channel router initialized")
}

// ChannelDrop 记录某个过滤器淘汰掉的渠道。
// Records the channels removed by one filter.
type ChannelDrop struct {
	Filter   string
	Channels []*model.Channel
}

// NoAvailableChannelError 表示候选渠道全部被过滤掉。
// Returned when every candidate channel was filtered out.
//
// 之前这里只返回一句 "no available channel"，网关对外统一报
// 「当前分组 X 下对于模型 Y 无可用渠道」。但"被过滤"和"根本没有渠道"是两回事：
// 上游配额打满时渠道进入冷却，客户端拿到的却是"没有渠道"这种误导性结论，
// 排查方向会被带偏（2026-11-24 线上事故即为此）。带上过滤原因后，
// 对外文案可以直说"冷却中 / 并发已满 / 触发限速"，并附上上游最后一次错误。
type NoAvailableChannelError struct {
	Group string
	Model string
	Drops []ChannelDrop
}

func (e *NoAvailableChannelError) Error() string {
	return fmt.Sprintf("no available channel for group=%s model=%s", e.Group, e.Model)
}

// Reason 返回可对外展示的中文原因，说明候选渠道为什么都不可用。
// Returns a human-readable reason describing why the candidates were dropped.
func (e *NoAvailableChannelError) Reason() string {
	parts := make([]string, 0, len(e.Drops))
	for _, drop := range e.Drops {
		label := filterReasonLabel(drop.Filter)
		if label == "" {
			continue
		}
		part := fmt.Sprintf("%s %d 个", label, len(drop.Channels))
		if reason := upstreamReason(drop.Channels); reason != "" {
			part += "（" + reason + "）"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "、")
}

// filterReasonLabel 把过滤器名翻译成客户端能看懂的原因。
// Maps a filter name to a user-facing reason.
func filterReasonLabel(name string) string {
	switch name {
	case "status":
		return "渠道已禁用"
	case "fallback":
		return "仅作兜底的渠道"
	case "excluded_channel":
		return "已被本次请求排除"
	case "cooldown":
		return "渠道冷却中"
	case "concurrency":
		return "渠道并发已满"
	case "rpm":
		return "渠道触发限速"
	case "sticky_session":
		return "会话粘滞未命中"
	case "priority":
		return "渠道优先级不匹配"
	default:
		return ""
	}
}

// upstreamReason 取被淘汰渠道记录的最后一次上游错误，便于直接看到真实原因。
// Picks the last upstream error recorded on the dropped channels.
func upstreamReason(channels []*model.Channel) string {
	for _, ch := range channels {
		message := strings.TrimSpace(ch.LastError)
		if message == "" {
			continue
		}
		runes := []rune(message)
		if len(runes) > 120 {
			message = string(runes[:120]) + "…"
		}
		return message
	}
	return ""
}

// removedChannels 返回本轮被过滤掉的渠道（按渠道 id 求差集）。
// Returns the channels removed by a filter.
func removedChannels(before, after []*model.Channel) []*model.Channel {
	kept := make(map[int]bool, len(after))
	for _, ch := range after {
		kept[ch.Id] = true
	}
	removed := make([]*model.Channel, 0, len(before))
	for _, ch := range before {
		if !kept[ch.Id] {
			removed = append(removed, ch)
		}
	}
	return removed
}

func (r *ChannelRouter) Route(ctx context.Context, req *RouteRequest, candidates []*model.Channel) (*model.Channel, error) {
	filtered := candidates
	var drops []ChannelDrop
	for _, f := range r.filters {
		before := filtered
		filtered = f.Filter(ctx, filtered, req)
		if removed := removedChannels(before, filtered); len(removed) > 0 {
			drops = append(drops, ChannelDrop{Filter: f.Name(), Channels: removed})
		}
		if len(filtered) == 0 {
			break
		}
	}
	if len(filtered) == 0 {
		return nil, &NoAvailableChannelError{Group: req.Group, Model: req.Model, Drops: drops}
	}
	return r.selector.Select(ctx, filtered, req)
}

func (r *ChannelRouter) SetCooldown(channelId int, seconds int, reason string, statusCode int) {
	r.Cooldown.SetCooldown(channelId, seconds, reason, statusCode)
}

func (r *ChannelRouter) IsInCooldown(channelId int) bool {
	return r.Cooldown.IsInCooldown(channelId)
}

func (r *ChannelRouter) TryAcquireConcurrency(channelId int, maxConcurrency int) bool {
	return r.Concurrency.TryAcquire(channelId, maxConcurrency)
}

func (r *ChannelRouter) ReleaseConcurrency(channelId int) {
	r.Concurrency.Release(channelId)
}

func (r *ChannelRouter) SetStickySession(sessionKey string, channelId int) {
	if sessionKey == "" {
		return
	}
	r.Sticky.Set(sessionKey, channelId)
}

func (r *ChannelRouter) GetStickySession(sessionKey string) int {
	if sessionKey == "" {
		return 0
	}
	return r.Sticky.Get(sessionKey)
}

func (r *ChannelRouter) IncrementRPM(channelId int) {
	r.RPM.Increment(channelId)
}
