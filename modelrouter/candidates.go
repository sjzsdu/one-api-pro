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
	return resolveCandidateNames(ctx, names, features, provider, group)
}

func resolveCandidateNames(ctx context.Context, names []string, features *RequestFeatures, provider ModelProfileProvider, groups ...string) (CandidateSet, error) {
	unique := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	filtered := CandidateSet{Profiles: map[string]ModelProfile{}, FilterReasons: map[string]string{}, Availability: map[string]float64{}}
	group := ""
	if len(groups) > 0 {
		group = groups[0]
	}
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
		availability, available := modelAvailability(group, name)
		if !available {
			filtered.FilteredOut = append(filtered.FilteredOut, name)
			filtered.FilterReasons[name] = "no currently available channel"
			continue
		}
		filtered.Models = append(filtered.Models, name)
		filtered.Profiles[name] = profile
		filtered.Availability[name] = availability
	}
	sort.Strings(filtered.FilteredOut)
	return filtered, nil
}

// modelAvailability mirrors the channel router's admission constraints. A
// model is eligible only when at least one channel can serve it; among those,
// preserve the best health score rather than averaging a healthy backup away.
func modelAvailability(group, name string) (float64, bool) {
	if group == "" || channelrouter.DefaultRouter == nil {
		return 1, true
	}
	best := 0.0
	for _, channel := range model.GetChannelCandidates(group, name) {
		if channel.Status != model.ChannelStatusEnabled || channelrouter.DefaultRouter.IsInCooldown(channel.Id) {
			continue
		}
		maxRPM := channel.GetRPM()
		rpm := channelrouter.DefaultRouter.RPM.CurrentRPM(channel.Id)
		if maxRPM > 0 && rpm >= maxRPM {
			continue
		}
		maxConcurrency := channel.GetMaxConcurrency()
		active := channelrouter.DefaultRouter.Concurrency.GetActiveCount(channel.Id)
		if maxConcurrency > 0 && active >= int64(maxConcurrency) {
			continue
		}
		health := 1.0
		if maxRPM > 0 {
			health -= .25 * float64(rpm) / float64(maxRPM)
		}
		if maxConcurrency > 0 {
			health -= .25 * float64(active) / float64(maxConcurrency)
		}
		if channel.ResponseTime > 0 {
			health -= min(.25, float64(channel.ResponseTime)/20_000)
		}
		if channel.LastErrorTime > 0 {
			age := time.Since(time.Unix(channel.LastErrorTime, 0))
			if age >= 0 && age < 10*time.Minute {
				health -= .25 * (1 - age.Seconds()/600)
			}
		}
		if health > best {
			best = health
		}
	}
	return clamp01(best), best > 0
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
