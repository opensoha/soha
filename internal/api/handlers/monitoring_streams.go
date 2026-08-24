package handlers

import (
	"time"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
)

func (h *eventHandler) StreamEvents(c *gin.Context) {
	signals, err := h.service.SubscribeEventSignals(
		c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Query("clusterId"),
	)
	if err != nil {
		writeError(c, err)
		return
	}
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
		case signal, ok := <-signals:
			if !ok {
				_ = session.WriteJSON(domainalert.AlertEventStreamSignal{
					Type: "error", ObservedAt: time.Now().UTC(), Message: "alert event stream closed", ResyncRequired: true,
				})
				return
			}
			if err := session.WriteJSON(signal); err != nil {
				return
			}
		}
	}
}
