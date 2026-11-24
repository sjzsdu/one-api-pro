package controller

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/common/logger"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay/meta"
	"github.com/modelbus/one-api-pro/relay/registry"
	relaymodel "github.com/modelbus/one-api-pro/relay/schema"
	"net/http"
	"strings"
)

type OpenAIModelPermission struct {
	Id                 string  `json:"id"`
	Object             string  `json:"object"`
	Created            int     `json:"created"`
	AllowCreateEngine  bool    `json:"allow_create_engine"`
	AllowSampling      bool    `json:"allow_sampling"`
	AllowLogprobs      bool    `json:"allow_logprobs"`
	AllowSearchIndices bool    `json:"allow_search_indices"`
	AllowView          bool    `json:"allow_view"`
	AllowFineTuning    bool    `json:"allow_fine_tuning"`
	Organization       string  `json:"organization"`
	Group              *string `json:"group"`
	IsBlocking         bool    `json:"is_blocking"`
}

type OpenAIModels struct {
	Id         string                  `json:"id"`
	Object     string                  `json:"object"`
	Created    int                     `json:"created"`
	OwnedBy    string                  `json:"owned_by"`
	Permission []OpenAIModelPermission `json:"permission"`
	Root       string                  `json:"root"`
	Parent     *string                 `json:"parent"`
}

var models []OpenAIModels
var modelsMap map[string]OpenAIModels
var channelId2Models map[int][]string

func init() {
	var permission []OpenAIModelPermission
	permission = append(permission, OpenAIModelPermission{
		Id:                 "modelperm-LwHkVFn8AcMItP432fKKDIKJ",
		Object:             "model_permission",
		Created:            1626777600,
		AllowCreateEngine:  true,
		AllowSampling:      true,
		AllowLogprobs:      true,
		AllowSearchIndices: false,
		AllowView:          true,
		AllowFineTuning:    false,
		Organization:       "*",
		Group:              nil,
		IsBlocking:         false,
	})
	modelsMap = make(map[string]OpenAIModels)
	channelId2Models = make(map[int][]string)
	for _, channelMeta := range registry.AllChannelMetas() {
		adaptor := registry.GetAdaptor(channelMeta.ID)
		if adaptor == nil {
			continue
		}
		m := &meta.Meta{ChannelType: channelMeta.LegacyType, ChannelID: channelMeta.ID}
		adaptor.Init(m)
		modelNames := adaptor.GetModelList()
		if channelMeta.LegacyType > 0 {
			channelId2Models[channelMeta.LegacyType] = modelNames
		}
		for _, modelName := range modelNames {
			if _, exists := modelsMap[modelName]; exists {
				continue
			}
			om := OpenAIModels{
				Id:         modelName,
				Object:     "model",
				Created:    1626777600,
				OwnedBy:    channelMeta.Name,
				Permission: permission,
				Root:       modelName,
				Parent:     nil,
			}
			models = append(models, om)
			modelsMap[modelName] = om
		}
	}
}

func DashboardListModels(c *gin.Context) {
	channelModels := make(map[int][]string, len(channelId2Models))
	for channelID, modelList := range channelId2Models {
		channelModels[channelID] = append([]string(nil), modelList...)
	}
	if openRouterModels, _, err := getOpenRouterModelList("", "", false); err == nil && len(openRouterModels) > 0 {
		channelModels[20] = openRouterModels
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    channelModels,
	})
}

func ListAllModels(c *gin.Context) {
	c.JSON(200, gin.H{
		"object": "list",
		"data":   models,
	})
}

