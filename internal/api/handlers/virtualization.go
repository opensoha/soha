package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	apiMiddleware "github.com/kubecrux/kubecrux/internal/api/middleware"
	apiresponse "github.com/kubecrux/kubecrux/internal/api/response"
	appvirtualization "github.com/kubecrux/kubecrux/internal/application/virtualization"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainvirtualization "github.com/kubecrux/kubecrux/internal/domain/virtualization"
	infravirtualization "github.com/kubecrux/kubecrux/internal/infrastructure/virtualization"
)

type VirtualizationService interface {
	Overview(context.Context, domainidentity.Principal) (appvirtualization.Overview, error)
	ListConnections(context.Context, domainidentity.Principal, domainvirtualization.ConnectionFilter) ([]domainvirtualization.Connection, error)
	CreateConnection(context.Context, domainidentity.Principal, appvirtualization.ConnectionInput) (domainvirtualization.Connection, error)
	UpdateConnection(context.Context, domainidentity.Principal, string, appvirtualization.ConnectionInput) (domainvirtualization.Connection, error)
	DeleteConnection(context.Context, domainidentity.Principal, string) error
	TestConnection(context.Context, domainidentity.Principal, string) (domainvirtualization.Task, error)
	SyncConnection(context.Context, domainidentity.Principal, string) (domainvirtualization.Task, error)
	SyncAll(context.Context, domainidentity.Principal) (domainvirtualization.Task, error)
	ListVMs(context.Context, domainidentity.Principal, domainvirtualization.VMFilter) ([]domainvirtualization.VM, error)
	ListVMsPage(context.Context, domainidentity.Principal, domainvirtualization.VMFilter) (domainvirtualization.Page[domainvirtualization.VM], error)
	GetVM(context.Context, domainidentity.Principal, string) (domainvirtualization.VM, error)
	GetVMDetail(context.Context, domainidentity.Principal, string) (appvirtualization.VMDetail, error)
	CreateVM(context.Context, domainidentity.Principal, appvirtualization.CreateVMInput) (domainvirtualization.Task, error)
	VMAction(context.Context, domainidentity.Principal, string, appvirtualization.VMActionInput) (domainvirtualization.Task, error)
	ListImages(context.Context, domainidentity.Principal, domainvirtualization.ImageFilter) ([]domainvirtualization.Image, error)
	ListImagesPage(context.Context, domainidentity.Principal, domainvirtualization.ImageFilter) (domainvirtualization.Page[domainvirtualization.Image], error)
	CreateImage(context.Context, domainidentity.Principal, appvirtualization.ImageInput) (domainvirtualization.Image, error)
	UpdateImage(context.Context, domainidentity.Principal, string, appvirtualization.ImageInput) (domainvirtualization.Image, error)
	DeleteImage(context.Context, domainidentity.Principal, string) error
	ListFlavors(context.Context, domainidentity.Principal, domainvirtualization.FlavorFilter) ([]domainvirtualization.Flavor, error)
	ListFlavorsPage(context.Context, domainidentity.Principal, domainvirtualization.FlavorFilter) (domainvirtualization.Page[domainvirtualization.Flavor], error)
	CreateFlavor(context.Context, domainidentity.Principal, appvirtualization.FlavorInput) (domainvirtualization.Flavor, error)
	UpdateFlavor(context.Context, domainidentity.Principal, string, appvirtualization.FlavorInput) (domainvirtualization.Flavor, error)
	DeleteFlavor(context.Context, domainidentity.Principal, string) error
	ListOperations(context.Context, domainidentity.Principal, domainvirtualization.TaskFilter) ([]domainvirtualization.Task, error)
	ListOperationsPage(context.Context, domainidentity.Principal, domainvirtualization.TaskFilter) (domainvirtualization.Page[domainvirtualization.Task], error)
	GetOperation(context.Context, domainidentity.Principal, string) (domainvirtualization.Task, error)
	ListOperationLogs(context.Context, domainidentity.Principal, string, int) ([]domainvirtualization.TaskLog, error)
	CancelOperation(context.Context, domainidentity.Principal, string) (domainvirtualization.Task, error)
	RetryOperation(context.Context, domainidentity.Principal, string) (domainvirtualization.Task, error)
	GetVMMetrics(context.Context, domainidentity.Principal, string, int, int) (infravirtualization.VMMetricsResult, error)
	GetConsoleURL(context.Context, domainidentity.Principal, string) (infravirtualization.ConsoleURLResult, error)
}

