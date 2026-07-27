package providerportal

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (h *outpostContractRuntimeHandler) ClaimIdentityOutpostRuntime(c *gin.Context) {
	var request sohaapi.IdentityOutpostClaimRequest
	if h.service == nil || c.ShouldBindJSON(&request) != nil {
		writeOutpostContractBoundaryError(c, h.service == nil)
		return
	}
	item, err := h.service.ClaimIdentityOutpostRuntime(c.Request.Context(), outpostTokenFromRequest(c, ""), request)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *outpostContractRuntimeHandler) HeartbeatIdentityOutpostRuntime(c *gin.Context) {
	var request sohaapi.IdentityOutpostHeartbeatRequest
	if h.service == nil || c.ShouldBindJSON(&request) != nil {
		writeOutpostContractBoundaryError(c, h.service == nil)
		return
	}
	item, err := h.service.HeartbeatIdentityOutpostRuntime(c.Request.Context(), c.Param("outpostID"), outpostTokenFromRequest(c, ""), request)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *outpostContractRuntimeHandler) CheckIdentityOutpostAccess(c *gin.Context) {
	var request sohaapi.IdentityOutpostAccessCheckRequest
	if h.service == nil || c.ShouldBindJSON(&request) != nil {
		writeOutpostContractBoundaryError(c, h.service == nil)
		return
	}
	item, err := h.service.CheckIdentityOutpostAccess(c.Request.Context(), c.Param("outpostID"), outpostTokenFromRequest(c, ""), request)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *outpostContractRuntimeHandler) RecordIdentityOutpostRuntimeEvents(c *gin.Context) {
	var request sohaapi.IdentityOutpostEventBatchRequest
	if h.service == nil || c.ShouldBindJSON(&request) != nil {
		writeOutpostContractBoundaryError(c, h.service == nil)
		return
	}
	item, err := h.service.RecordIdentityOutpostRuntimeEvents(c.Request.Context(), c.Param("outpostID"), outpostTokenFromRequest(c, ""), request)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func writeOutpostContractBoundaryError(c *gin.Context, unavailable bool) {
	if unavailable {
		writeError(c, fmt.Errorf("%w: outpost runtime is not configured", apperrors.ErrUnsupportedOperation))
		return
	}
	apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid outpost runtime payload")
}
