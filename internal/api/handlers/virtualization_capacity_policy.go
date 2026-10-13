package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
)

func (h *VirtualizationHandler) GetCapacityPolicy(c *gin.Context) {
	if h.capacityPolicies == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "unavailable", "capacity policy is unavailable")
		return
	}
	policy, err := h.capacityPolicies.GetCapacityPolicy(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, policy)
}

func (h *VirtualizationHandler) SaveCapacityPolicy(c *gin.Context) {
	if h.capacityPolicies == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "unavailable", "capacity policy is unavailable")
		return
	}
	var input struct {
		ExpectedRevision *int                                          `json:"expectedRevision"`
		PhysicalSourceID string                                        `json:"physicalSourceId"`
		Mode             sohaapi.VirtualizationCapacityPolicyInputMode `json:"mode"`
		CPU              *sohaapi.VirtualizationOvercommitPolicy       `json:"cpu"`
		Memory           *sohaapi.VirtualizationMemoryOvercommitPolicy `json:"memory"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.ExpectedRevision == nil || input.CPU == nil || input.Memory == nil || decoder.Decode(&struct{}{}) != io.EOF {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "expected revision and complete capacity policy are required")
		return
	}
	policy, err := h.capacityPolicies.SaveCapacityPolicy(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), sohaapi.VirtualizationCapacityPolicyInput{ExpectedRevision: *input.ExpectedRevision, PhysicalSourceID: input.PhysicalSourceID, Mode: input.Mode, CPU: *input.CPU, Memory: *input.Memory})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, policy)
}