type VirtualizationHandler struct {
	service VirtualizationService
}

func NewVirtualizationHandler(service VirtualizationService) *VirtualizationHandler {
	return &VirtualizationHandler{service: service}
}

func (h *VirtualizationHandler) Overview(c *gin.Context) {
	item, err := h.service.Overview(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *VirtualizationHandler) ListConnections(c *gin.Context) {
	items, err := h.service.ListConnections(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), domainvirtualization.ConnectionFilter{
		Provider:            c.Query("provider"),
		KubernetesClusterID: c.Query("kubernetesClusterId"),
		Limit:               queryLimit(c, 100),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, mapConnections(items))
}

func (h *VirtualizationHandler) CreateConnection(c *gin.Context) {
	var req appvirtualization.ConnectionInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid virtualization connection payload")
		return
	}
	item, err := h.service.CreateConnection(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, mapConnection(item))
}

func (h *VirtualizationHandler) UpdateConnection(c *gin.Context) {
	var req appvirtualization.ConnectionInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid virtualization connection payload")
		return
	}
	item, err := h.service.UpdateConnection(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, mapConnection(item))
}

func (h *VirtualizationHandler) DeleteConnection(c *gin.Context) {
	if err := h.service.DeleteConnection(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *VirtualizationHandler) TestConnection(c *gin.Context) {
	task, err := h.service.TestConnection(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, mapOperation(task))
}

func (h *VirtualizationHandler) SyncConnection(c *gin.Context) {
	task, err := h.service.SyncConnection(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, mapOperation(task))
}

func (h *VirtualizationHandler) SyncAll(c *gin.Context) {
	task, err := h.service.SyncAll(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, mapOperation(task))
}

func (h *VirtualizationHandler) ListVMs(c *gin.Context) {
	filter := domainvirtualization.VMFilter{
		Provider:     c.Query("provider"),
		ConnectionID: c.Query("connectionId"),
		Namespace:    c.Query("namespace"),
		Status:       c.Query("status"),
		Search:       c.Query("search"),
		Page:         queryInt(c, "page", 0),
		PageSize:     queryInt(c, "pageSize", 0),
		Limit:        queryLimit(c, 100),
	}
	if wantsPage(c) {
		page, err := h.service.ListVMsPage(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), filter)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, gin.H{
			"items":    mapVMs(page.Items),
			"total":    page.Total,
			"page":     page.Page,
			"pageSize": page.PageSize,
		})
		return
	}
	items, err := h.service.ListVMs(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), filter)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, mapVMs(items))
}

func (h *VirtualizationHandler) CreateVM(c *gin.Context) {
	var req appvirtualization.CreateVMInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid virtual machine payload")
		return
	}
	task, err := h.service.CreateVM(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, mapOperation(task))
}

func (h *VirtualizationHandler) GetVM(c *gin.Context) {
	item, err := h.service.GetVM(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, mapVM(item))
}

func (h *VirtualizationHandler) GetVMDetail(c *gin.Context) {
	item, err := h.service.GetVMDetail(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, mapVMDetail(item))
}

func (h *VirtualizationHandler) VMAction(c *gin.Context) {
	var req appvirtualization.VMActionInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid virtual machine action payload")
		return
	}
	task, err := h.service.VMAction(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, mapOperation(task))
}

