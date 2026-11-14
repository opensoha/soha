package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	k8sagent "github.com/opensoha/soha/agent/internal/agent/kubernetes"
	apiresponse "github.com/opensoha/soha/agent/internal/api/response"
)

type execPodRequest struct {
	Command        string `json:"command"`
	Container      string `json:"container,omitempty"`
	TimeoutSeconds int64  `json:"timeoutSeconds,omitempty"`
}

func registerPlatformWorkloadRoutes(platform *gin.RouterGroup, client *k8sagent.Client, actions actionPolicy) {
	registerPlatformPodRoutes(platform, client, actions)
	registerPlatformDeploymentRoutes(platform, client)
	registerPlatformControllerRoutes(platform, client)

}
func registerPlatformPodRoutes(platform *gin.RouterGroup, client *k8sagent.Client, actions actionPolicy) {
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
		c.Header("Cache-Control", "no-store")
		namespace := c.DefaultQuery("namespace", "default")
		item, err := client.GetPodDetail(c.Request.Context(), namespace, c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/workloads/pods/:name/logs", podLogSnapshotHandler(client, false))
	platform.GET("/workloads/pods/:name/yaml", func(c *gin.Context) {
		namespace := c.DefaultQuery("namespace", "default")
		item, err := client.GetPodYAML(c.Request.Context(), namespace, c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.POST("/workloads/pods/:name/exec", actions.Require(actionPlatformPodsExec), func(c *gin.Context) {
		namespace := c.DefaultQuery("namespace", "default")
		var req execPodRequest
		if err := c.ShouldBindJSON(&req); err != nil || req.Command == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "command is required")
			return
		}
		item, err := client.ExecPod(
			c.Request.Context(),
			namespace,
			c.Param("name"),
			req.Container,
			req.Command,
			req.TimeoutSeconds,
		)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
}

func podLogSnapshotHandler(client *k8sagent.Client, requireUID bool) gin.HandlerFunc {
	return podSessionHandler(requireUID, func(c *gin.Context, expectedUID string) {
		c.Header("Cache-Control", "no-store")
		namespace := c.DefaultQuery("namespace", "default")
		tailLines := int64(parseLimit(c.Query("tailLines"), 200))
		sinceSeconds := int64(parseLimit(c.Query("sinceSeconds"), 0))
		previous := c.Query("previous") == "true"
		item, err := client.GetPodLogs(
			c.Request.Context(),
			namespace,
			c.Param("name"),
			c.Query("container"),
			tailLines,
			sinceSeconds,
			previous,
			expectedUID,
		)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
}

func registerPlatformDeploymentRoutes(platform *gin.RouterGroup, client *k8sagent.Client) {
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
}
func registerPlatformControllerRoutes(platform *gin.RouterGroup, client *k8sagent.Client) {
	registerPlatformStatefulSetHistoryRoutes(platform, client)
	registerPlatformDaemonSetHistoryRoutes(platform, client)
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
	platform.GET("/workloads/replicasets", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListReplicaSets(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/workloads/replicasets/:name/detail", func(c *gin.Context) {
		item, err := client.GetReplicaSetDetail(c.Request.Context(), c.Query("namespace"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/workloads/replicasets/:name/yaml", func(c *gin.Context) {
		item, err := client.GetResourceYAML(c.Request.Context(), c.Query("namespace"), "ReplicaSet", c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
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
	platform.GET("/workloads/replicationcontrollers", func(c *gin.Context) {
		namespace := c.Query("namespace")
		items, err := client.ListReplicationControllers(c.Request.Context(), namespace)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	})
	platform.GET("/workloads/replicationcontrollers/:name/detail", func(c *gin.Context) {
		item, err := client.GetReplicationControllerDetail(c.Request.Context(), c.Query("namespace"), c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
	platform.GET("/workloads/replicationcontrollers/:name/yaml", func(c *gin.Context) {
		item, err := client.GetResourceYAML(c.Request.Context(), c.Query("namespace"), "ReplicationController", c.Param("name"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	})
}
