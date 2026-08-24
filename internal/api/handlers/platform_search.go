package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

const platformResourceSearchDefaultLimit = 20

func (h *resourceSearchHandler) SearchResources(c *gin.Context) {
	input := domainresource.ResourceSearchInput{
		Query:     c.Query("q"),
		Namespace: c.Query("namespace"),
		Kinds:     splitResourceSearchKinds(c.Query("kinds")),
		Limit:     parseLimit(c.Query("limit"), platformResourceSearchDefaultLimit),
	}
	result, err := h.service.SearchResources(
		c.Request.Context(),
		apiMiddleware.PrincipalFromContext(c),
		c.Param("clusterID"),
		input,
	)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, result)
}

func splitResourceSearchKinds(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if kind := strings.TrimSpace(part); kind != "" {
			result = append(result, kind)
		}
	}
	return result
}
