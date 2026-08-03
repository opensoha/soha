package routes

import "github.com/gin-gonic/gin"

func registerSecretRoutes(protected gin.IRoutes, deps Dependencies) {
	if deps.Secrets == nil {
		return
	}
	protected.GET("/secrets", deps.Secrets.List)
	protected.POST("/secrets", deps.Secrets.Create)
	protected.GET("/secrets/:secretID", deps.Secrets.Get)
	protected.PATCH("/secrets/:secretID", deps.Secrets.Update)
	protected.DELETE("/secrets/:secretID", deps.Secrets.Disable)
	protected.GET("/secrets/:secretID/versions", deps.Secrets.ListVersions)
	protected.POST("/secrets/:secretID/versions", deps.Secrets.Rotate)
	protected.POST("/secrets/:secretID/versions/:version/revoke", deps.Secrets.RevokeVersion)
}
