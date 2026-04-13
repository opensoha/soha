package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/kubecrux/kubecrux/internal/agent/config"
	k8sagent "github.com/kubecrux/kubecrux/internal/agent/kubernetes"
	apiMiddleware "github.com/kubecrux/kubecrux/internal/api/middleware"
	apiresponse "github.com/kubecrux/kubecrux/internal/api/response"
	"go.uber.org/zap"
)

type Server struct {
	httpServer *http.Server
}

type restartDeploymentRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type scaleDeploymentRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Replicas  int32  `json:"replicas"`
}

type updateDeploymentImageRequest struct {
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	ContainerName string `json:"containerName,omitempty"`
	Image         string `json:"image"`
}

type execPodRequest struct {
	Command        string `json:"command"`
	Container      string `json:"container,omitempty"`
	TimeoutSeconds int64  `json:"timeoutSeconds,omitempty"`
}

type rollbackDeploymentRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Revision  string `json:"revision"`
}

func New(cfg cfgpkg.Config, logger *zap.Logger, client *k8sagent.Client) *Server {
	if cfg.App.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(apiMiddleware.RequestID())

	router.GET("/healthz", func(c *gin.Context) {
		apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET(fmt.Sprintf("%s/healthz", cfg.HTTP.BasePath), func(c *gin.Context) {
		apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
	})

	platform := router.Group(fmt.Sprintf("%s/platform", cfg.HTTP.BasePath))
	platform.Use(authMiddleware(cfg.Auth.BearerToken))
	{
		platform.GET("/summary", func(c *gin.Context) {
			apiresponse.Item(c, http.StatusOK, client.Summary(c.Request.Context()))
		})
		platform.GET("/namespaces", func(c *gin.Context) {
			items, err := client.ListNamespaces(c.Request.Context())
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Items(c, http.StatusOK, items)
		})
		platform.GET("/infrastructure/nodes", func(c *gin.Context) {
			items, err := client.ListNodes(c.Request.Context())
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Items(c, http.StatusOK, items)
		})
		platform.GET("/infrastructure/nodes/:name/detail", func(c *gin.Context) {
			item, err := client.GetNodeDetail(c.Request.Context(), c.Param("name"))
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Item(c, http.StatusOK, item)
		})
		platform.GET("/workloads/pods", func(c *gin.Context) {
			namespace := c.DefaultQuery("namespace", "default")
			items, err := client.ListPods(c.Request.Context(), namespace)
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Items(c, http.StatusOK, items)
		})
		platform.GET("/workloads/pods/:name/detail", func(c *gin.Context) {
			namespace := c.DefaultQuery("namespace", "default")
			item, err := client.GetPodDetail(c.Request.Context(), namespace, c.Param("name"))
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Item(c, http.StatusOK, item)
		})
		platform.GET("/workloads/pods/:name/logs", func(c *gin.Context) {
			namespace := c.DefaultQuery("namespace", "default")
			tailLines := int64(parseLimit(c.Query("tailLines"), 200))
			sinceSeconds := int64(parseLimit(c.Query("sinceSeconds"), 0))
			previous := c.Query("previous") == "true"
			item, err := client.GetPodLogs(c.Request.Context(), namespace, c.Param("name"), c.Query("container"), tailLines, sinceSeconds, previous)
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Item(c, http.StatusOK, item)
		})
		platform.GET("/workloads/pods/:name/yaml", func(c *gin.Context) {
			namespace := c.DefaultQuery("namespace", "default")
			item, err := client.GetPodYAML(c.Request.Context(), namespace, c.Param("name"))
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Item(c, http.StatusOK, item)
		})
		platform.POST("/workloads/pods/:name/exec", func(c *gin.Context) {
			namespace := c.DefaultQuery("namespace", "default")
			var req execPodRequest
			if err := c.ShouldBindJSON(&req); err != nil || req.Command == "" {
				apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "command is required")
				return
			}
			item, err := client.ExecPod(c.Request.Context(), namespace, c.Param("name"), req.Container, req.Command, req.TimeoutSeconds)
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Item(c, http.StatusOK, item)
		})
		platform.GET("/workloads/deployments", func(c *gin.Context) {
			namespace := c.DefaultQuery("namespace", "default")
			items, err := client.ListDeployments(c.Request.Context(), namespace)
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Items(c, http.StatusOK, items)
		})
		platform.GET("/workloads/deployments/:name/detail", func(c *gin.Context) {
			namespace := c.DefaultQuery("namespace", "default")
			item, err := client.GetDeploymentDetail(c.Request.Context(), namespace, c.Param("name"))
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Item(c, http.StatusOK, item)
		})
		platform.GET("/workloads/deployments/:name/yaml", func(c *gin.Context) {
			namespace := c.DefaultQuery("namespace", "default")
			item, err := client.GetDeploymentYAML(c.Request.Context(), namespace, c.Param("name"))
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Item(c, http.StatusOK, item)
		})
		platform.GET("/workloads/deployments/:name/rollout-status", func(c *gin.Context) {
			namespace := c.DefaultQuery("namespace", "default")
			item, err := client.GetDeploymentRolloutStatus(c.Request.Context(), namespace, c.Param("name"))
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Item(c, http.StatusOK, item)
		})
		platform.GET("/workloads/deployments/:name/rollouts", func(c *gin.Context) {
			namespace := c.DefaultQuery("namespace", "default")
			items, err := client.ListDeploymentRolloutHistory(c.Request.Context(), namespace, c.Param("name"))
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Items(c, http.StatusOK, items)
		})
		platform.GET("/workloads/statefulsets", func(c *gin.Context) {
			namespace := c.Query("namespace")
			items, err := client.ListStatefulSets(c.Request.Context(), namespace)
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Items(c, http.StatusOK, items)
		})
		platform.GET("/workloads/statefulsets/:name/detail", func(c *gin.Context) {
			namespace := c.Query("namespace")
			item, err := client.GetStatefulSetDetail(c.Request.Context(), namespace, c.Param("name"))
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Item(c, http.StatusOK, item)
		})
		platform.GET("/workloads/statefulsets/:name/yaml", func(c *gin.Context) {
			namespace := c.Query("namespace")
			item, err := client.GetStatefulSetYAML(c.Request.Context(), namespace, c.Param("name"))
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Item(c, http.StatusOK, item)
		})
		platform.GET("/workloads/daemonsets", func(c *gin.Context) {
			namespace := c.Query("namespace")
			items, err := client.ListDaemonSets(c.Request.Context(), namespace)
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Items(c, http.StatusOK, items)
		})
		platform.GET("/workloads/daemonsets/:name/detail", func(c *gin.Context) {
			namespace := c.Query("namespace")
			item, err := client.GetDaemonSetDetail(c.Request.Context(), namespace, c.Param("name"))
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Item(c, http.StatusOK, item)
		})
		platform.GET("/workloads/daemonsets/:name/yaml", func(c *gin.Context) {
			namespace := c.Query("namespace")
			item, err := client.GetDaemonSetYAML(c.Request.Context(), namespace, c.Param("name"))
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Item(c, http.StatusOK, item)
		})
		platform.GET("/workloads/jobs", func(c *gin.Context) {
			namespace := c.Query("namespace")
			items, err := client.ListJobs(c.Request.Context(), namespace)
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Items(c, http.StatusOK, items)
		})
		platform.GET("/workloads/jobs/:name/detail", func(c *gin.Context) {
			namespace := c.Query("namespace")
			item, err := client.GetJobDetail(c.Request.Context(), namespace, c.Param("name"))
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Item(c, http.StatusOK, item)
		})
		platform.GET("/workloads/jobs/:name/yaml", func(c *gin.Context) {
			namespace := c.Query("namespace")
			item, err := client.GetJobYAML(c.Request.Context(), namespace, c.Param("name"))
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Item(c, http.StatusOK, item)
		})
		platform.GET("/workloads/cronjobs", func(c *gin.Context) {
			namespace := c.Query("namespace")
			items, err := client.ListCronJobs(c.Request.Context(), namespace)
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Items(c, http.StatusOK, items)
		})
		platform.GET("/workloads/cronjobs/:name/detail", func(c *gin.Context) {
			namespace := c.Query("namespace")
			item, err := client.GetCronJobDetail(c.Request.Context(), namespace, c.Param("name"))
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Item(c, http.StatusOK, item)
		})
		platform.GET("/workloads/cronjobs/:name/yaml", func(c *gin.Context) {
			namespace := c.Query("namespace")
			item, err := client.GetCronJobYAML(c.Request.Context(), namespace, c.Param("name"))
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Item(c, http.StatusOK, item)
		})
		platform.GET("/network/services", func(c *gin.Context) {
			namespace := c.Query("namespace")
			items, err := client.ListServices(c.Request.Context(), namespace)
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Items(c, http.StatusOK, items)
		})
		platform.GET("/network/ingresses", func(c *gin.Context) {
			namespace := c.Query("namespace")
			items, err := client.ListIngresses(c.Request.Context(), namespace)
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Items(c, http.StatusOK, items)
		})
		platform.GET("/network/gateways", func(c *gin.Context) {
			namespace := c.Query("namespace")
			items, err := client.ListGateways(c.Request.Context(), namespace)
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Items(c, http.StatusOK, items)
		})
		platform.GET("/network/http-routes", func(c *gin.Context) {
			namespace := c.Query("namespace")
			items, err := client.ListHTTPRoutes(c.Request.Context(), namespace)
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Items(c, http.StatusOK, items)
		})
		platform.GET("/storage/persistentvolumeclaims", func(c *gin.Context) {
			namespace := c.Query("namespace")
			items, err := client.ListPersistentVolumeClaims(c.Request.Context(), namespace)
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Items(c, http.StatusOK, items)
		})
		platform.GET("/storage/persistentvolumes", func(c *gin.Context) {
			items, err := client.ListPersistentVolumes(c.Request.Context())
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Items(c, http.StatusOK, items)
		})
		platform.GET("/storage/storageclasses", func(c *gin.Context) {
			items, err := client.ListStorageClasses(c.Request.Context())
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Items(c, http.StatusOK, items)
		})
		platform.GET("/extensions/crds", func(c *gin.Context) {
			items, err := client.ListCRDs(c.Request.Context())
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Items(c, http.StatusOK, items)
		})
		platform.GET("/helm/releases", func(c *gin.Context) {
			namespace := c.Query("namespace")
			items, err := client.ListHelmReleases(c.Request.Context(), namespace)
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Items(c, http.StatusOK, items)
		})
		platform.GET("/events", func(c *gin.Context) {
			namespace := c.Query("namespace")
			limit := parseLimit(c.Query("limit"), 20)
			items, err := client.ListClusterEvents(c.Request.Context(), namespace, limit)
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Items(c, http.StatusOK, items)
		})
		platform.POST("/actions/deployments/restart", func(c *gin.Context) {
			var req restartDeploymentRequest
			if err := c.ShouldBindJSON(&req); err != nil || req.Namespace == "" || req.Name == "" {
				apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "namespace and name are required")
				return
			}
			if err := client.RestartDeployment(c.Request.Context(), req.Namespace, req.Name); err != nil {
				writeError(c, err)
				return
			}
			apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
		})
		platform.POST("/actions/deployments/scale", func(c *gin.Context) {
			var req scaleDeploymentRequest
			if err := c.ShouldBindJSON(&req); err != nil || req.Namespace == "" || req.Name == "" {
				apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "namespace, name, and replicas are required")
				return
			}
			if req.Replicas < 0 {
				apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "replicas must be greater than or equal to zero")
				return
			}
			if err := client.ScaleDeployment(c.Request.Context(), req.Namespace, req.Name, req.Replicas); err != nil {
				writeError(c, err)
				return
			}
			apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
		})
		platform.POST("/actions/deployments/image", func(c *gin.Context) {
			var req updateDeploymentImageRequest
			if err := c.ShouldBindJSON(&req); err != nil || req.Namespace == "" || req.Name == "" || req.Image == "" {
				apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "namespace, name, and image are required")
				return
			}
			containerName, previousImage, err := client.UpdateDeploymentImage(c.Request.Context(), req.Namespace, req.Name, req.ContainerName, req.Image)
			if err != nil {
				writeError(c, err)
				return
			}
			apiresponse.Item(c, http.StatusOK, gin.H{
				"containerName": containerName,
				"previousImage": previousImage,
			})
		})
		platform.POST("/actions/deployments/rollback", func(c *gin.Context) {
			var req rollbackDeploymentRequest
			if err := c.ShouldBindJSON(&req); err != nil || req.Namespace == "" || req.Name == "" || req.Revision == "" {
				apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "namespace, name, and revision are required")
				return
			}
			if err := client.RollbackDeployment(c.Request.Context(), req.Namespace, req.Name, req.Revision); err != nil {
				writeError(c, err)
				return
			}
			apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
		})
	}

	logger.Info("agent server configured",
		zap.String("addr", cfg.HTTP.Addr),
		zap.String("base_path", cfg.HTTP.BasePath),
		zap.String("cluster_id", cfg.Kubernetes.ID),
	)

	return &Server{httpServer: &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
	}}
}

func (s *Server) Run() error {
	err := s.httpServer.ListenAndServe()
	if err == nil || err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func authMiddleware(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token == "" {
			c.Next()
			return
		}
		if c.GetHeader("Authorization") != fmt.Sprintf("Bearer %s", token) {
			apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid agent token")
			c.Abort()
			return
		}
		c.Next()
	}
}

func writeError(c *gin.Context, err error) {
	apiresponse.Error(c, http.StatusBadGateway, "cluster_unavailable", fmt.Sprintf("request failed: %v", err))
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
