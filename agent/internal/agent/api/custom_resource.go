package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
	contractresource "github.com/opensoha/soha-contracts/resource"
)

type customResourceListRequest struct {
	Definition domainresource.CRDResourceDefinition `json:"definition"`
	Namespace  string                               `json:"namespace"`
}

type customResourceYAMLRequest = domainresource.CustomResourceYAMLRequest

func registerCustomResourceGraphRoutes(platform *gin.RouterGroup, client *k8sagent.Client, actions actionPolicy, token string) {
	platform.POST("/extensions/custom-resources/graph-page", authRequiredAnyMiddleware(token), actions.Require(actionPlatformCustomResourcesList), func(c *gin.Context) {
		var req customResourceListRequest
		decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&req) != nil || decoder.Decode(&struct{}{}) != io.EOF || invalidCRDDefinition(req.Definition) {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "valid custom resource definition is required")
			return
		}
		page, err := client.ReadCustomResourceGraphPage(c.Request.Context(), req.Definition, req.Namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, page)
	})
	platform.POST(contractresource.CustomResourceGraphObjectPath, authRequiredAnyMiddleware(token), func(c *gin.Context) {
		var req contractresource.CustomResourceGraphObjectRequest
		decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&req) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "valid named custom resource request is required")
			return
		}
		item, err := client.ReadCustomResourceGraphObject(c.Request.Context(), req)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
}

func registerCustomResourceRoutes(platform *gin.RouterGroup, client *k8sagent.Client, actions actionPolicy) {
	platform.POST("/extensions/custom-resources/access", func(c *gin.Context) {
		var req contractresource.CustomResourceAccessRequest
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
		if err := c.ShouldBindJSON(&req); err != nil || invalidCRDDefinition(req.Definition) {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "valid custom resource definition is required")
			return
		}
		allowed, err := client.CustomResourceActions(c.Request.Context(), req.Definition, req.Namespace, req.Name)
		if err != nil {
			writeError(c, err)
			return
		}
		result := contractresource.CustomResourceAccess{AllowedActions: []string{}}
		for _, action := range allowed {
			policy := map[string]string{"list": actionPlatformCustomResourcesList, "create": actionPlatformCustomResourcesCreate, "update": actionPlatformCustomResourcesApply, "delete": actionPlatformCustomResourcesDelete}[action]
			if action == "view" || actions.allows(policy) {
				result.AllowedActions = append(result.AllowedActions, action)
			}
		}
		apiresponse.Item(c, http.StatusOK, result)
	})
	platform.POST("/extensions/custom-resources/list", actions.Require(actionPlatformCustomResourcesList), func(c *gin.Context) {
		var req customResourceListRequest
		if err := c.ShouldBindJSON(&req); err != nil || invalidCRDDefinition(req.Definition) {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "valid custom resource definition is required")
			return
		}
		items, err := client.ListCustomResources(c.Request.Context(), req.Definition, req.Namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.POST("/extensions/custom-resources", actions.Require(actionPlatformCustomResourcesCreate), func(c *gin.Context) {
		var req customResourceYAMLRequest
		if err := c.ShouldBindJSON(&req); err != nil || invalidCRDDefinition(req.Definition) || strings.TrimSpace(req.Content) == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "definition and content are required")
			return
		}
		item, err := client.CreateCustomResourceYAML(c.Request.Context(), req.Definition, req.Namespace, req.Content)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusCreated, item)
	})
	platform.POST("/extensions/custom-resources/yaml", func(c *gin.Context) {
		var req customResourceYAMLRequest
		if err := c.ShouldBindJSON(&req); err != nil || invalidCRDDefinition(req.Definition) || strings.TrimSpace(req.Name) == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "definition and name are required")
			return
		}
		item, err := client.GetCustomResourceYAML(c.Request.Context(), req.Definition, req.Namespace, req.Name)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.PUT("/extensions/custom-resources/yaml", actions.Require(actionPlatformCustomResourcesApply), func(c *gin.Context) {
		var req customResourceYAMLRequest
		if err := c.ShouldBindJSON(&req); err != nil || invalidCRDDefinition(req.Definition) || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Content) == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "definition, name, and content are required")
			return
		}
		item, err := client.ApplyCustomResourceYAML(c.Request.Context(), req.Definition, req.Namespace, req.Name, req.Content)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	deleteObserved := func(c *gin.Context) {
		var req customResourceYAMLRequest
		if err := c.ShouldBindJSON(&req); err != nil || invalidCRDDefinition(req.Definition) || strings.TrimSpace(req.Name) == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "definition and name are required")
			return
		}
		if err := client.DeleteCustomResource(c.Request.Context(), req.Definition, req.Namespace, req.Name, req.ExpectedUID); err != nil {
			writeError(c, err)
			return
		}
		apiresponse.JSON(c, http.StatusOK, gin.H{"status": "accepted"})
	}
	platform.DELETE("/extensions/custom-resources", actions.Require(actionPlatformCustomResourcesDelete), deleteObserved)
	// Separate endpoint makes older Agents fail closed instead of ignoring UID fences.
	platform.POST("/extensions/custom-resources/delete-observed", actions.Require(actionPlatformCustomResourcesDelete), deleteObserved)
}

func invalidCRDDefinition(definition domainresource.CRDResourceDefinition) bool {
	return strings.TrimSpace(definition.Group) == "" ||
		strings.TrimSpace(definition.Version) == "" ||
		strings.TrimSpace(definition.Resource) == "" ||
		strings.TrimSpace(definition.Kind) == ""
}
