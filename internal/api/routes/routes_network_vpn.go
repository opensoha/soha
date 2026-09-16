package routes

import "github.com/gin-gonic/gin"

func registerNetworkVPNRoutes(protected gin.IRoutes, deps Dependencies) {
	h := deps.NetworkVPN
	if h == nil {
		return
	}
	protected.GET("/network-access/vpn/dashboard", h.Dashboard)
	protected.GET("/network-access/vpn/decisions/:id", h.Decision)
	protected.GET("/network-access/vpn/current-connection", h.CurrentConnection)
	protected.POST("/network-access/vpn/selection-preview", h.PreviewSelection)
	protected.GET("/network-access/vpn/profiles", h.ListProfiles)
	protected.POST("/network-access/vpn/profiles", h.CreateProfile)
	protected.GET("/network-access/vpn/profiles/:id", h.GetProfile)
	protected.PUT("/network-access/vpn/profiles/:id", h.UpdateProfile)
	protected.DELETE("/network-access/vpn/profiles/:id", h.DeleteProfile)
	protected.POST("/network-access/vpn/profiles/:id/publish", h.PublishProfile)
	protected.POST("/network-access/vpn/profiles/:id/rollback", h.RollbackProfile)
	protected.GET("/network-access/vpn/profiles/:id/revisions", h.ProfileRevisions)
	protected.GET("/network-access/vpn/selection-policies", h.ListPolicies)
	protected.POST("/network-access/vpn/selection-policies", h.CreatePolicy)
	protected.GET("/network-access/vpn/selection-policies/:id", h.GetPolicy)
	protected.PUT("/network-access/vpn/selection-policies/:id", h.UpdatePolicy)
	protected.DELETE("/network-access/vpn/selection-policies/:id", h.DeletePolicy)
	protected.POST("/network-access/vpn/selection-policies/:id/publish", h.PublishPolicy)
	protected.POST("/network-access/vpn/selection-policies/:id/rollback", h.RollbackPolicy)
	protected.GET("/network-access/vpn/selection-policies/:id/revisions", h.PolicyRevisions)
	protected.GET("/network-access/vpn/connection-options", h.ConnectionOptions)
	protected.POST("/network-access/vpn/connection-intents", h.CreateIntent)
}
