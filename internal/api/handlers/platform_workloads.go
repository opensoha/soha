package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (h *PlatformHandler) ListPods(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListPods(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) GetWorkloadOverview(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetWorkloadOverview(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) GetPodDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.resources.GetPodDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) DeletePod(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	if err := h.resources.DeletePod(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
func (h *PlatformHandler) GetPodLogs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	tailLines := int64(parseLimit(c.Query("tailLines"), 200))
	sinceSeconds := int64(parseLimit(c.Query("sinceSeconds"), 0))
	previous := strings.EqualFold(c.Query("previous"), "true")
	item, err := h.resources.GetPodLogs(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName"), c.Query("container"), tailLines, sinceSeconds, previous)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) GetPodYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.resources.GetPodYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) ApplyPodYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid pod yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.resources.ApplyPodYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) GetPodMetrics(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	rangeMinutes := parseLimit(c.Query("rangeMinutes"), 60)
	stepSeconds := parseLimit(c.Query("stepSeconds"), 60)
	item, err := h.resources.GetPodMetrics(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName"), rangeMinutes, stepSeconds)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) ExecPod(c *gin.Context) {
	var req dto.ExecPodRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Command == "" {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "command is required")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.resources.ExecPod(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName"), req.Container, req.Command, req.TimeoutSeconds)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) ListDeployments(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListDeployments(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) GetDeploymentDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.resources.GetDeploymentDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("deploymentName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) GetDeploymentYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.resources.GetDeploymentYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("deploymentName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) ApplyDeploymentYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid deployment yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.resources.ApplyDeploymentYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("deploymentName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) GetDeploymentMetrics(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	rangeMinutes := parseLimit(c.Query("rangeMinutes"), 60)
	stepSeconds := parseLimit(c.Query("stepSeconds"), 60)
	item, err := h.resources.GetDeploymentMetrics(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("deploymentName"), rangeMinutes, stepSeconds)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) GetDeploymentRolloutStatus(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.resources.GetDeploymentRolloutStatus(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("deploymentName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) ListDeploymentRollouts(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	items, err := h.resources.ListDeploymentRolloutHistory(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("deploymentName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListStatefulSets(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListStatefulSets(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) GetStatefulSetDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetStatefulSetDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("statefulSetName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) GetStatefulSetYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetStatefulSetYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("statefulSetName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) ApplyStatefulSetYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid statefulset yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.ApplyStatefulSetYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("statefulSetName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) ListDaemonSets(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListDaemonSets(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) GetDaemonSetDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetDaemonSetDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("daemonSetName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) GetDaemonSetYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetDaemonSetYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("daemonSetName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) ApplyDaemonSetYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid daemonset yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.ApplyDaemonSetYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("daemonSetName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) ListJobs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListJobs(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) GetJobDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetJobDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("jobName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) GetJobYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetJobYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("jobName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) ApplyJobYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid job yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.ApplyJobYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("jobName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) ListCronJobs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListCronJobs(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) GetCronJobDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetCronJobDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("cronJobName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) GetCronJobYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetCronJobYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("cronJobName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) ApplyCronJobYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid cronjob yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.ApplyCronJobYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("cronJobName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) ListReplicaSets(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListReplicaSets(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListHorizontalPodAutoscalers(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListHorizontalPodAutoscalers(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListPodDisruptionBudgets(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListPodDisruptionBudgets(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

// RegisterWorkloadDeleteRoutes wires DELETE endpoints for built-in workloads using
// the existing route param names (`:deploymentName`, etc.) to avoid route conflicts
// with other GET/PUT entries already registered in router.go.
func (h *PlatformHandler) RegisterWorkloadDeleteRoutes(group gin.IRoutes) {
	entries := []struct {
		path  string
		param string
		kind  string
	}{
		{"/clusters/:clusterID/workloads/deployments/:deploymentName", "deploymentName", "Deployment"},
		{"/clusters/:clusterID/workloads/statefulsets/:statefulSetName", "statefulSetName", "StatefulSet"},
		{"/clusters/:clusterID/workloads/daemonsets/:daemonSetName", "daemonSetName", "DaemonSet"},
		{"/clusters/:clusterID/workloads/jobs/:jobName", "jobName", "Job"},
		{"/clusters/:clusterID/workloads/cronjobs/:cronJobName", "cronJobName", "CronJob"},
	}
	for _, entry := range entries {
		kind := entry.kind
		paramName := entry.param
		group.DELETE(entry.path, func(c *gin.Context) {
			principal := apiMiddleware.PrincipalFromContext(c)
			namespace := c.Query("namespace")
			if err := h.resources.DeleteResourceByKind(c.Request.Context(), principal, c.Param("clusterID"), namespace, kind, c.Param(paramName)); err != nil {
				writeError(c, err)
				return
			}
			c.Status(http.StatusNoContent)
		})
	}
}
func (h *PlatformHandler) RestartDeployment(c *gin.Context) {
	var req dto.RestartDeploymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid restart deployment payload")
		return
	}
	if req.Namespace == "" || req.Name == "" {
		writeError(c, fmt.Errorf("%w: namespace and name are required", apperrors.ErrInvalidArgument))
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.resources.RestartDeployment(c.Request.Context(), principal, c.Param("clusterID"), req.Namespace, req.Name); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}
func (h *PlatformHandler) ScaleDeployment(c *gin.Context) {
	var req dto.ScaleDeploymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid scale deployment payload")
		return
	}
	if req.Namespace == "" || req.Name == "" {
		writeError(c, fmt.Errorf("%w: namespace and name are required", apperrors.ErrInvalidArgument))
		return
	}
	if req.Replicas < 0 {
		writeError(c, fmt.Errorf("%w: replicas must be greater than or equal to zero", apperrors.ErrInvalidArgument))
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.resources.ScaleDeployment(c.Request.Context(), principal, c.Param("clusterID"), req.Namespace, req.Name, req.Replicas); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}
func (h *PlatformHandler) RollbackDeployment(c *gin.Context) {
	var req dto.RollbackDeploymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid rollback deployment payload")
		return
	}
	if req.Namespace == "" || req.Name == "" || req.Revision == "" {
		writeError(c, fmt.Errorf("%w: namespace, name, and revision are required", apperrors.ErrInvalidArgument))
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.resources.RollbackDeployment(c.Request.Context(), principal, c.Param("clusterID"), req.Namespace, req.Name, req.Revision)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
