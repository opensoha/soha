package handlers

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
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
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appvirtualization "github.com/opensoha/soha/internal/application/virtualization"
	"github.com/opensoha/soha/internal/application/virtualization/consoleport"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
)

type VirtualizationConnectionService interface {
	ListConnections(context.Context, domainidentity.Principal, domainvirtualization.ConnectionFilter) ([]domainvirtualization.Connection, error)
	CreateConnection(context.Context, domainidentity.Principal, appvirtualization.ConnectionInput) (domainvirtualization.Connection, error)
	UpdateConnection(context.Context, domainidentity.Principal, string, appvirtualization.ConnectionInput) (domainvirtualization.Connection, error)
	GetConnectionDeleteDependencies(context.Context, domainidentity.Principal, string) (domainvirtualization.ConnectionDeleteDependencies, error)
	DeleteConnection(context.Context, domainidentity.Principal, string, appvirtualization.DeleteConnectionOptions) error
	TestConnection(context.Context, domainidentity.Principal, string) (domainvirtualization.Task, error)
}

type VirtualizationSyncService interface {
	SyncConnection(context.Context, domainidentity.Principal, string) (domainvirtualization.Task, error)
	SyncAll(context.Context, domainidentity.Principal) (domainvirtualization.Task, error)
}

type VirtualizationVMService interface {
	ListVMs(context.Context, domainidentity.Principal, domainvirtualization.VMFilter) ([]domainvirtualization.VM, error)
	ListVMsPage(context.Context, domainidentity.Principal, domainvirtualization.VMFilter) (domainvirtualization.Page[domainvirtualization.VM], error)
	GetVM(context.Context, domainidentity.Principal, string) (domainvirtualization.VM, error)
	GetVMDetail(context.Context, domainidentity.Principal, string) (appvirtualization.VMDetail, error)
	CreateVM(context.Context, domainidentity.Principal, appvirtualization.CreateVMInput) (domainvirtualization.Task, error)
	VMAction(context.Context, domainidentity.Principal, string, appvirtualization.VMActionInput) (domainvirtualization.Task, error)
}

type VirtualizationImageService interface {
	ListImages(context.Context, domainidentity.Principal, domainvirtualization.ImageFilter) ([]domainvirtualization.Image, error)
	ListImagesPage(context.Context, domainidentity.Principal, domainvirtualization.ImageFilter) (domainvirtualization.Page[domainvirtualization.Image], error)
	CreateImage(context.Context, domainidentity.Principal, appvirtualization.ImageInput) (domainvirtualization.Image, error)
	UpdateImage(context.Context, domainidentity.Principal, string, appvirtualization.ImageInput) (domainvirtualization.Image, error)
	DeleteImage(context.Context, domainidentity.Principal, string) error
}

type VirtualizationFlavorService interface {
	ListFlavors(context.Context, domainidentity.Principal, domainvirtualization.FlavorFilter) ([]domainvirtualization.Flavor, error)
	ListFlavorsPage(context.Context, domainidentity.Principal, domainvirtualization.FlavorFilter) (domainvirtualization.Page[domainvirtualization.Flavor], error)
	CreateFlavor(context.Context, domainidentity.Principal, appvirtualization.FlavorInput) (domainvirtualization.Flavor, error)
	UpdateFlavor(context.Context, domainidentity.Principal, string, appvirtualization.FlavorInput) (domainvirtualization.Flavor, error)
	DeleteFlavor(context.Context, domainidentity.Principal, string) error
}

type VirtualizationOperationService interface {
	ListOperations(context.Context, domainidentity.Principal, domainvirtualization.TaskFilter) ([]domainvirtualization.Task, error)
	ListOperationsPage(context.Context, domainidentity.Principal, domainvirtualization.TaskFilter) (domainvirtualization.Page[domainvirtualization.Task], error)
	GetOperation(context.Context, domainidentity.Principal, string) (domainvirtualization.Task, error)
	ListOperationLogs(context.Context, domainidentity.Principal, string, int) ([]domainvirtualization.TaskLog, error)
	CancelOperation(context.Context, domainidentity.Principal, string) (domainvirtualization.Task, error)
	RetryOperation(context.Context, domainidentity.Principal, string) (domainvirtualization.Task, error)
}

type VirtualizationRuntimeService interface {
	GetVMMetrics(context.Context, domainidentity.Principal, string, int, int) (domainvirtualization.VMMetricsResult, error)
	GetConsoleURL(context.Context, domainidentity.Principal, string) (consoleport.ConsoleURLResult, error)
}

