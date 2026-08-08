package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
)

type PluginMarketplaceService interface {
	ListMarketplace(context.Context, domainidentity.Principal, domainplugin.MarketplaceFilter) ([]domainplugin.MarketplacePlugin, error)
	GetMarketplace(context.Context, domainidentity.Principal, domainplugin.PluginVersionRef) (domainplugin.MarketplacePlugin, error)
}

type PluginInventoryService interface {
	ListInstalled(context.Context, domainidentity.Principal) ([]domainplugin.InstalledPlugin, error)
	GetInstalled(context.Context, domainidentity.Principal, string) (domainplugin.InstalledPlugin, error)
	GetManifest(context.Context, domainidentity.Principal, string) (domainplugin.PluginManifest, error)
}

type PluginLifecycleService interface {
	Install(context.Context, domainidentity.Principal, domainplugin.PluginInstallRequest) (domainplugin.InstalledPlugin, error)
	Enable(context.Context, domainidentity.Principal, string) (domainplugin.InstalledPlugin, error)
	Disable(context.Context, domainidentity.Principal, string) (domainplugin.InstalledPlugin, error)
	Upgrade(context.Context, domainidentity.Principal, string, domainplugin.PluginInstallRequest) (domainplugin.InstalledPlugin, error)
	Configure(context.Context, domainidentity.Principal, string, domainplugin.PluginConfigRequest) (domainplugin.InstalledPlugin, error)
	Remove(context.Context, domainidentity.Principal, string) error
}

type PluginExtensionService interface {
	ListExtensions(context.Context, domainidentity.Principal, string) ([]domainplugin.ExtensionRecord, error)
}

type PluginCompanionLifecycleService interface {
	ActivateCompanion(context.Context, domainidentity.Principal, string, string, domainplugin.CompanionActivationRequest) (domainplugin.InstalledPlugin, error)
	RollbackCompanion(context.Context, domainidentity.Principal, string, string, domainplugin.CompanionRollbackRequest) (domainplugin.InstalledPlugin, error)
}

type PluginAssetService interface {
	OpenCompanionAsset(context.Context, domainidentity.Principal, string, string) (io.ReadCloser, string, string, error)
}

type PluginService interface {
	PluginMarketplaceService
	PluginInventoryService
	PluginLifecycleService
	PluginExtensionService
}

type PluginHandler struct {
	marketplace PluginMarketplaceService
	inventory   PluginInventoryService
	lifecycle   PluginLifecycleService
	extensions  PluginExtensionService
	companions  PluginCompanionLifecycleService
	assets      PluginAssetService
}

func NewPluginHandler(service PluginService) *PluginHandler {
	return NewPluginHandlerWithServices(service, service, service, service)
}

func NewPluginHandlerWithServices(marketplace PluginMarketplaceService, inventory PluginInventoryService, lifecycle PluginLifecycleService, extensions PluginExtensionService) *PluginHandler {
	handler := &PluginHandler{marketplace: marketplace, inventory: inventory, lifecycle: lifecycle, extensions: extensions}
	handler.companions, _ = lifecycle.(PluginCompanionLifecycleService)
	handler.assets, _ = inventory.(PluginAssetService)
	return handler
}

