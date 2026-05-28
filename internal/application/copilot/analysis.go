package copilot

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/soha/soha/internal/application/access"
	domainalert "github.com/soha/soha/internal/domain/alert"
	domainaudit "github.com/soha/soha/internal/domain/audit"
	domainbuild "github.com/soha/soha/internal/domain/build"
	domaincluster "github.com/soha/soha/internal/domain/cluster"
	domaincopilot "github.com/soha/soha/internal/domain/copilot"
	domainevent "github.com/soha/soha/internal/domain/event"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainrelease "github.com/soha/soha/internal/domain/release"
	aperrors "github.com/soha/soha/internal/platform/apperrors"
)

func (s *Service) ListRootCauseRuns(ctx context.Context, principal domainidentity.Principal, filter domaincopilot.RootCauseRunFilter) ([]domaincopilot.RootCauseRun, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIView); err != nil {
		return nil, err
	}
	return s.repo.ListRootCauseRuns(ctx, principal.UserID, filter)
}

func (s *Service) GetRootCauseRun(ctx context.Context, principal domainidentity.Principal, runID string) (domaincopilot.RootCauseRun, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIView); err != nil {
		return domaincopilot.RootCauseRun{}, err
	}
	return s.repo.GetRootCauseRun(ctx, principal.UserID, strings.TrimSpace(runID))
}

func (s *Service) RunRootCauseAnalysis(ctx context.Context, principal domainidentity.Principal, input domaincopilot.RootCauseRunInput, locale string) (domaincopilot.RootCauseRun, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIRootCauseRun); err != nil {
		return domaincopilot.RootCauseRun{}, err
	}
	if s.shouldUseExternalAgent(input.AgentProviderID) {
		return s.queueRootCauseAgentRun(ctx, principal, principal.UserID, input, "", locale)
	}
	return s.executeRootCauseRun(ctx, principal, principal.UserID, input, "", domaincopilot.SessionToolset{}, locale)
}

func (s *Service) runRootCauseAnalysisWithToolset(ctx context.Context, principal domainidentity.Principal, input domaincopilot.RootCauseRunInput, toolset domaincopilot.SessionToolset, locale string) (domaincopilot.RootCauseRun, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIRootCauseRun); err != nil {
		return domaincopilot.RootCauseRun{}, err
	}
	return s.executeRootCauseRun(ctx, principal, principal.UserID, input, "", toolset, locale)
}

type rootCauseAnalysis struct {
	evidence        []domaincopilot.RootCauseEvidence
	hypotheses      []domaincopilot.RootCauseHypothesis
	recommendations []string
	summary         string
	severity        string
	playbookResults map[string]any
	toolExecutions  []domaincopilot.ToolExecution
}

func (s *Service) executeRootCauseRun(ctx context.Context, principal domainidentity.Principal, createdBy string, input domaincopilot.RootCauseRunInput, dedupKey string, toolset domaincopilot.SessionToolset, locale string) (domaincopilot.RootCauseRun, error) {
	input = normalizeRootCauseInput(input)
	if strings.TrimSpace(input.AlertID) == "" && strings.TrimSpace(input.ClusterID) == "" && strings.TrimSpace(input.WorkloadName) == "" {
		return domaincopilot.RootCauseRun{}, fmt.Errorf("%w: clusterId, alertId, or workloadName is required", aperrors.ErrInvalidArgument)
	}

	profile, err := s.resolveRootCauseProfile(ctx, input)
	if err != nil {
		return domaincopilot.RootCauseRun{}, err
	}
	if input.TimeRangeMinutes <= 0 {
		input.TimeRangeMinutes = profile.DefaultTimeRangeMinutes
		if input.TimeRangeMinutes <= 0 {
			input.TimeRangeMinutes = 60
		}
	}
	analysis := s.collectRootCauseAnalysis(ctx, principal, input, profile, toolset, locale)
	startedAt := time.Now().UTC()
	dataSourceSnapshot, remediationPlan := s.buildRootCauseExecutionMetadata(ctx, profile, toolset, analysis)
	run := domaincopilot.RootCauseRun{
		ID:                 "rca:" + uuid.NewString(),
		Kind:               normalizeAnalysisKind(input.Kind),
		SessionID:          strings.TrimSpace(input.SessionID),
		Title:              analysisTitle(input, locale),
		CreatedBy:          createdBy,
		AnalysisProfileID:  profile.ID,
		TriggerType:        normalizedTriggerType(input.TriggerType),
		Status:             "completed",
		Severity:           analysis.severity,
		Summary:            analysis.summary,
		ClusterID:          input.ClusterID,
		Namespace:          input.Namespace,
		WorkloadKind:       input.WorkloadKind,
		WorkloadName:       input.WorkloadName,
		AlertID:            input.AlertID,
		TimeRangeMinutes:   input.TimeRangeMinutes,
		Question:           input.Question,
		Evidence:           analysis.evidence,
		Hypotheses:         analysis.hypotheses,
		Recommendations:    analysis.recommendations,
		ToolExecutions:     analysis.toolExecutions,
		DataSourceSnapshot: dataSourceSnapshot,
		PlaybookResults:    analysis.playbookResults,
		RemediationPlan:    remediationPlan,
		DedupKey:           strings.TrimSpace(dedupKey),
		CreatedAt:          startedAt,
		UpdatedAt:          startedAt,
	}
	if strings.TrimSpace(input.Title) != "" {
		run.Title = strings.TrimSpace(input.Title)
	}
	return s.repo.CreateRootCauseRun(ctx, run)
}

