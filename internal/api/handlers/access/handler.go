package access

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type UserService interface {
	ListUsers(context.Context, domainidentity.Principal) ([]domainaccess.UserRecord, error)
	CreateUser(context.Context, domainidentity.Principal, domainaccess.UserInput) (domainaccess.UserRecord, error)
	UpdateUser(context.Context, domainidentity.Principal, string, domainaccess.UserInput) (domainaccess.UserRecord, error)
	DeleteUser(context.Context, domainidentity.Principal, string) error
	RevokeUserSessions(context.Context, domainidentity.Principal, string) error
	ReplaceUserRoles(context.Context, domainidentity.Principal, string, []string) error
	ReplaceUserTeams(context.Context, domainidentity.Principal, string, []string) error
}

type CatalogService interface {
	ListRoles(context.Context, domainidentity.Principal) ([]domainaccess.RoleRecord, error)
	ListTeams(context.Context, domainidentity.Principal) ([]domainaccess.TeamRecord, error)
	ListPolicies(context.Context, domainidentity.Principal) ([]domainaccess.Policy, error)
	PermissionSnapshot(context.Context, domainidentity.Principal) (domainaccess.PermissionSnapshot, error)
}

type RoleService interface {
	CreateRole(context.Context, domainidentity.Principal, domainaccess.RoleInput) (domainaccess.RoleRecord, error)
	UpdateRole(context.Context, domainidentity.Principal, string, domainaccess.RoleInput) (domainaccess.RoleRecord, error)
	DeleteRole(context.Context, domainidentity.Principal, string) error
}

type TeamService interface {
	CreateTeam(context.Context, domainidentity.Principal, domainaccess.TeamInput) (domainaccess.TeamRecord, error)
	UpdateTeam(context.Context, domainidentity.Principal, string, domainaccess.TeamInput) (domainaccess.TeamRecord, error)
	DeleteTeam(context.Context, domainidentity.Principal, string) error
}

type PolicyService interface {
	CreatePolicy(context.Context, domainidentity.Principal, domainaccess.PolicyInput) (domainaccess.Policy, error)
	UpdatePolicy(context.Context, domainidentity.Principal, string, domainaccess.PolicyInput) (domainaccess.Policy, error)
	DeletePolicy(context.Context, domainidentity.Principal, string) error
}

type Services struct {
	Users    UserService
	Catalog  CatalogService
	Roles    RoleService
	Teams    TeamService
	Policies PolicyService
}

type Handler struct {
	userHandler
	catalogHandler
	roleHandler
	teamHandler
	policyHandler
}

type userHandler struct{ service UserService }
type catalogHandler struct{ service CatalogService }
type roleHandler struct{ service RoleService }
type teamHandler struct{ service TeamService }
type policyHandler struct{ service PolicyService }

func New(services Services) *Handler {
	return &Handler{
		userHandler:    userHandler{service: services.Users},
		catalogHandler: catalogHandler{service: services.Catalog},
		roleHandler:    roleHandler{service: services.Roles},
		teamHandler:    teamHandler{service: services.Teams},
		policyHandler:  policyHandler{service: services.Policies},
	}
}

func (h *userHandler) ListUsers(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListUsers(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *userHandler) CreateUser(c *gin.Context) {
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

func (h *userHandler) UpdateUser(c *gin.Context) {
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

func (h *userHandler) DeleteUser(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteUser(c.Request.Context(), principal, c.Param("userID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *userHandler) RevokeUserSessions(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.RevokeUserSessions(c.Request.Context(), principal, c.Param("userID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *catalogHandler) ListRoles(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListRoles(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *catalogHandler) ListTeams(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListTeams(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *catalogHandler) ListPolicies(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListPolicies(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *catalogHandler) PermissionSnapshot(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.PermissionSnapshot(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *roleHandler) CreateRole(c *gin.Context) {
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

func (h *roleHandler) UpdateRole(c *gin.Context) {
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

func (h *roleHandler) DeleteRole(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteRole(c.Request.Context(), principal, c.Param("roleID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *teamHandler) CreateTeam(c *gin.Context) {
	var req dto.UpsertTeamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid team payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateTeam(c.Request.Context(), principal, domainaccess.TeamInput{
		ID:         req.ID,
		ParentID:   req.ParentID,
		Name:       req.Name,
		Slug:       req.Slug,
		Path:       req.Path,
		Source:     req.Source,
		ExternalID: req.ExternalID,
		Metadata:   req.Metadata,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *teamHandler) UpdateTeam(c *gin.Context) {
	var req dto.UpsertTeamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid team payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateTeam(c.Request.Context(), principal, c.Param("teamID"), domainaccess.TeamInput{
		ID:         req.ID,
		ParentID:   req.ParentID,
		Name:       req.Name,
		Slug:       req.Slug,
		Path:       req.Path,
		Source:     req.Source,
		ExternalID: req.ExternalID,
		Metadata:   req.Metadata,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *teamHandler) DeleteTeam(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteTeam(c.Request.Context(), principal, c.Param("teamID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *policyHandler) CreatePolicy(c *gin.Context) {
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

func (h *policyHandler) UpdatePolicy(c *gin.Context) {
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

func (h *policyHandler) DeletePolicy(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeletePolicy(c.Request.Context(), principal, c.Param("policyID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *userHandler) ReplaceUserRoles(c *gin.Context) {
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

func (h *userHandler) ReplaceUserTeams(c *gin.Context) {
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