func (h *VirtualizationHandler) ListImages(c *gin.Context) {
	filter := domainvirtualization.ImageFilter{
		Provider:     c.Query("provider"),
		ConnectionID: c.Query("connectionId"),
		Status:       c.Query("status"),
		Search:       c.Query("search"),
		Page:         queryInt(c, "page", 0),
		PageSize:     queryInt(c, "pageSize", 0),
		Limit:        queryLimit(c, 100),
	}
	if wantsPage(c) {
		page, err := h.service.ListImagesPage(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), filter)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, gin.H{
			"items":    mapImages(page.Items),
			"total":    page.Total,
			"page":     page.Page,
			"pageSize": page.PageSize,
		})
		return
	}
	items, err := h.service.ListImages(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), filter)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, mapImages(items))
}

func (h *VirtualizationHandler) CreateImage(c *gin.Context) {
	var req appvirtualization.ImageInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid virtualization image payload")
		return
	}
	item, err := h.service.CreateImage(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, mapImage(item))
}

func (h *VirtualizationHandler) UpdateImage(c *gin.Context) {
	var req appvirtualization.ImageInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid virtualization image payload")
		return
	}
	item, err := h.service.UpdateImage(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, mapImage(item))
}

func (h *VirtualizationHandler) DeleteImage(c *gin.Context) {
	if err := h.service.DeleteImage(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *VirtualizationHandler) ListFlavors(c *gin.Context) {
	filter := domainvirtualization.FlavorFilter{
		Provider:     c.Query("provider"),
		ConnectionID: c.Query("connectionId"),
		Status:       c.Query("status"),
		Search:       c.Query("search"),
		Page:         queryInt(c, "page", 0),
		PageSize:     queryInt(c, "pageSize", 0),
		Limit:        queryLimit(c, 100),
	}
	if wantsPage(c) {
		page, err := h.service.ListFlavorsPage(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), filter)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, gin.H{
			"items":    mapFlavors(page.Items),
			"total":    page.Total,
			"page":     page.Page,
			"pageSize": page.PageSize,
		})
		return
	}
	items, err := h.service.ListFlavors(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), filter)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, mapFlavors(items))
}

func (h *VirtualizationHandler) CreateFlavor(c *gin.Context) {
	var req appvirtualization.FlavorInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid virtualization flavor payload")
		return
	}
	item, err := h.service.CreateFlavor(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, mapFlavor(item))
}

func (h *VirtualizationHandler) UpdateFlavor(c *gin.Context) {
	var req appvirtualization.FlavorInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid virtualization flavor payload")
		return
	}
	item, err := h.service.UpdateFlavor(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, mapFlavor(item))
}

func (h *VirtualizationHandler) DeleteFlavor(c *gin.Context) {
	if err := h.service.DeleteFlavor(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *VirtualizationHandler) ListOperations(c *gin.Context) {
	taskKind := firstQuery(c, "taskKind", "assetType", "type")
	statuses := splitQueryList(c.Query("statuses"))
	filter := domainvirtualization.TaskFilter{
		Provider:     c.Query("provider"),
		ConnectionID: c.Query("connectionId"),
		VMID:         c.Query("vmId"),
		Status:       c.Query("status"),
		Statuses:     statuses,
		Abnormal:     queryBool(c, "abnormal"),
		Pending:      queryBool(c, "pending"),
		TaskKind:     taskKind,
		Search:       c.Query("search"),
		Page:         queryInt(c, "page", 0),
		PageSize:     queryInt(c, "pageSize", 0),
		Limit:        queryLimit(c, 100),
	}
	if wantsPage(c) {
		page, err := h.service.ListOperationsPage(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), filter)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, gin.H{
			"items":    mapOperations(page.Items),
			"total":    page.Total,
			"page":     page.Page,
			"pageSize": page.PageSize,
		})
		return
	}
	items, err := h.service.ListOperations(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), filter)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, mapOperations(items))
}

