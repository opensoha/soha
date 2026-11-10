package routes

import (
	"github.com/gin-gonic/gin"
	appaccess "github.com/opensoha/soha/internal/application/access"
)

func registerDirectorySyncRoutes(protected gin.IRoutes, deps Dependencies) {
	if deps.DirectorySync == nil {
		return
	}
	h := deps.DirectorySync
	protected.GET("/access/directory-connections", h.RequirePermission(appaccess.PermAccessDirectoryView), h.ListConnections)
	protected.POST("/access/directory-connections", h.RequirePermission(appaccess.ManagedActionPermission(appaccess.PermAccessDirectoryManage, "create")), h.CreateConnection)
	protected.GET("/access/directory-connections/:connectionID", h.RequirePermission(appaccess.PermAccessDirectoryView), h.GetConnection)
	protected.PUT("/access/directory-connections/:connectionID", h.RequirePermission(appaccess.ManagedActionPermission(appaccess.PermAccessDirectoryManage, "update")), h.UpdateConnection)
	protected.DELETE("/access/directory-connections/:connectionID", h.RequirePermission(appaccess.ManagedActionPermission(appaccess.PermAccessDirectoryManage, "delete")), h.DeleteConnection)
	protected.POST("/access/directory-connections/:connectionID/validate", h.RequirePermission(appaccess.ManagedActionPermission(appaccess.PermAccessDirectoryManage, "validate")), h.ValidateConnection)
	protected.POST("/access/directory-connections/:connectionID/sync/preview", h.RequirePermission(appaccess.PermAccessDirectorySync), h.Preview)
	protected.POST("/access/directory-connections/:connectionID/sync", h.RequirePermission(appaccess.PermAccessDirectorySync), h.Sync)
	protected.POST("/access/directory-connections/:connectionID/sync-requests", h.RequirePermission(appaccess.PermAccessDirectorySync), h.CreateSyncRequest)
	protected.GET("/access/directory-connections/:connectionID/sync-requests/:idempotencyKey", h.RequirePermission(appaccess.PermAccessDirectoryView), h.RequirePermission(appaccess.PermAccessDirectorySync), h.GetSyncRequest)
	protected.GET("/access/directory-connections/:connectionID/runs", h.RequirePermission(appaccess.PermAccessDirectoryView), h.ListRuns)
	protected.GET("/access/directory-connections/:connectionID/runtime-status", h.RequirePermission(appaccess.PermAccessDirectoryView), h.RuntimeStatus)
	protected.GET("/access/directory-connections/:connectionID/events", h.RequirePermission(appaccess.PermAccessDirectoryView), h.ListEvents)
	protected.POST("/access/directory-connections/:connectionID/events/:eventID/retry", h.RequirePermission(appaccess.PermAccessDirectorySync), h.RetryEvent)
	protected.POST("/access/directory-connections/:connectionID/runs/:runID/cancel", h.RequirePermission(appaccess.PermAccessDirectorySync), h.Cancel)
	protected.POST("/access/directory-connections/:connectionID/sync/cancel", h.RequirePermission(appaccess.PermAccessDirectorySync), h.Cancel)
	protected.GET("/access/directory-runs/:runID", h.RequirePermission(appaccess.PermAccessDirectoryView), h.GetRun)
	protected.GET("/access/directory-conflicts", h.RequirePermission(appaccess.PermAccessDirectoryView), h.ListConflicts)
	protected.POST("/access/directory-conflicts/:conflictID/resolve", h.RequirePermission(appaccess.ManagedActionPermission(appaccess.PermAccessDirectoryPeopleManage, "resolve")), h.ResolveConflict)
	protected.POST("/access/identity-links/:identityID/unlink", h.RequirePermission(appaccess.ManagedActionPermission(appaccess.PermAccessIdentityLinkManage, "unlink")), h.UnlinkIdentity)
	protected.POST("/access/identity-link-suppressions/:suppressionID/clear", h.RequirePermission(appaccess.ManagedActionPermission(appaccess.PermAccessIdentityLinkManage, "unlink")), h.ClearSuppression)
}
