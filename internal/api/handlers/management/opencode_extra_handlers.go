package management

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/auth/opencode"
	log "github.com/sirupsen/logrus"
)

// opencodeFetchGoUsageRequest is the request body for fetching Go plan usage.
type opencodeFetchGoUsageRequest struct {
	Cookie      string `json:"cookie" binding:"required"`
	WorkspaceID string `json:"workspace_id" binding:"required"`
}

// FetchOpenCodeGoUsage retrieves Go plan usage data from the OpenCode SSR HTML page.
func (h *Handler) FetchOpenCodeGoUsage(c *gin.Context) {
	var req opencodeFetchGoUsageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cookie and workspace_id are required"})
		return
	}
	cookie := strings.TrimSpace(req.Cookie)
	if !strings.HasPrefix(cookie, "auth=") {
		cookie = "auth=" + cookie
	}
	client := opencode.NewClient(h.cfg)
	usage, err := opencode.FetchGoUsage(client, cookie, req.WorkspaceID)
	if err != nil {
		log.Errorf("opencode: failed to fetch Go usage: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to fetch Go usage"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"usage": usage})
}

// FetchOpenCodeModels retrieves the available OpenCode Go models via the API key.
func (h *Handler) FetchOpenCodeModels(c *gin.Context) {
	var req struct {
		APIKey string `json:"api_key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.APIKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "api_key is required"})
		return
	}
	client := opencode.NewClient(h.cfg)
	models, err := opencode.FetchModels(client, req.APIKey)
	if err != nil {
		log.Errorf("opencode: failed to fetch models: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to fetch models"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"models": models, "count": len(models)})
}
