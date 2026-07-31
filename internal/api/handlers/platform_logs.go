package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func (h *clusterLogResourceHandler) QueryClusterLogs(c *gin.Context) {
	var query domainresource.LogQuery
	if err := c.ShouldBindJSON(&query); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid log query")
		return
	}
	page, err := h.service.QueryClusterLogs(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), query)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, page)
}

func (h *clusterLogResourceHandler) IssueClusterLogStreamTicket(c *gin.Context) {
	var query domainresource.LogQuery
	if err := c.ShouldBindJSON(&query); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid log query")
		return
	}
	ticket, err := h.service.IssueClusterLogStreamTicket(
		c.Request.Context(),
		apiMiddleware.PrincipalFromContext(c),
		apiMiddleware.AccessContextFromContext(c),
		c.Param("clusterID"),
		query,
	)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, ticket)
}

func (h *clusterLogResourceHandler) StreamClusterLogs(c *gin.Context) {
	session, err := newWebSocketStreamSession(c)
	if err != nil {
		return
	}
	defer session.Close()
	session.SetPongWait(podLogPongWait)
	session.StartPing(podLogPingInterval)

	principal := apiMiddleware.PrincipalFromContext(c)
	accessCtx := apiMiddleware.AccessContextFromContext(c)
	clusterID := c.Param("clusterID")
	streamDone := make(chan error, 1)
	go func() {
		streamDone <- h.service.StreamClusterLogsFromTicket(
			session.Context(),
			principal,
			accessCtx,
			clusterID,
			func(event domainresource.LogStreamEvent) error { return session.WriteJSON(event) },
		)
	}()
	readDone := session.ReadMessages(func(message terminalMessage) bool {
		return message.Type != "close"
	}, nil)
	select {
	case streamErr := <-streamDone:
		if streamErr != nil {
			_ = session.WriteJSON(domainresource.LogStreamEvent{Type: "source_error", Status: &domainresource.LogStreamStatus{State: "degraded", Message: "log stream unavailable"}})
			_ = session.WriteJSON(domainresource.LogStreamEvent{Type: "end", Status: &domainresource.LogStreamStatus{State: "ended"}})
		}
		session.Cancel()
	case <-readDone:
		session.Cancel()
		<-streamDone
	}
}
