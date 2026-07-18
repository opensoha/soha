package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func serviceAccountSubjectFilter(c *gin.Context) (namespace, name string, requested bool, err error) {
	kind := strings.TrimSpace(c.Query("subjectKind"))
	name = strings.TrimSpace(c.Query("subjectName"))
	namespace = strings.TrimSpace(c.Query("subjectNamespace"))
	requested = kind != "" || name != "" || namespace != ""
	if !requested {
		return "", "", false, nil
	}
	if kind != "ServiceAccount" || name == "" || namespace == "" {
		return "", "", true, fmt.Errorf("%w: complete ServiceAccount subject filter is required", apperrors.ErrInvalidArgument)
	}
	return namespace, name, true, nil
}

func (h *namespacedRBACResourceHandler) ListServiceAccounts(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.service.ListServiceAccounts(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *namespacedRBACResourceHandler) GetServiceAccountDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.service.GetServiceAccountDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *namespacedRBACResourceHandler) CreateServiceAccount(c *gin.Context) {
	createResourceFromYAML(c, h.creator, "ServiceAccount")
}
func (h *namespacedRBACResourceHandler) ListRoles(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.service.ListRoles(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *namespacedRBACResourceHandler) GetRoleDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.service.GetRoleDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *namespacedRBACResourceHandler) CreateRole(c *gin.Context) {
	createResourceFromYAML(c, h.creator, "Role")
}
func (h *namespacedRBACResourceHandler) ListRoleBindings(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	subjectNamespace, subjectName, filtered, err := serviceAccountSubjectFilter(c)
	if err != nil {
		writeError(c, err)
		return
	}
	var items []domainresource.RoleBindingView
	if filtered {
		if strings.TrimSpace(namespace) != subjectNamespace {
			writeError(c, fmt.Errorf("%w: binding namespace must match service account namespace", apperrors.ErrInvalidArgument))
			return
		}
		items, err = h.service.ListRoleBindingsForServiceAccount(c.Request.Context(), principal, c.Param("clusterID"), subjectNamespace, subjectName)
	} else {
		items, err = h.service.ListRoleBindings(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	}
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *namespacedRBACResourceHandler) GetRoleBindingDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.service.GetRoleBindingDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *namespacedRBACResourceHandler) CreateRoleBinding(c *gin.Context) {
	createResourceFromYAML(c, h.creator, "RoleBinding")
}
func (h *clusterRBACResourceHandler) ListClusterRoles(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListClusterRoles(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *clusterRBACResourceHandler) GetClusterRoleDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetClusterRoleDetail(c.Request.Context(), principal, c.Param("clusterID"), c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *clusterRBACResourceHandler) CreateClusterRole(c *gin.Context) {
	createResourceFromYAML(c, h.creator, "ClusterRole")
}
func (h *clusterRBACResourceHandler) ListClusterRoleBindings(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	subjectNamespace, subjectName, filtered, err := serviceAccountSubjectFilter(c)
	if err != nil {
		writeError(c, err)
		return
	}
	var items []domainresource.ClusterRoleBindingView
	if filtered {
		items, err = h.service.ListClusterRoleBindingsForServiceAccount(c.Request.Context(), principal, c.Param("clusterID"), subjectNamespace, subjectName)
	} else {
		items, err = h.service.ListClusterRoleBindings(c.Request.Context(), principal, c.Param("clusterID"))
	}
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *clusterRBACResourceHandler) GetClusterRoleBindingDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetClusterRoleBindingDetail(c.Request.Context(), principal, c.Param("clusterID"), c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *clusterRBACResourceHandler) CreateClusterRoleBinding(c *gin.Context) {
	createResourceFromYAML(c, h.creator, "ClusterRoleBinding")
}
