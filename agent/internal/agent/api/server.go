package api

import (
	"context"
	"errors"
	"fmt"
	contractresource "github.com/opensoha/soha/contracts/resource"
	resourceruntime "github.com/opensoha/soha/contracts/resource/runtime"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/agent/internal/agent/buildinfo"
	cfgpkg "github.com/opensoha/soha/agent/internal/agent/config"
	"github.com/opensoha/soha/agent/internal/agent/dockerruntime"
	"github.com/opensoha/soha/agent/internal/agent/environment"
	k8sagent "github.com/opensoha/soha/agent/internal/agent/kubernetes"
	runnerpkg "github.com/opensoha/soha/agent/internal/agent/runner"
	apiMiddleware "github.com/opensoha/soha/agent/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/agent/internal/api/response"
	"go.uber.org/zap"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type Server struct {
	volumeReader          *dockerruntime.VolumeReader
	nativeTerminals       *nativePodTerminals
	nativeDockerTerminals *nativePodTerminals
	personalMCPStdio      *nativePodTerminals
	sshSessions           *sshSessions
	httpServer            *http.Server
	personalEnvironment   *environment.DockerExecutor
	cleanupContext        context.Context
	cleanupCancel         context.CancelFunc
	logger                *zap.Logger
}

type RuntimeTaskController interface {
	ListActiveTasks() []runnerpkg.ActiveTask
	GetActiveTask(string) (runnerpkg.ActiveTask, bool)
	CancelActiveTask(string, string) bool
}

type RuntimeMetricsController interface {
	MetricsSnapshot() runnerpkg.MetricsSnapshot
}

func New(cfg cfgpkg.Config, logger *zap.Logger, client *k8sagent.Client, runtime RuntimeTaskController) *Server {
	if logger == nil {
		logger = zap.NewNop()
	}
	if cfg.App.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	if err := router.SetTrustedProxies(nil); err != nil {
		panic(fmt.Sprintf("disable trusted proxies: %v", err))
	}
	router.Use(apiMiddleware.RequestID())
	httpLogger := logger.Named("http")
	router.Use(apiMiddleware.RequestLogger(httpLogger))
	router.Use(gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, recovered any) {
		httpLogger.Error("http request panic recovered",
			zap.String("event", "http.request.panic"),
			zap.String("request_id", c.GetString("request_id")),
			zap.String("panic_type", fmt.Sprintf("%T", recovered)),
		)
		c.AbortWithStatus(http.StatusInternalServerError)
	}))
	auditSink := newActionAuditSink(cfg.Audit, logger)
	actions := newActionPolicy(cfg.Security, logger, auditSink)
	registerSystemRoutes(router, cfg, client, runtime)
	nativeTerminals := registerPlatformRoutes(router, cfg, client, actions)
	registerRuntimeRoutes(router, cfg, runtime, actions)
	sshSessions := registerSSHRoutes(router, cfg, actions)
	registerAgentToolRoutes(router, cfg, runtime)
	volumeReader := registerDockerRuntimeRoutes(router, cfg, logger, actions)
	nativeDockerTerminals := registerNativeDockerTerminalRoutes(router, cfg, actions)
	registerOutpostRoutes(router, cfg, runtime)
	personalMCPStdio := newNativePodTerminals()
	personalEnvironment := registerPersonalEnvironmentRoutes(router, cfg, logger, actions, personalMCPStdio)

	httpLogger.Info("agent server configured",
		zap.String("event", "agent.server.configured"),
		zap.String("addr", cfg.HTTP.Addr),
		zap.String("base_path", cfg.HTTP.BasePath),
		zap.String("cluster_id", cfg.Kubernetes.ID),
		zap.Bool("authentication_enabled", len(allowedAuthTokens(cfg.Auth.BearerToken)) > 0),
		zap.Int("allowed_actions", len(actions.allowed)),
		zap.String("version", buildinfo.Current().Version),
		zap.String("commit", buildinfo.Current().Commit),
	)

	cleanupContext, cleanupCancel := context.WithCancel(context.Background())
	return &Server{volumeReader: volumeReader, nativeTerminals: nativeTerminals, nativeDockerTerminals: nativeDockerTerminals, personalMCPStdio: personalMCPStdio, sshSessions: sshSessions, personalEnvironment: personalEnvironment, cleanupContext: cleanupContext, cleanupCancel: cleanupCancel, logger: logger, httpServer: &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
	}}
}

