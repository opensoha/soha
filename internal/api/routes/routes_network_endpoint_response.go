package routes

import "github.com/gin-gonic/gin"

func registerNetworkEndpointResponseRoutes(protected gin.IRoutes, deps Dependencies) {
	if c := deps.EndpointControls; c != nil {
		protected.GET("/network-access/endpoint-devices/:deviceID/channel-controls", c.Get)
		protected.PUT("/network-access/endpoint-devices/:deviceID/channel-controls", c.Save)
		protected.POST("/network-access/endpoint-devices/:deviceID/channel-controls/publish", c.Publish)
	}
	h := deps.NetworkEndpointResponse
	if h == nil {
		return
	}
	protected.GET("/network-access/endpoint-response-policies", h.ListPolicies)
	protected.POST("/network-access/endpoint-response-policies", h.CreatePolicy)
	protected.GET("/network-access/endpoint-response-policies/:policyID", h.GetPolicy)
	protected.PUT("/network-access/endpoint-response-policies/:policyID", h.UpdatePolicy)
	protected.DELETE("/network-access/endpoint-response-policies/:policyID", h.DeletePolicy)
	protected.POST("/network-access/endpoint-response-policies/:policyID/publish", h.PublishPolicy)
	protected.POST("/network-access/endpoint-response-policies/:policyID/rollback", h.RollbackPolicy)
	protected.GET("/network-access/endpoint-response-policies/:policyID/revisions", h.ListRevisions)
	protected.POST("/network-access/endpoint-response-policies/:policyID/preview", h.Preview)
	protected.GET("/network-access/endpoint-response-events", h.ListEvents)
	protected.GET("/network-access/endpoint-response-events/:eventID", h.GetEvent)
	protected.POST("/network-access/endpoint-response-events/:eventID/restore", h.RestoreEvent)
	protected.GET("/network-access/endpoint-devices/:deviceID/presence", h.GetPresence)
	protected.GET("/network-access/endpoint-maintenance-windows", h.ListMaintenance)
	protected.POST("/network-access/endpoint-maintenance-windows", h.CreateMaintenance)
	protected.DELETE("/network-access/endpoint-maintenance-windows/:windowID", h.DeleteMaintenance)
}