type queuedRootCauseAgentRun struct {
	RootCauseRun domaincopilot.RootCauseRun
	AgentRun     domaincopilot.AgentRun
}

func (s *Service) queueRootCauseAgentRun(ctx context.Context, principal domainidentity.Principal, createdBy string, input domaincopilot.RootCauseRunInput, dedupKey string, locale string) (domaincopilot.RootCauseRun, error) {
	queued, err := s.queueRootCauseAgentRunWithToolset(ctx, principal, createdBy, createdBy, input, dedupKey, domaincopilot.SessionToolset{}, locale)
	if err != nil {
		return domaincopilot.RootCauseRun{}, err
	}
	return queued.RootCauseRun, nil
}

func (s *Service) queueRootCauseAgentRunWithToolset(ctx context.Context, principal domainidentity.Principal, rootCauseCreatedBy, agentCreatedBy string, input domaincopilot.RootCauseRunInput, dedupKey string, toolsetOverride domaincopilot.SessionToolset, locale string) (queuedRootCauseAgentRun, error) {
	input = normalizeRootCauseInput(input)
	if strings.TrimSpace(input.AlertID) == "" && strings.TrimSpace(input.ClusterID) == "" && strings.TrimSpace(input.WorkloadName) == "" {
		return queuedRootCauseAgentRun{}, fmt.Errorf("%w: clusterId, alertId, or workloadName is required", aperrors.ErrInvalidArgument)
	}
	profile, err := s.resolveRootCauseProfile(ctx, input)
	if err != nil {
		return queuedRootCauseAgentRun{}, err
	}
	if input.TimeRangeMinutes <= 0 {
		input.TimeRangeMinutes = profile.DefaultTimeRangeMinutes
		if input.TimeRangeMinutes <= 0 {
			input.TimeRangeMinutes = 60
		}
	}
	rootCauseCreatedBy = strings.TrimSpace(rootCauseCreatedBy)
	if rootCauseCreatedBy == "" {
		rootCauseCreatedBy = automationRootCauseCreatedBy
	}
	agentCreatedBy = strings.TrimSpace(agentCreatedBy)
	if agentCreatedBy == "" {
		agentCreatedBy = rootCauseCreatedBy
	}
	now := time.Now().UTC()
	provider := s.resolveAgentProvider(input.AgentProviderID)
	run := domaincopilot.RootCauseRun{
		ID:                 "rca:" + uuid.NewString(),
		Kind:               normalizeAnalysisKind(input.Kind),
		SessionID:          strings.TrimSpace(input.SessionID),
		Title:              analysisTitle(input, locale),
		CreatedBy:          rootCauseCreatedBy,
		AnalysisProfileID:  profile.ID,
		TriggerType:        normalizedTriggerType(input.TriggerType),
		Status:             domaincopilot.AgentRunStatusQueued,
		Severity:           "info",
		Summary:            localize(locale, "已提交给 Agent Runtime，等待外部 agent runner 领取并回写根因分析结果。", "Queued for Agent Runtime. Waiting for an external agent runner to write back the root-cause result."),
		ClusterID:          input.ClusterID,
		Namespace:          input.Namespace,
		WorkloadKind:       input.WorkloadKind,
		WorkloadName:       input.WorkloadName,
		AlertID:            input.AlertID,
		TimeRangeMinutes:   input.TimeRangeMinutes,
		Question:           input.Question,
		Recommendations:    []string{localize(locale, "等待外部 agent 回写后再执行处置动作。", "Wait for the external agent callback before taking remediation action.")},
		DataSourceSnapshot: map[string]any{"providerId": provider.ID, "providerKind": provider.Kind, "capabilityId": "root_cause", "status": domaincopilot.AgentRunStatusQueued},
		DedupKey:           strings.TrimSpace(dedupKey),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if strings.TrimSpace(input.Title) != "" {
		run.Title = strings.TrimSpace(input.Title)
	}
	created, err := s.repo.CreateRootCauseRun(ctx, run)
	if err != nil {
		return queuedRootCauseAgentRun{}, err
	}
	toolset := mergeRootCauseAgentToolset(rootCauseAgentToolset(profile, input), toolsetOverride)
	agentRun, err := s.createAgentRun(ctx, domaincopilot.AgentRunInput{
		ProviderID:     input.AgentProviderID,
		CapabilityID:   created.Kind,
		SkillIDs:       automationAgentSkillIDs(created.Kind, toolset.EnabledSkillIDs),
		SessionID:      created.SessionID,
		RootCauseRunID: created.ID,
		CreatedBy:      agentCreatedBy,
		Scope: domaincopilot.SessionScope{
			ClusterID:        created.ClusterID,
			Namespace:        created.Namespace,
			Workload:         created.WorkloadName,
			AlertID:          created.AlertID,
			TimeRangeMinutes: created.TimeRangeMinutes,
		},
		Toolset: toolset,
		Input: map[string]any{
			"question":          created.Question,
			"mode":              created.Kind,
			"analysisProfileId": profile.ID,
			"analysisProfile":   profile.Name,
			"triggerType":       created.TriggerType,
			"locale":            normalizeLocale(locale),
			"rootCauseRunId":    created.ID,
			"rootCauseRunOwner": rootCauseCreatedBy,
			"sessionOwnerId":    agentCreatedBy,
			"dedupKey":          created.DedupKey,
			"capabilityId":      created.Kind,
		},
		TimeoutSeconds: firstPositive(profile.TimeoutSeconds, 600),
	})
	if err != nil {
		created.Status = domaincopilot.AgentRunStatusFailed
		created.Summary = err.Error()
		created.UpdatedAt = time.Now().UTC()
		_, _ = s.repo.UpdateRootCauseRun(ctx, created)
		return queuedRootCauseAgentRun{}, err
	}
	created.DataSourceSnapshot = mergeAgentRunCallbackPayload(created.DataSourceSnapshot, map[string]any{
		"agentRunId":     agentRun.ID,
		"agentRuntimeId": agentRun.ID,
		"status":         agentRun.Status,
		"skillIds":       agentRun.SkillIDs,
		"toolset":        agentRun.Toolset,
	})
	created.UpdatedAt = time.Now().UTC()
	updated, err := s.repo.UpdateRootCauseRun(ctx, created)
	if err != nil {
		return queuedRootCauseAgentRun{RootCauseRun: created, AgentRun: agentRun}, nil
	}
	return queuedRootCauseAgentRun{RootCauseRun: updated, AgentRun: agentRun}, nil
}

func rootCauseAgentToolset(profile domaincopilot.AnalysisProfile, input domaincopilot.RootCauseRunInput) domaincopilot.SessionToolset {
	return domaincopilot.SessionToolset{
		EnabledAdapterIDs: profile.EnabledSources,
		EnabledSkillIDs:   profile.EnabledPlaybooks,
		BudgetOverrides: map[string]any{
			"timeoutSeconds":   profile.TimeoutSeconds,
			"maxEvidenceItems": intCondition(profile.QueryBudgets["maxEvidenceItems"]),
		},
		ScopeOverrides: map[string]any{
			"clusterId":        input.ClusterID,
			"namespace":        input.Namespace,
			"workload":         input.WorkloadName,
			"alertId":          input.AlertID,
			"timeRangeMinutes": input.TimeRangeMinutes,
		},
	}
}

func mergeRootCauseAgentToolset(base, override domaincopilot.SessionToolset) domaincopilot.SessionToolset {
	if len(override.EnabledAdapterIDs) > 0 {
		base.EnabledAdapterIDs = normalizeStringList(override.EnabledAdapterIDs)
	}
	if len(override.EnabledSkillIDs) > 0 {
		base.EnabledSkillIDs = normalizeStringList(override.EnabledSkillIDs)
	}
	if len(override.DisabledToolNames) > 0 {
		base.DisabledToolNames = normalizeStringList(append(base.DisabledToolNames, override.DisabledToolNames...))
	}
	if len(override.BudgetOverrides) > 0 {
		base.BudgetOverrides = mergeAgentRunCallbackPayload(base.BudgetOverrides, override.BudgetOverrides)
	}
	if len(override.ScopeOverrides) > 0 {
		base.ScopeOverrides = mergeAgentRunCallbackPayload(base.ScopeOverrides, override.ScopeOverrides)
	}
	return base
}

func (s *Service) resolveRootCauseProfile(ctx context.Context, input domaincopilot.RootCauseRunInput) (domaincopilot.AnalysisProfile, error) {
	if strings.TrimSpace(input.AnalysisProfileID) != "" {
		profile, err := s.repo.GetAnalysisProfile(ctx, strings.TrimSpace(input.AnalysisProfileID))
		if err != nil {
			return domaincopilot.AnalysisProfile{}, err
		}
		return profile, nil
	}
	items, err := s.repo.ListAnalysisProfiles(ctx)
	if err == nil {
		for _, item := range items {
			if item.Enabled && item.Mode == "root_cause" {
				return item, nil
			}
		}
	}
	return domaincopilot.AnalysisProfile{
		ID:                      "builtin:root-cause",
		Name:                    "Built-in Root Cause",
		Mode:                    "root_cause",
		EnabledSources:          []string{"platform-native", "logs", "metrics", "traces"},
		EnabledPlaybooks:        []string{"release-correlation", "cluster-health", "access-drift", "runtime-instability", "alert-pressure", "build-queue", "error-burst", "dependency-timeout"},
		RemediationPolicy:       "suggest_only",
		DefaultTimeRangeMinutes: input.TimeRangeMinutes,
		TimeoutSeconds:          90,
		Enabled:                 true,
	}, nil
}

func (s *Service) buildRootCauseExecutionMetadata(ctx context.Context, profile domaincopilot.AnalysisProfile, toolset domaincopilot.SessionToolset, analysis rootCauseAnalysis) (map[string]any, map[string]any) {
	dataSources, _ := s.repo.ListDataSources(ctx)
	available := make([]map[string]any, 0)
	for _, item := range dataSources {
		if !item.Enabled {
			continue
		}
		if len(profile.EnabledSources) > 0 && !containsString(profile.EnabledSources, item.ID) && !containsString(profile.EnabledSources, item.SourceKind) {
			continue
		}
		if !toolsetAllowsAdapter(toolset, item.MCPAdapter) {
			continue
		}
		available = append(available, map[string]any{
			"id":          item.ID,
			"name":        item.Name,
			"sourceKind":  item.SourceKind,
			"backendType": item.BackendType,
			"mcpAdapter":  item.MCPAdapter,
		})
	}
	dataSourceSnapshot := map[string]any{
		"enabledSources":   profile.EnabledSources,
		"availableSources": available,
	}
	remediationPlan := map[string]any{
		"policy":  profile.RemediationPolicy,
		"actions": buildRemediationActions(analysis.recommendations),
	}
	return dataSourceSnapshot, remediationPlan
}

func buildRemediationActions(recommendations []string) []map[string]any {
	items := make([]map[string]any, 0, len(recommendations))
	for index, item := range recommendations {
		items = append(items, map[string]any{
			"id":          fmt.Sprintf("action-%d", index+1),
			"type":        "suggestion",
			"description": item,
		})
	}
	return items
}

func normalizedTriggerType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "manual"
	}
	return value
}

