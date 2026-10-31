package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
	contractresource "github.com/opensoha/soha-contracts/resource"
)

type conditionalResourceClient interface {
	GetResourceIdentity(context.Context, string, string, string) (contractresource.ResourceIdentity, error)
	ApplyResourceYAMLConditional(context.Context, string, string, string, string, contractresource.ResourceMutationPreconditions) (domainresource.ResourceYAMLView, error)
	DryRunResourceYAMLConditional(context.Context, string, string, string, string, contractresource.ResourceMutationPreconditions) (domainresource.ResourceUpdateAnalysis, error)
	DeleteResourceConditional(context.Context, string, string, string, contractresource.ResourceMutationPreconditions) error
}

func decodeConditionalResource(c *gin.Context, value any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 5<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid conditional resource payload")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid conditional resource payload")
		return false
	}
	return true
}

func validateConditionalTarget(c *gin.Context, kind, name string, expected contractresource.ResourceMutationPreconditions) bool {
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(name) == "" || expected.Validate() != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "kind, name and valid UID/resourceVersion preconditions are required")
		return false
	}
	return true
}

// Distinct routes prevent an older Agent from ignoring new guard fields.
func registerConditionalResourceRoutes(platform *gin.RouterGroup, client conditionalResourceClient, actions actionPolicy) {
	registerResourcePresenceRoute(platform, client)
	group := platform.Group("/preconditions-v1")
	group.GET("/resources/identity", func(c *gin.Context) {
		kind, name := strings.TrimSpace(c.Query("kind")), strings.TrimSpace(c.Query("name"))
		if kind == "" || name == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "kind and name are required")
			return
		}
		item, err := client.GetResourceIdentity(c.Request.Context(), c.Query("namespace"), kind, name)
		if err != nil {
			writeError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		apiresponse.Item(c, http.StatusOK, item)
	})
	group.POST("/resources/yaml/preflight", actions.Require(actionPlatformResourcesApply), func(c *gin.Context) {
		var req contractresource.ConditionalResourceYAMLRequest
		if !decodeConditionalResource(c, &req) || !validateConditionalTarget(c, req.Kind, req.Name, req.Preconditions) {
			return
		}
		if strings.TrimSpace(req.Content) == "" || len(req.Content) > 4<<20 {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "bounded YAML content is required")
			return
		}
		analysis, err := client.DryRunResourceYAMLConditional(c.Request.Context(), req.Namespace, req.Kind, req.Name, req.Content, req.Preconditions)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, gin.H{"valid": len(analysis.Conflicts) == 0, "analysis": analysis})
	})
	group.PUT("/resources/yaml", actions.Require(actionPlatformResourcesApply), func(c *gin.Context) {
		var req contractresource.ConditionalResourceYAMLRequest
		if !decodeConditionalResource(c, &req) || !validateConditionalTarget(c, req.Kind, req.Name, req.Preconditions) {
			return
		}
		if strings.TrimSpace(req.Content) == "" || len(req.Content) > 4<<20 {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "bounded YAML content is required")
			return
		}
		item, err := client.ApplyResourceYAMLConditional(c.Request.Context(), req.Namespace, req.Kind, req.Name, req.Content, req.Preconditions)
		if err != nil {
			writeError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		apiresponse.Item(c, http.StatusOK, item)
	})
	group.DELETE("/resources", actions.Require(actionPlatformResourcesDelete), func(c *gin.Context) {
		var req contractresource.ConditionalResourceMutationRequest
		if !decodeConditionalResource(c, &req) || !validateConditionalTarget(c, req.Kind, req.Name, req.Preconditions) {
			return
		}
		if err := client.DeleteResourceConditional(c.Request.Context(), req.Namespace, req.Kind, req.Name, req.Preconditions); err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusAccepted, contractresource.ResourceDeletionReceipt{Accepted: true, State: "deletion_requested", Kind: req.Kind, Name: req.Name, Namespace: req.Namespace, Preconditions: req.Preconditions})
	})
}