func (h *PluginHandler) ListMarketplace(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.marketplace.ListMarketplace(c.Request.Context(), principal, domainplugin.MarketplaceFilter{
		Query:          c.Query("q"),
		Type:           c.Query("type"),
		Publisher:      c.Query("publisher"),
		SourceID:       c.Query("sourceId"),
		MarketplaceURL: c.Query("marketplaceUrl"),
		Version:        c.Query("version"),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PluginHandler) GetMarketplace(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.marketplace.GetMarketplace(c.Request.Context(), principal, domainplugin.PluginVersionRef{
		PluginID:       c.Param("pluginID"),
		Version:        c.Query("version"),
		SourceID:       c.Query("sourceId"),
		MarketplaceURL: c.Query("marketplaceUrl"),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PluginHandler) ListInstalled(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.inventory.ListInstalled(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PluginHandler) GetInstalled(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.inventory.GetInstalled(c.Request.Context(), principal, c.Param("pluginID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PluginHandler) GetManifest(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.inventory.GetManifest(c.Request.Context(), principal, c.Param("pluginID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PluginHandler) Install(c *gin.Context) {
	var req domainplugin.PluginInstallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid plugin install payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.lifecycle.Install(c.Request.Context(), principal, req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *PluginHandler) Enable(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.lifecycle.Enable(c.Request.Context(), principal, c.Param("pluginID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PluginHandler) Disable(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.lifecycle.Disable(c.Request.Context(), principal, c.Param("pluginID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PluginHandler) Upgrade(c *gin.Context) {
	var req domainplugin.PluginInstallRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid plugin upgrade payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.lifecycle.Upgrade(c.Request.Context(), principal, c.Param("pluginID"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PluginHandler) Configure(c *gin.Context) {
	var req domainplugin.PluginConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid plugin config payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.lifecycle.Configure(c.Request.Context(), principal, c.Param("pluginID"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PluginHandler) Remove(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.lifecycle.Remove(c.Request.Context(), principal, c.Param("pluginID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *PluginHandler) ActivateCompanion(c *gin.Context) {
	if h.companions == nil {
		apiresponse.Error(c, http.StatusNotImplemented, "unsupported_operation", "companion lifecycle is unavailable")
		return
	}
	var req domainplugin.CompanionActivationRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid companion activation payload")
		return
	}
	item, err := h.companions.ActivateCompanion(
		c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("pluginID"), c.GetHeader("Idempotency-Key"), req,
	)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PluginHandler) RollbackCompanion(c *gin.Context) {
	if h.companions == nil {
		apiresponse.Error(c, http.StatusNotImplemented, "unsupported_operation", "companion lifecycle is unavailable")
		return
	}
	var req domainplugin.CompanionRollbackRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid companion rollback payload")
		return
	}
	item, err := h.companions.RollbackCompanion(
		c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("pluginID"), c.GetHeader("Idempotency-Key"), req,
	)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PluginHandler) GetCompanionAsset(c *gin.Context) {
	if h.assets == nil {
		apiresponse.Error(c, http.StatusNotImplemented, "unsupported_operation", "companion assets are unavailable")
		return
	}
	assetPath := strings.TrimPrefix(c.Param("assetPath"), "/")
	reader, contentType, etag, err := h.assets.OpenCompanionAsset(
		c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("pluginID"), assetPath,
	)
	if err != nil {
		writeError(c, err)
		return
	}
	defer func() { _ = reader.Close() }()
	c.Header("Cache-Control", "private, max-age=31536000, immutable")
	c.Header("ETag", `"`+etag+`"`)
	c.Header("X-Content-Type-Options", "nosniff")
	if contentType == "image/svg+xml" {
		c.Header("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
	}
	c.Header("Content-Type", contentType)
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, reader)
}

func (h *PluginHandler) ListRuntimeExtensions(c *gin.Context) {
	h.listExtensions(c, "runtime")
}

func (h *PluginHandler) ListResourceExtensions(c *gin.Context) {
	h.listExtensions(c, "resource")
}

func (h *PluginHandler) ListMetricExtensions(c *gin.Context) {
	h.listExtensions(c, "metrics")
}

func (h *PluginHandler) ListAlertExtensions(c *gin.Context) {
	h.listExtensions(c, "alerts")
}

func (h *PluginHandler) ListAIExtensions(c *gin.Context) {
	h.listExtensions(c, "ai")
}

func (h *PluginHandler) ListAuthExtensions(c *gin.Context) {
	h.listExtensions(c, "auth")
}

func (h *PluginHandler) ListIdentityExtensions(c *gin.Context) {
	h.listExtensions(c, "identity")
}

func (h *PluginHandler) ListUIExtensions(c *gin.Context) {
	h.listExtensions(c, "ui")
}

func (h *PluginHandler) listExtensions(c *gin.Context, scope string) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.extensions.ListExtensions(c.Request.Context(), principal, scope)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