func enabledPlaybooks(items []string) map[string]bool {
	values := map[string]bool{
		"release-correlation": true,
		"cluster-health":      true,
		"access-drift":        true,
		"runtime-instability": true,
		"alert-pressure":      true,
		"build-queue":         true,
	}
	if len(items) == 0 {
		return values
	}
	selected := make(map[string]bool, len(items))
	for _, item := range items {
		selected[strings.TrimSpace(item)] = true
	}
	return selected
}

func sourceEnabled(enabledSources []string, expected string) bool {
	if len(enabledSources) == 0 {
		return true
	}
	expected = strings.TrimSpace(expected)
	for _, item := range enabledSources {
		if strings.TrimSpace(item) == expected {
			return true
		}
	}
	return false
}

func (s *Service) collectRootCauseAnalysis(ctx context.Context, principal domainidentity.Principal, input domaincopilot.RootCauseRunInput, profile domaincopilot.AnalysisProfile, toolset domaincopilot.SessionToolset, locale string) rootCauseAnalysis {
	now := time.Now().UTC()
	playbooks := enabledPlaybooks(profile.EnabledPlaybooks)
	platformNativeEnabled := sourceEnabled(profile.EnabledSources, "platform-native") && toolsetAllowsAdapter(toolset, "platform-native.v1")
	clusters, _ := s.clusters.List(ctx)
	alerts, _ := s.alerts.ListAlerts(ctx, principal, domainalert.Filter{ClusterID: input.ClusterID, Limit: 30})
	events, _ := s.events.List(ctx, 50)
	audits, _ := s.audits.List(ctx, domainaudit.Filter{Limit: 50})
	releases, _ := s.releases.List(ctx, domainrelease.Filter{ClusterID: input.ClusterID, Limit: 20})
	builds, _ := s.builds.List(ctx, domainbuild.Filter{Limit: 20})

	if input.ClusterID != "" {
		clusters = filterClustersByID(clusters, input.ClusterID)
		audits = filterAuditsByCluster(audits, input.ClusterID)
	}
	if input.Namespace != "" {
		alerts = filterAlertsByNamespace(alerts, input.Namespace)
		releases = filterReleasesByNamespace(releases, input.Namespace)
		audits = filterAuditsByNamespace(audits, input.Namespace)
	}

	alerts = filterRootCauseAlerts(alerts, input)
	events = filterRootCauseEvents(events, input)
	audits = filterRootCauseAudits(audits, input, now)
	releases = filterRootCauseReleases(releases, input, now)
	builds = filterRootCauseBuilds(builds, now)

	evidence := make([]domaincopilot.RootCauseEvidence, 0)
	hypotheses := make([]domaincopilot.RootCauseHypothesis, 0)
	playbookResults := map[string]any{}

	if !platformNativeEnabled {
		return rootCauseAnalysis{
			evidence:        nil,
			hypotheses:      nil,
			recommendations: []string{localize(locale, "当前分析 profile 没有启用 platform-native 数据源，暂时无法生成根因证据。", "The selected analysis profile does not enable platform-native sources, so no local root cause evidence can be generated yet.")},
			summary:         localize(locale, "当前 profile 没有启用可执行的数据源。", "The selected profile does not enable any executable local data source."),
			severity:        "info",
			playbookResults: playbookResults,
		}
	}

	degradedClusters := degradedClusterEvidence(clusters)
	evidence = append(evidence, degradedClusters...)

	alertEvidence := alertRootCauseEvidence(alerts)
	evidence = append(evidence, alertEvidence...)

	eventEvidence := eventRootCauseEvidence(events, input)
	evidence = append(evidence, eventEvidence...)

	auditEvidence := auditRootCauseEvidence(audits)
	evidence = append(evidence, auditEvidence...)

	releaseEvidence := releaseRootCauseEvidence(releases)
	evidence = append(evidence, releaseEvidence...)

	buildEvidence := buildRootCauseEvidence(builds)
	evidence = append(evidence, buildEvidence...)

	logAnalysis := s.collectRootCauseLogEvidence(ctx, input, profile, toolset, locale)
	evidence = append(evidence, logAnalysis.evidence...)
	hypotheses = append(hypotheses, logAnalysis.hypotheses...)
	playbookResults = mergePlaybookResults(playbookResults, logAnalysis.playbookResults)
	toolExecutions := append([]domaincopilot.ToolExecution{}, logAnalysis.toolExecutions...)

	metricAnalysis := s.collectRootCauseMetricEvidence(ctx, input, profile, toolset, locale)
	evidence = append(evidence, metricAnalysis.evidence...)
	hypotheses = append(hypotheses, metricAnalysis.hypotheses...)
	playbookResults = mergePlaybookResults(playbookResults, metricAnalysis.playbookResults)
	toolExecutions = append(toolExecutions, metricAnalysis.toolExecutions...)

	traceAnalysis := s.collectRootCauseTraceEvidence(ctx, input, profile, toolset, locale)
	evidence = append(evidence, traceAnalysis.evidence...)
	hypotheses = append(hypotheses, traceAnalysis.hypotheses...)
	playbookResults = mergePlaybookResults(playbookResults, traceAnalysis.playbookResults)
	toolExecutions = append(toolExecutions, traceAnalysis.toolExecutions...)

	if playbooks["release-correlation"] && len(releaseEvidence) > 0 && len(alertEvidence) > 0 {
		hypotheses = append(hypotheses, domaincopilot.RootCauseHypothesis{
			ID:              "release-change",
			Title:           localize(locale, "近期发布变更高度可疑", "Recent release change is the most likely trigger"),
			Summary:         localize(locale, "最近发布记录与当前告警范围重合，优先检查最近镜像、配置变更和 rollout 事件。", "Recent release activity overlaps with the current alert scope. Review image, config, and rollout changes first."),
			Confidence:      86,
			EvidenceIDs:     collectEvidenceIDs(append(releaseEvidence, alertEvidence...)...),
			Recommendations: []string{localize(locale, "先比对最近一次发布记录和 deployment rollout 状态。", "Compare the latest release record with current deployment rollout status first."), localize(locale, "检查变更镜像、环境变量和配置挂载差异。", "Inspect recent image, environment variable, and mounted config changes.")},
		})
		playbookResults["release-correlation"] = "matched"
	}
	if playbooks["cluster-health"] && len(degradedClusters) > 0 {
		hypotheses = append(hypotheses, domaincopilot.RootCauseHypothesis{
			ID:              "cluster-instability",
			Title:           localize(locale, "集群或平台稳定性异常", "Cluster or platform instability is likely involved"),
			Summary:         localize(locale, "存在异常集群或平台健康信号，当前问题可能不是单一工作负载缺陷。", "Degraded cluster health suggests the issue may extend beyond a single workload."),
			Confidence:      78,
			EvidenceIDs:     collectEvidenceIDs(degradedClusters...),
			Recommendations: []string{localize(locale, "优先确认节点、网络和控制面事件。", "Check node, network, and control-plane signals first."), localize(locale, "确认是否存在多业务同时受影响。", "Confirm whether multiple workloads are impacted at the same time.")},
		})
		playbookResults["cluster-health"] = "matched"
	}
	if playbooks["access-drift"] && len(auditEvidence) > 0 {
		hypotheses = append(hypotheses, domaincopilot.RootCauseHypothesis{
			ID:              "access-or-config-drift",
			Title:           localize(locale, "权限或配置漂移可能导致失败", "Access or configuration drift may be blocking execution"),
			Summary:         localize(locale, "最近审计中出现失败或拒绝记录，建议检查策略和手工操作。", "Recent audit failures or denies indicate policy or manual change issues may be involved."),
			Confidence:      68,
			EvidenceIDs:     collectEvidenceIDs(auditEvidence...),
			Recommendations: []string{localize(locale, "检查最近失败操作的目标资源和策略命中情况。", "Review recent failed operations and policy matches on the target resources."), localize(locale, "区分预期访问拒绝与非预期配置漂移。", "Distinguish expected access denies from unintended drift.")},
		})
		playbookResults["access-drift"] = "matched"
	}
	if playbooks["runtime-instability"] && len(eventEvidence) > 0 && len(hypotheses) == 0 {
		hypotheses = append(hypotheses, domaincopilot.RootCauseHypothesis{
			ID:              "runtime-instability",
			Title:           localize(locale, "运行时异常正在主导当前问题", "Runtime instability is driving the current issue"),
			Summary:         localize(locale, "最近事件流中已出现与目标范围相关的异常信号，建议先沿事件时间线排查。", "Recent event stream signals already point to runtime instability in the selected scope."),
			Confidence:      61,
			EvidenceIDs:     collectEvidenceIDs(eventEvidence...),
			Recommendations: []string{localize(locale, "优先按照事件时间顺序复盘。", "Reconstruct the issue first in event timeline order.")},
		})
		playbookResults["runtime-instability"] = "matched"
	}
	if playbooks["alert-pressure"] && len(alertEvidence) > 0 && len(hypotheses) == 0 {
		hypotheses = append(hypotheses, domaincopilot.RootCauseHypothesis{
			ID:              "workload-incident",
			Title:           localize(locale, "工作负载本身存在故障征兆", "The workload itself shows failure symptoms"),
			Summary:         localize(locale, "当前已有明确告警但缺少更强上游证据，建议先从工作负载和依赖面排查。", "Active alerts are present but upstream evidence is limited. Start with the workload and its direct dependencies."),
			Confidence:      55,
			EvidenceIDs:     collectEvidenceIDs(alertEvidence...),
			Recommendations: []string{localize(locale, "先核对容器重启、依赖超时和错误率变化。", "Check container restarts, dependency timeouts, and error-rate shifts first.")},
		})
		playbookResults["alert-pressure"] = "matched"
	}
	if playbooks["build-queue"] && len(buildEvidence) > 0 {
		playbookResults["build-queue"] = "matched"
	}

	sort.Slice(hypotheses, func(i, j int) bool {
		return hypotheses[i].Confidence > hypotheses[j].Confidence
	})

	recommendations := uniqueStrings(nil, flattenRecommendations(hypotheses))
	if len(recommendations) == 0 {
		recommendations = []string{localize(locale, "先缩小范围到单个集群/命名空间/工作负载，再重新运行根因分析。", "Narrow the scope to a single cluster, namespace, or workload and rerun the analysis.")}
	}
	recommendations = uniqueStrings(recommendations, logAnalysis.recommendations)
	recommendations = uniqueStrings(recommendations, metricAnalysis.recommendations)
	recommendations = uniqueStrings(recommendations, traceAnalysis.recommendations)

	summary := localize(locale, "当前证据还不足以给出明确根因。", "Current evidence is not yet sufficient for a decisive root cause.")
	severity := "info"
	if len(hypotheses) > 0 {
		summary = hypotheses[0].Summary
	}
	evidence = limitEvidenceItems(evidence, evidenceBudget(toolset, len(evidence)))
	hypotheses = pruneHypothesisEvidenceIDs(hypotheses, evidence)
	severity = highestEvidenceSeverity(evidence)
	return rootCauseAnalysis{
		evidence:        evidence,
		hypotheses:      hypotheses,
		recommendations: recommendations,
		summary:         summary,
		severity:        severity,
		playbookResults: playbookResults,
		toolExecutions:  toolExecutions,
	}
}

