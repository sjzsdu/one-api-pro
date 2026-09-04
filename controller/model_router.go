package controller

import (
	"net/http"
	"strconv"

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
