package controller

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/modelbus/one-api-pro/common/ctxkey"
	"github.com/modelbus/one-api-pro/model"
	"github.com/modelbus/one-api-pro/modelrouter"
)

const (
	defaultRoutingDecisionLimit = 50
	maxRoutingDecisionLimit     = 200
)

// GetRoutingDecisions returns recent decisions newest-first. The route is
// protected by AdminAuth where it is registered.
func GetRoutingDecisions(c *gin.Context) {
	limit := defaultRoutingDecisionLimit
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "limit must be a positive integer"})
			return
		}
		limit = parsed
	}
	if limit > maxRoutingDecisionLimit {
		limit = maxRoutingDecisionLimit
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    modelrouter.RecentRoutingDecisions(limit),
	})
}

// QuizRequest is the request body for the model routing quiz.
type QuizRequest struct {
	Prompt   string `json:"prompt" binding:"required"`
	Strategy string `json:"strategy,omitempty"` // optional: override strategy
}

// ModelRouterQuiz simulates model routing without making real LLM calls.
// It analyzes the prompt and shows which model would be selected.
func ModelRouterQuiz(c *gin.Context) {
	var req QuizRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Invalid request: " + err.Error(),
		})
		return
	}

	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Prompt cannot be empty",
		})
		return
	}
	if len([]rune(req.Prompt)) > 20000 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "Prompt cannot exceed 20000 characters",
		})
		return
	}

	userID := c.GetInt(ctxkey.Id)
	group, err := model.CacheGetUserGroup(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "Failed to determine the user's model group",
		})
		return
	}
	strategy := strings.TrimSpace(req.Strategy)
	if strategy == "" {
		strategy = "balanced"
	}
	result, err := modelrouter.SimulateRouting(c.Request.Context(), group, req.Prompt, strategy)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}

// GetEmbeddingManifest returns the current embedding manifest metadata.
func GetEmbeddingManifest(c *gin.Context) {
	manifest, err := modelrouter.LoadEmbeddingManifest(os.Getenv("EMBEDDING_MANIFEST_PATH"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	models := make([]gin.H, 0, len(manifest.Models))
	for name, info := range manifest.Models {
		files := make([]gin.H, 0, len(info.Files))
		for _, f := range info.Files {
			entry := gin.H{"filename": f.Filename, "url": f.URL}
			if f.SHA256 != "" {
				entry["sha256"] = f.SHA256
			}
			if f.Size > 0 {
				entry["size"] = f.Size
			}
			files = append(files, entry)
		}
		models = append(models, gin.H{
			"name":      name,
			"dimension": info.Dimension,
			"revision":  info.Revision,
			"files":     files,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"revision": manifest.Revision,
			"models":   models,
		},
	})
}

// ReloadEmbeddingManifest invalidates the remote manifest cache and reloads.
func ReloadEmbeddingManifest(c *gin.Context) {
	modelrouter.InvalidateRemoteManifestCache()

	manifestPath := os.Getenv("EMBEDDING_MANIFEST_PATH")
	manifest, err := modelrouter.LoadEmbeddingManifest(manifestPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	modelNames := make([]string, 0, len(manifest.Models))
	for name := range manifest.Models {
		modelNames = append(modelNames, name)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"revision": manifest.Revision,
			"models":   modelNames,
			"count":    len(manifest.Models),
		},
		"message": "manifest reloaded successfully",
	})
}