func normalizeRootCauseInput(input domaincopilot.RootCauseRunInput) domaincopilot.RootCauseRunInput {
	input.Title = strings.TrimSpace(input.Title)
	input.Kind = normalizeAnalysisKind(input.Kind)
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.ClusterID = strings.TrimSpace(input.ClusterID)
	input.Namespace = strings.TrimSpace(input.Namespace)
	input.WorkloadKind = strings.TrimSpace(input.WorkloadKind)
	input.WorkloadName = strings.TrimSpace(input.WorkloadName)
	input.AlertID = strings.TrimSpace(input.AlertID)
	input.Question = strings.TrimSpace(input.Question)
	if input.WorkloadKind == "" {
		input.WorkloadKind = "Deployment"
	}
	return input
}

func normalizeAnalysisKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case "performance", "trace", "inspection_review":
		return strings.TrimSpace(kind)
	default:
		return "root_cause"
	}
}

func analysisTitle(input domaincopilot.RootCauseRunInput, locale string) string {
	if strings.TrimSpace(input.Title) != "" {
		return input.Title
	}
	switch {
	case input.AlertID != "":
		return localize(locale, "告警根因分析", "Alert Root Cause Analysis")
	case input.WorkloadName != "":
		return localize(locale, fmt.Sprintf("%s 根因分析", input.WorkloadName), fmt.Sprintf("%s Root Cause Analysis", input.WorkloadName))
	default:
		return localize(locale, "根因分析", "Root Cause Analysis")
	}
}

