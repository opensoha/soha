package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	apiresponse "github.com/soha/soha/internal/api/response"
	"github.com/soha/soha/internal/platform/runtimeobs"
)

func NewSystemHandler(postgres ReadinessProbe, metrics RuntimeMetricsProvider) *SystemHandler {
	return &SystemHandler{postgres: postgres, metrics: metrics}
}
func (h *SystemHandler) Healthz(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	status := map[string]string{"status": "ok", "postgres": "ok"}
	httpStatus := http.StatusOK
	if err := h.postgres.Ping(ctx); err != nil {
		status["status"] = "degraded"
		status["postgres"] = err.Error()
		httpStatus = http.StatusServiceUnavailable
	}
	apiresponse.JSON(c, httpStatus, status)
}
func (h *SystemHandler) Readyz(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := h.postgres.Ping(ctx); err != nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "postgres_unavailable", fmt.Sprintf("postgres not ready: %v", err))
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ready"})
}
func (h *SystemHandler) RuntimeMetrics(c *gin.Context) {
	if h.metrics == nil {
		apiresponse.JSON(c, http.StatusOK, runtimeobs.Snapshot{})
		return
	}
	apiresponse.JSON(c, http.StatusOK, h.metrics.Snapshot())
}
