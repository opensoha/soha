package routes

import (
	"time"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
)

func registerProxyWorkspaceRoutes(protected gin.IRoutes, deps Dependencies) {
	const base = "/network-access/proxy-instances/:instanceID"
	if h := deps.ProxyCommands; h != nil {
		protected.POST(base+"/commands", h.Queue)
		protected.GET(base+"/commands/:commandID", h.Get)
		protected.POST(base+"/commands/:commandID/cancel", h.Cancel)
	}
	if h := deps.ProxySources; h != nil {
		protected.GET(base+"/sources", h.List)
		protected.POST(base+"/sources", h.Create)
		protected.POST(base+"/sources/preview", h.Preview)
		protected.GET(base+"/sources/:sourceID", h.Get)
		protected.PUT(base+"/sources/:sourceID", h.Put)
		protected.DELETE(base+"/sources/:sourceID", h.Delete)
	}
	if h := deps.ProxyBundles; h != nil {
		protected.GET(base+"/bundles", h.List)
		protected.POST(base+"/bundles", h.Prepare)
		protected.POST(base+"/bundles/:bundleID/activate", h.Activate)
	}
	if h := deps.ProxyRules; h != nil {
		protected.GET(base+"/rules", h.Get)
		protected.PUT(base+"/rules", h.Put)
		protected.GET(base+"/personal-rules", h.GetPersonal)
		protected.PUT(base+"/personal-rules", h.PutPersonal)
	}
	if h := deps.ProxyDeployment; h != nil {
		protected.GET(base+"/deployment", h.Get)
		protected.PUT(base+"/deployment", h.Bind)
		protected.POST(base+"/deployment/control", h.Control)
	}
}

func registerProxyExecutorRoutes(v1 *gin.RouterGroup, deps Dependencies) {
	if h := deps.ProxyDeployment; h != nil {
		limits := apiMiddleware.NewBoundedRateLimiter(10_000)
		group := v1.Group("/network/proxy-deployment-executor", limits.Middleware("proxy-executor", 120, time.Minute, nil))
		group.POST("/claim", h.Claim)
		group.GET("/status", h.Status)
		group.POST("/callback", h.Callback)
	}
}
