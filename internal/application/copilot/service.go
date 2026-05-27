package copilot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
	domainalert "github.com/kubecrux/kubecrux/internal/domain/alert"
	domainapp "github.com/kubecrux/kubecrux/internal/domain/application"
	domainaudit "github.com/kubecrux/kubecrux/internal/domain/audit"
	domainbuild "github.com/kubecrux/kubecrux/internal/domain/build"
	domaincluster "github.com/kubecrux/kubecrux/internal/domain/cluster"
	domaincopilot "github.com/kubecrux/kubecrux/internal/domain/copilot"
	domainevent "github.com/kubecrux/kubecrux/internal/domain/event"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainmcp "github.com/kubecrux/kubecrux/internal/domain/mcp"
	domainrelease "github.com/kubecrux/kubecrux/internal/domain/release"
	domainsettings "github.com/kubecrux/kubecrux/internal/domain/settings"
	aperrors "github.com/kubecrux/kubecrux/internal/platform/apperrors"
	"github.com/kubecrux/kubecrux/internal/platform/runtimeobs"
	"go.uber.org/zap"
)

type Repository interface {
	ListSessions(context.Context, string, int) ([]domaincopilot.Session, error)
	GetSession(context.Context, string, string) (domaincopilot.Session, error)
	CreateSession(context.Context, domaincopilot.Session) (domaincopilot.Session, error)
	UpdateSession(context.Context, string, string, domaincopilot.Session) (domaincopilot.Session, error)
	DeleteSession(context.Context, string, string) error
	ListMessages(context.Context, string, int) ([]domaincopilot.Message, error)
	CreateMessage(context.Context, domaincopilot.Message) (domaincopilot.Message, error)
	ListDataSources(context.Context) ([]domaincopilot.DataSource, error)
	GetDataSource(context.Context, string) (domaincopilot.DataSource, error)
	CreateDataSource(context.Context, domaincopilot.DataSource) (domaincopilot.DataSource, error)
	UpdateDataSource(context.Context, string, domaincopilot.DataSourceInput) (domaincopilot.DataSource, error)
	UpdateDataSourceValidation(context.Context, string, string, string, time.Time) (domaincopilot.DataSource, error)
	ListAnalysisProfiles(context.Context) ([]domaincopilot.AnalysisProfile, error)
	GetAnalysisProfile(context.Context, string) (domaincopilot.AnalysisProfile, error)
	CreateAnalysisProfile(context.Context, domaincopilot.AnalysisProfile) (domaincopilot.AnalysisProfile, error)
	UpdateAnalysisProfile(context.Context, string, domaincopilot.AnalysisProfileInput) (domaincopilot.AnalysisProfile, error)
	ListAutomationPolicies(context.Context) ([]domaincopilot.AutomationPolicy, error)
	CreateAutomationPolicy(context.Context, domaincopilot.AutomationPolicy) (domaincopilot.AutomationPolicy, error)
	UpdateAutomationPolicy(context.Context, string, domaincopilot.AutomationPolicyInput) (domaincopilot.AutomationPolicy, error)
	DeleteAutomationPolicy(context.Context, string) error
	ListRootCauseRuns(context.Context, string, domaincopilot.RootCauseRunFilter) ([]domaincopilot.RootCauseRun, error)
	GetRootCauseRun(context.Context, string, string) (domaincopilot.RootCauseRun, error)
	CreateRootCauseRun(context.Context, domaincopilot.RootCauseRun) (domaincopilot.RootCauseRun, error)
	UpdateRootCauseRun(context.Context, domaincopilot.RootCauseRun) (domaincopilot.RootCauseRun, error)
	ListAgentRuns(context.Context, domaincopilot.AgentRunFilter) ([]domaincopilot.AgentRun, error)
	GetAgentRun(context.Context, string, string) (domaincopilot.AgentRun, error)
	CreateAgentRun(context.Context, domaincopilot.AgentRun) (domaincopilot.AgentRun, error)
	ClaimAgentRun(context.Context, domaincopilot.AgentRunClaimInput) (domaincopilot.AgentRun, error)
	UpdateAgentRunCallback(context.Context, domaincopilot.AgentRunCallbackInput) (domaincopilot.AgentRun, error)
	ListInspectionTasks(context.Context, string, int) ([]domaincopilot.InspectionTask, error)
	GetInspectionTask(context.Context, string, string) (domaincopilot.InspectionTask, error)
	ListDueInspectionTasks(context.Context, time.Time, int) ([]domaincopilot.InspectionTask, error)
	CreateInspectionTask(context.Context, domaincopilot.InspectionTask) (domaincopilot.InspectionTask, error)
	UpdateInspectionTask(context.Context, string, string, domaincopilot.InspectionTaskInput) (domaincopilot.InspectionTask, error)
	DeleteInspectionTask(context.Context, string, string) error
	TouchInspectionTaskRun(context.Context, string, time.Time) error
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

type AISettingsResolver interface {
	ResolveAISettings(context.Context) (domainsettings.AISettings, error)
}

type Service struct {
	repo                  Repository
	clusters              ClusterReader
	alerts                AlertReader
	events                EventReader
	audits                AuditReader
	apps                  ApplicationReader
	builds                BuildReader
	releases              ReleaseReader
	settings              AISettingsResolver
	permissions           *appaccess.PermissionResolver
	http                  *http.Client
	logger                *zap.Logger
	metrics               *runtimeobs.Registry
	inspectionParallelism int
	mcpRegistry           MCPRegistry
	agentProviders        []domaincopilot.AgentProvider
}

type MCPRegistry interface {
	List() []domainmcp.Adapter
	Get(string) (domainmcp.Adapter, bool)
}

func New(repo Repository, clusters ClusterReader, alerts AlertReader, events EventReader, audits AuditReader, apps ApplicationReader, builds BuildReader, releases ReleaseReader, settings AISettingsResolver, permissions *appaccess.PermissionResolver) *Service {
	return &Service{
		repo:                  repo,
		clusters:              clusters,
		alerts:                alerts,
		events:                events,
		audits:                audits,
		apps:                  apps,
		builds:                builds,
		releases:              releases,
		settings:              settings,
		permissions:           permissions,
		http:                  &http.Client{Timeout: 30 * time.Second},
		inspectionParallelism: 2,
	}
}

func (s *Service) SetInstrumentation(logger *zap.Logger, metrics *runtimeobs.Registry) {
	s.logger = logger
	s.metrics = metrics
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
	s.agentProviders = append([]domaincopilot.AgentProvider(nil), providers...)
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
	return s.repo.ListSessions(ctx, principal.UserID, 20)
}

func (s *Service) ListAnalysisRuns(ctx context.Context, principal domainidentity.Principal, filter domaincopilot.RootCauseRunFilter) ([]domaincopilot.RootCauseRun, error) {
	return s.ListRootCauseRuns(ctx, principal, filter)
}

func (s *Service) GetSession(ctx context.Context, principal domainidentity.Principal, sessionID string) (domaincopilot.Session, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincopilot.Session{}, err
	}
	return s.repo.GetSession(ctx, principal.UserID, strings.TrimSpace(sessionID))
}