func (h *VirtualizationHandler) GetOperation(c *gin.Context) {
	item, err := h.service.GetOperation(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("taskID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, mapOperation(item))
}

func (h *VirtualizationHandler) ListOperationLogs(c *gin.Context) {
	items, err := h.service.ListOperationLogs(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("taskID"), queryLimit(c, 200))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *VirtualizationHandler) CancelOperation(c *gin.Context) {
	task, err := h.service.CancelOperation(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("taskID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, mapOperation(task))
}

func (h *VirtualizationHandler) RetryOperation(c *gin.Context) {
	task, err := h.service.RetryOperation(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("taskID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, mapOperation(task))
}

func queryLimit(c *gin.Context, fallback int) int {
	raw := c.Query("limit")
	if raw == "" {
		return fallback
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return fallback
	}
	return limit
}

func queryInt(c *gin.Context, key string, fallback int) int {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func queryBool(c *gin.Context, key string) bool {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return false
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false
	}
	return value
}

func splitQueryList(raw string) []string {
	parts := strings.Split(raw, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}

func wantsPage(c *gin.Context) bool {
	return strings.TrimSpace(c.Query("page")) != "" || strings.TrimSpace(c.Query("pageSize")) != "" || strings.TrimSpace(c.Query("search")) != ""
}

func firstQuery(c *gin.Context, keys ...string) string {
	for _, key := range keys {
		if value := c.Query(key); strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func mapConnections(items []domainvirtualization.Connection) []gin.H {
	out := make([]gin.H, 0, len(items))
	for _, item := range items {
		out = append(out, mapConnection(item))
	}
	return out
}

func mapConnection(item domainvirtualization.Connection) gin.H {
	riskLevel, riskReasons := connectionRiskMetadata(item)
	return gin.H{
		"id":                   item.ID,
		"name":                 item.Name,
		"provider":             item.Provider,
		"endpoint":             item.Endpoint,
		"kubernetesClusterId":  item.KubernetesClusterID,
		"defaultNamespace":     item.DefaultNamespace,
		"enabled":              item.Enabled,
		"verifyTls":            item.VerifyTLS,
		"credentialConfigured": item.CredentialConfigured,
		"config":               item.Config,
		"health":               stringValue(item.Health, "status"),
		"status":               stringValue(item.Health, "status"),
		"region":               stringValue(item.Config, "region"),
		"description":          stringValue(item.Config, "description"),
		"riskLevel":            riskLevel,
		"riskReasons":          riskReasons,
		"lastSyncedAt":         item.LastSyncedAt,
		"createdAt":            item.CreatedAt,
		"updatedAt":            item.UpdatedAt,
	}
}

func connectionRiskMetadata(item domainvirtualization.Connection) (string, []string) {
	reasons := make([]string, 0, 3)
	health := strings.ToLower(stringValue(item.Health, "status"))
	switch health {
	case "unavailable":
		reasons = append(reasons, "连接不可用")
	case "degraded":
		reasons = append(reasons, "连接降级")
	}
	if item.Enabled && !item.CredentialConfigured {
		reasons = append(reasons, "未配置凭证")
	}
	if item.LastSyncedAt == nil {
		reasons = append(reasons, "尚未同步")
	}
	switch {
	case health == "unavailable":
		return "critical", reasons
	case health == "degraded":
		return "warning", reasons
	case len(reasons) > 0:
		return "attention", reasons
	default:
		return "normal", reasons
	}
}

func mapVMs(items []domainvirtualization.VM) []gin.H {
	out := make([]gin.H, 0, len(items))
	for _, item := range items {
		out = append(out, mapVM(item))
	}
	return out
}

func mapVM(item domainvirtualization.VM) gin.H {
	cpu, memory, disk := vmResourceHints(item)
	return gin.H{
		"id":           item.ID,
		"name":         item.Name,
		"provider":     item.Provider,
		"connectionId": item.ConnectionID,
		"externalId":   item.ExternalID,
		"namespace":    item.Namespace,
		"node":         item.NodeName,
		"status":       item.Status,
		"powerState":   item.PowerState,
		"bootImageId":  item.ImageID,
		"imageId":      item.ImageID,
		"flavorId":     item.FlavorID,
		"sourceMode":   firstNonEmpty(stringValue(item.Config, "sourceMode"), stringValue(item.Raw, "sourceMode")),
		"sourceRef":    firstNonEmpty(stringValue(item.Config, "sourceRef"), stringValue(item.Raw, "sourceRef")),
		"cpu":          cpu,
		"memoryMiB":    memory,
		"diskGiB":      disk,
		"network":      firstNonEmpty(stringValue(item.Config, "network"), stringValue(item.Raw, "network")),
		"ipAddresses":  item.IPAddresses,
		"labels":       item.Labels,
		"config":       item.Config,
		"createdAt":    item.CreatedAt,
		"updatedAt":    item.UpdatedAt,
		"allowedActions": []string{
			"start",
			"stop",
			"restart",
			"shutdown",
			"delete",
		},
	}
}

func vmResourceHints(item domainvirtualization.VM) (int, int, int) {
	return intValue(item.Config, "cpu"), intValue(item.Config, "memoryMiB"), intValue(item.Config, "diskGiB")
}

func mapVMDetail(item appvirtualization.VMDetail) gin.H {
	operations := make([]gin.H, 0, len(item.Operations))
	for _, operation := range item.Operations {
		mapped := mapOperation(operation.Task)
		mapped["logs"] = operation.Logs
		operations = append(operations, mapped)
	}
	out := gin.H{
		"vm":          mapVM(item.VM),
		"providerRaw": item.VM.Raw,
		"operations":  operations,
		"logs":        item.Logs,
	}
	if item.Connection != nil {
		out["connection"] = mapConnection(*item.Connection)
	}
	if item.Image != nil {
		out["image"] = mapImage(*item.Image)
		out["vm"].(gin.H)["bootImageName"] = item.Image.Name
		out["vm"].(gin.H)["bootImageId"] = item.Image.ID
	}
	if item.Flavor != nil {
		out["flavor"] = mapFlavor(*item.Flavor)
		vm := out["vm"].(gin.H)
		vm["flavorName"] = item.Flavor.Name
		vm["flavorId"] = item.Flavor.ID
		vm["cpu"] = item.Flavor.CPUCores
		vm["memoryMiB"] = item.Flavor.MemoryMB
		vm["diskGiB"] = item.Flavor.DiskGB
	}
	return out
}

func mapImages(items []domainvirtualization.Image) []gin.H {
	out := make([]gin.H, 0, len(items))
	for _, item := range items {
		if item.Status == "deleted" {
			continue
		}
		out = append(out, mapImage(item))
	}
	return out
}

func mapImage(item domainvirtualization.Image) gin.H {
	return gin.H{
		"id":             item.ID,
		"name":           item.Name,
		"provider":       item.Provider,
		"connectionId":   item.ConnectionID,
		"externalId":     item.ExternalID,
		"status":         item.Status,
		"osType":         item.OSType,
		"architecture":   item.Architecture,
		"sizeBytes":      item.SizeBytes,
		"sizeGiB":        bytesToGiB(item.SizeBytes),
		"sourceKind":     stringValue(item.Config, "sourceKind"),
		"assetKind":      firstNonEmpty(stringValue(item.Config, "sourceKind"), stringValue(item.Config, "assetKind")),
		"source":         firstNonEmpty(stringValue(item.Config, "sourceKind"), stringValue(item.Config, "source")),
		"sourceRef":      firstNonEmpty(stringValue(item.Config, "sourceRef"), item.ExternalID),
		"namespace":      stringValue(item.Config, "namespace"),
		"node":           stringValue(item.Config, "node"),
		"storage":        stringValue(item.Config, "storage"),
		"storageClass":   stringValue(item.Config, "storageClass"),
		"ready":          item.Status != "stale" && item.Status != "deleted",
		"description":    stringValue(item.Config, "description"),
		"config":         item.Config,
		"createdAt":      item.CreatedAt,
		"updatedAt":      item.UpdatedAt,
		"allowedActions": []string{"update", "delete"},
	}
}

func mapFlavors(items []domainvirtualization.Flavor) []gin.H {
	out := make([]gin.H, 0, len(items))
	for _, item := range items {
		if item.Status == "deleted" {
			continue
		}
		out = append(out, mapFlavor(item))
	}
	return out
}

func mapFlavor(item domainvirtualization.Flavor) gin.H {
	return gin.H{
		"id":           item.ID,
		"name":         item.Name,
		"provider":     item.Provider,
		"connectionId": item.ConnectionID,
		"externalId":   item.ExternalID,
		"status":       item.Status,
		"cpuCores":     item.CPUCores,
		"cpu":          item.CPUCores,
		"memoryMb":     item.MemoryMB,
		"memoryMiB":    item.MemoryMB,
		"diskGb":       item.DiskGB,
		"diskGiB":      item.DiskGB,
		"description":  stringValue(item.Config, "description"),
		"enabled":      item.Status != "disabled",
		"config":       item.Config,
		"createdAt":    item.CreatedAt,
		"updatedAt":    item.UpdatedAt,
		"allowedActions": []string{
			"update",
			"delete",
		},
	}
}

func mapOperations(items []domainvirtualization.Task) []gin.H {
	out := make([]gin.H, 0, len(items))
	for _, item := range items {
		out = append(out, mapOperation(item))
	}
	return out
}

func mapOperation(item domainvirtualization.Task) gin.H {
	return gin.H{
		"id":              item.ID,
		"type":            item.TaskKind,
		"operationType":   item.TaskKind,
		"action":          stringValue(item.Payload, "action"),
		"assetType":       item.TaskKind,
		"targetType":      item.TaskKind,
		"targetName":      firstNonEmpty(stringValue(item.Result, "name"), stringValue(item.Payload, "name"), item.VMID, item.ConnectionID),
		"status":          item.Status,
		"message":         firstNonEmpty(stringValue(item.Result, "message"), stringValue(item.Result, "error")),
		"provider":        item.Provider,
		"connectionId":    item.ConnectionID,
		"vmId":            item.VMID,
		"actor":           item.RequestedBy,
		"payload":         item.Payload,
		"result":          item.Result,
		"startedAt":       item.StartedAt,
		"lastHeartbeatAt": item.LastHeartbeatAt,
		"completedAt":     item.FinishedAt,
		"createdAt":       item.CreatedAt,
		"updatedAt":       item.UpdatedAt,
		"allowedActions":  operationAllowedActions(item),
	}
}

func operationAllowedActions(item domainvirtualization.Task) []string {
	switch item.Status {
	case "queued", "running":
		return []string{"cancel"}
	case "failed", "canceled", "callback_timeout":
		if item.MaxRetries == 0 || item.AttemptCount <= item.MaxRetries {
			return []string{"retry"}
		}
	}
	return []string{}
}

func bytesToGiB(value int64) int64 {
	if value <= 0 {
		return 0
	}
	return value / 1024 / 1024 / 1024
}

func stringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	switch value := values[key].(type) {
	case string:
		return value
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	default:
		return ""
	}
}

func intValue(values map[string]any, key string) int {
	if values == nil {
		return 0
	}
	switch value := values[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		parsed, _ := strconv.Atoi(value.String())
		return parsed
	default:
		return 0
	}
}

func (h *VirtualizationHandler) StreamTaskUpdates(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	taskID := c.Param("taskID")

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			task, err := h.service.GetOperation(c.Request.Context(), principal, taskID)
			if err != nil {
				continue
			}

			data, _ := json.Marshal(task)
			fmt.Fprintf(c.Writer, "data: %s\n\n", data)
			c.Writer.Flush()

			if taskTerminal(task.Status) {
				return
			}
		}
	}
}

func taskTerminal(status string) bool {
	return status == "completed" || status == "failed" || status == "canceled" || status == "callback_timeout"
}

func (h *VirtualizationHandler) GetVMMetrics(c *gin.Context) {
	rangeMinutes, _ := strconv.Atoi(c.DefaultQuery("rangeMinutes", "60"))
	stepSeconds, _ := strconv.Atoi(c.DefaultQuery("stepSeconds", "60"))

	result, err := h.service.GetVMMetrics(
		c.Request.Context(),
		apiMiddleware.PrincipalFromContext(c),
		c.Param("id"),
		rangeMinutes,
		stepSeconds,
	)

	if err != nil {
		writeError(c, err)
		return
	}

	apiresponse.Item(c, http.StatusOK, result)
}

func (h *VirtualizationHandler) GetConsoleURL(c *gin.Context) {
	result, err := h.service.GetConsoleURL(
		c.Request.Context(),
		apiMiddleware.PrincipalFromContext(c),
		c.Param("id"),
	)

	if err != nil {
		writeError(c, err)
		return
	}

	apiresponse.Item(c, http.StatusOK, result)
}

var vncUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (h *VirtualizationHandler) StreamVMConsole(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	vmID := c.Param("id")

	consoleResult, err := h.service.GetConsoleURL(c.Request.Context(), principal, vmID)
	if err != nil {
		writeError(c, err)
		return
	}

	if consoleResult.Message != "" || !consoleResult.Ready {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": firstNonEmpty(consoleResult.Message, "console is not ready")})
		return
	}

	conn, err := vncUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	if consoleResult.Type == "novnc" && consoleResult.Token != "" {
		proxyPVEVNC(conn, firstNonEmpty(consoleResult.BackendURL, consoleResult.URL), consoleResult.Token)
	} else if consoleResult.Type == "vnc" {
		proxyKubeVirtVNC(conn, firstNonEmpty(consoleResult.BackendURL, consoleResult.URL))
	} else {
		conn.WriteMessage(websocket.TextMessage, []byte(`{"error":"VNC proxy not fully implemented for this provider"}`))
	}
}

func proxyPVEVNC(clientConn *websocket.Conn, backendURL, ticket string) {
	parsedURL, err := url.Parse(backendURL)
	if err != nil {
		clientConn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"error":"invalid backend URL: %v"}`, err)))
		return
	}

	parsedURL.Scheme = "wss"
	if strings.HasPrefix(backendURL, "http://") {
		parsedURL.Scheme = "ws"
	}

	header := http.Header{}
	header.Set("Cookie", "PVEAuthCookie="+ticket)

	backendConn, _, err := websocket.DefaultDialer.Dial(parsedURL.String(), header)
	if err != nil {
		clientConn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"error":"failed to connect to backend: %v"}`, err)))
		return
	}
	defer backendConn.Close()

	proxyWebsocket(clientConn, backendConn)
}

func proxyKubeVirtVNC(clientConn *websocket.Conn, backendURL string) {
	parsedURL, err := url.Parse(backendURL)
	if err != nil {
		clientConn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"error":"invalid backend URL: %v"}`, err)))
		return
	}
	parsedURL.Scheme = "wss"
	if strings.HasPrefix(backendURL, "http://") {
		parsedURL.Scheme = "ws"
	}
	backendConn, _, err := websocket.DefaultDialer.Dial(parsedURL.String(), nil)
	if err != nil {
		clientConn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"error":"failed to connect to kubevirt backend: %v"}`, err)))
		return
	}
	defer backendConn.Close()
	proxyWebsocket(clientConn, backendConn)
}

func proxyWebsocket(clientConn, backendConn *websocket.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(&wsWriter{conn: clientConn}, &wsReader{conn: backendConn})
	}()

	go func() {
		defer wg.Done()
		io.Copy(&wsWriter{conn: backendConn}, &wsReader{conn: clientConn})
	}()

	wg.Wait()
}

type wsReader struct {
	conn *websocket.Conn
}

func (r *wsReader) Read(p []byte) (int, error) {
	_, data, err := r.conn.ReadMessage()
	if err != nil {
		return 0, err
	}
	n := copy(p, data)
	return n, nil
}

type wsWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (w *wsWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	err := w.conn.WriteMessage(websocket.BinaryMessage, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}
