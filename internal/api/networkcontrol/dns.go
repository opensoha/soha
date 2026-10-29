package networkcontrol

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	"github.com/opensoha/soha/internal/networkidentity"
)

type siteDNSRuntime interface {
	DNSObservations(context.Context, networkidentity.Identity, []byte) error
}

func (h *handler) dnsObservations(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	raw, ok := h.body(c)
	if !ok {
		return
	}
	service, ok := h.service.(siteDNSRuntime)
	if !ok {
		apiresponse.Error(c, http.StatusServiceUnavailable, "site_dns_unavailable", "site DNS is unavailable")
		return
	}
	if err := service.DNSObservations(c.Request.Context(), identity, raw); err != nil {
		respondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
