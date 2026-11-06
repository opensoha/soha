package handlers

import (
	"context"
	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"net/http"
)

func writeControllerHistory(c *gin.Context, nameParam string, read func(context.Context, domainidentity.Principal, string, string, string) ([]domainresource.RolloutHistoryView, error)) {
	items, err := read(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), c.DefaultQuery("namespace", "default"), c.Param(nameParam))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func writeControllerRevision(c *gin.Context, nameParam string, read func(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)) {
	item, err := read(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), c.DefaultQuery("namespace", "default"), c.Param(nameParam), c.Param("revisionName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
