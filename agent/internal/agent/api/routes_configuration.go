package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
)

func registerPlatformConfigurationRoutes(platform *gin.RouterGroup, client *k8sagent.Client, actions actionPolicy) {
	platform.GET("/configuration/configmaps", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListConfigMaps(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/configuration/secrets", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListSecrets(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/configuration/hpas", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListHorizontalPodAutoscalers(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/configuration/hpas/:name/detail", func(c *gin.Context) {
		item, err := client.GetHorizontalPodAutoscalerDetail(c.Request.Context(), c.Query("namespace"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/configuration/poddisruptionbudgets", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListPodDisruptionBudgets(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/configuration/poddisruptionbudgets/:name/detail", func(c *gin.Context) {
		item, err := client.GetPodDisruptionBudgetDetail(c.Request.Context(), c.Query("namespace"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/configuration/priorityclasses", func(c *gin.Context) {
		items, err := client.ListPriorityClasses(c.Request.Context())
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/configuration/runtimeclasses", func(c *gin.Context) {
		items, err := client.ListRuntimeClasses(c.Request.Context())
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/configuration/mutatingwebhookconfigurations", func(c *gin.Context) {
		items, err := client.ListMutatingWebhookConfigurations(c.Request.Context())
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/configuration/mutatingwebhookconfigurations/:name/detail", func(c *gin.Context) {
		item, err := client.GetMutatingWebhookConfigurationDetail(c.Request.Context(), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/configuration/validatingwebhookconfigurations", func(c *gin.Context) {
		items, err := client.ListValidatingWebhookConfigurations(c.Request.Context())
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/configuration/validatingwebhookconfigurations/:name/detail", func(c *gin.Context) {
		item, err := client.GetValidatingWebhookConfigurationDetail(c.Request.Context(), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/configuration/resourcequotas", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListResourceQuotas(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/configuration/resourcequotas/:name/detail", func(c *gin.Context) {
		item, err := client.GetResourceQuotaDetail(c.Request.Context(), c.Query("namespace"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/configuration/limitranges", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListLimitRanges(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/configuration/limitranges/:name/detail", func(c *gin.Context) {
		item, err := client.GetLimitRangeDetail(c.Request.Context(), c.Query("namespace"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/configuration/leases", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListLeases(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/extensions/crds", func(c *gin.Context) {
		items, err := client.ListCRDs(c.Request.Context())
		if err != nil {
			writeError(c, err)
			return
		}
		for index := range items {
			items[index].AllowedActions = []string{"list", "view"}
			if actions.allows(actionPlatformCRDsDelete) {
				items[index].AllowedActions = append(items[index].AllowedActions, "delete")
			}
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.DELETE("/extensions/crds/:name", actions.Require(actionPlatformCRDsDelete), func(c *gin.Context) {
		var query struct {
			ExpectedUID string `form:"expectedUid" binding:"required,max=128"`
		}
		if err := c.ShouldBindQuery(&query); err != nil || strings.TrimSpace(query.ExpectedUID) == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid deletion identity")
			return
		}
		if err := client.DeleteCRDDefinition(c.Request.Context(), c.Param("name"), query.ExpectedUID); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
}
