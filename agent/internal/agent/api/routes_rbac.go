package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
)

func registerPlatformRBACRoutes(platform *gin.RouterGroup, client *k8sagent.Client) {
	platform.POST("/access-control/access-reviews", func(c *gin.Context) {
		var input domainresource.SubjectAccessReviewInput
		if err := c.ShouldBindJSON(&input); err != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid access review payload")
			return
		}
		input, err := normalizeSubjectAccessReviewInput(input)
		if err != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
			return
		}
		item, err := client.ReviewSubjectAccess(c.Request.Context(), input)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/access-control/serviceaccounts", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListServiceAccounts(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/access-control/serviceaccounts/:name/detail", func(c *gin.Context) {
		namespace := c.Query("namespace")
		item, err := client.GetServiceAccountDetail(c.Request.Context(), namespace, c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/access-control/roles", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListRoles(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/access-control/roles/:name/detail", func(c *gin.Context) {
		namespace := c.Query("namespace")
		item, err := client.GetRoleDetail(c.Request.Context(), namespace, c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/access-control/rolebindings", func(c *gin.Context) {
		namespace := strings.TrimSpace(c.Query("namespace"))
		subjectKind, subjectName, subjectNamespace, filtered := subjectFilter(c)
		if filtered && (namespace == "" || !validServiceAccountFilter(subjectKind, subjectName, subjectNamespace)) {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "a complete ServiceAccount subject filter is required")
			return
		}
		var items []domainresource.RoleBindingView
		var err error
		if filtered {
			items, err = client.ListRoleBindingsForSubject(c.Request.Context(), namespace, subjectKind, subjectName, subjectNamespace)
		} else {
			items, err = client.ListRoleBindings(c.Request.Context(), namespace)
		}
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/access-control/rolebindings/:name/detail", func(c *gin.Context) {
		namespace := c.Query("namespace")
		item, err := client.GetRoleBindingDetail(c.Request.Context(), namespace, c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/access-control/clusterroles", func(c *gin.Context) {
		items, err := client.ListClusterRoles(c.Request.Context())
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/access-control/clusterroles/:name/detail", func(c *gin.Context) {
		item, err := client.GetClusterRoleDetail(c.Request.Context(), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/access-control/clusterrolebindings", func(c *gin.Context) {
		subjectKind, subjectName, subjectNamespace, filtered := subjectFilter(c)
		if filtered && !validServiceAccountFilter(subjectKind, subjectName, subjectNamespace) {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "a complete ServiceAccount subject filter is required")
			return
		}
		var items []domainresource.ClusterRoleBindingView
		var err error
		if filtered {
			items, err = client.ListClusterRoleBindingsForSubject(c.Request.Context(), subjectKind, subjectName, subjectNamespace)
		} else {
			items, err = client.ListClusterRoleBindings(c.Request.Context())
		}
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/access-control/clusterrolebindings/:name/detail", func(c *gin.Context) {
		item, err := client.GetClusterRoleBindingDetail(c.Request.Context(), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
}

func normalizeSubjectAccessReviewInput(input domainresource.SubjectAccessReviewInput) (domainresource.SubjectAccessReviewInput, error) {
	input.Subject.Kind = strings.TrimSpace(input.Subject.Kind)
	input.Subject.Name = strings.TrimSpace(input.Subject.Name)
	input.Subject.Namespace = strings.TrimSpace(input.Subject.Namespace)
	if input.Subject.Name == "" {
		return input, fmt.Errorf("subject name is required")
	}
	switch input.Subject.Kind {
	case "User", "Group":
	case "ServiceAccount":
		if input.Subject.Namespace == "" {
			return input, fmt.Errorf("service account namespace is required")
		}
	default:
		return input, fmt.Errorf("subject kind must be User, Group, or ServiceAccount")
	}
	if len(input.Checks) == 0 || len(input.Checks) > 50 {
		return input, fmt.Errorf("checks must contain between 1 and 50 items")
	}
	for index := range input.Checks {
		input.Checks[index].Verb = strings.TrimSpace(input.Checks[index].Verb)
		input.Checks[index].Group = strings.TrimSpace(input.Checks[index].Group)
		input.Checks[index].Resource = strings.TrimSpace(input.Checks[index].Resource)
		input.Checks[index].Namespace = strings.TrimSpace(input.Checks[index].Namespace)
		input.Checks[index].Name = strings.TrimSpace(input.Checks[index].Name)
		if input.Checks[index].Verb == "" || input.Checks[index].Resource == "" {
			return input, fmt.Errorf("check verb and resource are required")
		}
	}
	return input, nil
}

func subjectFilter(c *gin.Context) (kind, name, namespace string, requested bool) {
	kind = strings.TrimSpace(c.Query("subjectKind"))
	name = strings.TrimSpace(c.Query("subjectName"))
	namespace = strings.TrimSpace(c.Query("subjectNamespace"))
	requested = kind != "" || name != "" || namespace != ""
	return kind, name, namespace, requested
}

func validServiceAccountFilter(kind, name, namespace string) bool {
	return kind == "ServiceAccount" && name != "" && namespace != ""
}
