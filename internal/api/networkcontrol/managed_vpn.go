package networkcontrol

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	response "github.com/opensoha/soha/internal/api/response"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
)

type ManagedVPNService interface {
	PrepareManagedVPN(context.Context, networkidentity.Identity, []byte) (networkprotocol.RuntimeMessage, error)
	ConnectManagedVPN(context.Context, networkidentity.Identity, []byte) (networkprotocol.RuntimeMessage, error)
}

func (h *handler) managedVPN(c *gin.Context, prepare bool) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	raw, ok := h.body(c)
	if !ok {
		return
	}
	service, ok := h.service.(ManagedVPNService)
	if !ok {
		response.Error(c, http.StatusServiceUnavailable, "managed_vpn_unavailable", "managed VPN is unavailable")
		return
	}
	var result networkprotocol.RuntimeMessage
	var err error
	if prepare {
		result, err = service.PrepareManagedVPN(c.Request.Context(), identity, raw)
	} else {
		result, err = service.ConnectManagedVPN(c.Request.Context(), identity, raw)
	}
	if err != nil {
		respondError(c, err)
		return
	}
	response.JSON(c, http.StatusOK, result)
}
