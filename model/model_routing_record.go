package model

import (
	"context"
	"time"

	"github.com/modelbus/one-api-pro/common/helper"
)

// ModelRoutingRecord is the persistent audit trail for a request that entered
// through model:auto. JSON fields contain routing metadata only; prompt text,
// message content, and API keys are deliberately never stored here.
type ModelRoutingRecord struct {
	Id                  int     `json:"id"`
	CreatedAt           int64   `json:"created_at" gorm:"bigint;index:idx_model_routing_created_at"`
	RequestId           string  `json:"request_id" gorm:"size:64;index"`
	UserId              int     `json:"user_id" gorm:"index"`
	UserGroup           string  `json:"user_group" gorm:"size:64;index"`
	Strategy            string  `json:"strategy" gorm:"size:32;index"`
	SelectedModel       string  `json:"selected_model" gorm:"size:255;index"`
	Score               float64 `json:"score"`
	TurnType            string  `json:"turn_type" gorm:"size:32;index"`
	Difficulty          string  `json:"difficulty" gorm:"size:32;index"`
	Category            string  `json:"category" gorm:"size:32;index"`
	EstimatedTokens     int     `json:"estimated_tokens"`
	MaxOutputTokens     int     `json:"max_output_tokens"`
	HasImages           bool    `json:"has_images"`
	HasTools            bool    `json:"has_tools"`
	HasToolResult       bool    `json:"has_tool_result"`
	RouterLatencyMs     int64   `json:"router_latency_ms"`
	Candidates          string  `json:"candidates" gorm:"type:text"`
	CandidateScores     string  `json:"candidate_scores" gorm:"type:text"`
	SelectedScoreDetail string  `json:"selected_score_detail" gorm:"type:text"`
	FilteredOut         string  `json:"filtered_out" gorm:"type:text"`
	FilterReasons       string  `json:"filter_reasons" gorm:"type:text"`
	ClusterMatches      string  `json:"cluster_matches" gorm:"type:text"`
	Reason              string  `json:"reason" gorm:"type:text"`
	Error               string  `json:"error" gorm:"type:text"`
}

func RecordModelRoutingRecord(ctx context.Context, record *ModelRoutingRecord) error {
	if DB == nil || record == nil {
		return nil
	}
	if record.CreatedAt == 0 {
		record.CreatedAt = time.Now().Unix()
	}
	if record.RequestId == "" {
		record.RequestId = helper.GetRequestID(ctx)
	}
	return DB.Create(record).Error
}
