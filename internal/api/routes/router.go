package routes

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	docsembed "github.com/soha/soha/docs"
	apiHandlers "github.com/soha/soha/internal/api/handlers"
	apiMiddleware "github.com/soha/soha/internal/api/middleware"
	cfgpkg "github.com/soha/soha/internal/infrastructure/config"
	swaggerinfra "github.com/soha/soha/internal/infrastructure/swagger"
	webembed "github.com/soha/soha/web"
	"go.uber.org/zap"
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
	Virtualization *apiHandlers.VirtualizationHandler
	Docker         *apiHandlers.DockerHandler
	Access         *apiHandlers.AccessHandler
	ScopeGrants    *apiHandlers.ScopeGrantHandler
	Menu           *apiHandlers.MenuHandler
	Settings       *apiHandlers.SettingsHandler
	Auth           *apiHandlers.AuthHandler
	Authn          apiMiddleware.AccessTokenParser
}

func New(cfg cfgpkg.Config, logger *zap.Logger, deps Dependencies) *http.Server {
	if cfg.App.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(apiMiddleware.RequestID())
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

	// Serve uploaded branding assets
	router.Static("/branding-assets", "data/branding")

	// Serve embedded documentation site
	registerDocs(router, logger)

	// Serve embedded frontend SPA assets
	registerSPA(router, logger)

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
	}
}

func registerDocs(router *gin.Engine, logger *zap.Logger) {
	buildFS, err := docsembed.StaticFS()
	if err != nil {
		logger.Warn("docs assets not available, docs serving disabled", zap.Error(err))
		return
	}

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

func registerSPA(router *gin.Engine, logger *zap.Logger) {
	distFS, err := webembed.StaticFS()
	if err != nil {
		logger.Warn("web assets not available, SPA serving disabled", zap.Error(err))
		return
	}

	fileServer := http.FileServer(http.FS(distFS))

	router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		// Let API and health routes fall through to 404
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/docs/") || path == "/docs" || path == "/healthz" || path == "/readyz" {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		// Try to serve the exact file first
		f, err := fs.Stat(distFS, strings.TrimPrefix(path, "/"))
		if err == nil && !f.IsDir() {
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}

		// SPA fallback: serve index.html for all other routes
		c.Request.URL.Path = "/"
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
}