func filterRootCauseAlerts(alerts []domainalert.Instance, input domaincopilot.RootCauseRunInput) []domainalert.Instance {
	items := make([]domainalert.Instance, 0, len(alerts))
	for _, item := range alerts {
		if input.AlertID != "" && item.ID != input.AlertID {
			continue
		}
		if input.WorkloadName != "" && !matchesWorkload(item.Labels, item.Annotations, input.WorkloadName) && !strings.Contains(strings.ToLower(item.Title), strings.ToLower(input.WorkloadName)) {
			continue
		}
		items = append(items, item)
	}
	if len(items) == 0 && input.AlertID == "" {
		return alerts
	}
	return items
}

func filterRootCauseEvents(events []domainevent.Envelope, input domaincopilot.RootCauseRunInput) []domainevent.Envelope {
	items := make([]domainevent.Envelope, 0, len(events))
	for _, item := range events {
		if input.ClusterID != "" && item.ClusterID != input.ClusterID {
			continue
		}
		if input.Namespace != "" && item.Namespace != input.Namespace {
			continue
		}
		if input.WorkloadName != "" && !strings.Contains(strings.ToLower(item.Summary), strings.ToLower(input.WorkloadName)) {
			continue
		}
		items = append(items, item)
	}
	return items
}

func filterRootCauseAudits(audits []domainaudit.Entry, input domaincopilot.RootCauseRunInput, now time.Time) []domainaudit.Entry {
	items := make([]domainaudit.Entry, 0, len(audits))
	windowStart := now.Add(-time.Duration(input.TimeRangeMinutes) * time.Minute)
	for _, item := range audits {
		if item.CreatedAt.Before(windowStart) {
			continue
		}
		if input.WorkloadName != "" && item.ResourceName != "" && item.ResourceName != input.WorkloadName {
			continue
		}
		if item.Result != "deny" && item.Result != "failure" {
			continue
		}
		items = append(items, item)
	}
	return items
}