func (s *Service) CreateSession(ctx context.Context, principal domainidentity.Principal, title, mode, agentProviderID string, scope map[string]any, tags []string, locale string) (domaincopilot.Session, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincopilot.Session{}, err
	}
	if strings.TrimSpace(title) == "" {
		title = localize(locale, "新的调查会话", "New Investigation")
	}
	metadata := sessionMetadataMap(domaincopilot.SessionMetadata{
		Mode:            normalizeSessionMode(mode),
		Status:          "active",
		AgentProviderID: normalizeAgentProviderID(agentProviderID),
		Scope:           scopeFromMap(scope),
		Tags:            normalizeStringList(tags),
		Source:          "manual",
	})
	metadata["locale"] = normalizeLocale(locale)
	return s.repo.CreateSession(ctx, domaincopilot.Session{
		ID:        uuid.NewString(),
		Title:     strings.TrimSpace(title),
		CreatedBy: principal.UserID,
		Metadata:  metadata,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
}

func (s *Service) UpdateSession(ctx context.Context, principal domainidentity.Principal, sessionID string, title, mode, agentProviderID, status, summary string, scope, toolset map[string]any, tags []string, archived bool) (domaincopilot.Session, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincopilot.Session{}, err
	}
	current, err := s.repo.GetSession(ctx, principal.UserID, strings.TrimSpace(sessionID))
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
	return s.repo.UpdateSession(ctx, principal.UserID, current.ID, current)
}

func (s *Service) DeleteSession(ctx context.Context, principal domainidentity.Principal, sessionID string) error {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return err
	}
	return s.repo.DeleteSession(ctx, principal.UserID, strings.TrimSpace(sessionID))
}

