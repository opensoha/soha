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
	contractresource "github.com/opensoha/soha-contracts/resource"
)

func registerPodStreamRoutes(platform *gin.RouterGroup, client *k8sagent.Client) {
	stream := func(c *gin.Context, expectedUID string) {
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
	}
	platform.GET("/workloads/pods/:name/logs/stream", podSessionHandler(false, stream))
	platform.GET("/workloads/pods/:name/uid-bound/logs/stream", podSessionHandler(true, stream))
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

func podSessionHandler(requireUID bool, handle func(*gin.Context, string)) gin.HandlerFunc {
	return func(c *gin.Context) {
		if expectedUID, valid := podExpectedUID(c, requireUID); valid {
			handle(c, expectedUID)
		}
	}
}

// Legacy routes accept omission; UID-bound routes reject invalid identity before opening a session.
func podExpectedUID(c *gin.Context, requireUID bool) (string, bool) {
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "malformed session query")
		c.Abort()
		return "", false
	}
	values, present := query["expectedUid"]
	if !present && !requireUID {
		return "", true
	}
	if len(values) != 1 || strings.TrimSpace(values[0]) == "" || (requireUID && !contractresource.ValidWorkloadUID(values[0])) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "expectedUid must be one valid, non-empty value")
		c.Abort()
		return "", false
	}
	return values[0], true
}
