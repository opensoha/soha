package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (h *podResourceHandler) ListPods(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.reader.ListPods(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *podResourceHandler) GetWorkloadOverview(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.reader.GetWorkloadOverview(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *podResourceHandler) GetPodDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.reader.GetPodDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *podResourceHandler) DeletePod(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	if err := h.editor.DeletePod(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
func (h *podResourceHandler) GetPodLogs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	tailLines := int64(parseLimit(c.Query("tailLines"), 200))
	sinceSeconds := int64(parseLimit(c.Query("sinceSeconds"), 0))
	previous := strings.EqualFold(c.Query("previous"), "true")
	item, err := h.reader.GetPodLogs(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName"), c.Query("container"), tailLines, sinceSeconds, previous)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *podResourceHandler) GetPodYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.editor.GetPodYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *podResourceHandler) ApplyPodYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid pod yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.editor.ApplyPodYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *podResourceHandler) GetPodMetrics(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	rangeMinutes := parseLimit(c.Query("rangeMinutes"), 60)
	stepSeconds := parseLimit(c.Query("stepSeconds"), 60)
	item, err := h.diagnostics.GetPodMetrics(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName"), rangeMinutes, stepSeconds)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *podResourceHandler) ExecPod(c *gin.Context) {
	var req dto.ExecPodRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Command == "" {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "command is required")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.diagnostics.ExecPod(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName"), req.Container, req.Command, req.TimeoutSeconds)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *deploymentResourceHandler) ListDeployments(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.reader.ListDeployments(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *deploymentResourceHandler) GetDeploymentDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.reader.GetDeploymentDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("deploymentName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *deploymentResourceHandler) GetDeploymentYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.reader.GetDeploymentYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("deploymentName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *deploymentResourceHandler) ApplyDeploymentYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid deployment yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.editor.ApplyDeploymentYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("deploymentName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *deploymentResourceHandler) GetDeploymentMetrics(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	rangeMinutes := parseLimit(c.Query("rangeMinutes"), 60)
	stepSeconds := parseLimit(c.Query("stepSeconds"), 60)
	item, err := h.reader.GetDeploymentMetrics(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("deploymentName"), rangeMinutes, stepSeconds)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *deploymentResourceHandler) GetDeploymentRolloutStatus(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.reader.GetDeploymentRolloutStatus(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("deploymentName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *deploymentResourceHandler) ListDeploymentRollouts(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	items, err := h.editor.ListDeploymentRolloutHistory(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("deploymentName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *statefulSetResourceHandler) ListStatefulSets(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.reader.ListStatefulSets(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *statefulSetResourceHandler) GetStatefulSetDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.reader.GetStatefulSetDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("statefulSetName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *statefulSetResourceHandler) GetStatefulSetYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.reader.GetStatefulSetYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("statefulSetName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *statefulSetResourceHandler) ApplyStatefulSetYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid statefulset yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.editor.ApplyStatefulSetYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("statefulSetName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *statefulSetResourceHandler) GetStatefulSetMetrics(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	rangeMinutes := parseLimit(c.Query("rangeMinutes"), 60)
	stepSeconds := parseLimit(c.Query("stepSeconds"), 60)
	item, err := h.reader.GetStatefulSetMetrics(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("statefulSetName"), rangeMinutes, stepSeconds)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *daemonSetResourceHandler) ListDaemonSets(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.reader.ListDaemonSets(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *daemonSetResourceHandler) GetDaemonSetDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.reader.GetDaemonSetDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("daemonSetName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *daemonSetResourceHandler) GetDaemonSetYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.reader.GetDaemonSetYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("daemonSetName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *daemonSetResourceHandler) ApplyDaemonSetYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid daemonset yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.editor.ApplyDaemonSetYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("daemonSetName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *daemonSetResourceHandler) GetDaemonSetMetrics(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	rangeMinutes := parseLimit(c.Query("rangeMinutes"), 60)
	stepSeconds := parseLimit(c.Query("stepSeconds"), 60)
	item, err := h.reader.GetDaemonSetMetrics(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("daemonSetName"), rangeMinutes, stepSeconds)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *jobResourceHandler) ListJobs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.service.ListJobs(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *jobResourceHandler) GetJobDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.service.GetJobDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("jobName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *jobResourceHandler) GetJobYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.service.GetJobYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("jobName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *jobResourceHandler) ApplyJobYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid job yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.service.ApplyJobYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("jobName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *cronJobResourceHandler) ListCronJobs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.service.ListCronJobs(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *cronJobResourceHandler) GetCronJobDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.service.GetCronJobDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("cronJobName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *cronJobResourceHandler) GetCronJobYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.service.GetCronJobYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("cronJobName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *cronJobResourceHandler) ApplyCronJobYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid cronjob yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.service.ApplyCronJobYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("cronJobName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *cronJobResourceHandler) SetCronJobSuspend(c *gin.Context) {
	var req struct {
		Suspend bool `json:"suspend"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid cronjob suspend payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.service.SetCronJobSuspend(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("cronJobName"), req.Suspend)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *workloadInventoryResourceHandler) ListReplicaSets(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.service.ListReplicaSets(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *workloadInventoryResourceHandler) GetReplicaSetDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetReplicaSetDetail(c.Request.Context(), principal, c.Param("clusterID"), c.Query("namespace"), c.Param("replicaSetName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *workloadInventoryResourceHandler) ListReplicationControllers(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.service.ListReplicationControllers(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *workloadInventoryResourceHandler) GetReplicationControllerDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetReplicationControllerDetail(c.Request.Context(), principal, c.Param("clusterID"), c.Query("namespace"), c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *workloadInventoryResourceHandler) GetReplicaSetYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.generic.GetResourceYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, "ReplicaSet", c.Param("replicaSetName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *workloadInventoryResourceHandler) GetReplicationControllerYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.generic.GetResourceYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, "ReplicationController", c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *workloadInventoryResourceHandler) ApplyReplicationControllerYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid replicationcontroller yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.generic.ApplyResourceYAMLByKind(c.Request.Context(), principal, c.Param("clusterID"), namespace, "ReplicationController", c.Param("name"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *workloadInventoryResourceHandler) ApplyReplicaSetYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid replicaset yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.generic.ApplyResourceYAMLByKind(c.Request.Context(), principal, c.Param("clusterID"), namespace, "ReplicaSet", c.Param("replicaSetName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *workloadInventoryResourceHandler) ListHorizontalPodAutoscalers(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.service.ListHorizontalPodAutoscalers(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *workloadInventoryResourceHandler) GetHorizontalPodAutoscalerDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetHorizontalPodAutoscalerDetail(c.Request.Context(), principal, c.Param("clusterID"), c.Query("namespace"), c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *workloadInventoryResourceHandler) ListPodDisruptionBudgets(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.service.ListPodDisruptionBudgets(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *workloadInventoryResourceHandler) GetPodDisruptionBudgetDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetPodDisruptionBudgetDetail(c.Request.Context(), principal, c.Param("clusterID"), c.Query("namespace"), c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

// RegisterWorkloadDeleteRoutes wires DELETE endpoints for built-in workloads using
// the existing route param names (`:deploymentName`, etc.) to avoid route conflicts
// with other GET/PUT entries already registered in router.go.
func (h *workloadInventoryResourceHandler) RegisterWorkloadDeleteRoutes(group gin.IRoutes) {
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
		{"/clusters/:clusterID/workloads/replicasets/:replicaSetName", "replicaSetName", "ReplicaSet"},
	}
	for _, entry := range entries {
		kind := entry.kind
		paramName := entry.param
		group.DELETE(entry.path, func(c *gin.Context) {
			principal := apiMiddleware.PrincipalFromContext(c)
			namespace := c.Query("namespace")
			if err := h.generic.DeleteResourceByKind(c.Request.Context(), principal, c.Param("clusterID"), namespace, kind, c.Param(paramName)); err != nil {
				writeError(c, err)
				return
			}
			c.Status(http.StatusNoContent)
		})
	}
}
func (h *deploymentResourceHandler) RestartDeployment(c *gin.Context) {
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
	if err := h.editor.RestartDeployment(c.Request.Context(), principal, c.Param("clusterID"), req.Namespace, req.Name); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}
func (h *deploymentResourceHandler) ScaleDeployment(c *gin.Context) {
	handleScaleResource(c, "invalid scale deployment payload", h.editor.ScaleDeployment)
}
func (h *statefulSetResourceHandler) RestartStatefulSet(c *gin.Context) {
	var req dto.RestartStatefulSetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid restart statefulset payload")
		return
	}
	if req.Namespace == "" || req.Name == "" {
		writeError(c, fmt.Errorf("%w: namespace and name are required", apperrors.ErrInvalidArgument))
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.editor.RestartStatefulSet(c.Request.Context(), principal, c.Param("clusterID"), req.Namespace, req.Name); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}
func (h *statefulSetResourceHandler) ScaleStatefulSet(c *gin.Context) {
	handleScaleResource(c, "invalid scale statefulset payload", h.editor.ScaleStatefulSet)
}

type scaleResourceRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Replicas  int32  `json:"replicas"`
}

func handleScaleResource(c *gin.Context, invalidPayload string, scale func(context.Context, domainidentity.Principal, string, string, string, int32) error) {
	var req scaleResourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", invalidPayload)
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
	if err := scale(c.Request.Context(), principal, c.Param("clusterID"), req.Namespace, req.Name, req.Replicas); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}
func (h *daemonSetResourceHandler) RestartDaemonSet(c *gin.Context) {
	var req dto.RestartDaemonSetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid restart daemonset payload")
		return
	}
	if req.Namespace == "" || req.Name == "" {
		writeError(c, fmt.Errorf("%w: namespace and name are required", apperrors.ErrInvalidArgument))
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.editor.RestartDaemonSet(c.Request.Context(), principal, c.Param("clusterID"), req.Namespace, req.Name); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}
func (h *deploymentResourceHandler) RollbackDeployment(c *gin.Context) {
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
	item, err := h.editor.RollbackDeployment(c.Request.Context(), principal, c.Param("clusterID"), req.Namespace, req.Name, req.Revision)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
