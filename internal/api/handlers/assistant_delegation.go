package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	middleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	mesh "github.com/opensoha/soha/internal/application/assistantmesh"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (h *AssistantMeshHandler) RegisterManagedDevice(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input struct {
		DeviceID  string `json:"deviceId"`
		ProofHash string `json:"proofHash"`
	}
	if !decodeMeshRequest(c, &input) {
		return
	}
	p := middleware.PrincipalFromContext(c)
	err := h.delegations.Register(c.Request.Context(), p, delegationSession(c), input.DeviceID, input.ProofHash)
	writeMeshAsset(c, http.StatusOK, gin.H{"ownerId": p.UserID, "deviceId": input.DeviceID}, err)
}

func (h *AssistantMeshHandler) CreateDelegation(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input mesh.DelegationInput
	if !decodeMeshRequest(c, &input) {
		return
	}
	item, err := h.delegations.Create(c.Request.Context(), middleware.PrincipalFromContext(c), input)
	writeMeshAsset(c, http.StatusOK, item, err)
}

func (h *AssistantMeshHandler) ListDelegations(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	items, err := h.delegations.List(c.Request.Context(), middleware.PrincipalFromContext(c), c.DefaultQuery("direction", "sent"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AssistantMeshHandler) GetDelegation(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	item, err := h.delegations.Get(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("id"))
	writeMeshAsset(c, http.StatusOK, item, err)
}

func (h *AssistantMeshHandler) DeclineDelegation(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input struct{}
	if !decodeMeshRequest(c, &input) {
		return
	}
	item, err := h.delegations.Decline(c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("id"))
	writeMeshAsset(c, http.StatusOK, item, err)
}

func (h *AssistantMeshHandler) NativeDelegation(action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		var input mesh.NativeDelegationInput
		if !decodeMeshRequest(c, &input) {
			return
		}
		session := delegationSession(c)
		if session == "" {
			writeError(c, apperrors.ErrAccessDenied)
			return
		}
		ctx, principal, id := c.Request.Context(), middleware.PrincipalFromContext(c), c.Param("id")
		var item any
		var err error
		switch action {
		case "approve":
			item, err = h.delegations.Approve(ctx, principal, id, session, input)
		case "claim":
			item, err = h.delegations.Claim(ctx, principal, id, session, input)
		case "report":
			item, err = h.delegations.Report(ctx, principal, id, session, input)
		case "deliver":
			item, err = h.delegations.Deliver(ctx, principal, id, session, input)
		default:
			err = apperrors.ErrNotFound
		}
		writeMeshAsset(c, http.StatusOK, item, err)
	}
}

func delegationSession(c *gin.Context) string {
	access := middleware.AccessContextFromContext(c)
	if access.TokenKind == "session_access" {
		return access.SessionID
	}
	return ""
}
