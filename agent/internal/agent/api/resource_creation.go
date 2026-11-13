package api

import (
	"context"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
)

const (
	resourceCreateMaxRequestBytes  = 2 << 20
	resourceCreateMaxDocumentBytes = 512 << 10
	resourceCreateMaxDocuments     = 50
)

type resourceCreationClient interface {
	PreflightResourceCreate(context.Context, domainresource.KubernetesResourceAgentCreateRequest) domainresource.KubernetesResourceAgentPreflightResult
	CreateResources(context.Context, domainresource.KubernetesResourceAgentCreateRequest) domainresource.KubernetesResourceAgentCreateResult
}

func registerResourceCreationRoutes(platform *gin.RouterGroup, client resourceCreationClient, actions actionPolicy) {
	platform.POST("/resources/preflight", func(c *gin.Context) {
		request, ok := bindResourceCreateRequest(c)
		if !ok {
			return
		}
		apiresponse.Item(c, http.StatusOK, client.PreflightResourceCreate(c.Request.Context(), request))
	})
	platform.POST("/resources", actions.Require(actionPlatformResourcesCreate), func(c *gin.Context) {
		request, ok := bindResourceCreateRequest(c)
		if !ok {
			return
		}
		apiresponse.Item(c, http.StatusCreated, client.CreateResources(c.Request.Context(), request))
	})
}

func bindResourceCreateRequest(c *gin.Context) (domainresource.KubernetesResourceAgentCreateRequest, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, resourceCreateMaxRequestBytes)
	var request domainresource.KubernetesResourceAgentCreateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "valid resource creation request is required")
		return domainresource.KubernetesResourceAgentCreateRequest{}, false
	}
	if err := validateResourceCreateRequest(request); err != "" {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err)
		return domainresource.KubernetesResourceAgentCreateRequest{}, false
	}
	return request, true
}

func validateResourceCreateRequest(request domainresource.KubernetesResourceAgentCreateRequest) string {
	if strings.TrimSpace(request.OperationID) == "" {
		return "operationId is required"
	}
	if len(request.OperationID) > 128 {
		return "operationId is too long"
	}
	if len(request.Documents) == 0 {
		return "at least one resource document is required"
	}
	if len(request.Documents) > resourceCreateMaxDocuments {
		return "resource document limit exceeded"
	}
	indexes := make(map[int]struct{}, len(request.Documents))
	for _, input := range request.Documents {
		document := input.Document
		ref := input.ResourceRef
		if document.Index < 0 || !validResourceContentHash(document.ContentHash) || strings.TrimSpace(input.Content) == "" ||
			strings.TrimSpace(document.APIVersion) == "" || strings.TrimSpace(document.Kind) == "" || strings.TrimSpace(document.Name) == "" || !document.ScopeMode.Valid() ||
			strings.TrimSpace(ref.ClusterID) == "" || strings.TrimSpace(ref.APIVersion) == "" || strings.TrimSpace(ref.Kind) == "" || strings.TrimSpace(ref.Name) == "" {
			return "each resource document requires index, contentHash, content, and a complete resolved resourceRef"
		}
		if len(input.Content) > resourceCreateMaxDocumentBytes {
			return "resource document size limit exceeded"
		}
		if _, exists := indexes[document.Index]; exists {
			return "resource document indexes must be unique"
		}
		indexes[document.Index] = struct{}{}
		if !ref.ScopeMode.Valid() {
			return "resolved resourceRef scopeMode is invalid"
		}
	}
	return ""
}

func validResourceContentHash(value string) bool {
	if len(value) != sha256HexLength || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256ByteLength
}

const (
	sha256ByteLength = 32
	sha256HexLength  = sha256ByteLength * 2
)
