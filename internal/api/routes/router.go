package routes

import (
	"io/fs"
	"mime"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	apiHandlers "github.com/opensoha/soha/internal/api/handlers"
	accesshandler "github.com/opensoha/soha/internal/api/handlers/access"
	directorysynchandler "github.com/opensoha/soha/internal/api/handlers/directorysync"
	providerportalhandler "github.com/opensoha/soha/internal/api/handlers/providerportal"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	swaggerinfra "github.com/opensoha/soha/internal/infrastructure/swagger"
	"github.com/opensoha/soha/internal/staticassets"
	"go.uber.org/zap"
)

const (
	assetModeEmbed    = "embed"
	assetModeDir      = "dir"
	assetModeProxy    = "proxy"
	assetModeExternal = "external"
	assetModeDisabled = "disabled"
)

type Dependencies struct {
	System         *apiHandlers.SystemHandler
	Platform       *apiHandlers.PlatformHandler
	Announcements  *apiHandlers.AnnouncementHandler
	Module         *apiHandlers.ModuleHandler
	Monitoring     *apiHandlers.MonitoringHandler
	Catalog        *apiHandlers.CatalogHandler
	Delivery       *apiHandlers.DeliveryHandler
	Applications   *apiHandlers.ApplicationHandler
	Builds         *apiHandlers.BuildHandler
	Workflows      *apiHandlers.WorkflowHandler
	Registries     *apiHandlers.RegistryHandler
	Releases       *apiHandlers.ReleaseHandler
	Copilot        *apiHandlers.CopilotHandler
	AIGateway      *apiHandlers.AIGatewayHandler
	Plugins        *apiHandlers.PluginHandler
	Compute        *apiHandlers.ComputeHandler
	Virtualization *apiHandlers.VirtualizationHandler
	Docker         *apiHandlers.DockerHandler
	Access         *accesshandler.Handler
	DirectorySync  *directorysynchandler.Handler
	ScopeGrants    *accesshandler.ScopeGrantHandler
	Menu           *apiHandlers.MenuHandler
	Settings       *apiHandlers.SettingsHandler
	Auth           *apiHandlers.AuthHandler
	ProviderPortal *providerportalhandler.Handler
	Authn          apiMiddleware.AccessTokenParser
}

func New(cfg cfgpkg.Config, logger *zap.Logger, deps Dependencies) *http.Server {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	if err := router.SetTrustedProxies(cfg.HTTP.TrustedProxies); err != nil {
		logger.Error("invalid trusted proxy configuration; proxy headers disabled", zap.Error(err))
		_ = router.SetTrustedProxies(nil)
	}
	router.Use(gin.Recovery())
	router.Use(apiMiddleware.RequestID())
	router.Use(apiMiddleware.RequestLogger(logger))
	router.Use(apiMiddleware.CORS(cfg.HTTP.CORSAllowedOrigins))
	router.Use(apiMiddleware.BuildPrincipalMiddleware(cfg.Auth, deps.Authn))

	router.GET("/healthz", deps.System.Healthz)
	router.GET("/readyz", deps.System.Readyz)
	swaggerinfra.Register(router, cfg.Swagger.Enabled, cfg.Swagger.Path)
	apiCompat := router.Group("/api")
	apiCompat.Use(apiMiddleware.RequireAuth())
	{
		apiCompat.GET("/currentUser", deps.Auth.ProCurrentUser)
		apiCompat.GET("/currentUserDetail", deps.Auth.ProCurrentUser)
		apiCompat.GET("/accountSettingCurrentUser", deps.Auth.ProCurrentUser)
		apiCompat.POST("/login/outLogin", deps.Auth.ProLogout)
	}
	router.POST("/api/login/account", deps.Auth.ProLogin)

	v1 := router.Group(cfg.HTTP.BasePath)
	registerPublicRoutes(v1, cfg, deps)

	protected := router.Group(cfg.HTTP.BasePath)
	protected.Use(apiMiddleware.RequireAuth())
	registerProtectedRoutes(protected, cfg, deps)
	registerStandardProviderProtocolRoutes(router, deps)

	registerDocs(router, logger, cfg.Assets.Docs)

	registerSPA(router, logger, cfg.Assets.Web)

	logger.Info("http server configured",
		zap.String("addr", cfg.HTTP.Addr),
		zap.String("base_path", cfg.HTTP.BasePath),
	)

	return &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
		IdleTimeout:       cfg.HTTP.IdleTimeout,
		MaxHeaderBytes:    cfg.HTTP.MaxHeaderBytes,
	}
}

func registerDocs(router *gin.Engine, logger *zap.Logger, cfg cfgpkg.DocsAssetsConfig) {
	switch normalizedAssetMode(cfg.Mode, assetModeExternal) {
	case assetModeExternal:
		registerDocsRedirect(router, logger, cfg.ExternalURL)
	case assetModeProxy:
		registerDocsProxy(router, logger, cfg.ProxyURL)
	case assetModeDir:
		buildFS, err := staticassets.DiskFS(cfg.Dir)
		if err != nil {
			logger.Warn("docs assets not available, docs serving disabled", zap.String("dir", cfg.Dir), zap.Error(err))
			return
		}
		registerDocsFS(router, buildFS)
	case assetModeDisabled:
		logger.Info("docs serving disabled")
	default:
		logger.Warn("unknown docs assets mode, docs serving disabled", zap.String("mode", cfg.Mode))
	}
}

