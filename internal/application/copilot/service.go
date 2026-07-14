package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainevent "github.com/opensoha/soha/internal/domain/event"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmcp "github.com/opensoha/soha/internal/domain/mcp"
	domainrelease "github.com/opensoha/soha/internal/domain/release"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
	aperrors "github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/runtimeobs"
	"github.com/opensoha/soha/internal/platform/telemetry"
	"go.uber.org/zap"
)

type SessionStore interface {
	ListSessions(context.Context, string, int) ([]domaincopilot.Session, error)
	GetSession(context.Context, string, string) (domaincopilot.Session, error)
	CreateSession(context.Context, domaincopilot.Session) (domaincopilot.Session, error)
	UpdateSession(context.Context, string, string, domaincopilot.Session) (domaincopilot.Session, error)
	DeleteSession(context.Context, string, string) error
}

type MessageStore interface {
	ListMessages(context.Context, string, int) ([]domaincopilot.Message, error)
	CreateMessage(context.Context, domaincopilot.Message) (domaincopilot.Message, error)
	UpdateMessageMetadata(context.Context, string, string, map[string]any) (domaincopilot.Message, error)
}

type DataSourceStore interface {
	ListDataSources(context.Context) ([]domaincopilot.DataSource, error)
	GetDataSource(context.Context, string) (domaincopilot.DataSource, error)
	CreateDataSource(context.Context, domaincopilot.DataSource) (domaincopilot.DataSource, error)
	UpdateDataSource(context.Context, string, domaincopilot.DataSourceInput) (domaincopilot.DataSource, error)
	UpdateDataSourceValidation(context.Context, string, string, string, time.Time) (domaincopilot.DataSource, error)
}

type AnalysisProfileStore interface {
	ListAnalysisProfiles(context.Context) ([]domaincopilot.AnalysisProfile, error)
	GetAnalysisProfile(context.Context, string) (domaincopilot.AnalysisProfile, error)
	CreateAnalysisProfile(context.Context, domaincopilot.AnalysisProfile) (domaincopilot.AnalysisProfile, error)
	UpdateAnalysisProfile(context.Context, string, domaincopilot.AnalysisProfileInput) (domaincopilot.AnalysisProfile, error)
}

type AutomationPolicyStore interface {
	ListAutomationPolicies(context.Context) ([]domaincopilot.AutomationPolicy, error)
	CreateAutomationPolicy(context.Context, domaincopilot.AutomationPolicy) (domaincopilot.AutomationPolicy, error)
	UpdateAutomationPolicy(context.Context, string, domaincopilot.AutomationPolicyInput) (domaincopilot.AutomationPolicy, error)
	DeleteAutomationPolicy(context.Context, string) error
}

type RootCauseRunStore interface {
	ListRootCauseRuns(context.Context, string, domaincopilot.RootCauseRunFilter) ([]domaincopilot.RootCauseRun, error)
	GetRootCauseRun(context.Context, string, string) (domaincopilot.RootCauseRun, error)
	CreateRootCauseRun(context.Context, domaincopilot.RootCauseRun) (domaincopilot.RootCauseRun, error)
	UpdateRootCauseRun(context.Context, domaincopilot.RootCauseRun) (domaincopilot.RootCauseRun, error)
}

// AgentRunStore owns the complete claim/callback/cancel state machine.
type AgentRunStore interface {
	ListAgentRuns(context.Context, domaincopilot.AgentRunFilter) ([]domaincopilot.AgentRun, error)
	GetAgentRun(context.Context, string, string) (domaincopilot.AgentRun, error)
	CreateAgentRun(context.Context, domaincopilot.AgentRun) (domaincopilot.AgentRun, error)
	ClaimAgentRun(context.Context, domaincopilot.AgentRunClaimInput) (domaincopilot.AgentRun, error)
	UpdateAgentRunCallback(context.Context, domaincopilot.AgentRunCallbackInput) (domaincopilot.AgentRun, error)
	CancelAgentRun(context.Context, domaincopilot.AgentRunCancelInput) (domaincopilot.AgentRun, error)
}

// InspectionTaskStore owns task scheduling and its last-run checkpoint.
type InspectionTaskStore interface {
	ListInspectionTasks(context.Context, string, int) ([]domaincopilot.InspectionTask, error)
	GetInspectionTask(context.Context, string, string) (domaincopilot.InspectionTask, error)
	ListDueInspectionTasks(context.Context, time.Time, int) ([]domaincopilot.InspectionTask, error)
	CreateInspectionTask(context.Context, domaincopilot.InspectionTask) (domaincopilot.InspectionTask, error)
	UpdateInspectionTask(context.Context, string, string, domaincopilot.InspectionTaskInput) (domaincopilot.InspectionTask, error)
	DeleteInspectionTask(context.Context, string, string) error
	TouchInspectionTaskRun(context.Context, string, time.Time) error
}

type InspectionRunStore interface {
	ListInspectionRuns(context.Context, string, domaincopilot.InspectionRunFilter) ([]domaincopilot.InspectionRun, error)
	CreateInspectionRun(context.Context, domaincopilot.InspectionRun) (domaincopilot.InspectionRun, error)
}

type ClusterReader interface {
	List(context.Context) ([]domaincluster.Summary, error)
}

type AlertReader interface {
	Summary(context.Context, domainidentity.Principal) (domainalert.Summary, error)
	ListAlerts(context.Context, domainidentity.Principal, domainalert.Filter) ([]domainalert.Instance, error)
	ListChannels(context.Context, domainidentity.Principal) ([]domainalert.NotificationChannel, error)
}

type EventReader interface {
	List(context.Context, int) ([]domainevent.Envelope, error)
}

type AuditReader interface {
	List(context.Context, domainaudit.Filter) ([]domainaudit.Entry, error)
	Record(context.Context, domainaudit.Entry) error
}

type ApplicationReader interface {
	List(context.Context, domainapp.Filter) ([]domainapp.App, error)
}

type BuildReader interface {
	List(context.Context, domainbuild.Filter) ([]domainbuild.Record, error)
}

type ReleaseReader interface {
	List(context.Context, domainrelease.Filter) ([]domainrelease.Record, error)
}

type ExecutionTaskReader interface {
	ListExecutionTasks(context.Context, domainidentity.Principal, domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error)
}

type PlatformResourceReader interface {
	ListNodes(context.Context, domainidentity.Principal, string) ([]domainresource.NodeView, error)
	ListPods(context.Context, domainidentity.Principal, string, string) ([]domainresource.PodView, error)
	ListDeployments(context.Context, domainidentity.Principal, string, string) ([]domainresource.DeploymentView, error)
	ListServices(context.Context, domainidentity.Principal, string, string) ([]domainresource.ServiceView, error)
}

type DockerReader interface {
	ListOperations(context.Context, domainidentity.Principal, domaindocker.OperationFilter) (domaindocker.Page[domaindocker.Operation], error)
	ListServices(context.Context, domainidentity.Principal, domaindocker.ServiceFilter) (domaindocker.Page[domaindocker.Service], error)
}

type VirtualizationReader interface {
	ListOperations(context.Context, domainidentity.Principal, domainvirtualization.TaskFilter) ([]domainvirtualization.Task, error)
}

type OnCallResolver interface {
	ResolveOnCall(context.Context, domainidentity.Principal, domainalert.OnCallResolveInput) (map[string]any, error)
}

type AISettingsResolver interface {
	ResolveAIWorkbenchSettings(context.Context) (domainsettings.AIWorkbenchModelSettings, error)
	ResolveAISkillsRegistry(context.Context) ([]domainsettings.AISkillSettings, error)
}

