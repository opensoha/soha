package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
)

func registerPodStreamRoutes(platform *gin.RouterGroup, client *k8sagent.Client) {
	platform.GET("/workloads/pods/:name/logs/stream", func(c *gin.Context) {
		expectedUID, valid := podExpectedUID(c)
		if !valid {
			return
		}
		namespace := c.DefaultQuery("namespace", "default")
		tailLines := int64(parseLimit(c.Query("tailLines"), 200))
		sinceSeconds := int64(parseLimit(c.Query("sinceSeconds"), 0))
		if err := clearResponseWriteDeadline(c); err != nil {
			apiresponse.Error(c, http.StatusInternalServerError, "stream_unavailable", "streaming response is unavailable")
			return
		}

		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.Header("Cache-Control", "no-store")
		flusher, _ := c.Writer.(http.Flusher)
		writer := podLogStreamFlushWriter{writer: c.Writer, flusher: flusher}
		err := client.StreamPodLogs(c.Request.Context(), namespace, c.Param("name"), c.Query("container"), tailLines, sinceSeconds, writer, expectedUID)
		if err == nil || errors.Is(err, context.Canceled) {
			return
		}
		if !c.Writer.Written() {
			writeError(c, err)
			return
		}
		_, _ = fmt.Fprintln(writer, "\n[agent] pod log stream ended with an error")
	})
}

type podLogStreamFlushWriter struct {
	writer  http.ResponseWriter
	flusher http.Flusher
}

func (w podLogStreamFlushWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	if w.flusher != nil {
		w.flusher.Flush()
	}
	return n, err
}

// An absent UID keeps the legacy protocol; an explicitly malformed UID never downgrades it.
func podExpectedUID(c *gin.Context) (string, bool) {
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "malformed session query")
		c.Abort()
		return "", false
	}
	values, present := query["expectedUid"]
	if !present {
		return "", true
	}
	if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "expectedUid must be one non-empty value")
		c.Abort()
		return "", false
	}
	return values[0], true
}
