package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
)

func registerAggregateLogRoutes(platform *gin.RouterGroup, client *k8sagent.Client) {
	platform.POST("/logs/query", func(c *gin.Context) {
		var query domainresource.LogQuery
		if err := c.ShouldBindJSON(&query); err != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid log query")
			return
		}
		page, err := client.QueryPodLogs(c.Request.Context(), query)
		if err != nil {
			writeAggregateLogError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, page)
	})

	platform.POST("/logs/stream", func(c *gin.Context) {
		var query domainresource.LogQuery
		if err := c.ShouldBindJSON(&query); err != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid log query")
			return
		}
		if err := clearResponseWriteDeadline(c); err != nil {
			apiresponse.Error(c, http.StatusInternalServerError, "stream_unavailable", "streaming response is unavailable")
			return
		}
		started := false
		encoder := json.NewEncoder(c.Writer)
		responseController := http.NewResponseController(c.Writer)
		err := client.StreamPodLogEvents(c.Request.Context(), query, func(event domainresource.LogStreamEvent) error {
			if !started {
				started = true
				c.Header("Content-Type", "application/x-ndjson")
				c.Status(http.StatusOK)
			}
			if err := responseController.SetWriteDeadline(time.Now().Add(30 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
				return err
			}
			if err := encoder.Encode(event); err != nil {
				return err
			}
			c.Writer.Flush()
			return nil
		})
		if err != nil && !started {
			writeAggregateLogError(c, err)
		}
	})
}

func writeAggregateLogError(c *gin.Context, err error) {
	if errors.Is(err, k8sagent.ErrInvalidLogQuery) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid log query")
		return
	}
	writeError(c, err)
}
