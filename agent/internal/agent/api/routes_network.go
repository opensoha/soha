package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
)

func registerPlatformNetworkRoutes(platform *gin.RouterGroup, client *k8sagent.Client) {
	platform.GET("/network/services", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListServices(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/network/services/:name/detail", func(c *gin.Context) {
		item, err := client.GetServiceDetail(c.Request.Context(), c.Query("namespace"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/network/ingresses", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListIngresses(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/network/ingresses/:name/detail", func(c *gin.Context) {
		item, err := client.GetIngressDetail(c.Request.Context(), c.Query("namespace"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/network/endpointslices", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListEndpointSlices(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/network/endpointslices/:name/detail", func(c *gin.Context) {
		item, err := client.GetEndpointSliceDetail(c.Request.Context(), c.Query("namespace"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/network/networkpolicies", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListNetworkPolicies(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/network/networkpolicies/:name/detail", func(c *gin.Context) {
		item, err := client.GetNetworkPolicyDetail(c.Request.Context(), c.Query("namespace"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/network/gatewayclasses", func(c *gin.Context) {
		items, err := client.ListGatewayClasses(c.Request.Context())
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/network/gatewayclasses/:name/detail", func(c *gin.Context) {
		item, err := client.GetGatewayClassDetail(c.Request.Context(), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/network/gateways", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListGateways(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/network/gateways/:name/detail", func(c *gin.Context) {
		item, err := client.GetGatewayDetail(c.Request.Context(), c.Query("namespace"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/network/httproutes", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListHTTPRoutes(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/network/httproutes/:name/detail", func(c *gin.Context) {
		item, err := client.GetHTTPRouteDetail(c.Request.Context(), c.Query("namespace"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/network/backendtlspolicies", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListBackendTLSPolicies(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/network/backendtlspolicies/:name/detail", func(c *gin.Context) {
		item, err := client.GetBackendTLSPolicyDetail(c.Request.Context(), c.Query("namespace"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/network/grpcroutes", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListGRPCRoutes(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/network/grpcroutes/:name/detail", func(c *gin.Context) {
		item, err := client.GetGRPCRouteDetail(c.Request.Context(), c.Query("namespace"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/network/referencegrants", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListReferenceGrants(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/network/referencegrants/:name/detail", func(c *gin.Context) {
		item, err := client.GetReferenceGrantDetail(c.Request.Context(), c.Query("namespace"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/network/ingressclasses", func(c *gin.Context) {
		items, err := client.ListIngressClasses(c.Request.Context())
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/network/ingressclasses/:name/detail", func(c *gin.Context) {
		item, err := client.GetIngressClassDetail(c.Request.Context(), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
}
