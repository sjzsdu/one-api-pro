package modelrouter

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/modelbus/one-api-pro/channelrouter"
	"github.com/modelbus/one-api-pro/model"
)

type CandidateSet struct {
	Models   []string
	Profiles map[string]ModelProfile
	// Availability is the best current health score of a serving channel for
	// each model. It is deliberately kept separate from static profiles.
	Availability  map[string]float64
	FilteredOut   []string
	FilterReasons map[string]string
}

func ResolveCandidates(ctx context.Context, group string, features *RequestFeatures) (CandidateSet, error) {
	names, err := model.CacheGetGroupModels(ctx, group)
	if err != nil {
		return CandidateSet{}, fmt.Errorf("get models for group %s: %w", group, err)
	}
	provider, err := defaultModelProfileProvider()
	if err != nil {
		return CandidateSet{}, err
	}
	set, err := resolveCandidateNames(ctx, names, features, provider)
	if err != nil {
		return CandidateSet{}, err
	}
	return applyChannelAvailability(group, set), nil
}

func resolveCandidateNames(ctx context.Context, names []string, features *RequestFeatures, provider ModelProfileProvider) (CandidateSet, error) {
	unique := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	filtered := CandidateSet{Profiles: map[string]ModelProfile{}, Availability: map[string]float64{}, FilterReasons: map[string]string{}}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.HasSuffix(name, ":batch") {
			filtered.FilteredOut = append(filtered.FilteredOut, name)
			filtered.FilterReasons[name] = "batch-only model"
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		unique = append(unique, name)
	}
	sort.Strings(unique)
	profiles, err := provider.Profiles(ctx, unique)
	if err != nil {
		return CandidateSet{}, fmt.Errorf("resolve model profiles: %w", err)
	}
	for _, name := range unique {
		profile, ok := profiles[name]
		if !ok {
			profile = genericProfile(name)
		}
		reason := incompatibilityReason(profile, features)
		if reason != "" {
			filtered.FilteredOut = append(filtered.FilteredOut, name)
			filtered.FilterReasons[name] = reason
			continue
		}
		filtered.Models = append(filtered.Models, name)
		filtered.Profiles[name] = profile
	}
	sort.Strings(filtered.FilteredOut)
	return filtered, nil
}

// applyChannelAvailability removes models which have no routeable channel and
// gives each remaining model its best live channel health.  Static candidate
// resolution intentionally remains usable in tests and offline tools; only a
// group-backed routing request reaches this function.
func applyChannelAvailability(group string, candidates CandidateSet) CandidateSet {
	kept := make([]string, 0, len(candidates.Models))
	for _, name := range candidates.Models {
		score, ok := bestChannelAvailability(model.GetChannelCandidates(group, name), channelrouter.DefaultRouter, time.Now())
		if !ok {
			candidates.FilteredOut = append(candidates.FilteredOut, name)
			candidates.FilterReasons[name] = "no currently serviceable channel"
			delete(candidates.Profiles, name)
			continue
		}
		candidates.Availability[name] = score
		kept = append(kept, name)
	}
	candidates.Models = kept
	sort.Strings(candidates.FilteredOut)
	return candidates
}

// bestChannelAvailability models the same operational constraints enforced by
// channelrouter. A model can be selected only when at least one non-fallback,
// enabled channel remains below its cooldown/RPM/concurrency limits. Recent
// errors decay over ten minutes, and observed response time penalizes health.
func bestChannelAvailability(channels []*model.Channel, router *channelrouter.ChannelRouter, now time.Time) (float64, bool) {
	best := 0.0
	found := false
	for _, channel := range channels {
		if channel == nil || channel.Status != model.ChannelStatusEnabled || channel.GetIsFallback() {
			continue
		}
		if router != nil {
			if router.IsInCooldown(channel.Id) || router.Concurrency.IsAtCapacity(channel.Id, channel.GetMaxConcurrency()) {
				continue
			}
			if maxRPM := channel.GetRPM(); maxRPM > 0 && router.RPM.CurrentRPM(channel.Id) >= maxRPM {
				continue
			}
		}
		found = true
		health := channelHealth(channel, router, now)
		if health > best {
			best = health
		}
	}
	return best, found
}

func channelHealth(channel *model.Channel, router *channelrouter.ChannelRouter, now time.Time) float64 {
	health := 1.0
	if router != nil {
		if maxRPM := channel.GetRPM(); maxRPM > 0 {
			health *= 1 - .35*clamp01(float64(router.RPM.CurrentRPM(channel.Id))/float64(maxRPM))
		}
		if maxConcurrency := channel.GetMaxConcurrency(); maxConcurrency > 0 {
			health *= 1 - .35*clamp01(float64(router.Concurrency.GetActiveCount(channel.Id))/float64(maxConcurrency))
		}
	}
	if channel.LastErrorTime > 0 {
		age := now.Sub(time.Unix(channel.LastErrorTime, 0))
		if age < 0 {
			age = 0
		}
		health *= 1 - .70*math.Exp(-age.Seconds()/(10*60))
	}
	if channel.ResponseTime > 0 {
		// 1 second is neutral; very slow channels approach a 60% penalty.
		health *= .4 + .6/(1+float64(channel.ResponseTime)/1000)
	}
	return clamp01(health)
}

func incompatibilityReason(profile ModelProfile, features *RequestFeatures) string {
	if features == nil {
		return ""
	}
	if features.HasImages && profile.Vision == CapabilityUnsupported {
		return "vision explicitly unsupported"
	}
	if (features.HasTools || features.HasToolResult) && profile.Tools == CapabilityUnsupported {
		return "tools explicitly unsupported"
	}
	if features.EstimatedTokens > 0 && profile.ContextWindow > 0 && profile.ContextWindow < features.EstimatedTokens {
		return fmt.Sprintf("context window %d is below required %d", profile.ContextWindow, features.EstimatedTokens)
	}
	return ""
}
