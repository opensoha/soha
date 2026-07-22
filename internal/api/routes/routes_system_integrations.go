package routes

import "github.com/gin-gonic/gin"

func registerSystemIntegrationRoutes(protected gin.IRoutes, deps Dependencies) {
	if deps.SystemIntegrations == nil {
		return
	}
	protected.GET("/system-integrations", deps.SystemIntegrations.List)
	protected.POST("/system-integrations", deps.SystemIntegrations.Create)
	protected.GET("/system-integrations/:integrationID", deps.SystemIntegrations.Get)
	protected.PATCH("/system-integrations/:integrationID", deps.SystemIntegrations.Update)
	protected.DELETE("/system-integrations/:integrationID", deps.SystemIntegrations.Delete)
	protected.POST("/system-integrations/:integrationID/test", deps.SystemIntegrations.Test)

	protected.GET("/source-connections", deps.SystemIntegrations.ListSources)
	protected.GET("/source-connections/:sourceConnectionID", deps.SystemIntegrations.GetSource)
	protected.GET("/source-connections/:sourceConnectionID/repositories", deps.SystemIntegrations.ListRepositories)
	protected.GET("/source-connections/:sourceConnectionID/repositories/:repositoryID/branches", deps.SystemIntegrations.ListBranches)
	protected.GET("/source-connections/:sourceConnectionID/repositories/:repositoryID/tags", deps.SystemIntegrations.ListTags)
	protected.GET("/source-connections/:sourceConnectionID/repositories/:repositoryID/files", deps.SystemIntegrations.GetFile)
}