type VirtualizationService interface {
	VirtualizationConnectionService
	VirtualizationSyncService
	VirtualizationVMService
	VirtualizationImageService
	VirtualizationFlavorService
	VirtualizationOperationService
	VirtualizationRuntimeService
}

type VirtualizationServices struct {
	Connections VirtualizationConnectionService
	Sync        VirtualizationSyncService
	VMs         VirtualizationVMService
	Images      VirtualizationImageService
	Flavors     VirtualizationFlavorService
	Operations  VirtualizationOperationService
	Runtime     VirtualizationRuntimeService
}

type VirtualizationHandler struct {
	connections VirtualizationConnectionService
	sync        VirtualizationSyncService
	vms         VirtualizationVMService
	images      VirtualizationImageService
	flavors     VirtualizationFlavorService
	operations  VirtualizationOperationService
	runtime     VirtualizationRuntimeService
}

func NewVirtualizationHandler(service VirtualizationService) *VirtualizationHandler {
	return NewVirtualizationHandlerWithServices(VirtualizationServices{
		Connections: service, Sync: service, VMs: service,
		Images: service, Flavors: service, Operations: service, Runtime: service,
	})
}

func NewVirtualizationHandlerWithServices(services VirtualizationServices) *VirtualizationHandler {
	return &VirtualizationHandler{
		connections: services.Connections, sync: services.Sync,
		vms: services.VMs, images: services.Images, flavors: services.Flavors,
		operations: services.Operations, runtime: services.Runtime,
	}
}

func (h *VirtualizationHandler) ListConnections(c *gin.Context) {
	items, err := h.connections.ListConnections(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), domainvirtualization.ConnectionFilter{
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
	item, err := h.connections.CreateConnection(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), req)
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
	item, err := h.connections.UpdateConnection(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, mapConnection(item))
}

func (h *VirtualizationHandler) DeleteConnection(c *gin.Context) {
	opts := appvirtualization.DeleteConnectionOptions{Force: strings.EqualFold(c.Query("force"), "true") || c.Query("force") == "1"}
	if err := h.connections.DeleteConnection(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), opts); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *VirtualizationHandler) GetConnectionDeleteDependencies(c *gin.Context) {
	deps, err := h.connections.GetConnectionDeleteDependencies(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, mapConnectionDeleteDependencies(deps))
}

func (h *VirtualizationHandler) TestConnection(c *gin.Context) {
	task, err := h.connections.TestConnection(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, mapOperation(task))
}

func (h *VirtualizationHandler) SyncConnection(c *gin.Context) {
	task, err := h.sync.SyncConnection(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, mapOperation(task))
}

func (h *VirtualizationHandler) SyncAll(c *gin.Context) {
	task, err := h.sync.SyncAll(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
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
		page, err := h.vms.ListVMsPage(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), filter)
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
	items, err := h.vms.ListVMs(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), filter)
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
	task, err := h.vms.CreateVM(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, mapOperation(task))
}

func (h *VirtualizationHandler) GetVM(c *gin.Context) {
	item, err := h.vms.GetVM(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, mapVM(item))
}

func (h *VirtualizationHandler) GetVMDetail(c *gin.Context) {
	item, err := h.vms.GetVMDetail(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
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
	task, err := h.vms.VMAction(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), req)
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
	listVirtualizationResources(c, virtualizationListSpec[domainvirtualization.Image, domainvirtualization.ImageFilter]{
		filter: filter, list: h.images.ListImages, listPage: h.images.ListImagesPage, mapItems: mapImageItems,
	})
}

func (h *VirtualizationHandler) CreateImage(c *gin.Context) {
	var req appvirtualization.ImageInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid virtualization image payload")
		return
	}
	item, err := h.images.CreateImage(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), req)
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
	item, err := h.images.UpdateImage(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, mapImage(item))
}

func (h *VirtualizationHandler) DeleteImage(c *gin.Context) {
	if err := h.images.DeleteImage(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id")); err != nil {
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
	listVirtualizationResources(c, virtualizationListSpec[domainvirtualization.Flavor, domainvirtualization.FlavorFilter]{
		filter: filter, list: h.flavors.ListFlavors, listPage: h.flavors.ListFlavorsPage, mapItems: mapFlavorItems,
	})
}

type virtualizationListSpec[T any, F any] struct {
	filter   F
	list     func(context.Context, domainidentity.Principal, F) ([]T, error)
	listPage func(context.Context, domainidentity.Principal, F) (domainvirtualization.Page[T], error)
	mapItems func([]T) any
}

func listVirtualizationResources[T any, F any](c *gin.Context, spec virtualizationListSpec[T, F]) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if wantsPage(c) {
		page, err := spec.listPage(c.Request.Context(), principal, spec.filter)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, gin.H{
			"items": spec.mapItems(page.Items), "total": page.Total,
			"page": page.Page, "pageSize": page.PageSize,
		})
		return
	}
	items, err := spec.list(c.Request.Context(), principal, spec.filter)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, spec.mapItems(items))
}