type WorkbenchModelInvoker interface {
	InvokeWorkbenchModel(context.Context, domainidentity.Principal, appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error)
}

type WorkbenchModelStreamInvoker interface {
	InvokeWorkbenchModelStream(context.Context, domainidentity.Principal, appaigateway.WorkbenchRelayRequest, func(appaigateway.WorkbenchRelayStreamDelta) bool) (appaigateway.WorkbenchRelayResponse, error)
}

type WorkbenchContextBuilder interface {
	BuildForCopilot(context.Context, domainidentity.Principal, domaincopilot.ContextBuildInput) (domaincopilot.ContextEnvelope, error)
}

type LogTelemetry interface {
	Validate(string, map[string]any) error
	Correlate(context.Context, string, string, map[string]any, telemetry.LogCorrelationQuery) (telemetry.LogCorrelationResult, error)
}

type MetricTelemetry interface {
	Analyze(context.Context, string, string, map[string]any, telemetry.MetricRangeQuery) (telemetry.MetricAnomalySummary, error)
}

type TraceTelemetry interface {
	FindSlowSpans(context.Context, string, string, map[string]any, telemetry.TraceQuery) (telemetry.TraceResult, error)
}

type chatReply struct {
	Content      string
	Source       string
	Model        string
	ProviderID   string
	ProviderKind string
	Error        string
}

type chatProviderMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type recentMessageRepository interface {
	ListRecentMessages(context.Context, string, int) ([]domaincopilot.Message, error)
}

type Service struct {
	sessions              SessionStore
	messages              MessageStore
	dataSources           DataSourceStore
	analysisProfiles      AnalysisProfileStore
	automationPolicies    AutomationPolicyStore
	rootCauseRuns         RootCauseRunStore
	agentRuns             AgentRunStore
	inspectionTasks       InspectionTaskStore
	inspectionRuns        InspectionRunStore
	clusters              ClusterReader
	alerts                AlertReader
	events                EventReader
	audits                AuditReader
	apps                  ApplicationReader
	builds                BuildReader
	releases              ReleaseReader
	execution             ExecutionTaskReader
	resources             PlatformResourceReader
	docker                DockerReader
	virtualization        VirtualizationReader
	oncall                OnCallResolver
	settings              AISettingsResolver
	workbenchInvoker      WorkbenchModelInvoker
	permissions           *appaccess.PermissionResolver
	logger                *zap.Logger
	metrics               *runtimeobs.Registry
	inspectionParallelism int
	mcpRegistry           MCPRegistry
	agentProvidersMu      sync.RWMutex
	agentProviders        []domaincopilot.AgentProvider
	logs                  LogTelemetry
	metricTelemetry       MetricTelemetry
	traceTelemetry        TraceTelemetry
	contextBuilder        WorkbenchContextBuilder
}

type MCPRegistry interface {
	List() []domainmcp.Adapter
	Get(string) (domainmcp.Adapter, bool)
}

type Option func(*Service)

type Dependencies struct {
	Sessions           SessionStore
	Messages           MessageStore
	DataSources        DataSourceStore
	AnalysisProfiles   AnalysisProfileStore
	AutomationPolicies AutomationPolicyStore
	RootCauseRuns      RootCauseRunStore
	AgentRuns          AgentRunStore
	InspectionTasks    InspectionTaskStore
	InspectionRuns     InspectionRunStore
	Clusters           ClusterReader
	Alerts             AlertReader
	Events             EventReader
	Audits             AuditReader
	Applications       ApplicationReader
	Builds             BuildReader
	Releases           ReleaseReader
	Settings           AISettingsResolver
	Permissions        *appaccess.PermissionResolver
}

func WithTelemetryBackends(logs LogTelemetry, metrics MetricTelemetry, traces TraceTelemetry) Option {
	return func(service *Service) {
		if logs != nil {
			service.logs = logs
		}
		if metrics != nil {
			service.metricTelemetry = metrics
		}
		if traces != nil {
			service.traceTelemetry = traces
		}
	}
}

func New(deps Dependencies, options ...Option) (*Service, error) {
	required := []struct {
		name  string
		value any
	}{
		{"sessions", deps.Sessions},
		{"messages", deps.Messages},
		{"data sources", deps.DataSources},
		{"analysis profiles", deps.AnalysisProfiles},
		{"automation policies", deps.AutomationPolicies},
		{"root cause runs", deps.RootCauseRuns},
		{"agent runs", deps.AgentRuns},
		{"inspection tasks", deps.InspectionTasks},
		{"inspection runs", deps.InspectionRuns},
	}
	for _, dependency := range required {
		if isNilDependency(dependency.value) {
			return nil, fmt.Errorf("copilot service: %s dependency is required", dependency.name)
		}
	}
	service := &Service{
		sessions:              deps.Sessions,
		messages:              deps.Messages,
		dataSources:           deps.DataSources,
		analysisProfiles:      deps.AnalysisProfiles,
		automationPolicies:    deps.AutomationPolicies,
		rootCauseRuns:         deps.RootCauseRuns,
		agentRuns:             deps.AgentRuns,
		inspectionTasks:       deps.InspectionTasks,
		inspectionRuns:        deps.InspectionRuns,
		clusters:              deps.Clusters,
		alerts:                deps.Alerts,
		events:                deps.Events,
		audits:                deps.Audits,
		apps:                  deps.Applications,
		builds:                deps.Builds,
		releases:              deps.Releases,
		settings:              deps.Settings,
		permissions:           deps.Permissions,
		inspectionParallelism: 2,
		logs:                  unavailableTelemetry{},
		metricTelemetry:       unavailableTelemetry{},
		traceTelemetry:        unavailableTelemetry{},
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service, nil
}

// MustNew constructs a service for static bootstrap wiring and panics on a missing dependency.
func MustNew(deps Dependencies, options ...Option) *Service {
	service, err := New(deps, options...)
	if err != nil {
		panic(err)
	}
	return service
}

func isNilDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (s *Service) SetInstrumentation(logger *zap.Logger, metrics *runtimeobs.Registry) {
	s.logger = logger
	s.metrics = metrics
}

func (s *Service) SetContextBuilder(builder WorkbenchContextBuilder) {
	s.contextBuilder = builder
}

func (s *Service) logBackend() LogTelemetry {
	if s.logs != nil {
		return s.logs
	}
	return unavailableTelemetry{}
}

func (s *Service) metricBackend() MetricTelemetry {
	if s.metricTelemetry != nil {
		return s.metricTelemetry
	}
	return unavailableTelemetry{}
}

func (s *Service) traceBackend() TraceTelemetry {
	if s.traceTelemetry != nil {
		return s.traceTelemetry
	}
	return unavailableTelemetry{}
}

type unavailableTelemetry struct{}

func (unavailableTelemetry) Validate(string, map[string]any) error {
	return errors.New("log telemetry backend is not configured")
}

func (unavailableTelemetry) Correlate(context.Context, string, string, map[string]any, telemetry.LogCorrelationQuery) (telemetry.LogCorrelationResult, error) {
	return telemetry.LogCorrelationResult{}, errors.New("log telemetry backend is not configured")
}

func (unavailableTelemetry) Analyze(context.Context, string, string, map[string]any, telemetry.MetricRangeQuery) (telemetry.MetricAnomalySummary, error) {
	return telemetry.MetricAnomalySummary{}, errors.New("metric telemetry backend is not configured")
}

func (unavailableTelemetry) FindSlowSpans(context.Context, string, string, map[string]any, telemetry.TraceQuery) (telemetry.TraceResult, error) {
	return telemetry.TraceResult{}, errors.New("trace telemetry backend is not configured")
}

func (s *Service) SetInspectionParallelism(parallelism int) {
	if parallelism > 0 {
		s.inspectionParallelism = parallelism
	}
}

func (s *Service) SetMCPRegistry(registry MCPRegistry) {
	s.mcpRegistry = registry
}

func (s *Service) SetAgentProviders(providers []domaincopilot.AgentProvider) {
	s.agentProvidersMu.Lock()
	defer s.agentProvidersMu.Unlock()
	s.agentProviders = append([]domaincopilot.AgentProvider(nil), providers...)
}

// SetExternalAgentProviders preserves the governed in-process provider while
// replacing legacy external defaults with the plugin catalog projection.
func (s *Service) SetExternalAgentProviders(providers []domaincopilot.AgentProvider) {
	configured := make([]domaincopilot.AgentProvider, 0, len(providers)+1)
	for _, provider := range defaultAgentProviders() {
		if provider.Kind == agentProviderInternal {
			configured = append(configured, provider)
			break
		}
	}
	configured = append(configured, providers...)
	s.agentProvidersMu.Lock()
	defer s.agentProvidersMu.Unlock()
	s.agentProviders = configured
}

func (s *Service) SetWorkbenchModelInvoker(invoker WorkbenchModelInvoker) {
	s.workbenchInvoker = invoker
}

func (s *Service) SetAgentRuntimeReaders(execution ExecutionTaskReader, resources PlatformResourceReader, docker DockerReader, virtualization VirtualizationReader, oncall OnCallResolver) {
	s.execution = execution
	s.resources = resources
	s.docker = docker
	s.virtualization = virtualization
	s.oncall = oncall
}

func (s *Service) logWarn(message string, fields ...zap.Field) {
	if s.logger != nil {
		s.logger.Warn(message, fields...)
	}
}

func (s *Service) logDebug(message string, fields ...zap.Field) {
	if s.logger != nil {
		s.logger.Debug(message, fields...)
	}
}

func (s *Service) ListSessions(ctx context.Context, principal domainidentity.Principal) ([]domaincopilot.Session, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return nil, err
	}
	return s.sessions.ListSessions(ctx, principal.UserID, 20)
}

