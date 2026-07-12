package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/keyring"
)

type DockerOverviewService interface {
	Overview(context.Context, domainidentity.Principal) (domaindocker.Overview, error)
}

type DockerHostService interface {
	ListHosts(context.Context, domainidentity.Principal, domaindocker.HostFilter) (domaindocker.Page[domaindocker.Host], error)
	GetHost(context.Context, domainidentity.Principal, string) (domaindocker.Host, error)
	CreateHost(context.Context, domainidentity.Principal, domaindocker.HostInput) (domaindocker.Host, error)
	UpdateHost(context.Context, domainidentity.Principal, string, domaindocker.HostInput) (domaindocker.Host, error)
	DeleteHost(context.Context, domainidentity.Principal, string) error
	QuickCreateHost(context.Context, domainidentity.Principal, domaindocker.QuickCreateHostInput) (domaindocker.Operation, error)
}

type DockerProjectService interface {
	ListProjects(context.Context, domainidentity.Principal, domaindocker.ProjectFilter) (domaindocker.Page[domaindocker.Project], error)
	GetProject(context.Context, domainidentity.Principal, string) (domaindocker.Project, error)
	CreateProject(context.Context, domainidentity.Principal, domaindocker.ProjectInput) (domaindocker.Project, error)
	UpdateProject(context.Context, domainidentity.Principal, string, domaindocker.ProjectInput) (domaindocker.Project, error)
	DeleteProject(context.Context, domainidentity.Principal, string) error
	DeployProject(context.Context, domainidentity.Principal, string, string) (domaindocker.Operation, error)
	StartContainer(context.Context, domainidentity.Principal, domaindocker.ContainerStartInput) (domaindocker.Operation, error)
}

type DockerProjectRuntimeService interface {
	GetProjectLogs(context.Context, domainidentity.Principal, string, string, int) (domaindocker.ProjectRuntimeLogs, error)
	StreamProjectLogs(context.Context, domainidentity.Principal, string, string, int, io.Writer) error
	StreamProjectTerminal(context.Context, domainidentity.Principal, string, string, string, io.Reader, io.Writer, io.Writer) error
}

type DockerProjectStorageService interface {
	ListProjectVolumes(context.Context, domainidentity.Principal, string, string) ([]domaindocker.ProjectVolume, error)
	ListProjectVolumeFiles(context.Context, domainidentity.Principal, string, domaindocker.ProjectVolumeFileListInput) (domaindocker.ProjectVolumeFileList, error)
	ReadProjectVolumeFile(context.Context, domainidentity.Principal, string, domaindocker.ProjectVolumeFileReadInput) (domaindocker.ProjectVolumeFileContent, error)
}

type DockerServiceRuntimeService interface {
	ListServices(context.Context, domainidentity.Principal, domaindocker.ServiceFilter) (domaindocker.Page[domaindocker.Service], error)
	ServiceAction(context.Context, domainidentity.Principal, string, string) (domaindocker.Operation, error)
}

type DockerPortMappingService interface {
	ListPortMappings(context.Context, domainidentity.Principal, domaindocker.PortMappingFilter) (domaindocker.Page[domaindocker.PortMapping], error)
	CreatePortMapping(context.Context, domainidentity.Principal, domaindocker.PortMappingInput) (domaindocker.PortMapping, error)
	UpdatePortMapping(context.Context, domainidentity.Principal, string, domaindocker.PortMappingInput) (domaindocker.PortMapping, error)
	DeletePortMapping(context.Context, domainidentity.Principal, string) error
}

type DockerTemplateService interface {
	ListTemplates(context.Context, domainidentity.Principal, domaindocker.TemplateFilter) (domaindocker.Page[domaindocker.Template], error)
	CreateTemplate(context.Context, domainidentity.Principal, domaindocker.TemplateInput) (domaindocker.Template, error)
	UpdateTemplate(context.Context, domainidentity.Principal, string, domaindocker.TemplateInput) (domaindocker.Template, error)
	DeleteTemplate(context.Context, domainidentity.Principal, string) error
}

