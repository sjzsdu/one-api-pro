// model_capability.go 全供应商模型能力目录及账号范围选项
// Exposes capability metadata without leaking channel topology to account users.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex
package controller

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/relay"
	"github.com/modelbus/one-api-pro/relay/registry"
	"gorm.io/gorm/clause"
)

type modelOption struct {
	ModelName      string                  `json:"model_name"`
	DisplayName    string                  `json:"display_name,omitempty"`
	Capabilities   model.ModelCapabilities `json:"capabilities"`
	GatewaySupport map[string][]string     `json:"gateway_support"`
}

func configuredModelItems(channels []*model.Channel) ([]configuredModelItem, error) {
	names := map[string][]string{}
	for _, channel := range channels {
		provider := registry.IDByLegacyType(channel.Type)
		mapping := channel.GetModelMapping()
		for _, name := range strings.Split(channel.Models, ",") {
			name = strings.TrimSpace(name)
			if name == "" || name == "*" {
				continue
			}
			actual := name
			if mapping[name] != "" {
				actual = mapping[name]
			}
			names[provider] = append(names[provider], actual)
		}
	}
	catalogs := map[string]map[string]model.ModelCatalog{}
	for provider, list := range names {
		items, err := model.GetModelCatalogs(provider, list)
		if err != nil {
			return nil, err
		}
		catalogs[provider] = items
	}
	items := make([]configuredModelItem, 0)
	for _, channel := range channels {
		provider := registry.IDByLegacyType(channel.Type)
		mapping := channel.GetModelMapping()
		for _, name := range strings.Split(channel.Models, ",") {
			name = strings.TrimSpace(name)
			if name == "" || name == "*" {
				continue
			}
			actual := name
			if mapping[name] != "" {
				actual = mapping[name]
			}
			item := configuredModelItem{CatalogModelName: actual, ModelName: name, ChannelID: channel.Id, ChannelName: channel.Name, ChannelType: channel.Type, ChannelStatus: channel.Status, Group: channel.Group, Provider: provider}
			var metadata *model.ModelCatalog
			if row, ok := catalogs[provider][actual]; ok {
				metadata = &row
				item.DisplayName = row.DisplayName
				item.Description = row.Description
				item.ContextLength = row.ContextLength
				item.InputPriceUSDPerMillion = row.InputPricePerMillion
				item.OutputPriceUSDPerMillion = row.OutputPricePerMillion
				_ = json.Unmarshal([]byte(row.SupportedParameters), &item.SupportedParameters)
			}
			item.Capabilities = model.ResolveModelCapabilities(provider, actual, metadata)
			item.InputModalities = item.Capabilities.InputModalities
			item.OutputModalities = item.Capabilities.OutputModalities
			item.GatewaySupport = relay.RuntimeOperationModes(provider, actual)
			item.ProbeOperations = ProbeOperations(provider, actual)
			for operation := range item.ProbeOperations {
				if !containsModelCapability(item.Capabilities.Operations[operation].Modes, "sync") {
					delete(item.ProbeOperations, operation)
				}
			}
			items = append(items, item)
		}
	}
	return items, nil
}

// ListModelOptions returns only models visible in the account's group, with no channel identifiers.
// 版本: v0.0.28
// 日期: 2026-11-23
func ListModelOptions(c *gin.Context) {
	all := c.Query("scope") == "all"
	if all && c.GetInt("role") < model.RoleAdminUser {
		c.JSON(200, gin.H{"success": false, "message": "无权查看全部模型"})
		return
	}
	group, err := model.GetUserGroup(c.GetInt(ctxkey.Id))
	if err != nil {
		c.JSON(200, gin.H{"success": false, "message": "无法读取当前账号分组"})
		return
	}
	rows, err := cachedConfiguredModelItems()
	if err != nil {
		c.JSON(200, gin.H{"success": false, "message": "无法读取模型目录"})
		return
	}
	byName := map[string]modelOption{}
	for _, row := range rows {
		if row.ChannelStatus != model.ChannelStatusEnabled || !all && !(&model.Channel{Group: row.Group}).ContainsGroup(group) {
			continue
		}
		if prior, ok := byName[row.ModelName]; ok {
			if prior.GatewaySupport == nil {
				prior.GatewaySupport = map[string][]string{}
			}
			// Options describe available paths. Keep per-operation limits from one binding;
			// runtime still filters the chosen binding and never trusts a flattened union.
			for operation, capability := range row.Capabilities.Operations {
				if _, exists := prior.Capabilities.Operations[operation]; !exists {
					prior.Capabilities.Operations[operation] = capability
				}
			}
			for operation, modes := range row.GatewaySupport {
				for _, mode := range modes {
					if !containsModelCapability(prior.GatewaySupport[operation], mode) {
						prior.GatewaySupport[operation] = append(prior.GatewaySupport[operation], mode)
					}
				}
			}
			byName[row.ModelName] = prior
		} else {
			byName[row.ModelName] = modelOption{ModelName: row.ModelName, DisplayName: row.DisplayName, Capabilities: row.Capabilities, GatewaySupport: row.GatewaySupport}
		}
	}
	if !all {
		aliases, visible, aliasErr := model.GetUserModelAliasesWithGroupModels(c.Request.Context(), c.GetInt(ctxkey.Id))
		if aliasErr != nil {
			c.JSON(200, gin.H{"success": false, "message": "无法读取模型别名"})
			return
		}
		for _, alias := range aliases {
			if visible[alias.Alias] {
				continue
			}
			var targets []modelOption
			for _, target := range alias.Targets {
				if item, ok := byName[target.Model]; ok && visible[target.Model] {
					targets = append(targets, item)
				}
			}
			if len(targets) == 0 {
				continue
			}
			option := intersectAliasModelOption(alias.Alias, targets)
			if operation := c.Query("operation"); operation != "" {
				if _, ok := option.Capabilities.Operations[operation]; !ok {
					continue
				}
			}
			byName[alias.Alias] = option
		}
	}
	items := make([]modelOption, 0, len(byName))
	for _, item := range byName {
		if operation := c.Query("operation"); operation != "" {
			if _, ok := item.Capabilities.Operations[operation]; !ok {
				continue
			}
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ModelName < items[j].ModelName })
	c.JSON(200, gin.H{"success": true, "message": "", "data": items})
}