type configuredModelItem struct {
	CatalogModelName         string                        `json:"catalog_model_name"`
	ModelName                string                        `json:"model_name"`
	DisplayName              string                        `json:"display_name,omitempty"`
	Description              string                        `json:"description,omitempty"`
	ChannelID                int                           `json:"channel_id"`
	ChannelName              string                        `json:"channel_name"`
	ChannelType              int                           `json:"channel_type"`
	ChannelStatus            int                           `json:"channel_status"`
	Group                    string                        `json:"group"`
	Provider                 string                        `json:"provider"`
	Capabilities             model.ModelCapabilities       `json:"capabilities"`
	GatewaySupport           map[string][]string           `json:"gateway_support"`
	ProbeOperations          map[string]OperationProbeSpec `json:"probe_operations"`
	ContextLength            int                           `json:"context_length,omitempty"`
	InputModalities          []string                      `json:"input_modalities,omitempty"`
	OutputModalities         []string                      `json:"output_modalities,omitempty"`
	SupportedParameters      []string                      `json:"supported_parameters,omitempty"`
	InputPriceUSDPerMillion  *float64                      `json:"input_price_usd_per_million,omitempty"`
	OutputPriceUSDPerMillion *float64                      `json:"output_price_usd_per_million,omitempty"`
}

// ListConfiguredModels expands each configured channel into its models. It is
// intentionally admin-only at routing registration because it exposes channel
// topology and metadata.
func ListConfiguredModels(c *gin.Context) {
	channels, err := model.GetAllChannels(0, 0, "all")
	if err != nil {
		c.JSON(200, gin.H{"success": false, "message": err.Error()})
		return
	}
	items, err := configuredModelItems(channels)
	if err != nil {
		c.JSON(200, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{"success": true, "data": items})
}

// tokenModelsHaveWildcard reports whether a token model whitelist contains the
// "*" wildcard. The wildcard only carries meaning inside the gateway's own
// authorization (middleware.isModelInList); OpenAI-compatible clients treat
// "/v1/models" as a literal list for validate_model and therefore reject real
// models such as qwen-plus when "*" is returned verbatim.
func tokenModelsHaveWildcard(models []string) bool {
	for _, name := range models {
		if strings.TrimSpace(name) == "*" {
			return true
		}
	}
	return false
}

// expandWildcardTokenModels replaces the "*" wildcard with the concrete models
// actually available to the user's group, while preserving explicitly listed
// models. Duplicates, blanks and the wildcard itself are dropped. When no
// wildcard is present the input is returned unchanged so exact whitelists keep
// their existing behaviour.
func expandWildcardTokenModels(tokenModels, groupModels []string) []string {
	if !tokenModelsHaveWildcard(tokenModels) {
		return tokenModels
	}
	seen := make(map[string]bool, len(tokenModels)+len(groupModels))
	expanded := make([]string, 0, len(tokenModels)+len(groupModels))
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || name == "*" || seen[name] {
			return
		}
		seen[name] = true
		expanded = append(expanded, name)
	}
	for _, name := range tokenModels {
		add(name)
	}
	for _, name := range groupModels {
		add(name)
	}
	return expanded
}

