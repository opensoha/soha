package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) RunProfessionalCapability(ctx context.Context, principal domainidentity.Principal, kind string, input sohaapi.WorkbenchProfessionalCapabilityInput) (domaincopilot.AnalysisArtifact, error) {
	if !isRunnableSessionAnalysisMode(kind) {
		return domaincopilot.AnalysisArtifact{}, apperrors.ErrUnsupportedOperation
	}
	if err := s.authorizeProfessionalCapability(ctx, principal, kind); err != nil {
		return domaincopilot.AnalysisArtifact{}, err
	}
	if err := validateNativeTaskIntent(&input.Intent); err != nil {
		return domaincopilot.AnalysisArtifact{}, err
	}
	session, err := s.ownedSession(ctx, principal, input.SessionID)
	if err != nil {
		return domaincopilot.AnalysisArtifact{}, err
	}
	metadata := parseSessionMetadata(session.Metadata)
	raw, err := json.Marshal(input.Toolset)
	if err != nil {
		return domaincopilot.AnalysisArtifact{}, err
	}
	var selected domaincopilot.SessionToolset
	if err = json.Unmarshal(raw, &selected); err != nil {
		return domaincopilot.AnalysisArtifact{}, err
	}
	scopeRaw, err := json.Marshal(input.Scope)
	if err != nil {
		return domaincopilot.AnalysisArtifact{}, err
	}
	var overrides map[string]any
	if err = json.Unmarshal(scopeRaw, &overrides); err != nil {
		return domaincopilot.AnalysisArtifact{}, err
	}
	toolset := narrowWorkbenchToolset(metadata.Toolset, selected, overrides, metadata.Scope)
	scope := mergeSessionScope(metadata.Scope, toolset.ScopeOverrides)
	if toolset.ScopeOverrides == nil {
		toolset.ScopeOverrides = map[string]any{}
	}
	toolset.ScopeOverrides["dataSourceId"] = professionalDataSource(input.Intent, kind)
	toolset.ScopeOverrides["metricKey"] = input.Intent.MetricKey
	toolset.ScopeOverrides["traceId"] = input.Intent.TraceID
	if input.Intent.MinDurationMs != nil {
		toolset.ScopeOverrides["minDurationMs"] = *input.Intent.MinDurationMs
	}
	switch kind {
	case "root_cause":
		_, _, artifact, err := s.runSessionRootCause(ctx, principal, session.ID, scope, toolset, input.Question, detectMessageLocale(input.Question, input.Locale), input.Intent.RootCauseProfileID)
		return artifact, err
	case "performance":
		_, artifact, err := s.runSessionPerformance(ctx, session.ID, scope, toolset, input.Question, principal.UserID, "native_task", "")
		return artifact, err
	case "trace":
		_, artifact, err := s.runSessionTrace(ctx, session.ID, scope, toolset, input.Question, principal.UserID, "native_task", "")
		return artifact, err
	default:
		return s.reviewActualInspection(ctx, principal, scope, input.Intent.InspectionRunIDs, detectMessageLocale(input.Question, input.Locale))
	}
}

func (s *Service) authorizeProfessionalCapability(ctx context.Context, principal domainidentity.Principal, kind string) error {
	permission := appaccess.PermObserveMonitoringView
	switch kind {
	case "root_cause":
		permission = appaccess.PermObserveAIRootCauseRun
	case "inspection_review":
		permission = appaccess.PermObserveAIView
	}
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return err
	}
	return s.authorizePrincipal(ctx, principal, permission)
}

func professionalDataSource(intent domaincopilot.TaskIntent, kind string) string {
	if kind == "performance" && intent.PerformanceDataSourceID != "" {
		return intent.PerformanceDataSourceID
	}
	if kind == "trace" && intent.TraceDataSourceID != "" {
		return intent.TraceDataSourceID
	}
	return intent.DataSourceID
}

func (s *Service) selectProfessionalSource(ctx context.Context, kind, adapter string, toolset domaincopilot.SessionToolset) (domaincopilot.DataSource, error) {
	sources, err := s.dataSources.ListDataSources(ctx)
	if err != nil {
		return domaincopilot.DataSource{}, err
	}
	selected := strings.TrimSpace(stringValue(toolset.ScopeOverrides["dataSourceId"]))
	var found []domaincopilot.DataSource
	for _, source := range sources {
		if source.Enabled && source.SourceKind == kind && source.MCPAdapter == adapter && toolsetAllowsAdapter(toolset, adapter) && (selected == "" || source.ID == selected) {
			found = append(found, source)
		}
	}
	if len(found) == 0 {
		return domaincopilot.DataSource{}, fmt.Errorf("%w: no selected %s data source is available", apperrors.ErrUnsupportedOperation, kind)
	}
	if len(found) > 1 {
		return domaincopilot.DataSource{}, fmt.Errorf("%w: select one %s data source explicitly", apperrors.ErrInvalidArgument, kind)
	}
	return found[0], nil
}

func (s *Service) reviewActualInspection(ctx context.Context, principal domainidentity.Principal, scope domaincopilot.SessionScope, ids []string, locale string) (domaincopilot.AnalysisArtifact, error) {
	if len(ids) == 0 || len(ids) > 20 {
		return domaincopilot.AnalysisArtifact{}, fmt.Errorf("%w: select actual inspection runs to review", apperrors.ErrInvalidArgument)
	}
	runs, err := s.inspectionRuns.ListInspectionRuns(ctx, principal.UserID, domaincopilot.InspectionRunFilter{RunIDs: ids, ClusterID: scope.ClusterID, Namespace: scope.Namespace, Limit: 20})
	if err != nil {
		return domaincopilot.AnalysisArtifact{}, err
	}
	if len(runs) != len(ids) {
		return domaincopilot.AnalysisArtifact{}, fmt.Errorf("%w: inspection records unavailable in selected scope", apperrors.ErrAccessDenied)
	}
	var combined domaincopilot.AnalysisArtifact
	for _, run := range runs {
		if run.CompletedAt == nil || run.Status == "running" {
			return domaincopilot.AnalysisArtifact{}, fmt.Errorf("%w: selected inspection is unfinished", apperrors.ErrInvalidArgument)
		}
		task, err := s.inspectionTasks.GetInspectionTask(ctx, principal.UserID, run.TaskID)
		if err != nil {
			return domaincopilot.AnalysisArtifact{}, err
		}
		artifact := buildInspectionReviewArtifact(domaincopilot.SessionScope{ClusterID: task.ClusterID, Namespace: task.Namespace}, run, locale)
		if combined.RunID == "" {
			combined = artifact
			combined.DataSourceSnapshot = maps.Clone(artifact.DataSourceSnapshot)
		} else {
			combined.Evidence = append(combined.Evidence, artifact.Evidence...)
			combined.Summary += "\n" + artifact.Summary
		}
	}
	combined.Kind = "inspection_review"
	combined.Scope = scope
	return combined, nil
}

func professionalTraceMinimum(toolset domaincopilot.SessionToolset) time.Duration {
	if value, ok := toolset.ScopeOverrides["minDurationMs"]; ok {
		return time.Duration(max(0, intCondition(value))) * time.Millisecond
	}
	return 250 * time.Millisecond
}
