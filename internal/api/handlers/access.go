package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kubecrux/kubecrux/internal/api/dto"
	apiMiddleware "github.com/kubecrux/kubecrux/internal/api/middleware"
	apiresponse "github.com/kubecrux/kubecrux/internal/api/response"
	domainaccess "github.com/kubecrux/kubecrux/internal/domain/access"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
)

type AccessCatalogService interface {
	ListUsers(context.Context, domainidentity.Principal) ([]domainaccess.UserRecord, error)
	CreateUser(context.Context, domainidentity.Principal, domainaccess.UserInput) (domainaccess.UserRecord, error)
	UpdateUser(context.Context, domainidentity.Principal, string, domainaccess.UserInput) (domainaccess.UserRecord, error)
	DeleteUser(context.Context, domainidentity.Principal, string) error
	RevokeUserSessions(context.Context, domainidentity.Principal, string) error
	ListRoles(context.Context, domainidentity.Principal) ([]domainaccess.RoleRecord, error)
	ListTeams(context.Context, domainidentity.Principal) ([]domainaccess.TeamRecord, error)
	ListPolicies(context.Context, domainidentity.Principal) ([]domainaccess.Policy, error)
	PermissionSnapshot(context.Context, domainidentity.Principal) (domainaccess.PermissionSnapshot, error)
	CreateRole(context.Context, domainidentity.Principal, domainaccess.RoleInput) (domainaccess.RoleRecord, error)
	UpdateRole(context.Context, domainidentity.Principal, string, domainaccess.RoleInput) (domainaccess.RoleRecord, error)
	DeleteRole(context.Context, domainidentity.Principal, string) error
	CreateTeam(context.Context, domainidentity.Principal, domainaccess.TeamInput) (domainaccess.TeamRecord, error)
	UpdateTeam(context.Context, domainidentity.Principal, string, domainaccess.TeamInput) (domainaccess.TeamRecord, error)
	DeleteTeam(context.Context, domainidentity.Principal, string) error
	CreatePolicy(context.Context, domainidentity.Principal, domainaccess.PolicyInput) (domainaccess.Policy, error)
	UpdatePolicy(context.Context, domainidentity.Principal, string, domainaccess.PolicyInput) (domainaccess.Policy, error)
	DeletePolicy(context.Context, domainidentity.Principal, string) error
	ReplaceUserRoles(context.Context, domainidentity.Principal, string, []string) error
	ReplaceUserTeams(context.Context, domainidentity.Principal, string, []string) error
}

type AccessHandler struct {
	service AccessCatalogService
}

func NewAccessHandler(service AccessCatalogService) *AccessHandler {
	return &AccessHandler{service: service}
}

