package channelrouter

import (
	"context"
	"fmt"
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

// AvailabilitySnapshot is a normalized real-time channel health view used by
// the model policy engine. A score of one means no known pressure; zero means
// a channel must not be selected.
type AvailabilitySnapshot struct {
	Score       float64 `json:"score"`
	Cooldown    bool    `json:"cooldown"`
	RPMUsage    float64 `json:"rpm_usage"`
	Concurrency float64 `json:"concurrency"`
	RecentError float64 `json:"recent_error"`
	LatencyMS   int     `json:"latency_ms"`
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

func (r *ChannelRouter) Route(ctx context.Context, req *RouteRequest, candidates []*model.Channel) (*model.Channel, error) {
	filtered := candidates
	for _, f := range r.filters {
		filtered = f.Filter(ctx, filtered, req)
		if len(filtered) == 0 {
			break
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no available channel for group=%s model=%s", req.Group, req.Model)
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

// Availability reports current channel pressure. It complements (rather than
// replaces) the hard channel filters: model selection uses it to prefer a
// healthy model before the request reaches channel selection.
func (r *ChannelRouter) Availability(channel *model.Channel) AvailabilitySnapshot {
	if channel == nil || channel.Status != model.ChannelStatusEnabled || r == nil {
		return AvailabilitySnapshot{}
	}
	snapshot := AvailabilitySnapshot{Score: 1, LatencyMS: channel.ResponseTime}
	if r.Cooldown.IsInCooldown(channel.Id) {
		snapshot.Cooldown, snapshot.Score = true, 0
		return snapshot
	}
	if maxRPM := channel.GetRPM(); maxRPM > 0 {
		snapshot.RPMUsage = min(1, float64(r.RPM.CurrentRPM(channel.Id))/float64(maxRPM))
	}
	if maxConcurrency := channel.GetMaxConcurrency(); maxConcurrency > 0 {
		snapshot.Concurrency = min(1, float64(r.Concurrency.GetActiveCount(channel.Id))/float64(maxConcurrency))
	}
	// LastError is persisted by providers. Its impact fades over ten minutes
	// so a transient failure does not permanently penalize a channel.
	if channel.LastError != "" && channel.LastErrorTime > 0 {
		age := time.Since(time.Unix(channel.LastErrorTime, 0))
		if age < 10*time.Minute {
			snapshot.RecentError = 1 - float64(age)/(10*float64(time.Minute))
		}
	}
	snapshot.Score = max(0, 1-.40*snapshot.RPMUsage-.35*snapshot.Concurrency-.50*snapshot.RecentError)
	return snapshot
}
