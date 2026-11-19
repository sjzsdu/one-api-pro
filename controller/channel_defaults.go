package controller

import (
	"net/http"
	"strconv"

	"github.com/modelbus/one-api-pro/relay/adaptor"
	"github.com/modelbus/one-api-pro/relay/meta"
	"github.com/modelbus/one-api-pro/relay/modelcatalog"
	"github.com/modelbus/one-api-pro/relay/registry"

	"github.com/gin-gonic/gin"
)

type channelDefaultPreset struct {
	BaseURL   string                       `json:"base_url"`
	Name      string                       `json:"name"`
	Models    []modelcatalog.FeaturedModel `json:"models"`
	AllModels []string                     `json:"all_models"`
	Source    string                       `json:"source"` // "curated" = dated mainstream picks, "adapter" = full capability list
}

type channelDefaultsData struct {
	AsOf    string                          `json:"as_of"`
	Presets map[string]channelDefaultPreset `json:"presets"`
}

// ListChannelDefaults powers "pick a provider, fill in the rest" on the
// channel form. For every channel type it returns a validated default BaseURL,
// a curated (time-aware) mainstream model list, and the adaptor's full model
// capability list as a fallback/supplement.
//
// It is intentionally read-only and needs no provider key: the operator sees
// sensible defaults immediately, then can use the existing "validate models"
// action (which calls the upstream /v1/models with their key) to confirm the
// account actually exposes them.
func ListChannelDefaults(c *gin.Context) {
	presets := make(map[string]channelDefaultPreset)

	// Start from every registered adaptor so custom/less common types still get
	// a BaseURL + full model list even without a curated preset.
	for _, cm := range registry.AllChannelMetas() {
		if cm.LegacyType <= 0 {
			continue
		}
		key := strconv.Itoa(cm.LegacyType)
		var fullModels []string
		if a := registry.GetAdaptor(cm.ID); a != nil {
			initAdaptor(a, cm)
			fullModels = a.GetModelList()
		}

		p := channelDefaultPreset{
			BaseURL:   cm.DefaultBaseURL,
			Name:      cm.Name,
			AllModels: fullModels,
			Source:    "adapter",
		}

		if curated, ok := modelcatalog.Get(cm.LegacyType); ok {
			p.Name = curated.Name
			p.Models = curated.Models
			if curated.BaseURL != "" {
				p.BaseURL = curated.BaseURL
			}
			p.Source = "curated"
		} else if len(fullModels) > 0 {
			// No curated list: expose the capability list as selectable models too,
			// so the form is still useful for provider types we haven't curated.
			p.Models = make([]modelcatalog.FeaturedModel, 0, len(fullModels))
			for _, m := range fullModels {
				p.Models = append(p.Models, modelcatalog.FeaturedModel{ID: m})
			}
		}
		presets[key] = p
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": channelDefaultsData{
			AsOf:    modelcatalog.AsOf,
			Presets: presets,
		},
	})
}

// initAdaptor mirrors the setup in controller/model.go so GetModelList and any
// other adaptor introspection see the correct channel identity.
func initAdaptor(a adaptor.Adaptor, cm registry.ChannelMeta) {
	m := &meta.Meta{ChannelType: cm.LegacyType, ChannelID: cm.ID}
	a.Init(m)
}
