package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

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
	Group    string `json:"group,omitempty"`
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

	// Use default strategy or override
	strategy := req.Strategy
	if strategy == "" {
		strategy = "scoring" // default to scoring for quiz
	}

	group := strings.TrimSpace(req.Group)
	if group == "" {
		group = "default"
	}
	result, err := modelrouter.SimulateRouting(c.Request.Context(), group, req.Prompt, strategy)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    result,
	})
}
