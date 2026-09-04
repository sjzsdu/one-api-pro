package modelrouter

import (
	"sync"
)

type ModelCapabilities struct {
	ContextWindow   int
	SupportsVision  bool
	SupportsTools   bool
	SupportsStreaming bool
	InputPricePer1K  float64
	OutputPricePer1K float64
	AvgLatencyMs     int
	QualityRank      float64
}

var (
	capsMu    sync.RWMutex
	capsCache = make(map[string]*ModelCapabilities)
)

func RegisterModelCaps(model string, caps *ModelCapabilities) {
	capsMu.Lock()
	defer capsMu.Unlock()
	capsCache[model] = caps
}

func GetModelCaps(model string) *ModelCapabilities {
	capsMu.RLock()
	defer capsMu.RUnlock()
	if caps, ok := capsCache[model]; ok {
		return caps
	}
	return defaultModelCaps()
}

func GetAllModelCaps() map[string]*ModelCapabilities {
	capsMu.RLock()
	defer capsMu.RUnlock()
	result := make(map[string]*ModelCapabilities, len(capsCache))
	for k, v := range capsCache {
		result[k] = v
	}
	return result
}

func defaultModelCaps() *ModelCapabilities {
	return &ModelCapabilities{
		ContextWindow:    128000,
		SupportsVision:   false,
		SupportsTools:    true,
		SupportsStreaming: true,
		InputPricePer1K:  0.01,
		OutputPricePer1K: 0.03,
		AvgLatencyMs:     2000,
		QualityRank:      0.5,
	}
}

func init() {
	defaultModels := map[string]*ModelCapabilities{
		"gpt-4o": {
			ContextWindow:    128000,
			SupportsVision:   true,
			SupportsTools:    true,
			SupportsStreaming: true,
			InputPricePer1K:  0.0025,
			OutputPricePer1K: 0.01,
			AvgLatencyMs:     1500,
			QualityRank:      0.9,
		},
		"gpt-4o-mini": {
			ContextWindow:    128000,
			SupportsVision:   true,
			SupportsTools:    true,
			SupportsStreaming: true,
			InputPricePer1K:  0.00015,
			OutputPricePer1K: 0.0006,
			AvgLatencyMs:     800,
			QualityRank:      0.7,
		},
		"gpt-4": {
			ContextWindow:    8192,
			SupportsVision:   false,
			SupportsTools:    true,
			SupportsStreaming: true,
			InputPricePer1K:  0.03,
			OutputPricePer1K: 0.06,
			AvgLatencyMs:     3000,
			QualityRank:      0.85,
		},
		"claude-3.5-sonnet": {
			ContextWindow:    200000,
			SupportsVision:   true,
			SupportsTools:    true,
			SupportsStreaming: true,
			InputPricePer1K:  0.003,
			OutputPricePer1K: 0.015,
			AvgLatencyMs:     1800,
			QualityRank:      0.92,
		},
		"claude-3-haiku": {
			ContextWindow:    200000,
			SupportsVision:   true,
			SupportsTools:    true,
			SupportsStreaming: true,
			InputPricePer1K:  0.00025,
			OutputPricePer1K: 0.00125,
			AvgLatencyMs:     600,
			QualityRank:      0.65,
		},
		"deepseek-chat": {
			ContextWindow:    64000,
			SupportsVision:   false,
			SupportsTools:    true,
			SupportsStreaming: true,
			InputPricePer1K:  0.00014,
			OutputPricePer1K: 0.00028,
			AvgLatencyMs:     2000,
			QualityRank:      0.8,
		},
		"deepseek-coder": {
			ContextWindow:    64000,
			SupportsVision:   false,
			SupportsTools:    true,
			SupportsStreaming: true,
			InputPricePer1K:  0.00014,
			OutputPricePer1K: 0.00028,
			AvgLatencyMs:     2000,
			QualityRank:      0.82,
		},
		"deepseek-reasoner": {
			ContextWindow:    64000,
			SupportsVision:   false,
			SupportsTools:    false,
			SupportsStreaming: true,
			InputPricePer1K:  0.00055,
			OutputPricePer1K: 0.00219,
			AvgLatencyMs:     5000,
			QualityRank:      0.88,
		},
	}

	for model, caps := range defaultModels {
		RegisterModelCaps(model, caps)
	}
}
