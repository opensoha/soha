package routes

import "github.com/gin-gonic/gin"

func registerDirectorySyncRoutes(protected gin.IRoutes, deps Dependencies) {
	if deps.DirectorySync == nil {
		return
	}
	h := deps.DirectorySync
	protected.GET("/access/directory-connections", h.ListConnections)
	protected.POST("/access/directory-connections", h.CreateConnection)
	protected.GET("/access/directory-connections/:connectionID", h.GetConnection)
	protected.PUT("/access/directory-connections/:connectionID", h.UpdateConnection)
	protected.DELETE("/access/directory-connections/:connectionID", h.DeleteConnection)
	protected.POST("/access/directory-connections/:connectionID/validate", h.ValidateConnection)
	protected.POST("/access/directory-connections/:connectionID/sync/preview", h.Preview)
	protected.POST("/access/directory-connections/:connectionID/sync", h.Sync)
	protected.GET("/access/directory-connections/:connectionID/runs", h.ListRuns)
	protected.POST("/access/directory-connections/:connectionID/runs/:runID/cancel", h.Cancel)
	protected.POST("/access/directory-connections/:connectionID/sync/cancel", h.Cancel)
	protected.GET("/access/directory-runs/:runID", h.GetRun)
	protected.GET("/access/directory-conflicts", h.ListConflicts)
	protected.POST("/access/directory-conflicts/:conflictID/resolve", h.ResolveConflict)
	protected.POST("/access/identity-links/:identityID/unlink", h.UnlinkIdentity)
	protected.POST("/access/identity-link-suppressions/:suppressionID/clear", h.ClearSuppression)
}