func registerPlatformRoutes(router *gin.Engine, cfg cfgpkg.Config, client *k8sagent.Client, actions actionPolicy) *nativePodTerminals {
	if client == nil {
		return nil
	}
	platform := router.Group(fmt.Sprintf("%s/platform", cfg.HTTP.BasePath))
	platform.Use(authMiddleware(cfg.Auth.BearerToken))
	registerResourceGraphCapabilityRoute(platform)
	registerResourceYAMLRoutes(platform, client, actions)
	registerResourceCreationRoutes(platform, client, actions)
	registerCustomResourceRoutes(platform, client, actions)
	registerCustomResourceGraphRoutes(platform, client, actions, cfg.Auth.BearerToken)
	registerPrometheusRoutes(platform, cfg.Prometheus)
	origins := newWebSocketOriginPolicy(cfg.HTTP.AllowedOrigins, cfg.Auth.BearerToken)
	registerPortForwardRoutes(
		platform,
		newPortForwardRegistry(cfg.Kubernetes, kubernetesPortForwardStarter(client)),
		actions,
		origins,
	)
	registerHelmRoutes(platform, client, actions)
	registerHelmDeliveryRoutes(platform, client, cfg.Kubernetes.ID)
	// Dedicated UID-bound routes make older Agents reject sessions they cannot fence.
	platform.GET("/workloads/pods/:name/uid-bound/logs", podLogSnapshotHandler(client, true))
	registerPodStreamRoutes(platform, client)
	registerAggregateLogRoutes(platform, client)
	registerPodTerminalRoutes(platform, client, actions, origins)
	registerPlatformInventoryRoutes(platform, client)
	registerPlatformWorkloadRoutes(platform, client, actions)
	registerPlatformConfigurationRoutes(platform, client, actions)
	registerConfigurationObjectReadRoutes(platform.Group("/ownership-v2"), client)
	registerBasicResourceMutationRoutes(platform.Group("/ownership-v2"), client, actions)
	registerConditionalResourceRoutes(platform, client, actions)
	registerConditionalWorkloadActionRoute(platform, client, actions)
	registerPodExecutionRoutes(platform, client, actions)
	nativeTerminals := newNativePodTerminals()
	registerNativePodTerminalRoute(platform, client, actions, nativeTerminals)
	registerPlatformRBACRoutes(platform, client)
	registerPlatformNetworkRoutes(platform, client)
	registerTopologySnapshotRoute(platform, client, actions)
	registerPlatformStorageRoutes(platform, client)
	registerPlatformHelmReadRoutes(platform, client)
	registerPlatformWorkloadMutationRoutes(platform, client, actions)
	// A distinct route family makes old Agents fail closed during rolling upgrades.
	for _, version := range []string{"/ownership-v1", "/ownership-v2"} {
		owned := platform.Group(version)
		registerResourceYAMLRoutes(owned, client, actions)
		registerCustomResourceRoutes(owned, client, actions)
		registerPlatformWorkloadRoutes(owned, client, actions)
		registerPlatformWorkloadMutationRoutes(owned, client, actions)
	}
	registerManifestRolloutRoutes(platform.Group("/ownership-v2"), client, cfg.Kubernetes.ID, actions)
	registerObservedWorkloadDeleteRoute(platform.Group("/ownership-v2"), client, actions)
	registerObservedResourceYAMLRoutes(platform.Group("/ownership-v2"), client, actions)
	return nativeTerminals
}

func (s *Server) Run() error {
	if s.volumeReader.Enabled() {
		done := make(chan struct{})
		go func() { defer close(done); s.collectVolumeReaders() }()
		defer func() { s.cleanupCancel(); <-done }()
	}
	if s.personalEnvironment != nil {
		done := make(chan struct{})
		go func() {
			defer close(done)
			s.collectPersonalEnvironments()
		}()
		defer func() { s.cleanupCancel(); <-done }()
	}
	err := s.httpServer.ListenAndServe()
	if err == nil || err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.sshSessions != nil {
		s.sshSessions.Close()
	}
	s.cleanupCancel()
	var terminalErr error
	if s.nativeTerminals != nil {
		terminalErr = s.nativeTerminals.shutdown(ctx)
	}
	if s.nativeDockerTerminals != nil {
		terminalErr = errors.Join(terminalErr, s.nativeDockerTerminals.shutdown(ctx))
	}
	if s.personalMCPStdio != nil {
		terminalErr = errors.Join(terminalErr, s.personalMCPStdio.shutdown(ctx))
	}
	return errors.Join(terminalErr, s.httpServer.Shutdown(ctx))
}

func (s *Server) collectPersonalEnvironments() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if _, err := s.personalEnvironment.GC(s.cleanupContext, time.Now().UTC()); err != nil && s.cleanupContext.Err() == nil {
			s.logger.Warn("personal environment cleanup could not confirm release")
		}
		select {
		case <-s.cleanupContext.Done():
			return
		case <-ticker.C:
		}
	}
}

func writeError(c *gin.Context, err error) {
	var failure *contractresource.NodeDrainError
	if errors.As(err, &failure) && contractresource.IsNodeDrainErrorCode(failure.Code) {
		apiresponse.Error(c, http.StatusConflict, failure.Code, failure.Error())
		return
	}
	if errors.Is(err, resourceruntime.ErrResourceOwnership) {
		apiresponse.Error(c, http.StatusConflict, "resource_ownership_conflict", "resource is externally managed or being deleted; use its delivery owner")
		return
	}
	if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
		apiresponse.Error(c, 403, "forbidden", "Agent ServiceAccount lacks permission for this resource operation")
		return
	}
	if apierrors.IsNotFound(err) {
		apiresponse.Error(c, 404, "not_found", "resource not found")
		return
	}
	if apierrors.IsBadRequest(err) || apierrors.IsInvalid(err) {
		apiresponse.Error(c, 400, "invalid_argument", "invalid resource input or immutable resource")
		return
	}
	if apierrors.IsConflict(err) {
		apiresponse.Error(c, 409, "conflict", "resource changed; refresh and try again")
		return
	}
	apiresponse.Error(c, http.StatusBadGateway, "cluster_unavailable", "cluster request failed")
}

func parseLimit(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 {
		return fallback
	}
	return limit
}
