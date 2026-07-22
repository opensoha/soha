package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	apiresponse "github.com/opensoha/soha/internal/api/response"
)

type ModuleState interface {
	ModuleEnabled(string) bool
}

func RequireModule(state ModuleState, moduleID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if state == nil || state.ModuleEnabled(moduleID) {
			c.Next()
			return
		}
		apiresponse.Error(c, http.StatusNotFound, "module_disabled", "module is disabled")
		c.Abort()
	}
}
