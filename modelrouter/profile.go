package modelrouter

import (
	"context"

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
	stored := p.store.Snapshot(names)
	for _, name := range names {
		profile := genericProfile(name)
		if price, ok := model.GetModelPrice(name); ok {
			input, output := price.InputPrice, price.OutputPrice
			profile.InputCost, profile.OutputCost = &input, &output
			profile.Confidence = max(profile.Confidence, .65)
			profile.Sources = append(profile.Sources, "model_price")
		}
		if override, ok := stored[name]; ok {
			profile = mergeProfile(profile, override)
		}
		result[name] = profile
	}
	return result, nil
}

func genericProfile(name string) ModelProfile {
	return ModelProfile{
		Model: name, CanonicalName: name, Vision: CapabilityUnknown,
		Tools: CapabilityUnknown, Confidence: .25, Sources: []string{"neutral_prior"},
	}
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