func filterRootCauseReleases(releases []domainrelease.Record, input domaincopilot.RootCauseRunInput, now time.Time) []domainrelease.Record {
	items := make([]domainrelease.Record, 0, len(releases))
	windowStart := now.Add(-time.Duration(input.TimeRangeMinutes) * time.Minute)
	for _, item := range releases {
		if item.CreatedAt.Before(windowStart) {
			continue
		}
		if input.Namespace != "" && item.Namespace != input.Namespace {
			continue
		}
		if input.WorkloadName != "" && item.DeploymentName != input.WorkloadName {
			continue
		}
		items = append(items, item)
	}
	return items
}

func filterRootCauseBuilds(builds []domainbuild.Record, now time.Time) []domainbuild.Record {
	windowStart := now.Add(-60 * time.Minute)
	items := make([]domainbuild.Record, 0, len(builds))
	for _, item := range builds {
		if item.CreatedAt.Before(windowStart) {
			continue
		}
		if item.Status == "queued" || item.Status == "failed" || item.Status == "running" {
			items = append(items, item)
		}
	}
	return items
}

func degradedClusterEvidence(clusters []domaincluster.Summary) []domaincopilot.RootCauseEvidence {
	items := make([]domaincopilot.RootCauseEvidence, 0)
	for _, item := range clusters {
		if item.Health.Status == "healthy" || item.Health.Status == "ok" {
			continue
		}
		timestamp := item.Health.LastChecked
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "cluster:" + item.ID,
			Kind:      "cluster",
			Title:     item.Name,
			Summary:   item.Health.Message,
			Severity:  ternarySeverity(item.Health.Status == "degraded", "warning", "info"),
			ClusterID: item.ID,
			Timestamp: &timestamp,
			Attributes: map[string]any{
				"status":      item.Health.Status,
				"environment": item.Environment,
				"clusterId":   item.ID,
			},
		})
	}
	return items
}

