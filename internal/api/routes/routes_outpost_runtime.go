package routes

import "github.com/gin-gonic/gin"

func registerOutpostLifecycleRoutes(protected gin.IRoutes, deps Dependencies) {
	if deps.OutpostLifecycle == nil {
		return
	}
	protected.GET("/identity/outposts/:outpostID/runtime", deps.OutpostLifecycle.Get)
	protected.PUT("/identity/outposts/:outpostID/runtime", deps.OutpostLifecycle.Bind)
	protected.POST("/identity/outposts/:outpostID/runtime/control", deps.OutpostLifecycle.Control)
}

func registerOutpostExecutorRoutes(public gin.IRoutes, deps Dependencies) {
	if deps.OutpostLifecycle == nil {
		return
	}
	public.POST("/identity/outpost-runtime-executor/claim", deps.OutpostLifecycle.Claim)
	public.GET("/identity/outpost-runtime-executor/status", deps.OutpostLifecycle.Status)
	public.POST("/identity/outpost-runtime-executor/callback", deps.OutpostLifecycle.Callback)
}
