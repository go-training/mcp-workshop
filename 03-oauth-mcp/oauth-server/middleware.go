package main

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// corsMiddleware is an optimized CORS handler for Gin.
// It merges allowed headers with defaults, sets standard options, and can be further customized.
func corsMiddleware(allowedHeaders ...string) gin.HandlerFunc {
	defaultHeaders := []string{"Mcp-Protocol-Version", "Authorization", "Content-Type"}
	var headersList []string
	if len(allowedHeaders) > 0 {
		headersMap := make(map[string]struct{})
		for _, h := range defaultHeaders {
			headersMap[strings.ToLower(h)] = struct{}{}
		}
		for _, h := range allowedHeaders {
			hNorm := strings.TrimSpace(h)
			if hNorm != "" && hNorm != "*" {
				headersMap[strings.ToLower(hNorm)] = struct{}{}
			}
		}
		// Output headers preserving canonical casing and custom order
		headers := []string{}
		for _, h := range defaultHeaders {
			headers = append(headers, h)
		}
		for _, h := range allowedHeaders {
			hNorm := strings.TrimSpace(h)
			if hNorm != "" && hNorm != "*" && !containsCI(defaultHeaders, hNorm) {
				headers = append(headers, hNorm)
			}
		}
		headersList = headers
	} else {
		headersList = defaultHeaders
	}

	allowedMethods := []string{"GET", "POST", "OPTIONS"}
	return func(c *gin.Context) {
		// For production, set allowlist for origins here; demo fallback is *
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Vary", "Origin")
		c.Header("Access-Control-Allow-Methods", strings.Join(allowedMethods, ", "))
		c.Header("Access-Control-Allow-Headers", strings.Join(headersList, ", "))
		c.Header("Access-Control-Max-Age", "86400")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}

// authMiddleware checks the HTTP Authorization header, aborts if missing
func authMiddleware(c *gin.Context) {
	if c.GetHeader("Authorization") == "" {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	c.Next()
}

// containsCI checks if slice contains item (case-insensitive).
func containsCI(slice []string, item string) bool {
	item = strings.ToLower(item)
	for _, s := range slice {
		if strings.ToLower(s) == item {
			return true
		}
	}
	return false
}
