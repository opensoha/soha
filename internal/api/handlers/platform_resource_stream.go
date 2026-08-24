package handlers

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func (h *resourceEventStreamHandler) StreamResourceEvents(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	events, unsubscribe, err := h.service.SubscribeResourceEvents(
		c.Request.Context(), principal, c.Param("clusterID"), c.Query("namespace"), splitResourceKinds(c.Query("kinds")),
	)
	if err != nil {
		writeError(c, err)
		return
	}
	defer unsubscribe()
	session, err := newWebSocketStreamSession(c)
	if err != nil {
		return
	}
	defer session.Close()
	session.SetPongWait(podLogPongWait)
	session.StartPing(podLogPingInterval)
	readDone := session.ReadMessages(func(message terminalMessage) bool { return message.Type != "close" }, nil)
	for {
		select {
		case <-session.Context().Done():
			return
		case <-readDone:
			return
		case event, ok := <-events:
			if !ok {
				_ = session.WriteJSON(domainresource.ResourceStreamEvent{
					Type: "error", ClusterID: c.Param("clusterID"), ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
					Source: "informer", CacheStatus: "degraded", Message: "resource event stream closed", ResyncRequired: true,
				})
				return
			}
			if err := session.WriteJSON(event); err != nil {
				return
			}
		}
	}
}

func splitResourceKinds(value string) []string {
	parts := strings.Split(value, ",")
	kinds := make([]string, 0, len(parts))
	for _, part := range parts {
		if kind := strings.TrimSpace(part); kind != "" {
			kinds = append(kinds, kind)
		}
	}
	return kinds
}