func alertRootCauseEvidence(alerts []domainalert.Instance) []domaincopilot.RootCauseEvidence {
	items := make([]domaincopilot.RootCauseEvidence, 0, len(alerts))
	for _, item := range alerts {
		timestamp := item.LastSeenAt
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "alert:" + item.ID,
			Kind:      "alert",
			Title:     item.Title,
			Summary:   item.Summary,
			Severity:  item.Severity,
			ClusterID: item.ClusterID,
			Namespace: item.Namespace,
			Timestamp: &timestamp,
			Attributes: map[string]any{
				"status":      item.Status,
				"fingerprint": item.Fingerprint,
				"clusterId":   item.ClusterID,
				"namespace":   item.Namespace,
			},
		})
	}
	return items
}

func eventRootCauseEvidence(events []domainevent.Envelope, input domaincopilot.RootCauseRunInput) []domaincopilot.RootCauseEvidence {
	items := make([]domaincopilot.RootCauseEvidence, 0, len(events))
	for _, item := range events {
		attributes := map[string]any{
			"source":   item.Source,
			"category": item.Category,
		}
		if input.WorkloadName != "" {
			attributes["workload"] = input.WorkloadName
		}
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:         "event:" + item.ID,
			Kind:       "event",
			Title:      item.Category,
			Summary:    item.Summary,
			Severity:   item.Severity,
			ClusterID:  item.ClusterID,
			Namespace:  item.Namespace,
			Attributes: attributes,
		})
	}
	return items
}

