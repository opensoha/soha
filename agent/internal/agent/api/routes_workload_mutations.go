package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
)

type restartDeploymentRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type scaleDeploymentRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Replicas  int32  `json:"replicas"`
}

type restartStatefulSetRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type scaleStatefulSetRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Replicas  int32  `json:"replicas"`
}

type restartDaemonSetRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type updateDeploymentImageRequest struct {
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	ContainerName string `json:"containerName,omitempty"`
	Image         string `json:"image"`
}

type rollbackDeploymentRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Revision  string `json:"revision"`
}

func registerPlatformWorkloadMutationRoutes(platform *gin.RouterGroup, client *k8sagent.Client, actions actionPolicy) {
	platform.POST("/actions/deployments/restart", actions.Require(actionPlatformDeploymentRestart), func(c *gin.Context) {
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
	platform.POST("/actions/deployments/scale", actions.Require(actionPlatformDeploymentScale), func(c *gin.Context) {
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
	platform.POST(
		"/actions/statefulsets/restart",
		actions.Require(actionPlatformStatefulSetRestart),
		func(c *gin.Context) {
			var req restartStatefulSetRequest
			if err := c.ShouldBindJSON(&req); err != nil || req.Namespace == "" || req.Name == "" {
				apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "namespace and name are required")
				return
			}
			if err := client.RestartStatefulSet(c.Request.Context(), req.Namespace, req.Name); err != nil {
				writeError(c, err)
				return
			}
			apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
		},
	)
	platform.POST("/actions/statefulsets/scale", actions.Require(actionPlatformStatefulSetScale), func(c *gin.Context) {
		var req scaleStatefulSetRequest
		if err := c.ShouldBindJSON(&req); err != nil || req.Namespace == "" || req.Name == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "namespace, name, and replicas are required")
			return
		}
		if req.Replicas < 0 {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "replicas must be greater than or equal to zero")
			return
		}
		if err := client.ScaleStatefulSet(c.Request.Context(), req.Namespace, req.Name, req.Replicas); err != nil {
			writeError(c, err)
			return
		}
		apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
	})
	platform.POST("/actions/daemonsets/restart", actions.Require(actionPlatformDaemonSetRestart), func(c *gin.Context) {
		var req restartDaemonSetRequest
		if err := c.ShouldBindJSON(&req); err != nil || req.Namespace == "" || req.Name == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "namespace and name are required")
			return
		}
		if err := client.RestartDaemonSet(c.Request.Context(), req.Namespace, req.Name); err != nil {
			writeError(c, err)
			return
		}
		apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
	})
	platform.POST("/actions/deployments/image", actions.Require(actionPlatformDeploymentImage), func(c *gin.Context) {
		var req updateDeploymentImageRequest
		if err := c.ShouldBindJSON(&req); err != nil || req.Namespace == "" || req.Name == "" || req.Image == "" {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "namespace, name, and image are required")
			return
		}
		containerName, previousImage, err := client.UpdateDeploymentImage(
			c.Request.Context(),
			req.Namespace,
			req.Name,
			req.ContainerName,
			req.Image,
		)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, gin.H{
			"containerName": containerName,
			"previousImage": previousImage,
		})
	})
	platform.POST(
		"/actions/deployments/rollback",
		actions.Require(actionPlatformDeploymentRollback),
		func(c *gin.Context) {
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
		},
	)
}