type DockerOperationService interface {
	ListOperations(context.Context, domainidentity.Principal, domaindocker.OperationFilter) (domaindocker.Page[domaindocker.Operation], error)
	GetOperation(context.Context, domainidentity.Principal, string) (domaindocker.Operation, error)
	ListOperationLogs(context.Context, domainidentity.Principal, string, int) ([]domaindocker.OperationLog, error)
	CancelOperation(context.Context, domainidentity.Principal, string) (domaindocker.Operation, error)
	RetryOperation(context.Context, domainidentity.Principal, string) (domaindocker.Operation, error)
}

type DockerRunnerOperationService interface {
	ClaimOperation(context.Context, domaindocker.OperationClaimInput) (domaindocker.Operation, error)
	GetOperationForRunner(context.Context, string) (domaindocker.Operation, error)
	RecordOperationCallback(context.Context, domaindocker.OperationCallbackInput) (domaindocker.Operation, error)
}

type DockerService interface {
	DockerOverviewService
	DockerHostService
	DockerProjectService
	DockerProjectRuntimeService
	DockerProjectStorageService
	DockerServiceRuntimeService
	DockerPortMappingService
	DockerTemplateService
	DockerOperationService
	DockerRunnerOperationService
}

type DockerServices struct {
	Overview         DockerOverviewService
	Hosts            DockerHostService
	Projects         DockerProjectService
	ProjectRuntime   DockerProjectRuntimeService
	ProjectStorage   DockerProjectStorageService
	Services         DockerServiceRuntimeService
	PortMappings     DockerPortMappingService
	Templates        DockerTemplateService
	Operations       DockerOperationService
	RunnerOperations DockerRunnerOperationService
}

type DockerHandler struct {
	overview         DockerOverviewService
	hosts            DockerHostService
	projects         DockerProjectService
	projectRuntime   DockerProjectRuntimeService
	projectStorage   DockerProjectStorageService
	services         DockerServiceRuntimeService
	portMappings     DockerPortMappingService
	templates        DockerTemplateService
	operations       DockerOperationService
	runnerOperations DockerRunnerOperationService
	runnerKeys       keyring.Ring
}

func NewDockerHandler(service DockerService, runnerToken ...string) *DockerHandler {
	token := ""
	if len(runnerToken) > 0 {
		token = runnerToken[0]
	}
	return NewDockerHandlerWithRunnerKeys(service, legacyRunnerKeyring(token))
}

func NewDockerHandlerWithRunnerKeys(service DockerService, keys keyring.Ring) *DockerHandler {
	return NewDockerHandlerWithServices(DockerServices{
		Overview: service, Hosts: service, Projects: service, ProjectRuntime: service,
		ProjectStorage: service, Services: service, PortMappings: service, Templates: service,
		Operations: service, RunnerOperations: service,
	}, keys)
}

func NewDockerHandlerWithServices(services DockerServices, keys keyring.Ring) *DockerHandler {
	return &DockerHandler{
		overview: services.Overview, hosts: services.Hosts, projects: services.Projects,
		projectRuntime: services.ProjectRuntime, projectStorage: services.ProjectStorage,
		services: services.Services, portMappings: services.PortMappings, templates: services.Templates,
		operations: services.Operations, runnerOperations: services.RunnerOperations, runnerKeys: keys,
	}
}

