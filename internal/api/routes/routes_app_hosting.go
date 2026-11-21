package routes

import (
	"time"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
)

func registerPublicAppHostingRoutes(public gin.IRoutes, deps Dependencies) {
	if deps.AppHosting == nil {
		return
	}
	limits := apiMiddleware.NewBoundedRateLimiter(10_000)
	hostKey := func(c *gin.Context) string { return c.Query("host") }
	public.GET("/app-hosting/auth/start", limits.Middleware("app-hosting-auth-start", 120, time.Minute, hostKey), deps.AppHosting.StartBrowserAuth)
	public.POST("/app-hosting/gateway/tickets/exchange", limits.Middleware("app-hosting-ticket-exchange", 300, time.Minute, nil), deps.AppHosting.ExchangeGatewayTicket)
	public.POST("/app-hosting/gateway/access-check", limits.Middleware("app-hosting-access-check", 2400, time.Minute, nil), deps.AppHosting.CheckGatewayAccess)
}

func registerProtectedAppHostingRoutes(protected gin.IRoutes, deps Dependencies) {
	if deps.AppHosting == nil {
		return
	}
	protected.GET("/app-hosting/settings", deps.AppHosting.GetSettings)
	protected.PUT("/app-hosting/settings", deps.AppHosting.UpdateSettings)
	protected.POST("/app-hosting/settings/gateway-token/rotate", deps.AppHosting.RotateGatewayToken)
	protected.GET("/app-hosting/audience-candidates", deps.AppHosting.ListAudienceCandidates)

	protected.GET("/app-hosting/apps", deps.AppHosting.ListApps)
	protected.POST("/app-hosting/apps", deps.AppHosting.CreateApp)
	protected.GET("/app-hosting/apps/:appId", deps.AppHosting.GetApp)
	protected.PATCH("/app-hosting/apps/:appId", deps.AppHosting.UpdateApp)
	protected.PUT("/app-hosting/apps/:appId/visibility", deps.AppHosting.UpdateVisibility)
	protected.POST("/app-hosting/apps/:appId/offline", deps.AppHosting.TakeOffline)
	protected.POST("/app-hosting/apps/:appId/online", deps.AppHosting.BringOnline)
	protected.POST("/app-hosting/apps/:appId/suspend", deps.AppHosting.SuspendApp)
	protected.POST("/app-hosting/apps/:appId/unsuspend", deps.AppHosting.UnsuspendApp)

	protected.POST("/app-hosting/apps/:appId/deployments/preflight", deps.AppHosting.PreflightDeployment)
	protected.GET("/app-hosting/apps/:appId/revisions", deps.AppHosting.ListRevisions)
	protected.POST("/app-hosting/apps/:appId/revisions", deps.AppHosting.UploadRevision)
	protected.GET("/app-hosting/apps/:appId/revisions/:revisionId", deps.AppHosting.GetRevision)
	protected.POST("/app-hosting/apps/:appId/rollback", deps.AppHosting.Rollback)
}