func intersectAliasModelOption(name string, targets []modelOption) modelOption {
	first := targets[0]
	capability := model.ModelCapabilities{PrimaryCategory: first.Capabilities.PrimaryCategory, Source: "alias", Operations: map[string]model.OperationCapability{}}
	support := map[string][]string{}
	for operation, descriptor := range first.Capabilities.Operations {
		modes := append([]string(nil), descriptor.Modes...)
		for _, target := range targets[1:] {
			other, ok := target.Capabilities.Operations[operation]
			if !ok {
				modes = nil
				break
			}
			modes = intersectCapabilityValues(modes, other.Modes)
		}
		if len(modes) == 0 {
			continue
		}
		// A pool guarantees only common operations and modes. Parameter validation
		// remains binding-specific, rather than advertising one target's limits for all.
		capability.Operations[operation] = model.OperationCapability{Modes: modes}
		actual := append([]string(nil), first.GatewaySupport[operation]...)
		for _, target := range targets[1:] {
			actual = intersectCapabilityValues(actual, target.GatewaySupport[operation])
		}
		if len(actual) > 0 {
			support[operation] = actual
		}
	}
	for _, target := range targets[1:] {
		if target.Capabilities.PrimaryCategory != capability.PrimaryCategory {
			capability.PrimaryCategory = "unknown"
		}
	}
	return modelOption{ModelName: name, Capabilities: capability, GatewaySupport: support}
}

func intersectCapabilityValues(left, right []string) []string {
	result := []string{}
	for _, value := range left {
		if containsModelCapability(right, value) {
			result = append(result, value)
		}
	}
	return result
}

func containsModelCapability(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

// UpdateModelCapabilities stores an explicit override without overwriting upstream discovery metadata.
// 版本: v0.0.28
// 日期: 2026-11-23
func UpdateModelCapabilities(c *gin.Context) {
	if c.GetInt("role") < model.RoleAdminUser {
		c.JSON(200, gin.H{"success": false, "message": "无权修改模型能力"})
		return
	}
	var request struct {
		Provider     string                   `json:"provider"`
		ModelName    string                   `json:"model_name"`
		Capabilities *model.ModelCapabilities `json:"capabilities"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Provider == "" || strings.TrimSpace(request.ModelName) == "" || len(request.ModelName) > 255 {
		c.JSON(200, gin.H{"success": false, "message": "无效的模型能力配置"})
		return
	}
	if _, ok := registry.GetChannelMeta(request.Provider); !ok {
		c.JSON(200, gin.H{"success": false, "message": "无效的供应商"})
		return
	}
	encoded := ""
	if request.Capabilities != nil {
		if err := model.ValidateModelCapabilities(*request.Capabilities); err != nil {
			c.JSON(200, gin.H{"success": false, "message": err.Error()})
			return
		}
		body, _ := json.Marshal(request.Capabilities)
		encoded = string(body)
	}
	row := model.ModelCatalog{Provider: request.Provider, ModelName: request.ModelName, CapabilityOverride: encoded, UpdatedAt: time.Now().Unix()}
	err := model.DB.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "provider"}, {Name: "model_name"}}, DoUpdates: clause.AssignmentColumns([]string{"capability_override", "updated_at"})}).Create(&row).Error
	if err != nil {
		c.JSON(200, gin.H{"success": false, "message": "保存能力配置失败"})
		return
	}
	if err := model.DB.Where("provider = ? AND model_name = ?", request.Provider, request.ModelName).First(&row).Error; err != nil {
		c.JSON(200, gin.H{"success": false, "message": "读取模型能力失败"})
		return
	}
	c.JSON(200, gin.H{"success": true, "message": "", "data": model.ResolveModelCapabilities(request.Provider, request.ModelName, &row)})
}