func (h *DockerHandler) Overview(c *gin.Context) {
	item, err := h.overview.Overview(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DockerHandler) ListHosts(c *gin.Context) {
	page, err := h.hosts.ListHosts(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), domaindocker.HostFilter{
		Status:       c.Query("status"),
		Search:       c.Query("search"),
		Environment:  c.Query("environment"),
		Architecture: c.Query("architecture"),
		Page:         queryInt(c, "page", 1),
		PageSize:     queryInt(c, "pageSize", 50),
		Limit:        queryLimit(c, 100),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, page)
}

func (h *DockerHandler) GetHost(c *gin.Context) {
	item, err := h.hosts.GetHost(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DockerHandler) CreateHost(c *gin.Context) {
	var req domaindocker.HostInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid docker host payload")
		return
	}
	item, err := h.hosts.CreateHost(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *DockerHandler) UpdateHost(c *gin.Context) {
	var req domaindocker.HostInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid docker host payload")
		return
	}
	item, err := h.hosts.UpdateHost(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DockerHandler) DeleteHost(c *gin.Context) {
	if err := h.hosts.DeleteHost(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *DockerHandler) QuickCreateHost(c *gin.Context) {
	var req domaindocker.QuickCreateHostInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid docker host provisioning payload")
		return
	}
	item, err := h.hosts.QuickCreateHost(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *DockerHandler) ListProjects(c *gin.Context) {
	page, err := h.projects.ListProjects(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), domaindocker.ProjectFilter{
		HostID:      c.Query("hostId"),
		Status:      c.Query("status"),
		SourceKind:  c.Query("sourceKind"),
		Search:      c.Query("search"),
		Environment: c.Query("environment"),
		Page:        queryInt(c, "page", 1),
		PageSize:    queryInt(c, "pageSize", 50),
		Limit:       queryLimit(c, 100),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, page)
}

func (h *DockerHandler) GetProject(c *gin.Context) {
	item, err := h.projects.GetProject(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DockerHandler) CreateProject(c *gin.Context) {
	var req domaindocker.ProjectInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid docker project payload")
		return
	}
	item, err := h.projects.CreateProject(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *DockerHandler) UpdateProject(c *gin.Context) {
	var req domaindocker.ProjectInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid docker project payload")
		return
	}
	item, err := h.projects.UpdateProject(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DockerHandler) DeleteProject(c *gin.Context) {
	if err := h.projects.DeleteProject(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *DockerHandler) DeployProject(c *gin.Context) {
	var req struct {
		Action string `json:"action"`
	}
	_ = c.ShouldBindJSON(&req)
	item, err := h.projects.DeployProject(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), req.Action)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *DockerHandler) StartContainer(c *gin.Context) {
	var req domaindocker.ContainerStartInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid docker container start payload")
		return
	}
	item, err := h.projects.StartContainer(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *DockerHandler) GetProjectLogs(c *gin.Context) {
	item, err := h.projectRuntime.GetProjectLogs(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), c.Query("serviceName"), queryInt(c, "tailLines", 200))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DockerHandler) StreamProjectLogs(c *gin.Context) {
	conn, err := podTerminalUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	configureWebSocketReadLimit(conn)

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	var writeMu sync.Mutex
	go func() {
		ticker := time.NewTicker(podLogPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := writeControlMessage(conn, &writeMu, websocket.PingMessage, nil); err != nil {
					cancel()
					return
				}
			}
		}
	}()

	streamErrCh := make(chan error, 1)
	go func() {
		streamErrCh <- h.projectRuntime.StreamProjectLogs(
			ctx,
			apiMiddleware.PrincipalFromContext(c),
			c.Param("id"),
			c.Query("serviceName"),
			queryInt(c, "tailLines", 100),
			&logStreamWriter{conn: conn, writeMu: &writeMu},
		)
	}()

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
			var message terminalMessage
			if err := json.Unmarshal(payload, &message); err != nil {
				continue
			}
			if message.Type == "close" {
				cancel()
				return
			}
		}
	}()

	select {
	case err := <-streamErrCh:
		_ = err
		_ = writeTerminalMessage(conn, &writeMu, terminalMessage{Type: "exit", Message: streamExitMessage(streamExitKindDockerLogs)})
	case <-readDone:
		_ = writeTerminalMessage(conn, &writeMu, terminalMessage{Type: "exit", Message: streamExitMessage(streamExitKindDockerLogs)})
	}
}

