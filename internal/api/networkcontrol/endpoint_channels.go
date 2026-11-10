package networkcontrol

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	ec "github.com/opensoha/soha-contracts/endpointcontrol"
	response "github.com/opensoha/soha/internal/api/response"
	"github.com/opensoha/soha/internal/networkidentity"
)

type endpointChannels interface {
	ChannelControls(context.Context, networkidentity.Identity) (ec.Desired, error)
	ChannelReport(context.Context, networkidentity.Identity, []byte) error
}

func (h *handler) channelControls(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	s, ok := h.service.(endpointChannels)
	if !ok {
		response.Error(c, 503, "endpoint_controls_unavailable", "Endpoint controls unavailable.")
		return
	}
	d, err := s.ChannelControls(c.Request.Context(), identity)
	if err != nil {
		respondError(c, err)
		return
	}
	response.Item(c, http.StatusOK, d)
}
func (h *handler) channelReport(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	raw, ok := h.body(c)
	if !ok {
		return
	}
	s, ok := h.service.(endpointChannels)
	if !ok {
		response.Error(c, 503, "endpoint_controls_unavailable", "Endpoint controls unavailable.")
		return
	}
	if err := s.ChannelReport(c.Request.Context(), identity, raw); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
