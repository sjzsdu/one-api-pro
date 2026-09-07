package modelrouter

import (
	"context"
	"sync"
	"time"
)

// ModelProfile holds merged metadata for a single model.
type ModelProfile struct {
	ModelName      string    `json:"model_name"`
	ContextWindow  int       `json:"context_window"`
	MaxOutput      int       `json:"max_output"`
	Vision         bool      `json:"vision"`
	Tools          bool      `json:"tools"`
	CostTier       int       `json:"cost_tier"`
	Lightweight    bool      `json:"lightweight"`
	Confidence     float64   `json:"confidence"`
	Source         string    `json:"source"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// ModelProfileProvider resolves ModelProfile for a batch of model names.
type ModelProfileProvider interface {
	GetProfiles(ctx context.Context, models []string) map[string]*ModelProfile
}

// HybridProfileProvider merges signals from multiple sources.
// Priority: admin config > ModelPrice > name-based heuristics.
type HybridProfileProvider struct {
	mu            sync.RWMutex
	adminProfiles map[string]*ModelProfile
}

func NewHybridProfileProvider() *HybridProfileProvider {
	return &HybridProfileProvider{adminProfiles: make(map[string]*ModelProfile)}
}

func (p *HybridProfileProvider) SetAdminProfile(model string, profile *ModelProfile) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.adminProfiles[model] = profile
}

func (p *HybridProfileProvider) GetProfiles(ctx context.Context, models []string) map[string]*ModelProfile {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make(map[string]*ModelProfile, len(models))
	for _, m := range models {
		result[m] = p.resolveProfile(m)
	}
	return result
}

func (p *HybridProfileProvider) resolveProfile(model string) *ModelProfile {
	if admin, ok := p.adminProfiles[model]; ok && admin != nil {
		profile := *admin
		profile.Source = "admin"
		profile.Confidence = 1.0
		return &profile
	}

	profile := inferModelProfile(model)
	return &ModelProfile{
		ModelName:     model,
		ContextWindow: profile.contextWindow,
		MaxOutput:     0,
		Vision:        profile.vision,
		Tools:         profile.tools,
		CostTier:      profile.costTier,
		Lightweight:   profile.lightweight,
		Confidence:    0.6,
		Source:        "inferred",
		UpdatedAt:     time.Now(),
	}
}

// filterByCapabilities removes models that clearly cannot satisfy the request.
func filterByCapabilities(models []string, profiles map[string]*ModelProfile, features *RequestFeatures) []string {
	if features == nil {
		return models
	}

	result := make([]string, 0, len(models))
	for _, m := range models {
		profile, ok := profiles[m]
		if !ok {
			result = append(result, m)
			continue
		}

		if features.HasImages && !profile.Vision {
			continue
		}
		if features.HasTools && !profile.Tools {
			continue
		}
		if features.MaxOutputTokens > 0 && profile.MaxOutput > 0 && features.MaxOutputTokens > profile.MaxOutput {
			continue
		}
		if features.EstimatedTokens > 0 && profile.ContextWindow > 0 && features.EstimatedTokens > profile.ContextWindow {
			continue
		}
		result = append(result, m)
	}
	return result
}