func ListModels(c *gin.Context) {
	ctx := c.Request.Context()
	var availableModels []string
	if c.GetString(ctxkey.AvailableModels) != "" {
		availableModels = strings.Split(c.GetString(ctxkey.AvailableModels), ",")
	} else {
		userId := c.GetInt(ctxkey.Id)
		userGroup, _ := model.CacheGetUserGroup(userId)
		availableModels, _ = model.CacheGetGroupModels(ctx, userGroup)
	}
	// A token whitelist containing "*" must be expanded before being returned:
	// the gateway authorizes "*" as "allow all", but clients see a literal "*"
	// and reject real model names in validate_model. Expand to the group's
	// concrete models so the list reflects what the account can actually call.
	if tokenModelsHaveWildcard(availableModels) {
		userId := c.GetInt(ctxkey.Id)
		userGroup, _ := model.CacheGetUserGroup(userId)
		groupModels, _ := model.CacheGetGroupModels(ctx, userGroup)
		availableModels = expandWildcardTokenModels(availableModels, groupModels)
	}
	modelSet := make(map[string]bool)
	for _, availableModel := range availableModels {
		modelSet[availableModel] = true
	}
	aliases, visibleAliases, concrete := userModelAliasDiscovery(c)
	for name := range modelSet {
		if aliases[strings.ToLower(name)] && !concrete[name] {
			delete(modelSet, name)
		}
	}
	for name := range visibleAliases {
		modelSet[name] = true
	}
	availableOpenAIModels := make([]OpenAIModels, 0)
	for _, model := range models {
		if aliases[model.Id] {
			continue
		}
		if _, ok := modelSet[model.Id]; ok {
			modelSet[model.Id] = false
			availableOpenAIModels = append(availableOpenAIModels, model)
		}
	}
	for modelName, ok := range modelSet {
		if ok {
			availableOpenAIModels = append(availableOpenAIModels, OpenAIModels{
				Id:      modelName,
				Object:  "model",
				Created: 1626777600,
				OwnedBy: "custom",
				Root:    modelName,
				Parent:  nil,
			})
		}
	}
	c.JSON(200, gin.H{
		"object": "list",
		"data":   availableOpenAIModels,
	})
}

// userModelAliasDiscovery classifies account aliases separately from static model names.
// 版本: v0.0.28
// 日期: 2026-11-22
func userModelAliasDiscovery(c *gin.Context) (map[string]bool, map[string]bool, map[string]bool) {
	aliases, visible := map[string]bool{}, map[string]bool{}
	items, available, err := model.GetUserModelAliasesWithGroupModels(c.Request.Context(), c.GetInt(ctxkey.Id))
	if err != nil {
		logger.Error(c.Request.Context(), "model alias discovery unavailable: "+err.Error())
		return aliases, visible, available
	}
	for _, item := range items {
		if available[item.Alias] {
			continue
		}
		aliases[item.Alias] = true
		for _, target := range item.Targets {
			if !available[target.Model] {
				continue
			}
			allowed := c.GetString(ctxkey.AvailableModels)
			if allowed == "" {
				visible[item.Alias] = true
				break
			}
			for _, name := range strings.Split(allowed, ",") {
				if name == "*" || name == target.Model || (item.Source == "global" && strings.EqualFold(name, item.Alias)) {
					visible[item.Alias] = true
					break
				}
			}
			if visible[item.Alias] {
				break
			}
		}
	}
	return aliases, visible, available
}

// RetrieveModel returns static model metadata or a visible account-local alias.
// 版本: v0.0.28
// 日期: 2026-11-22
func RetrieveModel(c *gin.Context) {
	modelId := c.Param("model")
	aliases, visible, concrete := userModelAliasDiscovery(c)
	aliasId := strings.ToLower(modelId)
	if visible[aliasId] && !concrete[modelId] {
		c.JSON(200, OpenAIModels{Id: aliasId, Object: "model", Created: 1626777600, OwnedBy: "custom", Root: aliasId})
		return
	}
	if !aliases[aliasId] || concrete[modelId] {
		if item, ok := modelsMap[modelId]; ok {
			c.JSON(200, item)
			return
		}
	}
	c.JSON(200, gin.H{"error": relaymodel.Error{Message: fmt.Sprintf("The model '%s' does not exist", modelId), Type: "invalid_request_error", Param: "model", Code: "model_not_found"}})
}

func GetUserAvailableModels(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.GetInt(ctxkey.Id)
	userGroup, err := model.CacheGetUserGroup(id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	models, err := model.CacheGetGroupModels(ctx, userGroup)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if aliases, available, e := model.GetUserModelAliasesWithGroupModels(ctx, id); e == nil {
		for _, alias := range aliases {
			if alias.Source != "global" || available[alias.Alias] {
				continue
			}
			for _, target := range alias.Targets {
				if available[target.Model] {
					models = append(models, alias.Alias)
					break
				}
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    models,
	})
	return
}