func auditRootCauseEvidence(audits []domainaudit.Entry) []domaincopilot.RootCauseEvidence {
	items := make([]domaincopilot.RootCauseEvidence, 0, len(audits))
	for _, item := range audits {
		timestamp := item.CreatedAt
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "audit:" + item.ID,
			Kind:      "audit",
			Title:     item.Action,
			Summary:   item.Summary,
			Severity:  ternarySeverity(item.Result == "deny", "warning", "info"),
			ClusterID: item.ClusterID,
			Namespace: item.Namespace,
			Timestamp: &timestamp,
			Attributes: map[string]any{
				"result":       item.Result,
				"resourceKind": item.ResourceKind,
				"resourceName": item.ResourceName,
				"clusterId":    item.ClusterID,
				"namespace":    item.Namespace,
			},
		})
	}
	return items
}

func releaseRootCauseEvidence(releases []domainrelease.Record) []domaincopilot.RootCauseEvidence {
	items := make([]domaincopilot.RootCauseEvidence, 0, len(releases))
	for _, item := range releases {
		timestamp := item.CreatedAt
		if item.DeployedAt != nil {
			timestamp = *item.DeployedAt
		}
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "release:" + item.ID,
			Kind:      "release",
			Title:     item.DeploymentName,
			Summary:   fmt.Sprintf("release status=%s", item.Status),
			Severity:  ternarySeverity(item.Status == "deployed", "info", "warning"),
			ClusterID: item.ClusterID,
			Namespace: item.Namespace,
			Timestamp: &timestamp,
			Attributes: map[string]any{
				"status":        item.Status,
				"applicationId": item.ApplicationID,
				"clusterId":     item.ClusterID,
				"namespace":     item.Namespace,
				"workload":      item.DeploymentName,
			},
		})
	}
	return items
}

func buildRootCauseEvidence(builds []domainbuild.Record) []domaincopilot.RootCauseEvidence {
	items := make([]domaincopilot.RootCauseEvidence, 0, len(builds))
	for _, item := range builds {
		timestamp := item.CreatedAt
		items = append(items, domaincopilot.RootCauseEvidence{
			ID:        "build:" + item.ID,
			Kind:      "build",
			Title:     item.ApplicationID,
			Summary:   fmt.Sprintf("build status=%s", item.Status),
			Severity:  ternarySeverity(item.Status == "failed", "warning", "info"),
			Timestamp: &timestamp,
			Attributes: map[string]any{
				"status":        item.Status,
				"applicationId": item.ApplicationID,
			},
		})
	}
	return items
}

func collectEvidenceIDs(items ...domaincopilot.RootCauseEvidence) []string {
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, item.ID)
	}
	return uniqueStrings(nil, values)
}

func flattenRecommendations(hypotheses []domaincopilot.RootCauseHypothesis) []string {
	values := make([]string, 0)
	for _, item := range hypotheses {
		values = append(values, item.Recommendations...)
	}
	return values
}

func uniqueStrings(seed []string, values []string) []string {
	seen := make(map[string]struct{}, len(seed)+len(values))
	out := make([]string, 0, len(seed)+len(values))
	for _, item := range append(seed, values...) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func highestEvidenceSeverity(evidence []domaincopilot.RootCauseEvidence) string {
	best := "info"
	for _, item := range evidence {
		switch item.Severity {
		case "critical":
			return "critical"
		case "warning":
			best = "warning"
		}
	}
	return best
}

func mergePlaybookResults(base, extra map[string]any) map[string]any {
	if base == nil {
		base = map[string]any{}
	}
	for key, value := range extra {
		base[key] = value
	}
	return base
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if strings.TrimSpace(item) == strings.TrimSpace(target) {
			return true
		}
	}
	return false
}

func matchesWorkload(labels, annotations map[string]string, workloadName string) bool {
	workloadName = strings.TrimSpace(strings.ToLower(workloadName))
	if workloadName == "" {
		return true
	}
	for _, source := range []map[string]string{labels, annotations} {
		for key, value := range source {
			if strings.Contains(strings.ToLower(key), "workload") && strings.EqualFold(value, workloadName) {
				return true
			}
		}
	}
	return false
}