func (h *DockerHandler) StreamProjectTerminal(c *gin.Context) {
	conn, err := podTerminalUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	configureWebSocketReadLimit(conn)

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	stdinReader, stdinWriter := io.Pipe()
	defer func() { _ = stdinWriter.Close() }()

	var writeMu sync.Mutex
	_ = writeTerminalMessage(conn, &writeMu, terminalMessage{
		Type:    "status",
		Message: "docker terminal session connected",
	})

	streamErrCh := make(chan error, 1)
	go func() {
		streamErrCh <- h.projectRuntime.StreamProjectTerminal(
			ctx,
			apiMiddleware.PrincipalFromContext(c),
			c.Param("id"),
			c.Query("serviceName"),
			c.DefaultQuery("shell", "/bin/sh"),
			stdinReader,
			terminalStreamWriter{conn: conn, writeMu: &writeMu, channel: "stdout"},
			terminalStreamWriter{conn: conn, writeMu: &writeMu, channel: "stderr"},
		)
	}()

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		defer func() { _ = stdinWriter.Close() }()
		for {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
			var message terminalMessage
			if err := json.Unmarshal(payload, &message); err != nil {
				_ = writeTerminalMessage(conn, &writeMu, terminalMessage{Type: "status", Message: "ignored invalid terminal message"})
				continue
			}
			switch message.Type {
			case "input":
				if _, err := io.WriteString(stdinWriter, message.Data); err != nil {
					cancel()
					return
				}
			case "close":
				cancel()
				return
			}
		}
	}()

	select {
	case streamErr := <-streamErrCh:
		cancel()
		_ = streamErr
		_ = writeTerminalMessage(conn, &writeMu, terminalMessage{Type: "exit", Message: streamExitMessage(streamExitKindDockerTerminal)})
	case <-readDone:
		cancel()
	}
}

