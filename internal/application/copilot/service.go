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
	"github.com/kubecrux/kubecrux/internal/platform/runtimeobs"
	"go.uber.org/zap"
)

type Repository interface {
	ListSessions(context.Context, string, int) ([]domaincopilot.Session, error)
	CreateSession(context.Context, domaincopilot.Session) (domaincopilot.Session, error)
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
	ListRootCauseRuns(context.Context, string, domaincopilot.RootCauseRunFilter) ([]domaincopilot.RootCauseRun, error)
	GetRootCauseRun(context.Context, string, string) (domaincopilot.RootCauseRun, error)
	CreateRootCauseRun(context.Context, domaincopilot.RootCauseRun) (domaincopilot.RootCauseRun, error)
	ListInspectionTasks(context.Context, string, int) ([]domaincopilot.InspectionTask, error)
	GetInspectionTask(context.Context, string, string) (domaincopilot.InspectionTask, error)
	ListDueInspectionTasks(context.Context, time.Time, int) ([]domaincopilot.InspectionTask, error)
	CreateInspectionTask(context.Context, domaincopilot.InspectionTask) (domaincopilot.InspectionTask, error)
	UpdateInspectionTask(context.Context, string, string, domaincopilot.InspectionTaskInput) (domaincopilot.InspectionTask, error)
	TouchInspectionTaskRun(context.Context, string, time.Time) error
	ListInspectionRuns(context.Context, string, domaincopilot.InspectionRunFilter) ([]domaincopilot.InspectionRun, error)
	CreateInspectionRun(context.Context, domaincopilot.InspectionRun) (domaincopilot.InspectionRun, error)
}

type ClusterReader interface {
	List(context.Context) ([]domaincluster.Summary, error)
}

type AlertReader interface {
	Summary(context.Context) (domainalert.Summary, error)
	ListAlerts(context.Context, domainalert.Filter) ([]domainalert.Instance, error)
	ListChannels(context.Context) ([]domainalert.NotificationChannel, error)
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
	http                  *http.Client
	logger                *zap.Logger
	metrics               *runtimeobs.Registry
	inspectionParallelism int
	mcpRegistry           MCPRegistry
}

type MCPRegistry interface {
	List() []domainmcp.Adapter
	Get(string) (domainmcp.Adapter, bool)
}

func New(repo Repository, clusters ClusterReader, alerts AlertReader, events EventReader, audits AuditReader, apps ApplicationReader, builds BuildReader, releases ReleaseReader, settings AISettingsResolver) *Service {
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
	return s.repo.ListSessions(ctx, principal.UserID, 20)
}

func (s *Service) CreateSession(ctx context.Context, principal domainidentity.Principal, title, locale string) (domaincopilot.Session, error) {
	if strings.TrimSpace(title) == "" {
		title = localize(locale, "新的调查会话", "New Investigation")
	}
	return s.repo.CreateSession(ctx, domaincopilot.Session{
		ID:        uuid.NewString(),
		Title:     strings.TrimSpace(title),
		CreatedBy: principal.UserID,
		Metadata:  map[string]any{"mode": "read-only", "locale": normalizeLocale(locale)},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
}

func (s *Service) ListMessages(ctx context.Context, _ domainidentity.Principal, sessionID string) ([]domaincopilot.Message, error) {
	return s.repo.ListMessages(ctx, sessionID, 100)
}

func (s *Service) SendMessage(ctx context.Context, principal domainidentity.Principal, sessionID, content, locale string) ([]domaincopilot.Message, error) {
	locale = detectMessageLocale(content, locale)
	userMessage, err := s.repo.CreateMessage(ctx, domaincopilot.Message{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      "user",
		Content:   strings.TrimSpace(content),
		Metadata:  map[string]any{"userId": principal.UserID, "locale": locale},
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	reply := s.generateReply(ctx, content, locale)
	assistantMessage, err := s.repo.CreateMessage(ctx, domaincopilot.Message{
		ID:        uuid.NewString(),
		SessionID: sessionID,
		Role:      "assistant",
		Content:   reply,
		Metadata:  map[string]any{"mode": "read-only", "source": "platform-context", "locale": locale},
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	return []domaincopilot.Message{userMessage, assistantMessage}, nil
}

func (s *Service) Insights(ctx context.Context, locale string) ([]domaincopilot.Insight, error) {
	clusters, _ := s.clusters.List(ctx)
	alertSummary, _ := s.alerts.Summary(ctx)
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

func (s *Service) composeReply(ctx context.Context, prompt, locale string) string {
	locale = detectMessageLocale(prompt, locale)
	alertSummary, _ := s.alerts.Summary(ctx)
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

func (s *Service) generateReply(ctx context.Context, prompt, locale string) string {
	if settings, err := s.resolveAISettings(ctx); err == nil && settings.Provider.Enabled && strings.TrimSpace(settings.Provider.BaseURL) != "" && strings.TrimSpace(settings.Provider.APIKey) != "" {
		if reply, err := s.externalAIReply(ctx, settings.Provider, prompt, locale); err == nil && strings.TrimSpace(reply) != "" {
			return strings.TrimSpace(reply)
		}
	}
	return s.composeReply(ctx, prompt, locale)
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
