package middleware

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/modelbus/one-api-pro/common/logger"
)

// IP blacklist for security
var (
	ipBlacklist   = make(map[string]time.Time)
	ipBlacklistMu sync.RWMutex
	blacklistDuration = 24 * time.Hour
)

// AddIPToBlacklist adds an IP to the blacklist for a specified duration
func AddIPToBlacklist(ip string, duration time.Duration) {
	ipBlacklistMu.Lock()
	defer ipBlacklistMu.Unlock()
	ipBlacklist[ip] = time.Now().Add(duration)
}

// IsIPBlacklisted checks if an IP is currently blacklisted
func IsIPBlacklisted(ip string) bool {
	ipBlacklistMu.RLock()
	defer ipBlacklistMu.RUnlock()
	expiry, exists := ipBlacklist[ip]
	if !exists {
		return false
	}
	if time.Now().After(expiry) {
		delete(ipBlacklist, ip)
		return false
	}
	return true
}

// CleanupBlacklist removes expired entries periodically
func CleanupBlacklist() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		ipBlacklistMu.Lock()
		now := time.Now()
		for ip, expiry := range ipBlacklist {
			if now.After(expiry) {
				delete(ipBlacklist, ip)
			}
		}
		ipBlacklistMu.Unlock()
	}
}

// IPBlacklistMiddleware blocks blacklisted IPs
func IPBlacklistMiddleware() func(c *gin.Context) {
	return func(c *gin.Context) {
		clientIP := c.ClientIP()
		if IsIPBlacklisted(clientIP) {
			logger.SysLog("blocked request from blacklisted IP: " + clientIP)
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "IP已被临时封禁",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// SecurityAuditMiddleware logs security-relevant events
func SecurityAuditMiddleware() func(c *gin.Context) {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		latency := time.Since(start)
		statusCode := c.Writer.Status()
		clientIP := c.ClientIP()
		method := c.Request.Method
		path := c.Request.URL.Path

		// Log suspicious activities
		if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
			logger.SysLog(fmt.Sprintf("SECURITY: %s %s from %s returned %d (latency: %v)",
				method, path, clientIP, statusCode, latency))
		}
	}
}
