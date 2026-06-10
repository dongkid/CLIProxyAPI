package management

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	internalusage "github.com/router-for-me/CLIProxyAPI/v7/internal/usage"
)

// GetUsage returns the full in-memory usage statistics snapshot.
func (h *Handler) GetUsage(c *gin.Context) {
	snapshot := internalusage.GetRequestStatistics().Snapshot()
	c.JSON(http.StatusOK, snapshot)
}

// ExportUsage exports the usage statistics snapshot as a downloadable JSON file.
func (h *Handler) ExportUsage(c *gin.Context) {
	snapshot := internalusage.GetRequestStatistics().Snapshot()
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to marshal usage data"})
		return
	}
	c.Header("Content-Type", "application/json")
	c.Header("Content-Disposition", "attachment; filename=usage_stats.json")
	c.Data(http.StatusOK, "application/json", data)
}

// ImportUsage merges an uploaded usage statistics snapshot into the current store.
// ImportUsage merges an uploaded usage statistics snapshot into the current store.
// Limits request body to 50 MB to prevent memory exhaustion.
func (h *Handler) ImportUsage(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 50<<20)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}
	if len(body) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "empty request body"})
		return
	}
	var snapshot internalusage.StatisticsSnapshot
	if err := json.Unmarshal(body, &snapshot); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON format: " + err.Error()})
		return
	}
	result := internalusage.GetRequestStatistics().MergeSnapshot(snapshot)
	c.JSON(http.StatusOK, gin.H{
		"added":   result.Added,
		"skipped": result.Skipped,
	})
}
