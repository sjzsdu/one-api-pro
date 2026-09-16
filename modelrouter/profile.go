package modelrouter

import (
	"context"
	"strings"

	"github.com/modelbus/one-api-pro/model"
)

// Capability is tri-state so missing metadata does not incorrectly reject a
// newly configured model.
type Capability string

const (
	CapabilityUnknown     Capability = "unknown"
	CapabilitySupported   Capability = "supported"
	CapabilityUnsupported Capability = "unsupported"
)

// ModelProfile is the normalized metadata consumed by every routing path.
// Quality is keyed by task category and can be populated by a store without
// changing routing code.
type ModelProfile struct {
	Model         string             `json:"model"`
	CanonicalName string             `json:"canonical_name,omitempty"`
	ContextWindow int                `json:"context_window,omitempty"`
	Vision        Capability         `json:"vision"`
	Tools         Capability         `json:"tools"`
	InputCost     *float64           `json:"input_cost,omitempty"`
	OutputCost    *float64           `json:"output_cost,omitempty"`
	Latency       *float64           `json:"latency,omitempty"`
	Reliability   *float64           `json:"reliability,omitempty"`
	Quality       map[string]float64 `json:"quality,omitempty"`
	Confidence    float64            `json:"confidence"`
	Sources       []string           `json:"sources,omitempty"`
}

type ModelProfileProvider interface {
	Profiles(ctx context.Context, models []string) (map[string]ModelProfile, error)
}

type defaultProfileProvider struct {
	store *ProfileStore
}

func (p defaultProfileProvider) Profiles(_ context.Context, names []string) (map[string]ModelProfile, error) {
	result := make(map[string]ModelProfile, len(names))
	canonicalNames := make([]string, 0, len(names))
	for _, name := range names {
		canonicalNames = append(canonicalNames, CanonicalModelName(name))
	}
	stored := p.store.Snapshot(canonicalNames)
	for _, name := range names {
		profile := genericProfile(name)
		canonicalName := CanonicalModelName(name)
		profile.CanonicalName = canonicalName
		if price, ok := model.GetModelPrice(name); ok {
			input, output := price.InputPrice, price.OutputPrice
			profile.InputCost, profile.OutputCost = &input, &output
			profile.Confidence = max(profile.Confidence, .65)
			profile.Sources = append(profile.Sources, "model_price")
		} else if price, ok := model.GetModelPrice(canonicalName); ok {
			input, output := price.InputPrice, price.OutputPrice
			profile.InputCost, profile.OutputCost = &input, &output
			profile.Confidence = max(profile.Confidence, .65)
			profile.Sources = append(profile.Sources, "canonical_model_price")
		}
		if override, ok := stored[canonicalName]; ok {
			profile = mergeProfile(profile, override)
		}
		// The profile describes the canonical catalog model, while the map key
		// and Model field must remain the channel-visible name.
		profile.Model = name
		profile.CanonicalName = canonicalName
		result[name] = profile
	}
	return result, nil
}

func genericProfile(name string) ModelProfile {
	canonical := CanonicalModelName(name)
	lower := strings.ToLower(canonical)
	profile := ModelProfile{Model: name, CanonicalName: canonical, Vision: CapabilityUnknown,
		Tools: CapabilityUnknown, Confidence: .45, Sources: []string{"name_inference"},
		Quality: map[string]float64{"default": .55}}
	// The prior is intentionally broad rather than a fixed catalog. It gives a
	// newly synchronized model a usable tier until an operator catalog override
	// arrives, while keeping its confidence below explicit metadata.
	switch {
	case strings.Contains(lower, "opus"), strings.Contains(lower, "max"), strings.Contains(lower, "ultra"), strings.Contains(lower, "reasoner"), strings.HasPrefix(lower, "o1"), strings.HasPrefix(lower, "o3"):
		profile.Quality = map[string]float64{"default": .88, "reason": .93, "code": .88}
		profile.Confidence = .6
	case strings.Contains(lower, "sonnet"), strings.Contains(lower, "pro"), strings.Contains(lower, "gpt-4"), strings.Contains(lower, "coder"):
		profile.Quality = map[string]float64{"default": .78, "reason": .8, "code": .86}
		profile.Confidence = .55
	case strings.Contains(lower, "mini"), strings.Contains(lower, "flash"), strings.Contains(lower, "haiku"), strings.Contains(lower, "lite"), strings.Contains(lower, "turbo"):
		profile.Quality = map[string]float64{"default": .58, "chat": .7, "translate": .72}
		profile.Confidence = .5
	}
	if strings.Contains(lower, "vision") || strings.Contains(lower, "gpt-4o") || strings.Contains(lower, "gemini") || strings.Contains(lower, "claude") {
		profile.Vision = CapabilitySupported
	}
	if strings.Contains(lower, "gpt") || strings.Contains(lower, "claude") || strings.Contains(lower, "gemini") || strings.Contains(lower, "qwen") || strings.Contains(lower, "deepseek") {
		profile.Tools = CapabilitySupported
	}
	return profile
}

func mergeProfile(base, override ModelProfile) ModelProfile {
	if override.CanonicalName != "" {
		base.CanonicalName = override.CanonicalName
	}
	if override.ContextWindow > 0 {
		base.ContextWindow = override.ContextWindow
	}
	if override.Vision != "" && override.Vision != CapabilityUnknown {
		base.Vision = override.Vision
	}
	if override.Tools != "" && override.Tools != CapabilityUnknown {
		base.Tools = override.Tools
	}
	if override.InputCost != nil {
		base.InputCost = override.InputCost
	}
	if override.OutputCost != nil {
		base.OutputCost = override.OutputCost
	}
	if override.Latency != nil {
		base.Latency = override.Latency
	}
	if override.Reliability != nil {
		base.Reliability = override.Reliability
	}
	if override.Quality != nil {
		base.Quality = cloneFloatMap(override.Quality)
	}
	if override.Confidence > 0 {
		base.Confidence = override.Confidence
	}
	base.Sources = append(base.Sources, override.Sources...)
	if len(override.Sources) == 0 {
		base.Sources = append(base.Sources, "profile_store")
	}
	return base
}
