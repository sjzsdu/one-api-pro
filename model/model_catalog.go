package model

import (
	"time"

	"gorm.io/gorm/clause"
)

// ModelCatalog keeps provider-supplied discovery metadata separate from the
// gateway's billing ModelPrice table. OpenRouter prices are USD per million
// tokens and must never be used as local quota pricing without an explicit
// conversion policy.
type ModelCatalog struct {
	Id                    uint     `json:"id" gorm:"primaryKey;autoIncrement"`
	Provider              string   `json:"provider" gorm:"size:64;uniqueIndex:idx_provider_model;not null"`
	ModelName             string   `json:"model_name" gorm:"size:255;uniqueIndex:idx_provider_model;not null"`
	DisplayName           string   `json:"display_name" gorm:"size:255"`
	Description           string   `json:"description" gorm:"type:text"`
	ContextLength         int      `json:"context_length"`
	InputModalities       string   `json:"input_modalities" gorm:"type:text"`
	OutputModalities      string   `json:"output_modalities" gorm:"type:text"`
	SupportedParameters   string   `json:"supported_parameters" gorm:"type:text"`
	InputPricePerMillion  *float64 `json:"input_price_usd_per_million"`
	OutputPricePerMillion *float64 `json:"output_price_usd_per_million"`
	SourceURL             string   `json:"source_url" gorm:"type:text"`
	UpdatedAt             int64    `json:"updated_at" gorm:"bigint;index"`
}

func UpsertModelCatalog(items []ModelCatalog) error {
	if len(items) == 0 {
		return nil
	}
	now := time.Now().Unix()
	for i := range items {
		items[i].UpdatedAt = now
	}
	return DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "provider"}, {Name: "model_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"display_name", "description", "context_length", "input_modalities", "output_modalities", "supported_parameters", "input_price_per_million", "output_price_per_million", "source_url", "updated_at"}),
	}).Create(&items).Error
}

func GetModelCatalogs(provider string, names []string) (map[string]ModelCatalog, error) {
	result := make(map[string]ModelCatalog, len(names))
	if len(names) == 0 {
		return result, nil
	}
	var items []ModelCatalog
	if err := DB.Where("provider = ? AND model_name IN ?", provider, names).Find(&items).Error; err != nil {
		return nil, err
	}
	for _, item := range items {
		result[item.ModelName] = item
	}
	return result, nil
}
