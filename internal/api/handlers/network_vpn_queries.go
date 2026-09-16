package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
)

type NetworkVPNQueries interface {
	Dashboard(context.Context, identity.Principal, domain.VPNDashboardFilter) (domain.VPNDashboard, error)
	Decision(context.Context, identity.Principal, string) (domain.VPNDecision, error)
	CurrentConnection(context.Context, identity.Principal, string) (*domain.VPNConnectionView, error)
	PreviewSelection(context.Context, identity.Principal, domain.VPNPreviewInput) (domain.VPNDecision, error)
}

func (h *NetworkVPNHandler) Dashboard(c *gin.Context) {
	var q api.GetNetworkVPNDashboardParams
	if !bindNetworkQuery(c, &q) {
		return
	}
	item, err := h.queries.Dashboard(c.Request.Context(), principal(c), domain.VPNDashboardFilter{From: q.From, To: q.To, ProfileID: q.ProfileID, SiteID: q.SiteID, GatewayID: q.GatewayID, SubjectID: q.SubjectID, TeamID: q.TeamID, ProviderCode: q.ProviderCode, Limit: q.Limit})
	c.Header("Cache-Control", "no-store")
	respondItem(c, http.StatusOK, item, err)
}
func (h *NetworkVPNHandler) Decision(c *gin.Context) {
	item, err := h.queries.Decision(c.Request.Context(), principal(c), c.Param("id"))
	c.Header("Cache-Control", "no-store")
	respondItem(c, http.StatusOK, item, err)
}
func (h *NetworkVPNHandler) CurrentConnection(c *gin.Context) {
	var q api.GetCurrentNetworkVPNConnectionParams
	if !bindNetworkQuery(c, &q) {
		return
	}
	item, err := h.queries.CurrentConnection(c.Request.Context(), principal(c), q.DeviceID)
	c.Header("Cache-Control", "no-store")
	respondItem(c, http.StatusOK, item, err)
}
func (h *NetworkVPNHandler) PreviewSelection(c *gin.Context) {
	var input api.NetworkVPNPreviewInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.queries.PreviewSelection(c.Request.Context(), principal(c), domain.VPNPreviewInput{DeviceID: input.DeviceID, ProfileID: input.ProfileID, Selection: string(input.Selection), GatewayID: input.GatewayID, ProbeBatchID: input.ProbeBatchID})
	c.Header("Cache-Control", "no-store")
	respondItem(c, http.StatusOK, item, err)
}