func (h *AccessHandler) ListUsers(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListUsers(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AccessHandler) CreateUser(c *gin.Context) {
	var req dto.UpsertUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid user payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateUser(c.Request.Context(), principal, domainaccess.UserInput{
		ID:          req.ID,
		Username:    req.Username,
		Email:       req.Email,
		DisplayName: req.DisplayName,
		Status:      req.Status,
		Tags:        req.Tags,
		RoleIDs:     req.RoleIDs,
		TeamIDs:     req.TeamIDs,
		Preferences: req.Preferences,
		Password:    req.Password,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *AccessHandler) UpdateUser(c *gin.Context) {
	var req dto.UpsertUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid user payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateUser(c.Request.Context(), principal, c.Param("userID"), domainaccess.UserInput{
		ID:          req.ID,
		Username:    req.Username,
		Email:       req.Email,
		DisplayName: req.DisplayName,
		Status:      req.Status,
		Tags:        req.Tags,
		RoleIDs:     req.RoleIDs,
		TeamIDs:     req.TeamIDs,
		Preferences: req.Preferences,
		Password:    req.Password,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AccessHandler) DeleteUser(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteUser(c.Request.Context(), principal, c.Param("userID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *AccessHandler) RevokeUserSessions(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.RevokeUserSessions(c.Request.Context(), principal, c.Param("userID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *AccessHandler) ListRoles(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListRoles(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AccessHandler) ListTeams(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListTeams(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AccessHandler) ListPolicies(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListPolicies(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *AccessHandler) PermissionSnapshot(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.PermissionSnapshot(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AccessHandler) CreateRole(c *gin.Context) {
	var req dto.UpsertRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid role payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateRole(c.Request.Context(), principal, mapRoleInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *AccessHandler) UpdateRole(c *gin.Context) {
	var req dto.UpsertRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid role payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateRole(c.Request.Context(), principal, c.Param("roleID"), mapRoleInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AccessHandler) DeleteRole(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteRole(c.Request.Context(), principal, c.Param("roleID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *AccessHandler) CreateTeam(c *gin.Context) {
	var req dto.UpsertTeamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid team payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateTeam(c.Request.Context(), principal, domainaccess.TeamInput{
		ID:       req.ID,
		Name:     req.Name,
		Slug:     req.Slug,
		Metadata: req.Metadata,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *AccessHandler) UpdateTeam(c *gin.Context) {
	var req dto.UpsertTeamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid team payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateTeam(c.Request.Context(), principal, c.Param("teamID"), domainaccess.TeamInput{
		ID:       req.ID,
		Name:     req.Name,
		Slug:     req.Slug,
		Metadata: req.Metadata,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AccessHandler) DeleteTeam(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteTeam(c.Request.Context(), principal, c.Param("teamID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *AccessHandler) CreatePolicy(c *gin.Context) {
	var req dto.UpsertPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid policy payload")
		return
	}
	if err := validateEffect(req.Effect); err != nil {
		writeError(c, err)
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreatePolicy(c.Request.Context(), principal, mapPolicyInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *AccessHandler) UpdatePolicy(c *gin.Context) {
	var req dto.UpsertPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid policy payload")
		return
	}
	if err := validateEffect(req.Effect); err != nil {
		writeError(c, err)
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdatePolicy(c.Request.Context(), principal, c.Param("policyID"), mapPolicyInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *AccessHandler) DeletePolicy(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeletePolicy(c.Request.Context(), principal, c.Param("policyID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *AccessHandler) ReplaceUserRoles(c *gin.Context) {
	var req dto.ReplaceUserRolesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid user role binding payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.ReplaceUserRoles(c.Request.Context(), principal, c.Param("userID"), req.RoleIDs); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *AccessHandler) ReplaceUserTeams(c *gin.Context) {
	var req dto.ReplaceUserTeamsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid user team binding payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.ReplaceUserTeams(c.Request.Context(), principal, c.Param("userID"), req.TeamIDs); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func mapRoleInput(req dto.UpsertRoleRequest) domainaccess.RoleInput {
	actions := make([]domainaccess.Action, 0, len(req.Capabilities))
	for _, item := range req.Capabilities {
		actions = append(actions, domainaccess.Action(item))
	}
	return domainaccess.RoleInput{
		ID:             req.ID,
		Name:           req.Name,
		Scope:          req.Scope,
		Capabilities:   actions,
		PermissionKeys: req.PermissionKeys,
	}
}

func mapPolicyInput(req dto.UpsertPolicyRequest) domainaccess.PolicyInput {
	return domainaccess.PolicyInput{
		ID:       req.ID,
		Name:     req.Name,
		Effect:   parseEffect(req.Effect),
		Priority: req.Priority,
		Subjects: domainaccess.Matcher{
			Roles:    req.Subjects.Roles,
			Teams:    req.Subjects.Teams,
			Projects: req.Subjects.Projects,
			Users:    req.Subjects.Users,
			Tags:     req.Subjects.Tags,
		},
		Clusters: domainaccess.ClusterMatcher{
			IDs:          req.Clusters.IDs,
			Regions:      req.Clusters.Regions,
			Environments: req.Clusters.Environments,
			Labels:       req.Clusters.Labels,
		},
		Namespaces: domainaccess.NamespaceMatcher{
			Names:      req.Namespaces.Names,
			OwnerTeams: req.Namespaces.OwnerTeams,
			Labels:     req.Namespaces.Labels,
		},
		Resources: domainaccess.ResourceMatcher{
			Kinds:  req.Resources.Kinds,
			Names:  req.Resources.Names,
			Labels: req.Resources.Labels,
		},
		Actions:    parseActions(req.Actions),
		Conditions: domainaccess.Conditions{Sources: req.Conditions.Sources, ApprovalStates: req.Conditions.ApprovalStates},
		Reason:     req.Reason,
	}
}

func parseActions(values []string) []domainaccess.Action {
	items := make([]domainaccess.Action, 0, len(values))
	for _, value := range values {
		items = append(items, domainaccess.Action(value))
	}
	return items
}

func parseEffect(value string) domainaccess.PolicyEffect {
	switch value {
	case string(domainaccess.EffectDeny):
		return domainaccess.EffectDeny
	case "", string(domainaccess.EffectAllow):
		return domainaccess.EffectAllow
	default:
		return domainaccess.PolicyEffect(value)
	}
}

func validateEffect(value string) error {
	switch value {
	case "", string(domainaccess.EffectAllow), string(domainaccess.EffectDeny):
		return nil
	default:
		return fmt.Errorf("%w: unsupported policy effect %q", apperrors.ErrInvalidArgument, value)
	}
}
