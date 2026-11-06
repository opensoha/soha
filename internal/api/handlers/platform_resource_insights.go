package handlers

import (
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (h *resourceGraphHandler) GetResourceGraph(c *gin.Context) {
	params, parseErr := url.ParseQuery(c.Request.URL.RawQuery)
	kind, name := params.Get("kind"), params.Get("name")
	if parseErr != nil || len(params["kind"]) != 1 || len(params["name"]) != 1 || len(params["namespace"]) > 1 || kind == "" || name == "" {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	ctx, principal := c.Request.Context(), apiMiddleware.PrincipalFromContext(c)
	cluster, namespace := c.Param("clusterID"), params.Get("namespace")
	var item domainresource.ResourceGraph
	var err error
	if params.Has("apiVersion") || params.Has("resource") {
		if len(params["apiVersion"]) != 1 || len(params["resource"]) != 1 || params.Get("apiVersion") == "" || params.Get("resource") == "" {
			writeError(c, apperrors.ErrInvalidArgument)
			return
		}
		item, err = h.service.GetCustomResourceGraph(ctx, principal, domainresource.ResourceRef{
			ClusterID: cluster, Namespace: namespace, APIVersion: params.Get("apiVersion"), Kind: kind, Name: name,
		}, params.Get("resource"))
	} else {
		item, err = h.service.GetResourceGraph(ctx, principal, cluster, namespace, kind, name)
	}
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *securityPostureHandler) GetSecurityPosture(c *gin.Context) {
	item, err := h.service.GetSecurityPosture(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), c.Query("namespace"), parseLimit(c.Query("limit"), 100))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
