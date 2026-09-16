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
	set.Availability = scoreCandidateAvailability(group, set.Models)
	return set, nil
}

func resolveCandidateNames(ctx context.Context, names []string, features *RequestFeatures, provider ModelProfileProvider) (CandidateSet, error) {
	unique := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	filtered := CandidateSet{Profiles: map[string]ModelProfile{}, FilterReasons: map[string]string{}}
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

func scoreCandidateAvailability(group string, names []string) map[string]float64 {
	availability := make(map[string]float64, len(names))
	now := time.Now()
	for _, name := range names {
		best := 0.0
		for _, channel := range model.GetChannelCandidates(group, name) {
			if channel.Status != model.ChannelStatusEnabled || channel.GetIsFallback() {
				continue
			}
			health := 1.0
			if channelrouter.DefaultRouter != nil {
				if channelrouter.DefaultRouter.IsInCooldown(channel.Id) {
					continue
				}
				if maxRPM := channel.GetRPM(); maxRPM > 0 {
					used := channelrouter.DefaultRouter.RPM.CurrentRPM(channel.Id)
					if used >= maxRPM {
						continue
					}
					health *= 1 - 0.45*float64(used)/float64(maxRPM)
				}
				if maxConcurrency := channel.GetMaxConcurrency(); maxConcurrency > 0 {
					active := channelrouter.DefaultRouter.Concurrency.GetActiveCount(channel.Id)
					if active >= int64(maxConcurrency) {
						continue
					}
					health *= 1 - 0.45*float64(active)/float64(maxConcurrency)
				}
			}
			if channel.LastErrorTime > 0 {
				age := now.Sub(time.Unix(channel.LastErrorTime, 0))
				if age >= 0 && age < 10*time.Minute {
					health *= 0.5 + 0.5*float64(age)/(10*60)
				}
			}
			if channel.ResponseTime > 0 {
				health *= 1 / (1 + float64(channel.ResponseTime)/10_000)
			}
			if health > best {
				best = health
			}
		}
		availability[name] = best
	}
	return availability
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
