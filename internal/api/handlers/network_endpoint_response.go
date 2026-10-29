package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
)

type EndpointResponsePolicies interface {
	Policies(context.Context, identity.Principal, domain.EndpointResponseFilter) ([]domain.EndpointResponsePolicy, error)
	Policy(context.Context, identity.Principal, string) (domain.EndpointResponsePolicy, error)
	SavePolicy(context.Context, identity.Principal, string, domain.EndpointResponsePolicyInput) (domain.EndpointResponsePolicy, error)
	PublishPolicy(context.Context, identity.Principal, string, int, int) (domain.EndpointResponsePolicy, error)
	DeletePolicy(context.Context, identity.Principal, string, int) error
	Revisions(context.Context, identity.Principal, string) ([]domain.EndpointResponsePolicyRevision, error)
}

type EndpointResponsePreviewer interface {
	Preview(context.Context, identity.Principal, string, sohaapi.NetworkEndpointResponsePreviewInput) (domain.EndpointResponsePreview, error)
}

type EndpointResponseEvents interface {
	Presence(context.Context, identity.Principal, string) (domain.EndpointPresence, error)
	Events(context.Context, identity.Principal, domain.EndpointResponseFilter) ([]domain.EndpointResponseEvent, error)
	Event(context.Context, identity.Principal, string) (domain.EndpointResponseEvent, error)
	Restore(context.Context, identity.Principal, string, sohaapi.NetworkEndpointResponseRestoreInput) (domain.EndpointResponseEvent, error)
	Maintenance(context.Context, identity.Principal, domain.EndpointResponseFilter) ([]domain.EndpointMaintenance, error)
	CreateMaintenance(context.Context, identity.Principal, domain.EndpointMaintenanceInput) (domain.EndpointMaintenance, error)
	EndMaintenance(context.Context, identity.Principal, string) error
}

type NetworkEndpointResponseHandler struct {
	policies EndpointResponsePolicies
	preview  EndpointResponsePreviewer
	events   EndpointResponseEvents
}

func NewNetworkEndpointResponseHandler(policies EndpointResponsePolicies, preview EndpointResponsePreviewer, events EndpointResponseEvents) *NetworkEndpointResponseHandler {
	return &NetworkEndpointResponseHandler{policies: policies, preview: preview, events: events}
}

func (h *NetworkEndpointResponseHandler) ListPolicies(c *gin.Context) {
	var params sohaapi.ListNetworkEndpointResponsePoliciesParams
	if !bindNetworkQuery(c, &params) {
		return
	}
	items, err := h.policies.Policies(c.Request.Context(), principal(c), domain.EndpointResponseFilter{SiteID: params.SiteID, DeviceID: params.DeviceID, Limit: params.Limit})
	respondItems(c, items, err)
}

func (h *NetworkEndpointResponseHandler) GetPolicy(c *gin.Context) {
	item, err := h.policies.Policy(c.Request.Context(), principal(c), c.Param("policyID"))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkEndpointResponseHandler) CreatePolicy(c *gin.Context) {
	var input sohaapi.NetworkEndpointResponsePolicyInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.policies.SavePolicy(c.Request.Context(), principal(c), "", input)
	respondItem(c, http.StatusCreated, item, err)
}

func (h *NetworkEndpointResponseHandler) UpdatePolicy(c *gin.Context) {
	var input sohaapi.NetworkEndpointResponsePolicyInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.policies.SavePolicy(c.Request.Context(), principal(c), c.Param("policyID"), input)
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkEndpointResponseHandler) DeletePolicy(c *gin.Context) {
	var params sohaapi.DeleteNetworkEndpointResponsePolicyParams
	if !bindNetworkQuery(c, &params) {
		return
	}
	respondDelete(c, h.policies.DeletePolicy(c.Request.Context(), principal(c), c.Param("policyID"), params.ExpectedRevision))
}

func (h *NetworkEndpointResponseHandler) PublishPolicy(c *gin.Context) {
	var input sohaapi.NetworkEndpointResponsePolicyPublishInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.policies.PublishPolicy(c.Request.Context(), principal(c), c.Param("policyID"), input.ExpectedRevision, 0)
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkEndpointResponseHandler) RollbackPolicy(c *gin.Context) {
	var input sohaapi.NetworkEndpointResponsePolicyRollbackInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.policies.PublishPolicy(c.Request.Context(), principal(c), c.Param("policyID"), input.ExpectedRevision, input.Revision)
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkEndpointResponseHandler) ListRevisions(c *gin.Context) {
	items, err := h.policies.Revisions(c.Request.Context(), principal(c), c.Param("policyID"))
	respondItems(c, items, err)
}

func (h *NetworkEndpointResponseHandler) Preview(c *gin.Context) {
	var input sohaapi.NetworkEndpointResponsePreviewInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.preview.Preview(c.Request.Context(), principal(c), c.Param("policyID"), input)
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkEndpointResponseHandler) GetPresence(c *gin.Context) {
	item, err := h.events.Presence(c.Request.Context(), principal(c), c.Param("deviceID"))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkEndpointResponseHandler) ListEvents(c *gin.Context) {
	var params sohaapi.ListNetworkEndpointResponseEventsParams
	if !bindNetworkQuery(c, &params) {
		return
	}
	items, err := h.events.Events(c.Request.Context(), principal(c), domain.EndpointResponseFilter{SiteID: params.SiteID, DeviceID: params.DeviceID, Status: string(params.Status), PolicyID: params.PolicyID, Limit: params.Limit})
	respondItems(c, items, err)
}

func (h *NetworkEndpointResponseHandler) GetEvent(c *gin.Context) {
	item, err := h.events.Event(c.Request.Context(), principal(c), c.Param("eventID"))
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkEndpointResponseHandler) RestoreEvent(c *gin.Context) {
	var input sohaapi.NetworkEndpointResponseRestoreInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.events.Restore(c.Request.Context(), principal(c), c.Param("eventID"), input)
	respondItem(c, http.StatusOK, item, err)
}

func (h *NetworkEndpointResponseHandler) ListMaintenance(c *gin.Context) {
	var params sohaapi.ListNetworkEndpointMaintenanceWindowsParams
	if !bindNetworkQuery(c, &params) {
		return
	}
	items, err := h.events.Maintenance(c.Request.Context(), principal(c), domain.EndpointResponseFilter{SiteID: params.SiteID, DeviceID: params.DeviceID, Limit: params.Limit})
	respondItems(c, items, err)
}

func (h *NetworkEndpointResponseHandler) CreateMaintenance(c *gin.Context) {
	var input sohaapi.NetworkEndpointMaintenanceInput
	if !bindNetworkJSON(c, &input) {
		return
	}
	item, err := h.events.CreateMaintenance(c.Request.Context(), principal(c), input)
	respondItem(c, http.StatusCreated, item, err)
}

func (h *NetworkEndpointResponseHandler) DeleteMaintenance(c *gin.Context) {
	respondDelete(c, h.events.EndMaintenance(c.Request.Context(), principal(c), c.Param("windowID")))
}