func (s *Service) ListAnalysisRuns(ctx context.Context, principal domainidentity.Principal, filter domaincopilot.RootCauseRunFilter) ([]domaincopilot.RootCauseRun, error) {
	return s.ListRootCauseRuns(ctx, principal, filter)
}

func (s *Service) GetSession(ctx context.Context, principal domainidentity.Principal, sessionID string) (domaincopilot.Session, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincopilot.Session{}, err
	}
	return s.sessions.GetSession(ctx, principal.UserID, strings.TrimSpace(sessionID))
}

func (s *Service) CreateSession(ctx context.Context, principal domainidentity.Principal, title, mode, agentProviderID string, scope, pinnedContext map[string]any, source string, tags []string, locale string) (domaincopilot.Session, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincopilot.Session{}, err
	}
	if strings.TrimSpace(title) == "" {
		title = localize(locale, "新的会话", "New Chat")
	}
	metadata := sessionMetadataMap(domaincopilot.SessionMetadata{
		Mode:            normalizeSessionMode(mode),
		Status:          "active",
		AgentProviderID: normalizeAgentProviderID(agentProviderID),
		Scope:           scopeFromMap(scope),
		PinnedContext:   compactMetadataMap(pinnedContext),
		Tags:            normalizeStringList(tags),
		Source:          normalizeSessionSource(source),
	})
	metadata["locale"] = normalizeLocale(locale)
	session, err := s.sessions.CreateSession(ctx, domaincopilot.Session{
		ID:        uuid.NewString(),
		Title:     strings.TrimSpace(title),
		CreatedBy: principal.UserID,
		Metadata:  metadata,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		return domaincopilot.Session{}, err
	}
	s.recordGlobalAssistantAudit(ctx, principal, domaincopilot.WorkbenchGlobalAssistantEventInput{
		Action:    "open",
		SessionID: session.ID,
		Source:    stringValue(metadata["source"]),
	}, parseSessionMetadata(session.Metadata), "success")
	return session, nil
}

func (s *Service) UpdateSession(ctx context.Context, principal domainidentity.Principal, sessionID string, title, mode, agentProviderID, status, summary string, scope, pinnedContext map[string]any, source string, toolset map[string]any, tags []string, archived bool) (domaincopilot.Session, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincopilot.Session{}, err
	}
	current, err := s.sessions.GetSession(ctx, principal.UserID, strings.TrimSpace(sessionID))
	if err != nil {
		return domaincopilot.Session{}, err
	}
	metadata := parseSessionMetadata(current.Metadata)
	if strings.TrimSpace(mode) != "" {
		metadata.Mode = normalizeSessionMode(mode)
	}
	if strings.TrimSpace(agentProviderID) != "" {
		metadata.AgentProviderID = normalizeAgentProviderID(agentProviderID)
	}
	if strings.TrimSpace(status) != "" {
		metadata.Status = strings.TrimSpace(status)
	}
	if strings.TrimSpace(summary) != "" {
		metadata.Summary = strings.TrimSpace(summary)
	}
	if len(tags) > 0 {
		metadata.Tags = normalizeStringList(tags)
	}
	if scope != nil {
		metadata.Scope = scopeFromMap(scope)
	}
	if pinnedContext != nil {
		metadata.PinnedContext = compactMetadataMap(pinnedContext)
	}
	if strings.TrimSpace(source) != "" {
		metadata.Source = normalizeSessionSource(source)
	}
	if toolset != nil {
		metadata.Toolset = toolsetFromMap(toolset)
	}
	if archived {
		metadata.ArchivedAt = time.Now().UTC().Format(time.RFC3339)
		metadata.Status = "archived"
	}
	if strings.TrimSpace(title) != "" {
		current.Title = strings.TrimSpace(title)
	}
	current.Metadata = sessionMetadataMap(metadata)
	current.UpdatedAt = time.Now().UTC()
	return s.sessions.UpdateSession(ctx, principal.UserID, current.ID, current)
}

func (s *Service) DeleteSession(ctx context.Context, principal domainidentity.Principal, sessionID string) error {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return err
	}
	return s.sessions.DeleteSession(ctx, principal.UserID, strings.TrimSpace(sessionID))
}

func (s *Service) ListMessages(ctx context.Context, principal domainidentity.Principal, sessionID string) ([]domaincopilot.Message, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return nil, err
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	if _, err := s.sessions.GetSession(ctx, principal.UserID, trimmedSessionID); err != nil {
		return nil, err
	}
	messages, err := s.messages.ListMessages(ctx, trimmedSessionID, 100)
	if err != nil {
		return nil, err
	}
	messages = s.migrateLegacyPlatformContextMessages(ctx, trimmedSessionID, messages)
	return messages, nil
}

func (s *Service) SendMessage(ctx context.Context, principal domainidentity.Principal, sessionID, content, locale string) (domaincopilot.SessionMessageEnvelope, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	session, err := s.sessions.GetSession(ctx, principal.UserID, strings.TrimSpace(sessionID))
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	sessionMeta := parseSessionMetadata(session.Metadata)
	return s.sendMessageWithSessionConfig(ctx, principal, session, sessionMeta, content, locale)
}

