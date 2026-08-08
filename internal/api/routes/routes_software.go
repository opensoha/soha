package routes

import "github.com/gin-gonic/gin"

func registerSoftwareRoutes(protected gin.IRoutes, deps Dependencies) {
	if deps.Software == nil {
		return
	}
	protected.GET("/software/packages", deps.Software.List)
	protected.POST("/software/packages", deps.Software.Upload)
	protected.POST("/software/packages/import", deps.Software.ImportURL)
	protected.GET("/software/storage", deps.Software.Storage)
	protected.DELETE("/software/packages/:packageID", deps.Software.Delete)
	protected.GET("/software/packages/:packageID/download", deps.Software.Download)
}
