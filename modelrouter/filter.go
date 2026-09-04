package modelrouter

type ModelFilter interface {
	Name() string
	Filter(models []string, features *RequestFeatures) []string
}

type ContextWindowFilter struct{}

func (f *ContextWindowFilter) Name() string { return "context_window" }

func (f *ContextWindowFilter) Filter(models []string, features *RequestFeatures) []string {
	if features.EstimatedTokens == 0 {
		return models
	}
	var result []string
	for _, m := range models {
		caps := GetModelCaps(m)
		if caps.ContextWindow <= 0 || features.EstimatedTokens <= caps.ContextWindow/2 {
			result = append(result, m)
		}
	}
	if len(result) == 0 {
		return models
	}
	return result
}

type VisionFilter struct{}

func (f *VisionFilter) Name() string { return "vision" }

func (f *VisionFilter) Filter(models []string, features *RequestFeatures) []string {
	if !features.HasImages {
		return models
	}
	var result []string
	for _, m := range models {
		caps := GetModelCaps(m)
		if caps.SupportsVision {
			result = append(result, m)
		}
	}
	if len(result) == 0 {
		return models
	}
	return result
}

type ToolFilter struct{}

func (f *ToolFilter) Name() string { return "tool" }

func (f *ToolFilter) Filter(models []string, features *RequestFeatures) []string {
	if !features.HasTools {
		return models
	}
	var result []string
	for _, m := range models {
		caps := GetModelCaps(m)
		if caps.SupportsTools {
			result = append(result, m)
		}
	}
	if len(result) == 0 {
		return models
	}
	return result
}

func DefaultFilters() []ModelFilter {
	return []ModelFilter{
		&ContextWindowFilter{},
		&VisionFilter{},
		&ToolFilter{},
	}
}