func (h *DockerHandler) ListProjectVolumes(c *gin.Context) {
	items, err := h.projectStorage.ListProjectVolumes(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), c.Query("serviceName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *DockerHandler) ListProjectVolumeFiles(c *gin.Context) {
	item, err := h.projectStorage.ListProjectVolumeFiles(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), domaindocker.ProjectVolumeFileListInput{
		ServiceName: c.Query("serviceName"),
		Target:      c.Query("target"),
		Path:        c.Query("path"),
		Limit:       queryInt(c, "limit", 200),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DockerHandler) ReadProjectVolumeFile(c *gin.Context) {
	item, err := h.projectStorage.ReadProjectVolumeFile(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), domaindocker.ProjectVolumeFileReadInput{
		ServiceName: c.Query("serviceName"),
		Target:      c.Query("target"),
		Path:        c.Query("path"),
		LimitBytes:  int64(queryInt(c, "limitBytes", 262144)),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DockerHandler) ListServices(c *gin.Context) {
	page, err := h.services.ListServices(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), domaindocker.ServiceFilter{
		HostID:    c.Query("hostId"),
		ProjectID: c.Query("projectId"),
		Status:    c.Query("status"),
		Search:    c.Query("search"),
		Page:      queryInt(c, "page", 1),
		PageSize:  queryInt(c, "pageSize", 50),
		Limit:     queryLimit(c, 100),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, page)
}

func (h *DockerHandler) ServiceAction(c *gin.Context) {
	var req struct {
		Action string `json:"action"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid docker service action payload")
		return
	}
	item, err := h.services.ServiceAction(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), req.Action)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *DockerHandler) ListPortMappings(c *gin.Context) {
	page, err := h.portMappings.ListPortMappings(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), domaindocker.PortMappingFilter{
		HostID:    c.Query("hostId"),
		ProjectID: c.Query("projectId"),
		ServiceID: c.Query("serviceId"),
		Status:    c.Query("status"),
		Search:    c.Query("search"),
		Page:      queryInt(c, "page", 1),
		PageSize:  queryInt(c, "pageSize", 50),
		Limit:     queryLimit(c, 100),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, page)
}

func (h *DockerHandler) CreatePortMapping(c *gin.Context) {
	var req domaindocker.PortMappingInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid docker port mapping payload")
		return
	}
	item, err := h.portMappings.CreatePortMapping(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *DockerHandler) UpdatePortMapping(c *gin.Context) {
	var req domaindocker.PortMappingInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid docker port mapping payload")
		return
	}
	item, err := h.portMappings.UpdatePortMapping(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DockerHandler) DeletePortMapping(c *gin.Context) {
	if err := h.portMappings.DeletePortMapping(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *DockerHandler) ListTemplates(c *gin.Context) {
	filter := domaindocker.TemplateFilter{
		Kind:     c.Query("kind"),
		Search:   c.Query("search"),
		Page:     queryInt(c, "page", 1),
		PageSize: queryInt(c, "pageSize", 50),
		Limit:    queryLimit(c, 100),
	}
	if value := strings.TrimSpace(c.Query("enabled")); value != "" {
		enabled := value == "true" || value == "1"
		filter.Enabled = &enabled
	}
	page, err := h.templates.ListTemplates(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), filter)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, page)
}

func (h *DockerHandler) CreateTemplate(c *gin.Context) {
	var req domaindocker.TemplateInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid docker template payload")
		return
	}
	item, err := h.templates.CreateTemplate(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *DockerHandler) UpdateTemplate(c *gin.Context) {
	var req domaindocker.TemplateInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid docker template payload")
		return
	}
	item, err := h.templates.UpdateTemplate(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DockerHandler) DeleteTemplate(c *gin.Context) {
	if err := h.templates.DeleteTemplate(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *DockerHandler) ListOperations(c *gin.Context) {
	filter := domaindocker.OperationFilter{
		HostID:        c.Query("hostId"),
		ProjectID:     c.Query("projectId"),
		ServiceID:     c.Query("serviceId"),
		Status:        c.Query("status"),
		OperationKind: c.Query("operationKind"),
		Abnormal:      queryBool(c, "abnormal"),
		Pending:       queryBool(c, "pending"),
		Search:        c.Query("search"),
		Page:          queryInt(c, "page", 1),
		PageSize:      queryInt(c, "pageSize", 50),
		Limit:         queryLimit(c, 100),
	}
	if statuses := strings.TrimSpace(c.Query("statuses")); statuses != "" {
		filter.Statuses = strings.Split(statuses, ",")
	}
	page, err := h.operations.ListOperations(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), filter)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, page)
}

func (h *DockerHandler) GetOperation(c *gin.Context) {
	item, err := h.operations.GetOperation(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DockerHandler) ListOperationLogs(c *gin.Context) {
	items, err := h.operations.ListOperationLogs(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"), queryLimit(c, 200))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *DockerHandler) CancelOperation(c *gin.Context) {
	item, err := h.operations.CancelOperation(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DockerHandler) RetryOperation(c *gin.Context) {
	item, err := h.operations.RetryOperation(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *DockerHandler) ClaimOperation(c *gin.Context) {
	if !authorizeDockerRunnerKeys(c, h.runnerKeys) {
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid docker runner token")
		return
	}
	var req domaindocker.OperationClaimInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid docker operation claim payload")
		return
	}
	item, err := h.runnerOperations.ClaimOperation(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *DockerHandler) GetOperationRunnerStatus(c *gin.Context) {
	if !authorizeDockerRunnerKeys(c, h.runnerKeys) {
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid docker runner token")
		return
	}
	item, err := h.runnerOperations.GetOperationForRunner(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DockerHandler) RecordOperationCallback(c *gin.Context) {
	if !authorizeDockerRunnerKeys(c, h.runnerKeys) {
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid docker runner token")
		return
	}
	var req domaindocker.OperationCallbackInput
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid docker operation callback payload")
		return
	}
	item, err := h.runnerOperations.RecordOperationCallback(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}