func (s *Service) ListMessages(ctx context.Context, principal domainidentity.Principal, sessionID string) ([]domaincopilot.Message, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return nil, err
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	if _, err := s.repo.GetSession(ctx, principal.UserID, trimmedSessionID); err != nil {
		return nil, err
	}
	return s.repo.ListMessages(ctx, trimmedSessionID, 100)
}

func (s *Service) SendMessage(ctx context.Context, principal domainidentity.Principal, sessionID, content, locale string) (domaincopilot.SessionMessageEnvelope, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	session, err := s.repo.GetSession(ctx, principal.UserID, strings.TrimSpace(sessionID))
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	sessionMeta := parseSessionMetadata(session.Metadata)
	locale = detectMessageLocale(content, locale)
	userMessage, err := s.repo.CreateMessage(ctx, domaincopilot.Message{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      "user",
		Content:   strings.TrimSpace(content),
		Metadata:  map[string]any{"userId": principal.UserID, "locale": locale, "mode": sessionMeta.Mode},
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	toolCalls, artifacts, sessionPatch := s.analyzeConversation(ctx, principal, session, content, locale)
	reply := s.generateReply(ctx, principal, content, locale)
	if len(artifacts) > 0 {
		reply = artifacts[0].Summary
	}
	assistantMessage, err := s.repo.CreateMessage(ctx, domaincopilot.Message{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      "assistant",
		Content:   reply,
		Metadata:  map[string]any{"mode": sessionMeta.Mode, "source": "platform-context", "locale": locale, "analysisArtifacts": artifacts},
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
		_, _ = s.repo.UpdateSession(ctx, principal.UserID, session.ID, session)
	}
	return domaincopilot.SessionMessageEnvelope{
		Messages:          []domaincopilot.Message{userMessage, assistantMessage},
		ToolCalls:         toolCalls,
		AnalysisArtifacts: artifacts,
		SessionPatch:      sessionPatch,
	}, nil
}

func (s *Service) RunSessionAnalysis(ctx context.Context, principal domainidentity.Principal, sessionID string, input domaincopilot.RootCauseRunInput, locale string) (domaincopilot.SessionMessageEnvelope, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	session, err := s.repo.GetSession(ctx, principal.UserID, strings.TrimSpace(sessionID))
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
	toolCalls := make([]domaincopilot.ToolExecution, 0)
	artifacts := make([]domaincopilot.AnalysisArtifact, 0)
	refs := append([]domaincopilot.AnalysisRunRef{}, metadata.AnalysisRunRefs...)
	switch mode {
	case "root_cause":
		run, calls, artifact, runErr := s.runSessionRootCause(ctx, principal, session.ID, scope, metadata.Toolset, input.Question, locale)
		if runErr != nil {
			return domaincopilot.SessionMessageEnvelope{}, runErr
		}
		toolCalls = append(toolCalls, calls...)
		artifacts = append(artifacts, artifact)
		refs = append(refs, domaincopilot.AnalysisRunRef{ID: run.ID, Kind: run.Kind, Status: run.Status, CreatedAt: run.CreatedAt.Format(time.RFC3339)})
	case "performance":
		calls, artifact, runErr := s.runSessionPerformance(ctx, session.ID, scope, metadata.Toolset, input.Question, runCreatedBy, runTriggerType, runDedupKey)
		if runErr != nil {
			return domaincopilot.SessionMessageEnvelope{}, runErr
		}
		toolCalls = append(toolCalls, calls...)
		artifacts = append(artifacts, artifact)
		refs = append(refs, domaincopilot.AnalysisRunRef{ID: artifact.RunID, Kind: artifact.Kind, Status: "completed", CreatedAt: time.Now().UTC().Format(time.RFC3339)})
	case "trace":
		calls, artifact, runErr := s.runSessionTrace(ctx, session.ID, scope, metadata.Toolset, input.Question, runCreatedBy, runTriggerType, runDedupKey)
		if runErr != nil {
			return domaincopilot.SessionMessageEnvelope{}, runErr
		}
		toolCalls = append(toolCalls, calls...)
		artifacts = append(artifacts, artifact)
		refs = append(refs, domaincopilot.AnalysisRunRef{ID: artifact.RunID, Kind: artifact.Kind, Status: "completed", CreatedAt: time.Now().UTC().Format(time.RFC3339)})
	case "inspection_review":
		calls, artifact := s.runSessionInspectionReview(session.ID, scope, metadata.Toolset, input.Question, locale)
		toolCalls = append(toolCalls, calls...)
		artifacts = append(artifacts, artifact)
		refs = append(refs, domaincopilot.AnalysisRunRef{ID: artifact.RunID, Kind: artifact.Kind, Status: "completed", CreatedAt: time.Now().UTC().Format(time.RFC3339)})
	default:
		toolCalls, artifacts, _ = s.analyzeConversation(ctx, principal, domaincopilot.Session{
			ID:        session.ID,
			Title:     session.Title,
			CreatedBy: session.CreatedBy,
			Metadata:  sessionMetadataMap(domaincopilot.SessionMetadata{Mode: mode, Status: "active", Scope: scope}),
			CreatedAt: session.CreatedAt,
			UpdatedAt: session.UpdatedAt,
		}, input.Question, locale)
	}
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
	assistantMessage, err := s.repo.CreateMessage(ctx, domaincopilot.Message{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      "assistant",
		Content:   reply,
		Metadata:  map[string]any{"mode": mode, "source": "explicit-analysis", "locale": locale, "analysisArtifacts": artifacts},
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return domaincopilot.SessionMessageEnvelope{}, err
	}
	if len(sessionPatch) > 0 {
		merged := applySessionAnalysisPatch(parseSessionMetadata(session.Metadata), sessionPatch)
		session.Metadata = sessionMetadataMap(merged)
		session.UpdatedAt = time.Now().UTC()
		_, _ = s.repo.UpdateSession(ctx, principal.UserID, session.ID, session)
	}
	return domaincopilot.SessionMessageEnvelope{
		Messages:          []domaincopilot.Message{assistantMessage},
		ToolCalls:         toolCalls,
		AnalysisArtifacts: artifacts,
		SessionPatch:      sessionPatch,
	}, nil
}

func (s *Service) queueSessionAgentAnalysis(ctx context.Context, principal domainidentity.Principal, session domaincopilot.Session, mode, providerID string, scope domaincopilot.SessionScope, toolset domaincopilot.SessionToolset, input domaincopilot.RootCauseRunInput, locale string) (domaincopilot.SessionMessageEnvelope, error) {
	if normalizeSessionMode(mode) == "root_cause" {
		return s.queueSessionRootCauseAgentAnalysis(ctx, principal, session, providerID, scope, toolset, input, locale)
	}
	capabilityID := normalizeAnalysisKind(mode)
	if capabilityID == "" {
		capabilityID = mode
	}
	run, err := s.createAgentRun(ctx, domaincopilot.AgentRunInput{
		ProviderID:     providerID,
		CapabilityID:   capabilityID,
		SkillIDs:       toolset.EnabledSkillIDs,
		SessionID:      session.ID,
		RootCauseRunID: strings.TrimSpace(input.SessionID),
		CreatedBy:      principal.UserID,
		Scope:          scope,
		Toolset:        toolset,
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
			"agentRuntimeId": run.ID,
			"status":         run.Status,
		},
	}
	message, err := s.repo.CreateMessage(ctx, domaincopilot.Message{
		ID:        uuid.NewString(),
		SessionID: session.ID,
		Role:      "assistant",
		Content:   artifact.Summary,
		Metadata: map[string]any{
			"mode":              mode,
			"source":            "agent-runtime",
			"locale":            normalizeLocale(locale),
			"agentRunId":        run.ID,
			"agentProviderId":   run.ProviderID,
			"analysisArtifacts": []domaincopilot.AnalysisArtifact{artifact},
		},
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
	_, _ = s.repo.UpdateSession(ctx, principal.UserID, session.ID, session)
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
	message, err := s.repo.CreateMessage(ctx, domaincopilot.Message{
		ID:        uuid.NewString(),
		SessionID: session.ID,
		Role:      "assistant",
		Content:   artifact.Summary,
		Metadata: map[string]any{
			"mode":              "root_cause",
			"source":            "agent-runtime",
			"locale":            normalizeLocale(locale),
			"agentRunId":        run.ID,
			"rootCauseRunId":    rootRun.ID,
			"agentProviderId":   run.ProviderID,
			"analysisArtifacts": []domaincopilot.AnalysisArtifact{artifact},
		},
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
	_, _ = s.repo.UpdateSession(ctx, principal.UserID, session.ID, session)
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

func (s *Service) composeReply(ctx context.Context, principal domainidentity.Principal, prompt, locale string) string {
	locale = detectMessageLocale(prompt, locale)
	alertSummary, _ := s.alerts.Summary(ctx, principal)
	clusters, _ := s.clusters.List(ctx)
	events, _ := s.events.List(ctx, 5)
	audits, _ := s.audits.List(ctx, domainaudit.Filter{Limit: 5})
	apps, _ := s.apps.List(ctx, domainapp.Filter{Limit: 200})
	builds, _ := s.builds.List(ctx, domainbuild.Filter{Limit: 5})

	degraded := 0
	for _, item := range clusters {
		if item.Health.Status != "healthy" && item.Health.Status != "ok" {
			degraded++
		}
	}

	lower := strings.ToLower(prompt)
	focus := localize(locale, "平台", "platform")
	switch {
	case strings.Contains(lower, "cluster") || strings.Contains(prompt, "集群"):
		focus = localize(locale, "集群", "clusters")
	case strings.Contains(lower, "build") || strings.Contains(prompt, "构建"):
		focus = localize(locale, "构建", "builds")
	case strings.Contains(lower, "alert") || strings.Contains(prompt, "告警"):
		focus = localize(locale, "告警", "alerts")
	case strings.Contains(lower, "audit") || strings.Contains(prompt, "审计"):
		focus = localize(locale, "审计", "audit")
	}

	if locale == "zh-CN" {
		return fmt.Sprintf(
			"当前%s上下文：平台可见 %d 个集群，其中 %d 个处于异常状态；当前有 %d 条触发中的告警，其中 %d 条为严重告警；应用注册中心内有 %d 个应用，最近有 %d 条构建、%d 条事件、%d 条审计记录。当前助手仍然是只读模式，回答基于已经存储在 PostgreSQL 中的实时平台数据。",
			focus,
			len(clusters),
			degraded,
			alertSummary.FiringCount,
			alertSummary.CriticalCount,
			len(apps),
			len(builds),
			len(events),
			len(audits),
		)
	}
	return fmt.Sprintf(
		"Current %s context: %d clusters visible (%d degraded), %d firing alerts (%d critical), %d applications in the registry, %d recent builds, %d recent events, and %d recent audit entries. This assistant is read-only for now and is answering from live platform data already stored in PostgreSQL.",
		focus,
		len(clusters),
		degraded,
		alertSummary.FiringCount,
		alertSummary.CriticalCount,
		len(apps),
		len(builds),
		len(events),
		len(audits),
	)
}

func (s *Service) generateReply(ctx context.Context, principal domainidentity.Principal, prompt, locale string) string {
	if settings, err := s.resolveAISettings(ctx); err == nil && settings.Provider.Enabled && strings.TrimSpace(settings.Provider.BaseURL) != "" && strings.TrimSpace(settings.Provider.APIKey) != "" {
		if reply, err := s.externalAIReply(ctx, settings.Provider, prompt, locale); err == nil && strings.TrimSpace(reply) != "" {
			return strings.TrimSpace(reply)
		}
	}
	return s.composeReply(ctx, principal, prompt, locale)
}

func (s *Service) resolveAISettings(ctx context.Context) (domainsettings.AISettings, error) {
	if s.settings == nil {
		return domainsettings.AISettings{}, fmt.Errorf("ai settings unavailable")
	}
	return s.settings.ResolveAISettings(ctx)
}

func (s *Service) externalAIReply(ctx context.Context, settings domainsettings.AIProviderSettings, prompt, locale string) (string, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(settings.BaseURL), "/")
	if !strings.HasSuffix(endpoint, "/chat/completions") {
		endpoint += "/chat/completions"
	}
	payload := map[string]any{
		"model": settings.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt(locale)},
			{"role": "user", "content": strings.TrimSpace(prompt)},
		},
		"temperature": 0.2,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(settings.APIKey))
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("ai provider returned %s", resp.Status)
	}
	var body struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if len(body.Choices) == 0 {
		return "", nil
	}
	return body.Choices[0].Message.Content, nil
}

func systemPrompt(locale string) string {
	if locale == "zh-CN" {
		return "你是 kubecrux 的平台 AI 助手。回答时尽量简洁、可执行，优先根据平台上下文给出排查建议和结论。"
	}
	return "You are the kubecrux platform AI assistant. Keep answers concise and actionable, and prioritize investigation guidance grounded in platform context."
}

func ternarySeverity(condition bool, truthy, falsy string) string {
	if condition {
		return truthy
	}
	return falsy
}
