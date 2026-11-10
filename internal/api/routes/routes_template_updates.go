package routes

import "github.com/gin-gonic/gin"

func registerTemplateUpdateRoutes(protected gin.IRoutes, deps Dependencies) {
	protected.GET("/delivery/template-updates/targets", deps.TemplateUpdates.Targets)
	protected.POST("/delivery/template-updates/previews", deps.TemplateUpdates.Preview)
	protected.GET("/delivery/template-updates/previews/:previewID", deps.TemplateUpdates.GetPreview)
	protected.POST("/delivery/template-updates/previews/:previewID/batches", deps.TemplateUpdates.Batch)
	protected.GET("/delivery/template-updates/policies", deps.TemplateUpdates.Policies)
	protected.PUT("/delivery/template-updates/policies", deps.TemplateUpdates.SavePolicy)
}
