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
	// Availability is a live, 0..1 health score derived from channels that
	// can presently serve each model. It is intentionally not persisted.
	Availability map[string]float64
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
	return resolveCandidateNamesForGroup(ctx, group, names, features, provider)
}

func resolveCandidateNames(ctx context.Context, names []string, features *RequestFeatures, provider ModelProfileProvider) (CandidateSet, error) {
	return resolveCandidateNamesForGroup(ctx, "", names, features, provider)
}

func resolveCandidateNamesForGroup(ctx context.Context, group string, names []string, features *RequestFeatures, provider ModelProfileProvider) (CandidateSet, error) {
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
		filtered.Availability[name] = modelAvailability(group, name)
	}
	sort.Strings(filtered.FilteredOut)
	return filtered, nil
}

func modelAvailability(group, name string) float64 {
	if group == "" {
		return .5
	}
	channels := model.GetChannelCandidates(group, name)
	if len(channels) == 0 {
		return .5
	}
	best := 0.0
	for _, channel := range channels {
		best = max(best, channelAvailability(channel))
	}
	return best
}

func channelAvailability(channel *model.Channel) float64 {
	if channel == nil || channel.Status != model.ChannelStatusEnabled {
		return 0
	}
	score := 1.0
	router := channelrouter.DefaultRouter
	if router != nil {
		if router.IsInCooldown(channel.Id) {
			return 0
		}
		if limit := channel.GetRPM(); limit > 0 {
			score *= max(0, 1-float64(router.RPM.CurrentRPM(channel.Id))/float64(limit))
		}
		if limit := channel.GetMaxConcurrency(); limit > 0 {
			score *= max(0, 1-float64(router.Concurrency.GetActiveCount(channel.Id))/float64(limit))
		}
	}
	// A recent upstream error fades over ten minutes so stale incidents do not
	// permanently distort routing.
	if channel.LastErrorTime > 0 {
		age := time.Since(time.Unix(channel.LastErrorTime, 0))
		if age >= 0 && age < 10*time.Minute {
			score *= .45 + .55*float64(age)/(10*float64(time.Minute))
		}
	}
	if channel.ResponseTime > 0 {
		score *= 1 / (1 + float64(channel.ResponseTime)/10_000)
	}
	return clamp01(score)
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
