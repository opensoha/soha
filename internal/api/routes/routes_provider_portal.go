package routes

import "github.com/gin-gonic/gin"

func registerProviderPortalRoutes(protected gin.IRoutes, deps Dependencies) {
	if deps.ProviderPortal == nil {
		return
	}

	protected.GET("/portal/bootstrap", deps.ProviderPortal.PortalBootstrap)
	protected.GET("/portal/applications", deps.ProviderPortal.ListPortalApplications)
	protected.GET("/portal/applications/:applicationID", deps.ProviderPortal.GetPortalApplication)
	protected.POST("/portal/applications/:applicationID/launch", deps.ProviderPortal.LaunchPortalApplication)
	protected.POST("/portal/applications/:applicationID/favorite", deps.ProviderPortal.SetFavorite)
	protected.DELETE("/portal/applications/:applicationID/favorite", deps.ProviderPortal.DeleteFavorite)
	protected.GET("/portal/recent", deps.ProviderPortal.ListRecent)
	protected.GET("/portal/security", deps.ProviderPortal.SecuritySummary)

	protected.GET("/identity/applications", deps.ProviderPortal.ListIdentityApplications)
	protected.POST("/identity/applications", deps.ProviderPortal.CreateIdentityApplication)
	protected.GET("/identity/applications/:applicationID", deps.ProviderPortal.GetIdentityApplication)
	protected.PUT("/identity/applications/:applicationID", deps.ProviderPortal.UpdateIdentityApplication)
	protected.PATCH("/identity/applications/:applicationID", deps.ProviderPortal.UpdateIdentityApplication)
	protected.DELETE("/identity/applications/:applicationID", deps.ProviderPortal.DeleteIdentityApplication)
	protected.GET("/identity/provider-capabilities", deps.ProviderPortal.ProviderCapabilities)

	protected.GET("/identity/policies", deps.ProviderPortal.ListIdentityPolicies)
	protected.GET("/identity/policies/:applicationID", deps.ProviderPortal.GetIdentityPolicy)
	protected.PUT("/identity/policies/:applicationID", deps.ProviderPortal.UpdateIdentityPolicy)
	protected.PATCH("/identity/policies/:applicationID", deps.ProviderPortal.UpdateIdentityPolicy)

	protected.GET("/identity/providers", deps.ProviderPortal.ListIdentityProviders)
	protected.POST("/identity/providers", deps.ProviderPortal.CreateIdentityProvider)
	protected.GET("/identity/providers/:providerID", deps.ProviderPortal.GetIdentityProvider)
	protected.PUT("/identity/providers/:providerID", deps.ProviderPortal.UpdateIdentityProvider)
	protected.PATCH("/identity/providers/:providerID", deps.ProviderPortal.UpdateIdentityProvider)
	protected.DELETE("/identity/providers/:providerID", deps.ProviderPortal.DeleteIdentityProvider)
	protected.GET("/identity/providers/:providerID/oidc-clients", deps.ProviderPortal.ListOIDCClients)
	protected.POST("/identity/providers/:providerID/oidc-clients", deps.ProviderPortal.CreateOIDCClient)
	protected.PUT("/identity/oidc-clients/:clientID", deps.ProviderPortal.UpdateOIDCClient)
	protected.PATCH("/identity/oidc-clients/:clientID", deps.ProviderPortal.UpdateOIDCClient)
	protected.DELETE("/identity/oidc-clients/:clientID", deps.ProviderPortal.DeleteOIDCClient)

	protected.GET("/identity/outposts", deps.ProviderPortal.ListOutposts)
	protected.POST("/identity/outposts", deps.ProviderPortal.CreateOutpost)
	protected.GET("/identity/outposts/:outpostID", deps.ProviderPortal.GetOutpost)
	protected.PUT("/identity/outposts/:outpostID", deps.ProviderPortal.UpdateOutpost)
	protected.PATCH("/identity/outposts/:outpostID", deps.ProviderPortal.UpdateOutpost)
	protected.DELETE("/identity/outposts/:outpostID", deps.ProviderPortal.DeleteOutpost)

	if deps.Platform != nil {
		protected.GET("/identity/audit/events", deps.Platform.ListAuditLogs)
	}
}

func registerProviderProtocolRoutes(public gin.IRoutes, deps Dependencies) {
	if deps.ProviderPortal == nil {
		return
	}

	public.GET("/provider/oidc/.well-known/openid-configuration", deps.ProviderPortal.OIDCDiscovery)
	public.GET("/provider/oidc/authorize", deps.ProviderPortal.OIDCAuthorize)
	public.POST("/provider/oidc/token", deps.ProviderPortal.OIDCToken)
	public.GET("/provider/oidc/userinfo", deps.ProviderPortal.OIDCUserInfo)
	public.POST("/provider/oidc/userinfo", deps.ProviderPortal.OIDCUserInfo)
	public.GET("/provider/oidc/jwks", deps.ProviderPortal.OIDCJWKS)
	public.POST("/provider/oidc/introspect", deps.ProviderPortal.OIDCIntrospect)
	public.POST("/provider/oidc/revoke", deps.ProviderPortal.OIDCRevoke)
	public.GET("/provider/oidc/logout", deps.ProviderPortal.OIDCEndSession)
	public.POST("/provider/oidc/logout", deps.ProviderPortal.OIDCEndSession)
	public.GET("/provider/proxy/auth", deps.ProviderPortal.ProxyAuth)
	public.POST("/provider/proxy/auth", deps.ProviderPortal.ProxyAuth)
	public.GET("/provider/proxy/start", deps.ProviderPortal.ProxyStart)
	public.GET("/provider/proxy/callback", deps.ProviderPortal.ProxyCallback)
	public.POST("/provider/proxy/logout", deps.ProviderPortal.ProxyLogout)
	public.Any("/provider/proxy/reverse/:providerID", deps.ProviderPortal.ProxyReverse)
	public.Any("/provider/proxy/reverse/:providerID/*proxyPath", deps.ProviderPortal.ProxyReverse)
	public.POST("/provider/outposts/claim", deps.ProviderPortal.ClaimOutpost)
	public.POST("/provider/outposts/:outpostID/heartbeat", deps.ProviderPortal.HeartbeatOutpost)
	public.POST("/provider/outposts/:outpostID/check", deps.ProviderPortal.CheckOutpost)
	public.POST("/provider/outposts/:outpostID/events", deps.ProviderPortal.OutpostEvents)
}

func registerStandardProviderProtocolRoutes(router *gin.Engine, deps Dependencies) {
	if deps.ProviderPortal == nil {
		return
	}

	router.GET("/.well-known/openid-configuration", deps.ProviderPortal.OIDCDiscovery)
	router.GET("/oauth2/authorize", deps.ProviderPortal.OIDCAuthorize)
	router.POST("/oauth2/token", deps.ProviderPortal.OIDCToken)
	router.GET("/oauth2/userinfo", deps.ProviderPortal.OIDCUserInfo)
	router.POST("/oauth2/userinfo", deps.ProviderPortal.OIDCUserInfo)
	router.GET("/oauth2/jwks", deps.ProviderPortal.OIDCJWKS)
	router.POST("/oauth2/introspect", deps.ProviderPortal.OIDCIntrospect)
	router.POST("/oauth2/revoke", deps.ProviderPortal.OIDCRevoke)
	router.GET("/oauth2/logout", deps.ProviderPortal.OIDCEndSession)
	router.POST("/oauth2/logout", deps.ProviderPortal.OIDCEndSession)
}
