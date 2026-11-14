package routes

import (
	"github.com/gin-gonic/gin"
	m "github.com/opensoha/soha/internal/api/middleware"
	"time"
)

func registerRadiusDeploymentRoutes(r gin.IRoutes, d Dependencies) {
	if h := d.RadiusDeployment; h != nil {
		const base = "/network-access/radius-pools"
		r.GET(base, h.List)
		r.GET(base+"/options", h.Options)
		r.POST(base, h.Create)
		r.GET(base+"/:poolID", h.Get)
		r.PUT(base+"/:poolID/configuration", h.Configure)
		r.POST(base+"/:poolID/control", h.Control)
	}
}
func registerRadiusExecutorRoutes(r *gin.RouterGroup, d Dependencies) {
	if h := d.RadiusExecutor; h != nil {
		l := m.NewBoundedRateLimiter(10000)
		g := r.Group("/network/radius-deployment-executor", l.Middleware("radius-executor", 120, time.Minute, nil))
		g.POST("/claim", h.Claim)
		g.GET("/status", h.Status)
		g.POST("/callback", h.Callback)
	}
}