func (s *Service) sendMessageWithSessionConfig(ctx context.Context, principal domainidentity.Principal, session domaincopilot.Session, sessionMeta domaincopilot.SessionMetadata, content, locale string) (domaincopilot.SessionMessageEnvelope, error) {
	locale = detectMessageLocale(content, locale)
	userMessage, err := s.messages.CreateMessage(ctx, domaincopilot.Message{
		ID:        uuid.NewString(),
		SessionID: session.ID,
		Role:      "user",
		Content:   strings.TrimSpace(content),
		Metadata:  map[string]any{"userId": principal.UserID, "locale": locale, "mode": sessionMeta.Mode},
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	toolCalls := make([]domaincopilot.ToolExecution, 0)
	artifacts := make([]domaincopilot.AnalysisArtifact, 0)
	sessionPatch := map[string]any{}
	if normalizeSessionMode(sessionMeta.Mode) != "general" {
		toolCalls, artifacts, sessionPatch = s.analyzeConversation(ctx, principal, session, content, locale)
	}
	reply := chatReply{}
	if len(artifacts) > 0 {
		reply = chatReply{Content: artifacts[0].Summary, Source: "analysis-artifact"}
	} else {
		priorMessages, _ := s.listRecentMessages(ctx, session.ID, 20)
		providerMessages, contextMeta, contextErr := s.groundedProviderMessages(ctx, principal, session, sessionMeta, priorMessages, userMessage, locale)
		if contextErr != nil {
			return domaincopilot.SessionMessageEnvelope{}, contextErr
		}
		reply = s.generateReply(ctx, principal, session.ID, sessionMeta.Mode, providerMessages, locale)
		if contextMeta != nil {
			sessionPatch["contextEnvelope"] = contextMeta
		}
	}
	assistantMetadata := map[string]any{
		"mode":              sessionMeta.Mode,
		"source":            reply.Source,
		"locale":            locale,
		"analysisArtifacts": []domaincopilot.AnalysisArtifact{},
	}
	if reply.Model != "" {
		assistantMetadata["model"] = reply.Model
	}
	if reply.Error != "" {
		assistantMetadata["error"] = reply.Error
	}
	if len(artifacts) > 0 {
		assistantMetadata["analysisArtifacts"] = artifacts
	}
	if contextMeta, ok := sessionPatch["contextEnvelope"]; ok {
		assistantMetadata["contextEnvelope"] = contextMeta
	}
	agentStatus := map[string]any{
		"status":       replyAgentStatus(reply),
		"providerId":   firstNonEmpty(reply.ProviderID, agentProviderInternal),
		"providerKind": firstNonEmpty(reply.ProviderKind, "internal"),
	}
	assistantMetadata = finalWorkbenchMessageMetadata(assistantMetadata, toolCalls, artifacts, agentStatus)
	assistantMessage, err := s.messages.CreateMessage(ctx, domaincopilot.Message{
		ID:        uuid.NewString(),
		SessionID: session.ID,
		Role:      "assistant",
		Content:   reply.Content,
		Metadata:  assistantMetadata,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	if len(sessionPatch) > 0 {
		merged := parseSessionMetadata(session.Metadata)
		if summary, ok := sessionPatch["summary"].(string); ok && strings.TrimSpace(summary) != "" {
			merged.Summary = strings.TrimSpace(summary)
		}
		if refs, ok := sessionPatch["analysisRunRefs"].([]domaincopilot.AnalysisRunRef); ok && len(refs) > 0 {
			merged.AnalysisRunRefs = refs
		}
		session.Metadata = sessionMetadataMap(merged)
		session.UpdatedAt = time.Now().UTC()
		_, _ = s.sessions.UpdateSession(ctx, principal.UserID, session.ID, session)
	}
	envelope := domaincopilot.SessionMessageEnvelope{
		Messages:          []domaincopilot.Message{userMessage, assistantMessage},
		ToolCalls:         toolCalls,
		AnalysisArtifacts: artifacts,
		SessionPatch:      sessionPatch,
	}
	s.recordGlobalAssistantAudit(ctx, principal, domaincopilot.WorkbenchGlobalAssistantEventInput{
		Action:    "send",
		SessionID: session.ID,
		Source:    sessionMeta.Source,
		Prompt:    content,
	}, sessionMeta, "success")
	return envelope, nil
}

func (s *Service) RunSessionAnalysis(ctx context.Context, principal domainidentity.Principal, sessionID string, input domaincopilot.RootCauseRunInput, locale string) (domaincopilot.SessionMessageEnvelope, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	session, err := s.sessions.GetSession(ctx, principal.UserID, strings.TrimSpace(sessionID))
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	metadata := parseSessionMetadata(session.Metadata)
	scope := mergeSessionScope(metadata.Scope, metadata.Toolset.ScopeOverrides)
	runCreatedBy, runTriggerType, runDedupKey := sessionAnalysisRunPersistence(session.ID, metadata)
	if strings.TrimSpace(input.ClusterID) != "" ||
		strings.TrimSpace(input.Namespace) != "" ||
		strings.TrimSpace(input.WorkloadName) != "" ||
		strings.TrimSpace(input.AlertID) != "" ||
		input.TimeRangeMinutes > 0 {
		scope = mergeSessionScope(scope, inputScopeMap(input))
	}
	mode := normalizeSessionMode(input.Kind)
	if mode == "general" && strings.TrimSpace(metadata.Mode) != "" {
		mode = normalizeSessionMode(metadata.Mode)
	}
	if !isRunnableSessionAnalysisMode(mode) {
		return domaincopilot.SessionMessageEnvelope{}, fmt.Errorf("%w: analyze mode must be root_cause, performance, trace, or inspection_review", aperrors.ErrInvalidArgument)
	}
	providerID := firstNonEmpty(input.AgentProviderID, metadata.AgentProviderID, agentProviderInternal)
	if s.shouldUseExternalAgent(providerID) {
		return s.queueSessionAgentAnalysis(ctx, principal, session, mode, providerID, scope, metadata.Toolset, input, locale)
	}
	result, err := s.runLocalSessionAnalysis(ctx, localSessionAnalysisRequest{
		principal:   principal,
		session:     session,
		metadata:    metadata,
		scope:       scope,
		input:       input,
		locale:      locale,
		mode:        mode,
		createdBy:   runCreatedBy,
		triggerType: runTriggerType,
		dedupKey:    runDedupKey,
	})
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	toolCalls := result.toolCalls
	artifacts := result.artifacts
	refs := result.refs
	sessionPatch := map[string]any{}
	if len(artifacts) > 0 {
		sessionPatch["mode"] = mode
		sessionPatch["summary"] = artifacts[0].Summary
		sessionPatch["analysisRunRefs"] = refs
	}
	reply := localize(locale, "已执行显式分析，但当前没有生成新的分析工件。", "Explicit analysis completed, but no new analysis artifact was produced.")
	if len(artifacts) > 0 {
		reply = artifacts[0].Summary
	}
	assistantMessage, err := s.messages.CreateMessage(ctx, domaincopilot.Message{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      "assistant",
		Content:   reply,
		Metadata: finalWorkbenchMessageMetadata(map[string]any{
			"mode":              mode,
			"source":            "explicit-analysis",
			"locale":            locale,
			"analysisArtifacts": artifacts,
		}, toolCalls, artifacts, map[string]any{
			"status":       "succeeded",
			"providerId":   firstNonEmpty(input.AgentProviderID, metadata.AgentProviderID, agentProviderInternal),
			"providerKind": providerKindOrInternalApp(firstNonEmpty(input.AgentProviderID, metadata.AgentProviderID, agentProviderInternal)),
			"runId":        firstStreamArtifactRunID(artifacts),
		}),
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	if len(sessionPatch) > 0 {
		merged := applySessionAnalysisPatch(parseSessionMetadata(session.Metadata), sessionPatch)
		session.Metadata = sessionMetadataMap(merged)
		session.UpdatedAt = time.Now().UTC()
		_, _ = s.sessions.UpdateSession(ctx, principal.UserID, session.ID, session)
	}
	return domaincopilot.SessionMessageEnvelope{
		Messages:          []domaincopilot.Message{assistantMessage},
		ToolCalls:         toolCalls,
		AnalysisArtifacts: artifacts,
		SessionPatch:      sessionPatch,
	}, nil
}

type localSessionAnalysisResult struct {
	toolCalls []domaincopilot.ToolExecution
	artifacts []domaincopilot.AnalysisArtifact
	refs      []domaincopilot.AnalysisRunRef
}

type localSessionAnalysisRequest struct {
	principal   domainidentity.Principal
	session     domaincopilot.Session
	metadata    domaincopilot.SessionMetadata
	scope       domaincopilot.SessionScope
	input       domaincopilot.RootCauseRunInput
	locale      string
	mode        string
	createdBy   string
	triggerType string
	dedupKey    string
}

func (s *Service) runLocalSessionAnalysis(ctx context.Context, request localSessionAnalysisRequest) (localSessionAnalysisResult, error) {
	result := localSessionAnalysisResult{
		toolCalls: make([]domaincopilot.ToolExecution, 0),
		artifacts: make([]domaincopilot.AnalysisArtifact, 0),
		refs:      append([]domaincopilot.AnalysisRunRef{}, request.metadata.AnalysisRunRefs...),
	}
	var err error
	switch request.mode {
	case "root_cause":
		var run domaincopilot.RootCauseRun
		var calls []domaincopilot.ToolExecution
		var artifact domaincopilot.AnalysisArtifact
		run, calls, artifact, err = s.runSessionRootCause(ctx, request.principal, request.session.ID, request.scope, request.metadata.Toolset, request.input.Question, request.locale)
		result.toolCalls = append(result.toolCalls, calls...)
		result.artifacts = append(result.artifacts, artifact)
		result.refs = append(result.refs, domaincopilot.AnalysisRunRef{ID: run.ID, Kind: run.Kind, Status: run.Status, CreatedAt: run.CreatedAt.Format(time.RFC3339)})
	case "performance":
		result, err = s.runPerformanceSessionAnalysis(ctx, request, result)
	case "trace":
		result, err = s.runTraceSessionAnalysis(ctx, request, result)
	case "inspection_review":
		calls, artifact := s.runSessionInspectionReview(request.session.ID, request.scope, request.metadata.Toolset, request.input.Question, request.locale)
		result = appendCompletedSessionAnalysis(result, calls, artifact)
	default:
		result.toolCalls, result.artifacts, _ = s.analyzeConversation(ctx, request.principal, domaincopilot.Session{
			ID:        request.session.ID,
			Title:     request.session.Title,
			CreatedBy: request.session.CreatedBy,
			Metadata:  sessionMetadataMap(domaincopilot.SessionMetadata{Mode: request.mode, Status: "active", Scope: request.scope}),
			CreatedAt: request.session.CreatedAt,
			UpdatedAt: request.session.UpdatedAt,
		}, request.input.Question, request.locale)
	}
	if err != nil {
		return localSessionAnalysisResult{}, err
	}
	return result, nil
}

func (s *Service) runPerformanceSessionAnalysis(ctx context.Context, request localSessionAnalysisRequest, result localSessionAnalysisResult) (localSessionAnalysisResult, error) {
	calls, artifact, err := s.runSessionPerformance(ctx, request.session.ID, request.scope, request.metadata.Toolset, request.input.Question, request.createdBy, request.triggerType, request.dedupKey)
	if err != nil {
		return localSessionAnalysisResult{}, err
	}
	return appendCompletedSessionAnalysis(result, calls, artifact), nil
}

func (s *Service) runTraceSessionAnalysis(ctx context.Context, request localSessionAnalysisRequest, result localSessionAnalysisResult) (localSessionAnalysisResult, error) {
	calls, artifact, err := s.runSessionTrace(ctx, request.session.ID, request.scope, request.metadata.Toolset, request.input.Question, request.createdBy, request.triggerType, request.dedupKey)
	if err != nil {
		return localSessionAnalysisResult{}, err
	}
	return appendCompletedSessionAnalysis(result, calls, artifact), nil
}

func appendCompletedSessionAnalysis(result localSessionAnalysisResult, calls []domaincopilot.ToolExecution, artifact domaincopilot.AnalysisArtifact) localSessionAnalysisResult {
	result.toolCalls = append(result.toolCalls, calls...)
	result.artifacts = append(result.artifacts, artifact)
	result.refs = append(result.refs, domaincopilot.AnalysisRunRef{ID: artifact.RunID, Kind: artifact.Kind, Status: "completed", CreatedAt: time.Now().UTC().Format(time.RFC3339)})
	return result
}

func (s *Service) queueSessionAgentAnalysis(ctx context.Context, principal domainidentity.Principal, session domaincopilot.Session, mode, providerID string, scope domaincopilot.SessionScope, toolset domaincopilot.SessionToolset, input domaincopilot.RootCauseRunInput, locale string) (domaincopilot.SessionMessageEnvelope, error) {
	if normalizeSessionMode(mode) == "root_cause" {
		return s.queueSessionRootCauseAgentAnalysis(ctx, principal, session, providerID, scope, toolset, input, locale)
	}
	capabilityID := normalizeAnalysisKind(mode)
	if capabilityID == "" {
		capabilityID = mode
	}
	run, err := s.createAgentRun(ctx, principal, domaincopilot.AgentRunInput{
		ProviderID:   providerID,
		CapabilityID: capabilityID,
		SkillIDs:     toolset.EnabledSkillIDs,
		SessionID:    session.ID,
		CreatedBy:    principal.UserID,
		Scope:        scope,
		Toolset:      toolset,
		Input: map[string]any{
			"question":          input.Question,
			"mode":              mode,
			"analysisProfileId": input.AnalysisProfileID,
			"triggerType":       normalizedTriggerType(input.TriggerType),
			"locale":            normalizeLocale(locale),
			"sessionId":         session.ID,
			"title":             session.Title,
			"capabilityId":      capabilityID,
		},
		TimeoutSeconds: budgetInt(toolset, "timeoutSeconds", 600),
	})
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	artifact := domaincopilot.AnalysisArtifact{
		Kind:    capabilityID,
		RunID:   run.ID,
		Title:   fmt.Sprintf("%s analysis queued", capabilityID),
		Summary: localize(locale, "已提交给 Agent Runtime，等待外部 agent runner 领取并回写结果。", "Queued for Agent Runtime. Waiting for an external agent runner to claim the task and write back results."),
		Scope:   scope,
		DataSourceSnapshot: map[string]any{
			"providerId":     run.ProviderID,
			"providerKind":   run.ProviderKind,
			"capabilityId":   run.CapabilityID,
			"skillIds":       run.SkillIDs,
			"sessionId":      session.ID,
			"agentRunId":     run.ID,
			"agentRuntimeId": run.ID,
			"analysisRunId":  run.ID,
			"analysisKind":   capabilityID,
			"status":         run.Status,
		},
	}
	message, err := s.messages.CreateMessage(ctx, domaincopilot.Message{
		ID:        uuid.NewString(),
		SessionID: session.ID,
		Role:      "assistant",
		Content:   artifact.Summary,
		Metadata: finalWorkbenchMessageMetadata(map[string]any{
			"mode":              mode,
			"source":            "agent-runtime",
			"locale":            normalizeLocale(locale),
			"agentRunId":        run.ID,
			"agentProviderId":   run.ProviderID,
			"analysisArtifacts": []domaincopilot.AnalysisArtifact{artifact},
		}, nil, []domaincopilot.AnalysisArtifact{artifact}, map[string]any{
			"status":       run.Status,
			"providerId":   run.ProviderID,
			"providerKind": run.ProviderKind,
			"runId":        run.ID,
			"agentRunId":   run.ID,
		}),
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	metadata := parseSessionMetadata(session.Metadata)
	metadata.AgentProviderID = run.ProviderID
	metadata.Mode = mode
	metadata.AnalysisRunRefs = append(metadata.AnalysisRunRefs, domaincopilot.AnalysisRunRef{
		ID:        run.ID,
		Kind:      run.CapabilityID,
		Status:    run.Status,
		CreatedAt: run.CreatedAt.Format(time.RFC3339),
	})
	session.Metadata = sessionMetadataMap(metadata)
	session.UpdatedAt = time.Now().UTC()
	_, _ = s.sessions.UpdateSession(ctx, principal.UserID, session.ID, session)
	return domaincopilot.SessionMessageEnvelope{
		Messages:          []domaincopilot.Message{message},
		AnalysisArtifacts: []domaincopilot.AnalysisArtifact{artifact},
		SessionPatch: map[string]any{
			"mode":            mode,
			"agentProviderId": run.ProviderID,
			"analysisRunRefs": metadata.AnalysisRunRefs,
			"agentRunId":      run.ID,
		},
	}, nil
}

func (s *Service) queueSessionRootCauseAgentAnalysis(ctx context.Context, principal domainidentity.Principal, session domaincopilot.Session, providerID string, scope domaincopilot.SessionScope, toolset domaincopilot.SessionToolset, input domaincopilot.RootCauseRunInput, locale string) (domaincopilot.SessionMessageEnvelope, error) {
	metadata := parseSessionMetadata(session.Metadata)
	rootCauseCreatedBy, triggerType, dedupKey := sessionAnalysisRunPersistence(session.ID, metadata)
	mergedInput := input
	mergedInput.Kind = "root_cause"
	mergedInput.AgentProviderID = providerID
	mergedInput.SessionID = session.ID
	mergedInput.TriggerType = firstNonEmpty(input.TriggerType, triggerType)
	mergedInput.ClusterID = firstNonEmpty(input.ClusterID, scope.ClusterID)
	mergedInput.Namespace = firstNonEmpty(input.Namespace, scope.Namespace)
	mergedInput.WorkloadName = firstNonEmpty(input.WorkloadName, scope.Workload)
	mergedInput.AlertID = firstNonEmpty(input.AlertID, scope.AlertID)
	if mergedInput.TimeRangeMinutes <= 0 {
		mergedInput.TimeRangeMinutes = scope.TimeRangeMinutes
	}
	queued, err := s.queueRootCauseAgentRunWithToolset(ctx, principal, rootCauseCreatedBy, principal.UserID, mergedInput, dedupKey, toolset, locale)
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	run := queued.AgentRun
	rootRun := queued.RootCauseRun
	artifact := domaincopilot.AnalysisArtifact{
		Kind:    "root_cause",
		RunID:   rootRun.ID,
		Title:   rootRun.Title,
		Summary: rootRun.Summary,
		Scope: domaincopilot.SessionScope{
			ClusterID:        rootRun.ClusterID,
			Namespace:        rootRun.Namespace,
			Workload:         rootRun.WorkloadName,
			AlertID:          rootRun.AlertID,
			TimeRangeMinutes: rootRun.TimeRangeMinutes,
		},
		Recommendations:    rootRun.Recommendations,
		DataSourceSnapshot: rootRun.DataSourceSnapshot,
	}
	message, err := s.messages.CreateMessage(ctx, domaincopilot.Message{
		ID:        uuid.NewString(),
		SessionID: session.ID,
		Role:      "assistant",
		Content:   artifact.Summary,
		Metadata: finalWorkbenchMessageMetadata(map[string]any{
			"mode":              "root_cause",
			"source":            "agent-runtime",
			"locale":            normalizeLocale(locale),
			"agentRunId":        run.ID,
			"rootCauseRunId":    rootRun.ID,
			"agentProviderId":   run.ProviderID,
			"analysisArtifacts": []domaincopilot.AnalysisArtifact{artifact},
		}, rootRun.ToolExecutions, []domaincopilot.AnalysisArtifact{artifact}, map[string]any{
			"status":         run.Status,
			"providerId":     run.ProviderID,
			"providerKind":   run.ProviderKind,
			"runId":          rootRun.ID,
			"agentRunId":     run.ID,
			"rootCauseRunId": rootRun.ID,
		}),
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	metadata.AgentProviderID = run.ProviderID
	metadata.Mode = "root_cause"
	metadata.AnalysisRunRefs = append(metadata.AnalysisRunRefs, domaincopilot.AnalysisRunRef{
		ID:        rootRun.ID,
		Kind:      rootRun.Kind,
		Status:    rootRun.Status,
		CreatedAt: rootRun.CreatedAt.Format(time.RFC3339),
	})
	session.Metadata = sessionMetadataMap(metadata)
	session.UpdatedAt = time.Now().UTC()
	_, _ = s.sessions.UpdateSession(ctx, principal.UserID, session.ID, session)
	return domaincopilot.SessionMessageEnvelope{
		Messages:          []domaincopilot.Message{message},
		AnalysisArtifacts: []domaincopilot.AnalysisArtifact{artifact},
		SessionPatch: map[string]any{
			"mode":             "root_cause",
			"agentProviderId":  run.ProviderID,
			"analysisRunRefs":  metadata.AnalysisRunRefs,
			"agentRunId":       run.ID,
			"rootCauseRunId":   rootRun.ID,
			"rootCauseRunKind": rootRun.Kind,
		},
	}, nil
}

func isRunnableSessionAnalysisMode(mode string) bool {
	switch normalizeSessionMode(mode) {
	case "root_cause", "performance", "trace", "inspection_review":
		return true
	default:
		return false
	}
}

func inputScopeMap(input domaincopilot.RootCauseRunInput) map[string]any {
	return map[string]any{
		"clusterId":        input.ClusterID,
		"namespace":        input.Namespace,
		"workload":         input.WorkloadName,
		"alertId":          input.AlertID,
		"timeRangeMinutes": input.TimeRangeMinutes,
	}
}

func applySessionAnalysisPatch(metadata domaincopilot.SessionMetadata, patch map[string]any) domaincopilot.SessionMetadata {
	if value, ok := patch["mode"].(string); ok && strings.TrimSpace(value) != "" {
		metadata.Mode = normalizeSessionMode(value)
	}
	if summary, ok := patch["summary"].(string); ok && strings.TrimSpace(summary) != "" {
		metadata.Summary = strings.TrimSpace(summary)
	}
	if refs, ok := patch["analysisRunRefs"].([]domaincopilot.AnalysisRunRef); ok && len(refs) > 0 {
		metadata.AnalysisRunRefs = refs
	}
	return metadata
}

func (s *Service) Insights(ctx context.Context, principal domainidentity.Principal, locale string) ([]domaincopilot.Insight, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIView); err != nil {
		return nil, err
	}
	clusters, _ := s.clusters.List(ctx)
	alertSummary, _ := s.alerts.Summary(ctx, principal)
	builds, _ := s.builds.List(ctx, domainbuild.Filter{Limit: 5})
	apps, _ := s.apps.List(ctx, domainapp.Filter{Limit: 200})

	healthy := 0
	degraded := 0
	for _, item := range clusters {
		if item.Health.Status == "healthy" || item.Health.Status == "ok" {
			healthy++
		} else {
			degraded++
		}
	}

	insights := []domaincopilot.Insight{
		{
			Title:       localize(locale, "集群态势", "Fleet posture"),
			Description: localize(locale, fmt.Sprintf("当前平台可见 %d 个健康集群、%d 个异常集群。", healthy, degraded), fmt.Sprintf("%d healthy clusters, %d degraded clusters currently visible to the platform.", healthy, degraded)),
			Severity:    ternarySeverity(degraded > 0, "warning", "info"),
			Actions: []string{
				localize(locale, "在集群详情里检查异常集群", "Review degraded clusters in Cluster Detail"),
				localize(locale, "查看最近事件和审计活动", "Check recent events and audit activity"),
			},
		},
		{
			Title:       localize(locale, "告警压力", "Alert pressure"),
			Description: localize(locale, fmt.Sprintf("当前有 %d 条触发中的告警、%d 条严重告警、%d 个已登记应用。", alertSummary.FiringCount, alertSummary.CriticalCount, len(apps)), fmt.Sprintf("%d firing alerts, %d critical alerts, %d registered applications.", alertSummary.FiringCount, alertSummary.CriticalCount, len(apps))),
			Severity:    ternarySeverity(alertSummary.CriticalCount > 0, "warning", "info"),
			Actions: []string{
				localize(locale, "打开告警中心查看当前事件", "Open Alerts to review current incidents"),
				localize(locale, "交叉检查受影响的应用", "Cross-check impacted applications"),
			},
		},
		{
			Title:       localize(locale, "构建队列", "Build queue"),
			Description: localize(locale, fmt.Sprintf("当前已存储 %d 条最近构建记录，应用注册中心已经支持手动触发构建。", len(builds)), fmt.Sprintf("%d recent build records are stored. Manual build trigger is now available from the application registry.", len(builds))),
			Severity:    "info",
			Actions: []string{
				localize(locale, "打开应用页并触发一次构建", "Open Applications and trigger a build"),
				localize(locale, "检查最近的构建活动", "Inspect build records for recent activity"),
			},
		},
	}
	return insights, nil
}

func (s *Service) resolveAIWorkbenchSettings(ctx context.Context) (domainsettings.AIWorkbenchModelSettings, error) {
	if s.settings == nil {
		return domainsettings.AIWorkbenchModelSettings{}, fmt.Errorf("ai workbench model settings unavailable")
	}
	return s.settings.ResolveAIWorkbenchSettings(ctx)
}

func (s *Service) listRecentMessages(ctx context.Context, sessionID string, limit int) ([]domaincopilot.Message, error) {
	if repo, ok := s.messages.(recentMessageRepository); ok {
		return repo.ListRecentMessages(ctx, sessionID, limit)
	}
	messages, err := s.messages.ListMessages(ctx, sessionID, 100)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || len(messages) <= limit {
		return messages, nil
	}
	return messages[len(messages)-limit:], nil
}

func (s *Service) generateReply(ctx context.Context, principal domainidentity.Principal, sessionID, mode string, messages []chatProviderMessage, locale string) chatReply {
	workbenchModel, err := s.resolveAIWorkbenchSettings(ctx)
	if err != nil {
		return chatReply{
			Content: localize(locale,
				"当前 AI Workbench 模型设置不可用。请先在 AI Gateway 中配置模型路由，并在 AI 设置中选择默认模型。",
				"AI Workbench model settings are unavailable. Configure a model route in AI Gateway and select the default model in AI settings.",
			),
			Source: "model-unconfigured",
			Error:  err.Error(),
		}
	}
	if !workbenchModel.Enabled || (strings.TrimSpace(workbenchModel.DefaultPublicModel) == "" && strings.TrimSpace(workbenchModel.DefaultRouteID) == "") {
		return chatReply{
			Content: localize(locale,
				"当前没有可用的 AI Workbench 默认模型。请在 AI Gateway 的模型路由中接入 Provider，并在 AI 设置里选择默认模型或路由。",
				"No AI Workbench default model is available. Add the provider through AI Gateway model routes and select a default model or route in AI settings.",
			),
			Source: "model-unconfigured",
			Model:  workbenchModel.DefaultPublicModel,
		}
	}
	if s.workbenchInvoker == nil {
		return chatReply{
			Content: localize(locale,
				"AI Workbench 默认模型已选择，但内部 Gateway relay invoker 尚未接入。请检查服务启动接线。",
				"AI Workbench default model is selected, but the internal Gateway relay invoker is not wired. Check service wiring.",
			),
			Source: "model-unconfigured",
			Model:  workbenchModel.DefaultPublicModel,
		}
	}
	resp, err := s.workbenchInvoker.InvokeWorkbenchModel(ctx, principal, appaigateway.WorkbenchRelayRequest{
		PublicModel: workbenchModel.DefaultPublicModel,
		RouteID:     workbenchModel.DefaultRouteID,
		Endpoint:    workbenchModel.DefaultEndpoint,
		Messages:    mapWorkbenchRelayMessages(messages),
		SessionID:   sessionID,
		Mode:        normalizeSessionMode(mode),
	})
	if err != nil {
		return chatReply{
			Content: localize(locale,
				"AI Workbench 模型调用失败。请检查 AI Gateway 的模型路由、上游状态、权限和限流配置。",
				"AI Workbench model call failed. Check AI Gateway model routes, upstream health, permissions, and rate limits.",
			),
			Source: "model-error",
			Model:  workbenchModel.DefaultPublicModel,
			Error:  err.Error(),
		}
	}
	return chatReply{
		Content:      resp.Content,
		Source:       "gateway-model-route",
		Model:        firstNonEmpty(resp.PublicModel, workbenchModel.DefaultPublicModel),
		ProviderID:   agentProviderInternal,
		ProviderKind: firstNonEmpty(resp.ProviderKind, workbenchModelProviderKindFromEndpoint(workbenchModel.DefaultEndpoint)),
	}
}

func (s *Service) generateReplyStream(ctx context.Context, principal domainidentity.Principal, sessionID, mode string, messages []chatProviderMessage, locale string, onDelta func(string) bool) chatReply {
	workbenchModel, err := s.resolveAIWorkbenchSettings(ctx)
	if err != nil {
		return chatReply{
			Content: localize(locale,
				"当前 AI Workbench 模型设置不可用。请先在 AI Gateway 中配置模型路由，并在 AI 设置中选择默认模型。",
				"AI Workbench model settings are unavailable. Configure a model route in AI Gateway and select a default model in AI settings.",
			),
			Source: "model-unconfigured",
			Error:  err.Error(),
		}
	}
	if !workbenchModel.Enabled || (strings.TrimSpace(workbenchModel.DefaultPublicModel) == "" && strings.TrimSpace(workbenchModel.DefaultRouteID) == "") {
		return chatReply{
			Content: localize(locale,
				"当前没有可用的 AI Workbench 默认模型。请在 AI Gateway 的模型路由中接入 Provider，并在 AI 设置里选择默认模型或路由。",
				"No AI Workbench default model is available. Add the provider through AI Gateway model routes and select a default model or route in AI settings.",
			),
			Source: "model-unconfigured",
			Model:  workbenchModel.DefaultPublicModel,
		}
	}
	streamInvoker, ok := s.workbenchInvoker.(WorkbenchModelStreamInvoker)
	if !ok || streamInvoker == nil {
		return s.generateReply(ctx, principal, sessionID, mode, messages, locale)
	}
	resp, err := streamInvoker.InvokeWorkbenchModelStream(ctx, principal, appaigateway.WorkbenchRelayRequest{
		PublicModel: workbenchModel.DefaultPublicModel,
		RouteID:     workbenchModel.DefaultRouteID,
		Endpoint:    workbenchModel.DefaultEndpoint,
		Messages:    mapWorkbenchRelayMessages(messages),
		SessionID:   sessionID,
		Mode:        normalizeSessionMode(mode),
	}, func(delta appaigateway.WorkbenchRelayStreamDelta) bool {
		if delta.ContentDelta == "" {
			return true
		}
		if onDelta == nil {
			return true
		}
		return onDelta(delta.ContentDelta)
	})
	if err != nil {
		if errors.Is(err, appaigateway.ErrWorkbenchRelayStreamStopped) || ctx.Err() != nil {
			return chatReply{
				Source:       "model-cancelled",
				Model:        workbenchModel.DefaultPublicModel,
				ProviderID:   agentProviderInternal,
				ProviderKind: workbenchModelProviderKindFromEndpoint(workbenchModel.DefaultEndpoint),
				Error:        err.Error(),
			}
		}
		return chatReply{
			Content: localize(locale,
				"AI Workbench 模型调用失败。请检查 AI Gateway 的模型路由、上游状态、权限和限流配置。",
				"AI Workbench model call failed. Check AI Gateway model routes, upstream health, permissions, and rate limits.",
			),
			Source: "model-error",
			Model:  workbenchModel.DefaultPublicModel,
			Error:  err.Error(),
		}
	}
	return chatReply{
		Content:      resp.Content,
		Source:       "gateway-model-route-stream",
		Model:        firstNonEmpty(resp.PublicModel, workbenchModel.DefaultPublicModel),
		ProviderID:   agentProviderInternal,
		ProviderKind: firstNonEmpty(resp.ProviderKind, workbenchModelProviderKindFromEndpoint(workbenchModel.DefaultEndpoint)),
	}
}

func mapWorkbenchRelayMessages(messages []chatProviderMessage) []appaigateway.WorkbenchRelayMessage {
	out := make([]appaigateway.WorkbenchRelayMessage, 0, len(messages))
	for _, message := range messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role != "system" && role != "user" && role != "assistant" {
			continue
		}
		out = append(out, appaigateway.WorkbenchRelayMessage{
			Role:    role,
			Content: strings.TrimSpace(message.Content),
		})
	}
	return out
}

func systemPrompt(locale string) string {
	if locale == "zh-CN" {
		return "你是 soha 的通用 AI 聊天助手。直接回答用户问题，保持简洁、自然、可执行。不要声称已经读取平台上下文、数据库、告警、集群或工具结果，除非用户消息或系统显式提供了这些内容。"
	}
	return "You are soha's general AI chat assistant. Answer the user's question directly, concisely, and naturally. Do not claim access to platform context, databases, alerts, clusters, or tool results unless they were explicitly provided in the conversation."
}

func buildProviderChatMessages(history []domaincopilot.Message, current domaincopilot.Message, locale string) []chatProviderMessage {
	messages := []chatProviderMessage{{Role: "system", Content: systemPrompt(locale)}}
	for _, item := range history {
		if current.ID != "" && item.ID == current.ID {
			continue
		}
		if isLegacyPlatformContextMessage(item) || messageHasLegacyPlatformContextMarkers(item.Metadata) {
			continue
		}
		role := strings.TrimSpace(item.Role)
		if role != "assistant" && role != "user" {
			continue
		}
		content := strings.TrimSpace(item.Content)
		if content == "" {
			continue
		}
		messages = append(messages, chatProviderMessage{Role: role, Content: content})
	}
	if strings.TrimSpace(current.Content) != "" {
		messages = append(messages, chatProviderMessage{Role: "user", Content: strings.TrimSpace(current.Content)})
	}
	return messages
}

func markLegacyPlatformContextMessages(messages []domaincopilot.Message) []domaincopilot.Message {
	out := append([]domaincopilot.Message(nil), messages...)
	for index := range out {
		if !isLegacyPlatformContextMessage(out[index]) {
			continue
		}
		metadata := copyMessageMetadata(out[index].Metadata)
		metadata["source"] = "legacy-platform-context"
		metadata["legacyFallback"] = true
		metadata["hiddenInGeneralChat"] = true
		out[index].Metadata = metadata
	}
	return out
}

func (s *Service) migrateLegacyPlatformContextMessages(ctx context.Context, sessionID string, messages []domaincopilot.Message) []domaincopilot.Message {
	out := append([]domaincopilot.Message(nil), messages...)
	if s == nil || s.messages == nil || strings.TrimSpace(sessionID) == "" {
		return markLegacyPlatformContextMessages(out)
	}
	for index := range out {
		if messageHasLegacyPlatformContextMarkers(out[index].Metadata) {
			continue
		}
		if !isLegacyPlatformContextMessage(out[index]) {
			continue
		}
		metadata := copyMessageMetadata(out[index].Metadata)
		metadata["source"] = "legacy-platform-context"
		metadata["legacyFallback"] = true
		metadata["hiddenInGeneralChat"] = true
		if _, err := s.messages.UpdateMessageMetadata(ctx, sessionID, out[index].ID, metadata); err != nil {
			continue
		}
		out[index].Metadata = metadata
	}
	return markLegacyPlatformContextMessages(out)
}

func copyMessageMetadata(input map[string]any) map[string]any {
	out := make(map[string]any, len(input)+3)
	for key, value := range input {
		out[key] = value
	}
	return out
}

func messageHasLegacyPlatformContextMarkers(metadata map[string]any) bool {
	if metadata == nil {
		return false
	}
	if legacyFallback, ok := metadata["legacyFallback"].(bool); ok && legacyFallback {
		return true
	}
	if hiddenInGeneralChat, ok := metadata["hiddenInGeneralChat"].(bool); ok && hiddenInGeneralChat {
		return true
	}
	return false
}

func isLegacyPlatformContextMessage(message domaincopilot.Message) bool {
	if message.Role != "assistant" {
		return false
	}
	if messageHasAnalysisArtifacts(message.Metadata) {
		return false
	}
	source := strings.TrimSpace(fmt.Sprint(message.Metadata["source"]))
	content := strings.TrimSpace(message.Content)
	return source == "platform-context" ||
		strings.HasPrefix(content, "当前平台上下文：") ||
		strings.HasPrefix(content, "当前集群上下文：") ||
		strings.HasPrefix(content, "当前构建上下文：") ||
		strings.HasPrefix(content, "当前告警上下文：") ||
		strings.HasPrefix(content, "当前审计上下文：") ||
		strings.HasPrefix(content, "Current platform context:") ||
		strings.HasPrefix(content, "Current clusters context:") ||
		strings.HasPrefix(content, "Current builds context:") ||
		strings.HasPrefix(content, "Current alerts context:") ||
		strings.HasPrefix(content, "Current audit context:")
}

func messageHasAnalysisArtifacts(metadata map[string]any) bool {
	raw, ok := metadata["analysisArtifacts"]
	if !ok || raw == nil {
		return false
	}
	switch value := raw.(type) {
	case []domaincopilot.AnalysisArtifact:
		return len(value) > 0
	case []any:
		return len(value) > 0
	case json.RawMessage:
		return len(value) > 2 && string(value) != "null"
	case string:
		trimmed := strings.TrimSpace(value)
		return trimmed != "" && trimmed != "[]" && trimmed != "null"
	default:
		return fmt.Sprint(value) != "" && fmt.Sprint(value) != "[]"
	}
}

func ternarySeverity(condition bool, truthy, falsy string) string {
	if condition {
		return truthy
	}
	return falsy
}
