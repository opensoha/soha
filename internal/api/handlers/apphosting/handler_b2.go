package apphosting

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/api/middleware"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (h *Handler) ListPortalApps(c *gin.Context) {
	if h.apps == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	limit, ok := parseOptionalIntQuery(c, "limit")
	if !ok {
		return
	}
	offset, ok := parseOptionalIntQuery(c, "offset")
	if !ok {
		return
	}
	params := sohaapi.ListPortalHostedAppsParams{
		Query:        c.Query("query"),
		Kind:         sohaapi.HostedAppKind(strings.TrimSpace(c.Query("kind"))),
		AuthorUserID: c.Query("authorUserId"),
		Limit:        limit,
		Offset:       offset,
	}
	out, err := h.apps.ListPortalApps(c.Request.Context(), middleware.PrincipalFromContext(c), params)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) UpdateMaintainers(c *gin.Context) {
	if h.lifecycle == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	var req sohaapi.UpdateHostedAppMaintainersRequest
	if !decodeStrictJSON(c, &req, false) {
		return
	}
	out, err := h.lifecycle.UpdateMaintainers(c.Request.Context(), middleware.PrincipalFromContext(c), appIDParam(c), req)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) TransferOwnership(c *gin.Context) {
	if h.lifecycle == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	var req sohaapi.TransferHostedAppOwnershipRequest
	if !decodeStrictJSON(c, &req, false) {
		return
	}
	out, err := h.lifecycle.TransferOwnership(c.Request.Context(), middleware.PrincipalFromContext(c), appIDParam(c), req)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) DeleteApp(c *gin.Context) {
	if h.lifecycle == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	var req sohaapi.DeleteHostedAppRequest
	if !decodeStrictJSON(c, &req, false) {
		return
	}
	out, err := h.lifecycle.DeleteApp(c.Request.Context(), middleware.PrincipalFromContext(c), appIDParam(c), req)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) WithdrawVisibilityApproval(c *gin.Context) {
	if h.approvals == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	var req sohaapi.HostedAppLifecycleActionRequest
	if !decodeStrictJSON(c, &req, true) {
		return
	}
	out, err := h.approvals.WithdrawVisibilityApproval(c.Request.Context(), middleware.PrincipalFromContext(c), appIDParam(c), req.Reason)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) ListVisibilityApprovals(c *gin.Context) {
	if h.approvals == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	limit, ok := parseOptionalIntQuery(c, "limit")
	if !ok {
		return
	}
	offset, ok := parseOptionalIntQuery(c, "offset")
	if !ok {
		return
	}
	params := sohaapi.ListHostedAppVisibilityApprovalsParams{
		Scope:  sohaapi.HostedAppApprovalListScope(strings.TrimSpace(c.Query("scope"))),
		Status: sohaapi.HostedAppVisibilityApprovalStatus(strings.TrimSpace(c.Query("status"))),
		AppID:  c.Query("appId"),
		Limit:  limit,
		Offset: offset,
	}
	out, err := h.approvals.ListVisibilityApprovals(c.Request.Context(), middleware.PrincipalFromContext(c), params)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) ApproveVisibilityApproval(c *gin.Context) {
	if h.approvals == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	var req sohaapi.ApproveHostedAppVisibilityRequest
	if !decodeStrictJSON(c, &req, true) {
		return
	}
	out, err := h.approvals.ApproveVisibilityApproval(c.Request.Context(), middleware.PrincipalFromContext(c), approvalIDParam(c), req.Comment)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) RejectVisibilityApproval(c *gin.Context) {
	if h.approvals == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	var req sohaapi.RejectHostedAppVisibilityRequest
	if !decodeStrictJSON(c, &req, false) {
		return
	}
	out, err := h.approvals.RejectVisibilityApproval(c.Request.Context(), middleware.PrincipalFromContext(c), approvalIDParam(c), req.Comment)
	respondItem(c, http.StatusOK, out, err)
}

func (h *Handler) DeployFromWorkspace(c *gin.Context) {
	if h.deployments == nil {
		fail(c, apperrors.ErrServiceUnavailable)
		return
	}
	var req sohaapi.WorkspaceHostedAppDeploymentRequest
	if !decodeStrictJSON(c, &req, false) {
		return
	}
	out, err := h.deployments.DeployFromWorkspace(c.Request.Context(), middleware.PrincipalFromContext(c), req)
	status := http.StatusOK
	if out.Created {
		status = http.StatusCreated
	}
	respondItem(c, status, out, err)
}

func approvalIDParam(c *gin.Context) string {
	if v := c.Param("approvalId"); v != "" {
		return v
	}
	return c.Param("approvalID")
}
