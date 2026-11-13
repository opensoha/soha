package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	contractresource "github.com/opensoha/soha-contracts/resource"
)

type resourceYAMLRequest struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Content   string `json:"content"`
}

type deleteResourceRequest struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
}

func registerResourceYAMLRoutes(platform *gin.RouterGroup, client *k8sagent.Client, actions actionPolicy) {
	platform.GET("/resources/stream", func(c *gin.Context) {
		events, unsubscribe, err := client.SubscribeResourceEvents(c.Query("namespace"), splitAgentResourceKinds(c.Query("kinds")))
		if err != nil {
			writeError(c, err)
			return
		}
		defer unsubscribe()
		c.Header("Content-Type", "application/x-ndjson")
		c.Status(http.StatusOK)
		encoder := json.NewEncoder(c.Writer)
		status := "warming"
		if client.ResourceEventsReady() {
			status = "live"
		}
		if err := encoder.Encode(map[string]any{"type": "status", "clusterId": client.Summary(c.Request.Context()).ID, "observedAt": time.Now().UTC().Format(time.RFC3339Nano), "source": "agent-informer", "cacheStatus": status}); err != nil {
			return
		}
		c.Writer.Flush()
		for {
			select {
			case <-c.Request.Context().Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				if err := encoder.Encode(event); err != nil {
					return
				}
				c.Writer.Flush()
			}
		}
	})
	platform.GET("/resources/graph", func(c *gin.Context) {
		kind, name := c.Query("kind"), c.Query("name")
		if strings.TrimSpace(kind) == "" || strings.TrimSpace(name) == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "kind and name are required")
			return
		}
		item, err := client.GetResourceGraph(c.Request.Context(), c.Query("namespace"), kind, name)
		if err != nil {
			writeResourceGraphError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/security/posture", func(c *gin.Context) {
		item, err := client.GetSecurityPosture(c.Request.Context(), c.Query("namespace"), parseLimit(c.Query("limit"), 100))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/resources/yaml", func(c *gin.Context) {
		namespace := c.Query("namespace")
		kind := c.Query("kind")
		name := c.Query("name")
		if strings.TrimSpace(kind) == "" || strings.TrimSpace(name) == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "kind and name are required")
			return
		}
		item, err := client.GetResourceYAML(c.Request.Context(), namespace, kind, name)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.POST("/resources/yaml/preflight", actions.Require(actionPlatformResourcesApply), func(c *gin.Context) {
		var req resourceYAMLRequest
		if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Kind) == "" || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Content) == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "kind, name, and content are required")
			return
		}
		analysis, err := client.DryRunResourceYAML(c.Request.Context(), req.Namespace, req.Kind, req.Name, req.Content)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, gin.H{"valid": len(analysis.Conflicts) == 0, "analysis": analysis})
	})
	platform.PUT("/resources/yaml", actions.Require(actionPlatformResourcesApply), func(c *gin.Context) {
		var req resourceYAMLRequest
		if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Kind) == "" || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Content) == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "kind, name, and content are required")
			return
		}
		item, err := client.ApplyResourceYAML(c.Request.Context(), req.Namespace, req.Kind, req.Name, req.Content)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.DELETE("/resources", actions.Require(actionPlatformResourcesDelete), func(c *gin.Context) {
		var req deleteResourceRequest
		if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Kind) == "" || strings.TrimSpace(req.Name) == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "kind and name are required")
			return
		}
		if err := client.DeleteResource(c.Request.Context(), req.Namespace, req.Kind, req.Name); err != nil {
			writeError(c, err)
			return
		}
		apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
	})
}

func registerObservedResourceYAMLRoutes(platform *gin.RouterGroup, client *k8sagent.Client, actions actionPolicy) {
	platform.POST("/resources/yaml/observed/preflight", actions.Require(actionPlatformResourcesApply), observedResourceYAMLHandler(client, true))
	platform.PUT("/resources/yaml/observed", actions.Require(actionPlatformResourcesApply), observedResourceYAMLHandler(client, false))
}

func observedResourceYAMLHandler(client *k8sagent.Client, dryRun bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req resourceYAMLRequest
		if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Kind) == "" || strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Content) == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "kind, name, and content are required")
			return
		}
		view, analysis, err := client.EditObservedResourceYAML(c.Request.Context(), req.Namespace, req.Kind, req.Name, req.Content, dryRun)
		if err != nil {
			writeError(c, err)
			return
		}
		if dryRun {
			apiresponse.Item(c, http.StatusOK, gin.H{"valid": len(analysis.Conflicts) == 0, "analysis": analysis})
			return
		}
		apiresponse.Item(c, http.StatusOK, view)
	}
}

func splitAgentResourceKinds(value string) []string {
	parts := strings.Split(value, ",")
	kinds := make([]string, 0, len(parts))
	for _, part := range parts {
		if kind := strings.TrimSpace(part); kind != "" {
			kinds = append(kinds, kind)
		}
	}
	return kinds
}

func registerResourceGraphCapabilityRoute(platform *gin.RouterGroup) {
	platform.HEAD("/resources/graph", func(c *gin.Context) {
		query, err := url.ParseQuery(c.Request.URL.RawQuery)
		capability := query["capability"]
		if err != nil || len(query) != 1 || len(capability) != 1 {
			c.Status(http.StatusNotFound)
			return
		}
		switch capability[0] {
		case contractresource.StorageResourceGraphFeature, contractresource.RBACResourceGraphFeature:
			c.Status(http.StatusNoContent)
		default:
			c.Status(http.StatusNotFound)
		}
	})
}
