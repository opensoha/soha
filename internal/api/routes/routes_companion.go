package routes

import "github.com/gin-gonic/gin"

func registerCompanionRoutes(protected gin.IRoutes, deps Dependencies) {
	if deps.Companion == nil {
		return
	}
	protected.GET("/companion/profile", deps.Companion.GetProfile)
	protected.POST("/companion/interactions", deps.Companion.RecordInteraction)
	protected.POST("/companion/profile/reset", deps.Companion.ResetProfile)
}
