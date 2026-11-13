package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	contractresource "github.com/opensoha/soha-contracts/resource"
)

type resourcePresenceClient interface {
	ObserveResourcePresence(context.Context, string, string, string) (contractresource.ResourcePresence, error)
}

func registerResourcePresenceRoute(platform *gin.RouterGroup, client conditionalResourceClient) {
	platform.GET("/resource-presence-v1/resources", func(c *gin.Context) {
		kind, name, namespace := strings.TrimSpace(c.Query("kind")), strings.TrimSpace(c.Query("name")), c.Query("namespace")
		if kind == "" || name == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "kind and name are required")
			return
		}
		definition, supported := contractresource.LookupKubernetesResourceKind(kind)
		if !supported || !definition.GenericYAML {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "supported resource kind is required")
			return
		}
		kind = definition.Kind
		reader, ok := client.(resourcePresenceClient)
		if !ok {
			apiresponse.Error(c, http.StatusNotImplemented, "unsupported_operation", "versioned resource presence is unavailable")
			return
		}
		item, err := reader.ObserveResourcePresence(c.Request.Context(), namespace, kind, name)
		if err != nil {
			writeError(c, err)
			return
		}
		if item.ValidateFor(kind, namespace, name) != nil {
			apiresponse.Error(c, http.StatusBadGateway, "invalid_response", "resource presence could not be confirmed")
			return
		}
		c.Header("Cache-Control", "no-store")
		apiresponse.Item(c, http.StatusOK, item)
	})
}
