package modelrouter

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/modelbus/one-api-pro/channelrouter"
	"github.com/modelbus/one-api-pro/model"
)

type CandidateSet struct {
	Models        []string
	Profiles      map[string]ModelProfile
	FilteredOut   []string
	FilterReasons map[string]string
	Availability  map[string]float64
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
	set.applyChannelAvailability(group)
	return set, nil
}

func resolveCandidateNames(ctx context.Context, names []string, features *RequestFeatures, provider ModelProfileProvider) (CandidateSet, error) {
	unique := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	filtered := CandidateSet{Profiles: map[string]ModelProfile{}, FilterReasons: map[string]string{}, Availability: map[string]float64{}}
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

// applyChannelAvailability keeps only models with at least one serviceable
// channel in this group and records the best live health score for scoring.
func (set *CandidateSet) applyChannelAvailability(group string) {
	if set.Availability == nil {
		set.Availability = make(map[string]float64, len(set.Models))
	}
	available := make([]string, 0, len(set.Models))
	for _, name := range set.Models {
		best := 0.0
		for _, channel := range model.GetChannelCandidates(group, name) {
			best = max(best, channelAvailability(channel))
		}
		if best == 0 {
			set.FilteredOut = append(set.FilteredOut, name)
			set.FilterReasons[name] = "no serviceable channel"
			delete(set.Profiles, name)
			continue
		}
		set.Availability[name] = best
		available = append(available, name)
	}
	set.Models = available
	sort.Strings(set.FilteredOut)
}

func channelAvailability(channel *model.Channel) float64 {
	if channel == nil || channel.Status != model.ChannelStatusEnabled {
		return 0
	}
	// Model routing can be initialized before the channel router during
	// startup. Preserve the channel as viable until live counters exist.
	if channelrouter.DefaultRouter == nil {
		return 1
	}
	router := channelrouter.DefaultRouter
	if router.IsInCooldown(channel.Id) || router.Concurrency.IsAtCapacity(channel.Id, channel.GetMaxConcurrency()) ||
		(channel.GetRPM() > 0 && router.RPM.CurrentRPM(channel.Id) >= channel.GetRPM()) {
		return 0
	}
	health := 1.0
	if maxConcurrency := channel.GetMaxConcurrency(); maxConcurrency > 0 {
		health *= 1 - .5*min(1, float64(router.Concurrency.GetActiveCount(channel.Id))/float64(maxConcurrency))
	}
	if maxRPM := channel.GetRPM(); maxRPM > 0 {
		health *= 1 - .4*min(1, float64(router.RPM.CurrentRPM(channel.Id))/float64(maxRPM))
	}
	if channel.LastErrorTime > 0 {
		age := time.Since(time.Unix(channel.LastErrorTime, 0))
		if age < 10*time.Minute {
			health *= 1 - .5*(1-age.Minutes()/10)
		}
	}
	if channel.ResponseTime > 0 {
		health *= 1 / (1 + float64(channel.ResponseTime)/5_000)
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
