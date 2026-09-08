package modelrouter

import (
	"time"

	"github.com/modelbus/one-api-pro/channelrouter"
	"github.com/modelbus/one-api-pro/model"
)

// enrichProfileWithAvailability folds the live channel pool into a catalog
// profile. The channel router remains the final authority when a channel is
// acquired; this is a non-mutating forecast used to avoid selecting a model
// whose only providers are cooling down or saturated.
func enrichProfileWithAvailability(group, name string, profile ModelProfile) ModelProfile {
	channels := model.GetChannelCandidates(group, name)
	if len(channels) == 0 {
		profile.Reliability = float64Ptr(.05)
		profile.Sources = append(profile.Sources, "no_live_channel")
		return profile
	}
	available, healthSum, latencyCount := 0, 0.0, 0
	latencySum := 0.0
	for _, channel := range channels {
		if channel.Status != model.ChannelStatusEnabled {
			continue
		}
		health := 1.0
		if channelrouter.DefaultRouter != nil {
			if channelrouter.DefaultRouter.IsInCooldown(channel.Id) {
				continue
			}
			if max := channel.GetMaxConcurrency(); max > 0 {
				ratio := float64(channelrouter.DefaultRouter.Concurrency.GetActiveCount(channel.Id)) / float64(max)
				if ratio >= 1 {
					continue
				}
				health *= 1 - .35*ratio
			}
			if limit := channel.GetRPM(); limit > 0 {
				ratio := float64(channelrouter.DefaultRouter.RPM.CurrentRPM(channel.Id)) / float64(limit)
				if ratio >= 1 {
					continue
				}
				health *= 1 - .25*ratio
			}
		}
		// A recent upstream error is a weak signal; cooldown is handled above.
		if channel.LastError != "" && time.Since(time.Unix(channel.LastErrorTime, 0)) < 5*time.Minute {
			health *= .6
		}
		available++
		healthSum += health
		if channel.ResponseTime > 0 {
			latencySum += float64(channel.ResponseTime)
			latencyCount++
		}
	}
	if available == 0 {
		profile.Reliability = float64Ptr(.05)
		profile.Sources = append(profile.Sources, "channels_unavailable")
		return profile
	}
	reliability := healthSum / float64(available)
	if profile.Reliability != nil {
		reliability = (.6 * *profile.Reliability) + (.4 * reliability)
	}
	profile.Reliability = float64Ptr(reliability)
	if latencyCount > 0 {
		latency := latencySum / float64(latencyCount)
		profile.Latency = &latency
	}
	profile.Confidence = max(profile.Confidence, .6)
	profile.Sources = append(profile.Sources, "live_channel_health")
	return profile
}

func float64Ptr(value float64) *float64 { return &value }