func mapImageItems(items []domainvirtualization.Image) any   { return mapImages(items) }
func mapFlavorItems(items []domainvirtualization.Flavor) any { return mapFlavors(items) }

func (h *VirtualizationHandler) CreateFlavor(c *gin.Context) {
	var req appvirtualization.FlavorInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid virtualization flavor payload")
		return
	}
	item, err := h.flavors.CreateFlavor(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), req)
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
	item, err := h.flavors.UpdateFlavor(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, mapFlavor(item))
}

func (h *VirtualizationHandler) DeleteFlavor(c *gin.Context) {
	if err := h.flavors.DeleteFlavor(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id")); err != nil {
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
		page, err := h.operations.ListOperationsPage(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), filter)
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
	items, err := h.operations.ListOperations(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), filter)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, mapOperations(items))
}

func (h *VirtualizationHandler) GetOperation(c *gin.Context) {
	item, err := h.operations.GetOperation(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("taskID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, mapOperation(item))
}

func (h *VirtualizationHandler) ListOperationLogs(c *gin.Context) {
	items, err := h.operations.ListOperationLogs(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("taskID"), queryLimit(c, 200))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *VirtualizationHandler) CancelOperation(c *gin.Context) {
	task, err := h.operations.CancelOperation(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("taskID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, mapOperation(task))
}

func (h *VirtualizationHandler) RetryOperation(c *gin.Context) {
	task, err := h.operations.RetryOperation(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("taskID"))
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
		"config":               sanitizeVirtualizationConnectionConfig(item.Provider, item.Config),
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

func sanitizeVirtualizationConnectionConfig(provider string, config map[string]any) map[string]any {
	out := cloneVirtualizationMap(config)
	if out == nil {
		return nil
	}
	tokenConfigured := strings.TrimSpace(stringValue(out, "prometheusBearerToken")) != ""

	delete(out, "prometheusBearerToken")
	if strings.EqualFold(provider, "kubevirt") && tokenConfigured {
		out["prometheusBearerTokenConfigured"] = true
	}
	return out
}

func cloneVirtualizationMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func mapConnectionDeleteDependencies(item domainvirtualization.ConnectionDeleteDependencies) gin.H {
	return gin.H{
		"connection":       mapConnection(item.Connection),
		"vmCount":          item.VMCount,
		"imageCount":       item.ImageCount,
		"flavorCount":      item.FlavorCount,
		"taskCount":        item.TaskCount,
		"pendingTaskCount": item.PendingTaskCount,
		"dockerHostCount":  item.DockerHostCount,
		"vmSamples":        item.VMSamples,
		"imageSamples":     item.ImageSamples,
		"flavorSamples":    item.FlavorSamples,
		"taskSamples":      item.TaskSamples,
		"forceRequired":    item.ForceRequired,
		"blocking":         item.Blocking,
		"blockingReasons":  item.BlockingReasons,
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
		"orphanHint":   virtualizationOrphanHint(item.Config),
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
	vm := mapVM(item.VM)
	out := gin.H{
		"vm":          vm,
		"providerRaw": item.VM.Raw,
		"operations":  operations,
		"logs":        item.Logs,
	}
	if item.Connection != nil {
		out["connection"] = mapConnection(*item.Connection)
	}
	if item.Image != nil {
		out["image"] = mapImage(*item.Image)
		vm["bootImageName"] = item.Image.Name
		vm["bootImageId"] = item.Image.ID
	}
	if item.Flavor != nil {
		out["flavor"] = mapFlavor(*item.Flavor)
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
		"orphanHint":     virtualizationOrphanHint(item.Config),
		"config":         item.Config,
		"createdAt":      item.CreatedAt,
		"updatedAt":      item.UpdatedAt,
		"allowedActions": []string{"update", "delete"},
	}
}

func virtualizationOrphanHint(config map[string]any) string {
	if hint := stringValue(config, "orphanHint"); hint != "" {
		return hint
	}
	if strings.EqualFold(stringValue(config, "source"), "sync") {
		return "provider_discovered"
	}
	return ""
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
		"payload":         sanitizeVirtualizationTaskMap(item.Payload),
		"result":          sanitizeVirtualizationTaskMap(item.Result),
		"startedAt":       item.StartedAt,
		"lastHeartbeatAt": item.LastHeartbeatAt,
		"completedAt":     item.FinishedAt,
		"createdAt":       item.CreatedAt,
		"updatedAt":       item.UpdatedAt,
		"operationState":  item.OperationState,
		"allowedActions":  operationAllowedActions(item),
	}
}

func sanitizeVirtualizationTaskMap(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		if virtualizationConfiguredFlag(key, value) {
			out[key] = value
			continue
		}
		if virtualizationSensitiveKey(key) {
			out[key+"Configured"] = value != nil && strings.TrimSpace(fmt.Sprint(value)) != ""
			continue
		}
		if nested, ok := value.(map[string]any); ok {
			out[key] = sanitizeVirtualizationTaskMap(nested)
			continue
		}
		out[key] = value
	}
	return out
}

func virtualizationSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	return normalized == "cloudinit" ||
		strings.Contains(normalized, "token") ||
		strings.Contains(normalized, "password") ||
		strings.Contains(normalized, "secret") ||
		strings.Contains(normalized, "credential") ||
		strings.Contains(normalized, "authorization")
}

func virtualizationConfiguredFlag(key string, value any) bool {
	if _, ok := value.(bool); !ok {
		return false
	}
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(key)), "configured")
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

	task, err := h.operations.GetOperation(c.Request.Context(), principal, taskID)
	if err != nil {
		writeError(c, err)
		return
	}
	if err := clearResponseWriteDeadline(c); err != nil {
		_ = c.Error(err)
		apiresponse.Error(c, http.StatusInternalServerError, "stream_unavailable", "streaming response is unavailable")
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	writeTaskEvent := func(task domainvirtualization.Task) bool {
		data, _ := json.Marshal(mapOperation(task))
		if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", data); err != nil {
			return true
		}
		c.Writer.Flush()
		return taskTerminal(task.Status)
	}

	if writeTaskEvent(task) {
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-ticker.C:
			task, err := h.operations.GetOperation(c.Request.Context(), principal, taskID)
			if err != nil {
				data, _ := json.Marshal(gin.H{"error": streamExitMessage(streamExitKindTaskUpdates)})
				_, _ = fmt.Fprintf(c.Writer, "event: error\ndata: %s\n\n", data)
				c.Writer.Flush()
				return
			}
			if writeTaskEvent(task) {
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

	result, err := h.runtime.GetVMMetrics(
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
	result, err := h.runtime.GetConsoleURL(
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
		return allowWebSocketOrigin(r)
	},
}

func (h *VirtualizationHandler) StreamVMConsole(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	vmID := c.Param("id")

	consoleResult, err := h.runtime.GetConsoleURL(c.Request.Context(), principal, vmID)
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
	defer func() { _ = conn.Close() }()
	configureWebSocketReadLimit(conn)

	if consoleResult.Type == "novnc" && consoleResult.Token != "" {
		proxyPVEVNC(c.Request.Context(), conn, firstNonEmpty(consoleResult.BackendURL, consoleResult.URL), consoleResult.Token, consoleResult)
	} else if consoleResult.Type == "vnc" {
		proxyKubeVirtVNC(c.Request.Context(), conn, firstNonEmpty(consoleResult.BackendURL, consoleResult.URL), consoleResult)
	} else {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"error":"VNC proxy not fully implemented for this provider"}`))
	}
}

func proxyPVEVNC(ctx context.Context, clientConn *websocket.Conn, backendURL, ticket string, consoleResult consoleport.ConsoleURLResult) {
	parsedURL, err := url.Parse(backendURL)
	if err != nil {
		writeWebsocketProxyError(clientConn, "invalid backend url")
		return
	}

	parsedURL.Scheme = "wss"
	if strings.HasPrefix(backendURL, "http://") {
		parsedURL.Scheme = "ws"
	}
	if ticket != "" && parsedURL.Query().Get("vncticket") == "" {
		query := parsedURL.Query()
		query.Set("vncticket", ticket)
		parsedURL.RawQuery = query.Encode()
	}

	header := http.Header{}
	header.Set("Cookie", "PVEAuthCookie="+ticket)

	dialer, err := backendWebSocketDialer(consoleResult)
	if err != nil {
		writeWebsocketProxyError(clientConn, "invalid backend TLS configuration")
		return
	}
	backendConn, response, err := dialer.DialContext(ctx, parsedURL.String(), header)
	if response != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil {
		writeWebsocketProxyError(clientConn, "backend connection failed")
		return
	}
	defer func() { _ = backendConn.Close() }()

	proxyWebsocket(ctx, clientConn, backendConn)
}

func proxyKubeVirtVNC(ctx context.Context, clientConn *websocket.Conn, backendURL string, consoleResult consoleport.ConsoleURLResult) {
	parsedURL, err := url.Parse(backendURL)
	if err != nil {
		writeWebsocketProxyError(clientConn, "invalid backend url")
		return
	}
	parsedURL.Scheme = "wss"
	if strings.HasPrefix(backendURL, "http://") {
		parsedURL.Scheme = "ws"
	}
	headers := consoleBackendHeaders(consoleResult)
	dialer, err := backendWebSocketDialer(consoleResult)
	if err != nil {
		writeWebsocketProxyError(clientConn, "invalid kubevirt backend TLS configuration")
		return
	}
	backendConn, response, err := dialer.DialContext(ctx, parsedURL.String(), headers)
	if response != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil {
		writeWebsocketProxyError(clientConn, "kubevirt backend connection failed")
		return
	}
	defer func() { _ = backendConn.Close() }()
	proxyWebsocket(ctx, clientConn, backendConn)
}

func writeWebsocketProxyError(conn *websocket.Conn, message string) {
	payload, _ := json.Marshal(gin.H{"error": message})
	_ = conn.WriteMessage(websocket.TextMessage, payload)
}

func backendWebSocketDialer(consoleResult consoleport.ConsoleURLResult) (*websocket.Dialer, error) {
	dialer := *websocket.DefaultDialer
	tlsConfig, err := consoleBackendTLSConfig(consoleResult)
	if err != nil {
		return nil, err
	}
	dialer.TLSClientConfig = tlsConfig
	return &dialer, nil
}

func consoleBackendHeaders(result consoleport.ConsoleURLResult) http.Header {
	headers := make(http.Header, len(result.BackendHeaders))
	for name, values := range result.BackendHeaders {
		headers[name] = append([]string(nil), values...)
	}
	return headers
}

func consoleBackendTLSConfig(result consoleport.ConsoleURLResult) (*tls.Config, error) {
	config := result.BackendTLS
	empty := config.ServerName == "" && !config.InsecureSkipVerify && len(config.CAData) == 0 &&
		len(config.CertData) == 0 && len(config.KeyData) == 0 && len(config.NextProtos) == 0
	if empty {
		return nil, nil
	}
	tlsConfig := &tls.Config{
		ServerName:         config.ServerName,
		InsecureSkipVerify: config.InsecureSkipVerify, //nolint:gosec // Explicit per-connection operator setting.
		NextProtos:         append([]string(nil), config.NextProtos...),
	}
	if len(config.CAData) > 0 {
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(config.CAData) {
			return nil, errors.New("virtualization backend TLS CA data is invalid")
		}
		tlsConfig.RootCAs = roots
	}
	if len(config.CertData) > 0 || len(config.KeyData) > 0 {
		if len(config.CertData) == 0 || len(config.KeyData) == 0 {
			return nil, errors.New("virtualization backend TLS client certificate and key must be provided together")
		}
		certificate, err := tls.X509KeyPair(config.CertData, config.KeyData)
		if err != nil {
			return nil, fmt.Errorf("parse virtualization backend TLS client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	return tlsConfig, nil
}

func proxyWebsocket(ctx context.Context, clientConn, backendConn *websocket.Conn) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	done := make(chan struct{}, 2)
	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			_ = clientConn.Close()
			_ = backendConn.Close()
		})
	}

	pipe := func(dst, src *websocket.Conn) {
		defer func() {
			cancel()
			closeBoth()
			done <- struct{}{}
		}()
		_ = copyWebsocketMessages(dst, src)
	}

	go pipe(clientConn, backendConn)
	go pipe(backendConn, clientConn)

	ctxDone := ctx.Done()
	for completed := 0; completed < 2; {
		select {
		case <-ctxDone:
			closeBoth()
			ctxDone = nil
		case <-done:
			completed++
		}
	}
}

func copyWebsocketMessages(dst, src *websocket.Conn) error {
	for {
		messageType, reader, err := src.NextReader()
		if err != nil {
			return err
		}
		writer, err := dst.NextWriter(messageType)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, reader)
		closeErr := writer.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
}
