package networkcontrol

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
)

type EAPLifecycleRuntime interface {
	RevokeEAPCertificate(context.Context, networkidentity.Identity, []byte) (networkprotocol.RuntimeMessage, error)
}

func (h *handler) revokeEAPCertificate(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	if h.options.EAPLifecycle == nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	raw, ok := h.body(c)
	if !ok {
		return
	}
	result, err := h.options.EAPLifecycle.RevokeEAPCertificate(c.Request.Context(), identity, raw)
	if err != nil {
		respondError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