func registerDocsFS(router *gin.Engine, buildFS fs.FS) {
	router.GET("/docs", func(c *gin.Context) {
		c.Redirect(http.StatusPermanentRedirect, "/docs/")
	})

	router.GET("/docs/*filepath", func(c *gin.Context) {
		requestPath := strings.TrimPrefix(c.Param("filepath"), "/")
		if requestPath == "" {
			requestPath = "index.html"
		}

		candidates := []string{requestPath}
		if !strings.Contains(path.Base(requestPath), ".") {
			candidates = append(candidates, path.Join(requestPath, "index.html"))
		}

		for _, candidate := range candidates {
			if info, err := fs.Stat(buildFS, candidate); err == nil && !info.IsDir() {
				serveEmbeddedFile(c, buildFS, candidate)
				return
			}
		}

		serveEmbeddedFile(c, buildFS, "404.html")
	})
}

func registerDocsRedirect(router *gin.Engine, logger *zap.Logger, externalURL string) {
	baseURL := strings.TrimSpace(externalURL)
	if baseURL == "" {
		logger.Warn("docs external URL is empty, docs redirect disabled")
		return
	}

	redirect := func(c *gin.Context) {
		requestPath := strings.TrimPrefix(c.Param("filepath"), "/")
		target := strings.TrimRight(baseURL, "/") + "/"
		if requestPath != "" {
			target += requestPath
		}
		if c.Request.URL.RawQuery != "" {
			target += "?" + c.Request.URL.RawQuery
		}
		c.Redirect(http.StatusTemporaryRedirect, target)
	}

	router.GET("/docs", redirect)
	router.GET("/docs/*filepath", redirect)
}

func registerDocsProxy(router *gin.Engine, logger *zap.Logger, proxyURL string) {
	proxy, err := newReverseProxy(proxyURL, logger, "docs")
	if err != nil {
		logger.Warn("docs proxy unavailable, docs serving disabled", zap.String("proxy_url", proxyURL), zap.Error(err))
		return
	}

	handler := func(c *gin.Context) {
		proxy.ServeHTTP(c.Writer, c.Request)
	}
	router.Any("/docs", handler)
	router.Any("/docs/*filepath", handler)
}

func serveEmbeddedFile(c *gin.Context, source fs.FS, name string) {
	content, err := fs.ReadFile(source, name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = http.DetectContentType(content)
	}

	c.Data(http.StatusOK, contentType, content)
}

func registerSPA(router *gin.Engine, logger *zap.Logger, cfg cfgpkg.WebAssetsConfig) {
	switch normalizedAssetMode(cfg.Mode, assetModeEmbed) {
	case assetModeEmbed:
		distFS, err := staticassets.DefaultWebFS(cfg.Dir)
		if err != nil {
			logger.Warn("web assets not available, SPA serving disabled", zap.String("dir", cfg.Dir), zap.Error(err))
			return
		}
		registerSPAFS(router, distFS)
	case assetModeDir:
		distFS, err := staticassets.DiskFS(cfg.Dir)
		if err != nil {
			logger.Warn("web assets not available, SPA serving disabled", zap.String("dir", cfg.Dir), zap.Error(err))
			return
		}
		registerSPAFS(router, distFS)
	case assetModeProxy:
		registerSPAProxy(router, logger, cfg.ProxyURL)
	case assetModeDisabled:
		logger.Info("web serving disabled")
	default:
		logger.Warn("unknown web assets mode, SPA serving disabled", zap.String("mode", cfg.Mode))
	}
}

func registerSPAFS(router *gin.Engine, distFS fs.FS) {
	fileServer := http.FileServer(http.FS(distFS))

	router.NoRoute(func(c *gin.Context) {
		if isReservedPath(c.Request.URL.Path) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		// Try to serve the exact file first
		requestPath := c.Request.URL.Path
		f, err := fs.Stat(distFS, strings.TrimPrefix(requestPath, "/"))
		if err == nil && !f.IsDir() {
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}

		// SPA fallback: serve index.html for all other routes
		c.Request.URL.Path = "/"
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
}

func registerSPAProxy(router *gin.Engine, logger *zap.Logger, proxyURL string) {
	proxy, err := newReverseProxy(proxyURL, logger, "web")
	if err != nil {
		logger.Warn("web proxy unavailable, SPA serving disabled", zap.String("proxy_url", proxyURL), zap.Error(err))
		return
	}

	router.NoRoute(func(c *gin.Context) {
		if isReservedPath(c.Request.URL.Path) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		proxy.ServeHTTP(c.Writer, c.Request)
	})
}

func isReservedPath(requestPath string) bool {
	return strings.HasPrefix(requestPath, "/api/") ||
		strings.HasPrefix(requestPath, "/docs/") ||
		requestPath == "/docs" ||
		requestPath == "/healthz" ||
		requestPath == "/readyz"
}

func normalizedAssetMode(raw, fallback string) string {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		return fallback
	}
	return mode
}

func newReverseProxy(rawURL string, logger *zap.Logger, name string) (*httputil.ReverseProxy, error) {
	target, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, err
	}
	if target.Scheme == "" || target.Host == "" {
		return nil, url.InvalidHostError(rawURL)
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Warn("static asset proxy request failed", zap.String("asset", name), zap.Error(err))
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}
	return proxy, nil
}
