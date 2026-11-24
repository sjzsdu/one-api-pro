// model_alias_targets_test.go 别名候选来源标签回归测试
// Ensures alias target metadata carries provider labels from channels without leaking topology.
// 版本: v0.0.28
// 日期: 2026-11-24
package controller

import (
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/modelbus/one-api-pro/model"
	"gorm.io/gorm"
)

func setupAliasTargetChannels(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:alias-targets-%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	if err = db.AutoMigrate(&model.Channel{}, &model.ModelCatalog{}); err != nil {
		t.Fatal(err)
	}
	old := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = old; sqlDB.Close() })

	// Type 17 maps to provider "ali" via the registry; name/key are private topology.
	db.Create(&model.Channel{Id: 1, Type: 17, Name: "private-name", Key: "secret-key", Status: model.ChannelStatusEnabled, Models: "chat,wan2.6-t2i", Group: "default"})
	db.Create(&model.Channel{Id: 2, Type: 17, Status: model.ChannelStatusManuallyDisabled, Models: "disabled-only", Group: "default"})
}

func TestEnrichAliasTargetMetadata_ProviderFromChannels(t *testing.T) {
	setupAliasTargetChannels(t)
	names := []string{"chat", "wan2.6-t2i", "from-catalog"}
	metadata := []model.ModelCatalog{{ModelName: "from-catalog", Provider: "openai"}}

	got := enrichAliasTargetMetadata(names, metadata)
	providers := map[string]string{}
	for _, item := range got {
		providers[item.ModelName] = item.Provider
	}
	if providers["chat"] != "ali" || providers["wan2.6-t2i"] != "ali" {
		t.Fatalf("channel-backed models missing provider label: %+v", providers)
	}
	if providers["from-catalog"] != "openai" {
		t.Fatalf("catalog provider was overwritten: %+v", providers)
	}
	if _, ok := providers["disabled-only"]; ok {
		t.Fatal("disabled channel model leaked into target metadata")
	}
	// ModelCatalog has no channel name/id fields, so topology cannot leak here.
}

func TestEnrichAliasTargetMetadata_ModelMappingActualName(t *testing.T) {
	setupAliasTargetChannels(t)
	mapping := `{"chat":"qwen-plus"}`
	model.DB.Create(&model.Channel{Id: 3, Type: 17, Status: model.ChannelStatusEnabled, Models: "chat", Group: "default", ModelMapping: &mapping})

	got := enrichAliasTargetMetadata([]string{"qwen-plus"}, nil)
	found := false
	for _, item := range got {
		if item.ModelName == "qwen-plus" && item.Provider == "ali" {
			found = true
		}
	}
	if !found {
		t.Fatalf("mapped actual model name missing provider label: %+v", got)
	}
}
