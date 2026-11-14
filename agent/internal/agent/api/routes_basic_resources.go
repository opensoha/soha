package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	k8sagent "github.com/opensoha/soha/agent/internal/agent/kubernetes"
	apiresponse "github.com/opensoha/soha/agent/internal/api/response"
	domainresource "github.com/opensoha/soha/agent/internal/domain/resource"
)

func registerConfigurationObjectReadRoutes(platform *gin.RouterGroup, client *k8sagent.Client) {
	platform.GET("/configuration/secrets/:name/certificate", func(c *gin.Context) {
		item, err := client.GetSecretCertificate(c.Request.Context(), c.Query("namespace"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/configuration/configmaps/:name/detail", func(c *gin.Context) {
		item, err := client.GetConfigMapDetail(c.Request.Context(), c.Query("namespace"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/configuration/secrets/:name/detail", func(c *gin.Context) {
		item, err := client.GetSecretDetail(c.Request.Context(), c.Query("namespace"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	for _, kind := range []string{"configmaps", "secrets"} {
		platform.GET("/configuration/"+kind+"/:name/references", func(c *gin.Context) {
			items, err := client.ListConfigReferences(c.Request.Context(), c.Query("namespace"), c.Param("name"), kind == "configmaps")
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Items(c, http.StatusOK, items)
		})
	}
}

func registerBasicResourceMutationRoutes(platform *gin.RouterGroup, client *k8sagent.Client, actions actionPolicy) {
	platform = platform.Group("", func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20)
		c.Next()
	})
	platform.PUT("/configuration/configmaps/:name/data", actions.Require(actionPlatformResourcesApply), func(c *gin.Context) {
		var req struct {
			Data       map[string]string `json:"data"`
			BinaryData map[string]string `json:"binaryData"`
		}
		if c.Query("namespace") == "" || c.ShouldBindJSON(&req) != nil {
			apiresponse.Error(c, 400, "invalid_argument", "namespace and valid data are required")
			return
		}
		item, err := client.UpdateConfigMapData(c.Request.Context(), c.Query("namespace"), c.Param("name"), req.Data, req.BinaryData)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.PUT("/configuration/secrets/:name/data", actions.Require(actionPlatformResourcesApply), func(c *gin.Context) {
		var req struct {
			Data map[string]string `json:"data"`
		}
		if c.Query("namespace") == "" || c.ShouldBindJSON(&req) != nil {
			apiresponse.Error(c, 400, "invalid_argument", "namespace and valid data are required")
			return
		}
		item, err := client.UpdateSecretData(c.Request.Context(), c.Query("namespace"), c.Param("name"), req.Data)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.POST("/namespaces", actions.Require(actionPlatformResourcesCreate), func(c *gin.Context) {
		var req domainresource.NamespaceUpsertInput
		if c.ShouldBindJSON(&req) != nil {
			apiresponse.Error(c, 400, "invalid_argument", "invalid namespace payload")
			return
		}
		item, err := client.CreateNamespace(c.Request.Context(), req)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusCreated, item)
	})
	platform.PUT("/namespaces/:name", actions.Require(actionPlatformResourcesApply), func(c *gin.Context) {
		var req domainresource.NamespaceUpsertInput
		if c.ShouldBindJSON(&req) != nil {
			apiresponse.Error(c, 400, "invalid_argument", "invalid namespace payload")
			return
		}
		item, err := client.UpdateNamespace(c.Request.Context(), c.Param("name"), req)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.DELETE("/namespaces/:name", actions.Require(actionPlatformResourcesDelete), func(c *gin.Context) {
		if err := client.DeleteNamespace(c.Request.Context(), c.Param("name")); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
	platform.PUT("/infrastructure/nodes/:name", actions.Require(actionPlatformResourcesApply), func(c *gin.Context) {
		var req domainresource.NodeUpdateInput
		if c.ShouldBindJSON(&req) != nil {
			apiresponse.Error(c, 400, "invalid_argument", "invalid node payload")
			return
		}
		item, err := client.UpdateNode(c.Request.Context(), c.Param("name"), req)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.PUT("/infrastructure/nodes/:name/schedulability", actions.Require(actionPlatformResourcesApply), func(c *gin.Context) {
		var req struct {
			Unschedulable *bool `json:"unschedulable"`
		}
		if c.ShouldBindJSON(&req) != nil || req.Unschedulable == nil {
			apiresponse.Error(c, 400, "invalid_argument", "unschedulable is required")
			return
		}
		if err := client.SetNodeUnschedulable(c.Request.Context(), c.Param("name"), *req.Unschedulable); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
	platform.POST("/infrastructure/nodes/:name/drain", actions.Require(actionPlatformNodesDrain), func(c *gin.Context) {
		var req domainresource.NodeDrainInput
		if c.ShouldBindJSON(&req) != nil {
			apiresponse.Error(c, 400, "invalid_argument", "invalid drain payload")
			return
		}
		if req.TimeoutSeconds == 0 {
			req.TimeoutSeconds = 300
		}
		if req.TimeoutSeconds < 30 || req.TimeoutSeconds > 1800 {
			apiresponse.Error(c, 400, "invalid_argument", "drain timeoutSeconds must be between 30 and 1800")
			return
		}
		if err := http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(time.Duration(req.TimeoutSeconds+10) * time.Second)); err != nil {
			apiresponse.Error(c, 500, "internal_error", "unable to set drain response deadline")
			return
		}
		if err := client.DrainNode(c.Request.Context(), c.Param("name"), req); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
	platform.POST("/workloads/cronjobs/:name/suspend", actions.Require(actionPlatformResourcesApply), func(c *gin.Context) {
		var req struct {
			Suspend *bool `json:"suspend"`
		}
		if c.Query("namespace") == "" || c.ShouldBindJSON(&req) != nil || req.Suspend == nil {
			apiresponse.Error(c, 400, "invalid_argument", "namespace and suspend are required")
			return
		}
		item, err := client.SetCronJobSuspend(c.Request.Context(), c.Query("namespace"), c.Param("name"), *req.Suspend)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
}
