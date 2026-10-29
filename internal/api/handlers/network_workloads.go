package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type networkWorkloadManagement interface {
	ListWorkloadBindings(context.Context, identity.Principal) ([]domain.WorkloadBinding, error)
	PutWorkloadBinding(context.Context, identity.Principal, domain.WorkloadBinding) (domain.WorkloadBinding, error)
	SubmitManagementProof(context.Context, identity.Principal, string, domain.ManagementProofInput) (domain.Device, error)
}

func (h *NetworkAccessHandler) ListWorkloadBindings(c *gin.Context) {
	service, ok := h.service.(networkWorkloadManagement)
	if !ok {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	items, err := service.ListWorkloadBindings(c.Request.Context(), principal(c))
	respondItems(c, items, err)
}
func (h *NetworkAccessHandler) PutWorkloadBinding(c *gin.Context) {
	var input sohaapi.NetworkWorkloadBindingInput
	if !bindDeviceRegistration(c, &input) {
		return
	}
	service, ok := h.service.(networkWorkloadManagement)
	if !ok {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	binding := domain.WorkloadBinding{ID: c.Param("bindingID"), ServiceAccountID: input.ServiceAccountID, RuntimeID: input.RuntimeID, DeviceID: input.DeviceID, SiteID: input.SiteID, NetworkSpaceID: input.NetworkSpaceID, ResourceIDs: input.ResourceIDs, Enabled: input.Enabled, ExpiresAt: input.ExpiresAt, Revision: input.Revision}
	item, err := service.PutWorkloadBinding(c.Request.Context(), principal(c), binding)
	respondItem(c, http.StatusOK, item, err)
}
func (h *NetworkAccessHandler) SubmitManagementProof(c *gin.Context) {
	var input sohaapi.NetworkManagementProofInput
	if !bindDeviceRegistration(c, &input) {
		return
	}
	service, ok := h.service.(networkWorkloadManagement)
	if !ok {
		writeError(c, apperrors.ErrServiceUnavailable)
		return
	}
	item, err := service.SubmitManagementProof(c.Request.Context(), principal(c), c.Param("deviceID"), domain.ManagementProofInput{Payload: input.Payload, Signature: input.Signature})
	respondItem(c, http.StatusOK, item, err)
}
