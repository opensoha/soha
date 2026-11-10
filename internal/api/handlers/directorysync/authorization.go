package directorysync

import (
	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	appaccess "github.com/opensoha/soha/internal/application/access"
)

// Management HTTP entry points are separate from authenticated provider
// callbacks and the internal scheduler. A missing resolver must deny access.
func (h *Handler) SetPermissionResolver(resolver *appaccess.PermissionResolver) {
	h.permissionResolver = resolver
}

func (h *Handler) RequirePermission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := appaccess.AuthorizeRuntimePermission(c.Request.Context(), h.permissionResolver, apiMiddleware.PrincipalFromContext(c), permission); err != nil {
			writeError(c, err)
			c.Abort()
			return
		}
		c.Next()
	}
}
