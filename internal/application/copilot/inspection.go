package copilot

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/soha/soha/internal/application/access"
	domainalert "github.com/soha/soha/internal/domain/alert"
	domainapp "github.com/soha/soha/internal/domain/application"
	domainaudit "github.com/soha/soha/internal/domain/audit"
	domainbuild "github.com/soha/soha/internal/domain/build"
	domaincluster "github.com/soha/soha/internal/domain/cluster"
	domaincopilot "github.com/soha/soha/internal/domain/copilot"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainrelease "github.com/soha/soha/internal/domain/release"
	"github.com/soha/soha/internal/platform/runtimeobs"
	"go.uber.org/zap"
)

func (s *Service) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				startedAt := time.Now()
				if s.metrics != nil {
					s.metrics.RecordStart(runtimeobs.ComponentCopilotInspection, "scheduled", 0, 0)
				}
				taskCount, err := s.runDueInspectionTasks(ctx)
				if s.metrics != nil {
					outcome := runtimeobs.OutcomeSucceeded
					if err != nil {
						outcome = runtimeobs.OutcomeFailed
					}
					s.metrics.RecordFinish(runtimeobs.ComponentCopilotInspection, "scheduled", time.Since(startedAt), 0, taskCount, outcome, err)
				}
				if err != nil {
					s.logWarn("copilot inspection cycle failed", zap.Int("tasks", taskCount), zap.Duration("duration", time.Since(startedAt)), zap.Error(err))
					continue
				}
				s.logDebug("copilot inspection cycle completed", zap.Int("tasks", taskCount), zap.Duration("duration", time.Since(startedAt)))
			}
		}
	}()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				count, err := s.sweepAgentRunTimeouts(ctx)
				if err != nil {
					s.logWarn("copilot agent runtime timeout sweep failed", zap.Error(err))
					continue
				}
				if count > 0 {
					s.logWarn("copilot agent runtime runs timed out", zap.Int("runs", count))
				}
			}
		}
	}()
}

func (s *Service) ListInspectionTasks(ctx context.Context, principal domainidentity.Principal) ([]domaincopilot.InspectionTask, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIView); err != nil {
		return nil, err
	}
	return s.repo.ListInspectionTasks(ctx, principal.UserID, 50)
}

func (s *Service) CreateInspectionTask(ctx context.Context, principal domainidentity.Principal, input domaincopilot.InspectionTaskInput, locale string) (domaincopilot.InspectionTask, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIInspectionManage); err != nil {
		return domaincopilot.InspectionTask{}, err
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return domaincopilot.InspectionTask{}, fmt.Errorf("inspection title is required")
	}
	scopeType := strings.TrimSpace(input.ScopeType)
	if scopeType == "" {
		scopeType = "platform"
	}
	task := domaincopilot.InspectionTask{
		ID:              normalizeInspectionID(input.ID, title),
		Title:           title,
		ScopeType:       scopeType,
		ClusterID:       strings.TrimSpace(input.ClusterID),
		Namespace:       strings.TrimSpace(input.Namespace),
		Checks:          normalizeChecks(input.Checks),
		Enabled:         input.Enabled,
		IntervalMinutes: normalizeInterval(input.IntervalMinutes),
		Metadata:        metadataWithLocale(defaultInspectionMetadata(input.Metadata), locale),
		CreatedBy:       principal.UserID,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	return s.repo.CreateInspectionTask(ctx, task)
}

func (s *Service) UpdateInspectionTask(ctx context.Context, principal domainidentity.Principal, taskID string, input domaincopilot.InspectionTaskInput, locale string) (domaincopilot.InspectionTask, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIInspectionManage); err != nil {
		return domaincopilot.InspectionTask{}, err
	}
	task, err := s.repo.GetInspectionTask(ctx, principal.UserID, strings.TrimSpace(taskID))
	if err != nil {
		return domaincopilot.InspectionTask{}, err
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = task.Title
	}
	scopeType := strings.TrimSpace(input.ScopeType)
	if scopeType == "" {
		scopeType = task.ScopeType
	}
	payload := domaincopilot.InspectionTaskInput{
		Title:           title,
		ScopeType:       scopeType,
		ClusterID:       strings.TrimSpace(input.ClusterID),
		Namespace:       strings.TrimSpace(input.Namespace),
		Checks:          normalizeChecks(input.Checks),
		Enabled:         input.Enabled,
		IntervalMinutes: normalizeInterval(input.IntervalMinutes),
		Metadata:        metadataWithLocale(defaultInspectionMetadata(input.Metadata), locale),
	}
	return s.repo.UpdateInspectionTask(ctx, principal.UserID, task.ID, payload)
}

func (s *Service) DeleteInspectionTask(ctx context.Context, principal domainidentity.Principal, taskID string) error {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIInspectionManage); err != nil {
		return err
	}
	return s.repo.DeleteInspectionTask(ctx, principal.UserID, strings.TrimSpace(taskID))
}

func (s *Service) ListInspectionRuns(ctx context.Context, principal domainidentity.Principal, filter domaincopilot.InspectionRunFilter) ([]domaincopilot.InspectionRun, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIView); err != nil {
		return nil, err
	}
	filter.TaskID = strings.TrimSpace(filter.TaskID)
	filter.ClusterID = strings.TrimSpace(filter.ClusterID)
	filter.Namespace = strings.TrimSpace(filter.Namespace)
	filter.Check = strings.TrimSpace(filter.Check)
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	return s.repo.ListInspectionRuns(ctx, principal.UserID, filter)
}

func (s *Service) ExecuteInspectionTask(ctx context.Context, principal domainidentity.Principal, taskID, locale string) (domaincopilot.InspectionRun, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIInspectionRun); err != nil {
		return domaincopilot.InspectionRun{}, err
	}
	task, err := s.repo.GetInspectionTask(ctx, principal.UserID, strings.TrimSpace(taskID))
	if err != nil {
		return domaincopilot.InspectionRun{}, err
	}
	run, err := s.executeInspection(ctx, principal, task, principal.UserID, localeFromInspectionMetadata(task.Metadata, locale))
	if err == nil {
		_ = s.repo.TouchInspectionTaskRun(ctx, task.ID, time.Now().UTC())
	}
	return run, err
}

func (s *Service) CreateSessionFromInspectionRun(ctx context.Context, principal domainidentity.Principal, runID, locale string) (domaincopilot.Session, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincopilot.Session{}, err
	}
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIView); err != nil {
		return domaincopilot.Session{}, err
	}
	runs, err := s.repo.ListInspectionRuns(ctx, principal.UserID, domaincopilot.InspectionRunFilter{Limit: 200})
	if err != nil {
		return domaincopilot.Session{}, err
	}
	var target *domaincopilot.InspectionRun
	for index := range runs {
		if runs[index].ID == strings.TrimSpace(runID) {
			target = &runs[index]
			break
		}
	}
	if target == nil {
		return domaincopilot.Session{}, fmt.Errorf("inspection run not found: %s", runID)
	}
	scope := map[string]any{}
	task, taskErr := s.repo.GetInspectionTask(ctx, principal.UserID, target.TaskID)
	if taskErr == nil {
		if task.ClusterID != "" {
			scope["clusterId"] = task.ClusterID
		}
		if task.Namespace != "" {
			scope["namespace"] = task.Namespace
		}
	}
	title := localize(locale, "巡检复盘会话", "Inspection Review")
	session, err := s.CreateSession(ctx, principal, title, "inspection_review", "", scope, []string{"inspection", "generated"}, locale)
	if err != nil {
		return domaincopilot.Session{}, err
	}
	metadata := parseSessionMetadata(session.Metadata)
	metadata.Summary = target.Summary
	metadata.Source = "inspection_run"
	metadata.PinnedContext = map[string]any{
		"inspectionRunId":  target.ID,
		"inspectionTaskId": target.TaskID,
		"severity":         target.Severity,
		"status":           target.Status,
	}
	metadata.AnalysisRunRefs = append(metadata.AnalysisRunRefs, domaincopilot.AnalysisRunRef{
		ID:        target.ID,
		Kind:      "inspection_review",
		Status:    target.Status,
		CreatedAt: target.StartedAt.Format(time.RFC3339),
	})
	session.Metadata = sessionMetadataMap(metadata)
	session.UpdatedAt = time.Now().UTC()
	updatedSession, err := s.repo.UpdateSession(ctx, principal.UserID, session.ID, session)
	if err != nil {
		return domaincopilot.Session{}, err
	}
	updatedMetadata := parseSessionMetadata(updatedSession.Metadata)
	artifact := buildInspectionReviewArtifact(updatedMetadata.Scope, *target, locale)
	if _, err := s.repo.CreateMessage(ctx, domaincopilot.Message{
		ID:        uuid.NewString(),
		SessionID: updatedSession.ID,
		Role:      "assistant",
		Content:   inspectionReviewInitialMessage(*target, locale),
		Metadata: map[string]any{
			"mode":              "inspection_review",
			"source":            "inspection-run",
			"locale":            normalizeLocale(locale),
			"analysisArtifacts": []domaincopilot.AnalysisArtifact{artifact},
		},
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		return domaincopilot.Session{}, err
	}
	return updatedSession, nil
}

func buildInspectionReviewArtifact(scope domaincopilot.SessionScope, run domaincopilot.InspectionRun, locale string) domaincopilot.AnalysisArtifact {
	timestamp := run.StartedAt
	evidence := make([]domaincopilot.RootCauseEvidence, 0, len(run.Findings))
	recommendations := make([]string, 0, len(run.Findings))
	seenRecommendations := map[string]struct{}{}
	for index, finding := range run.Findings {
		evidenceID := strings.TrimSpace(finding.ID)
		if evidenceID == "" {
			evidenceID = fmt.Sprintf("%s:finding:%d", run.ID, index+1)
		}
		attributes := map[string]any{
			"inspectionRunId":  run.ID,
			"inspectionTaskId": run.TaskID,
			"source":           finding.Source,
		}
		if strings.TrimSpace(finding.Recommendation) != "" {
			attributes["recommendation"] = strings.TrimSpace(finding.Recommendation)
			if _, ok := seenRecommendations[strings.TrimSpace(finding.Recommendation)]; !ok {
				seenRecommendations[strings.TrimSpace(finding.Recommendation)] = struct{}{}
				recommendations = append(recommendations, strings.TrimSpace(finding.Recommendation))
			}
		}
		if finding.Data != nil {
			attributes["data"] = finding.Data
		}
		evidence = append(evidence, domaincopilot.RootCauseEvidence{
			ID:         evidenceID,
			Kind:       "inspection.finding",
			Title:      finding.Title,
			Summary:    finding.Summary,
			Severity:   finding.Severity,
			ClusterID:  scope.ClusterID,
			Namespace:  scope.Namespace,
			Timestamp:  &timestamp,
			Attributes: attributes,
		})
	}
	if len(recommendations) == 0 {
		recommendations = append(recommendations, localize(locale, "复核巡检结果，并优先为高严重度项指定处理人。", "Review inspection findings and assign owners for high-severity items first."))
	}
	snapshot := map[string]any{
		"inspectionRunId":  run.ID,
		"inspectionTaskId": run.TaskID,
		"analysisRunId":    run.ID,
		"analysisKind":     "inspection_review",
		"status":           run.Status,
		"severity":         run.Severity,
	}
	for key, value := range run.Report {
		snapshot[key] = value
	}
	return domaincopilot.AnalysisArtifact{
		Kind:               "inspection_review",
		RunID:              run.ID,
		Title:              localize(locale, "巡检复盘", "Inspection Review"),
		Summary:            firstNonEmpty(run.Summary, localize(locale, "巡检复盘已创建。", "Inspection review created.")),
		Scope:              scope,
		Evidence:           evidence,
		Recommendations:    recommendations,
		Graph:              buildInspectionReviewGraph(scope, run, evidence),
		DataSourceSnapshot: snapshot,
	}
}

func inspectionReviewInitialMessage(run domaincopilot.InspectionRun, locale string) string {
	summary := strings.TrimSpace(run.Summary)
	if summary == "" {
		return localize(locale, fmt.Sprintf("已从巡检运行 %s 创建复盘。", run.ID), fmt.Sprintf("Created an inspection review from run %s.", run.ID))
	}
	return localize(locale, fmt.Sprintf("已从巡检运行 %s 创建复盘。%s", run.ID, summary), fmt.Sprintf("Created an inspection review from run %s. %s", run.ID, summary))
}

func buildInspectionReviewGraph(scope domaincopilot.SessionScope, run domaincopilot.InspectionRun, evidence []domaincopilot.RootCauseEvidence) *domaincopilot.AnalysisGraph {
	rootID := graphRootNodeID(scope)
	runNodeID := "inspection-run:" + run.ID
	nodes := []domaincopilot.AnalysisGraphNode{
		{
			ID:         rootID,
			Kind:       "scope",
			Title:      graphRootTitle(scope),
			Subtitle:   graphRootSubtitle(scope),
			SourceRefs: []string{"inspection"},
			Attributes: map[string]any{
				"clusterId": scope.ClusterID,
				"namespace": scope.Namespace,
			},
		},
		{
			ID:         runNodeID,
			Kind:       "inspection_run",
			Title:      firstNonEmpty(run.Summary, run.ID),
			Subtitle:   run.Status,
			Severity:   run.Severity,
			SourceRefs: []string{"inspection"},
			Attributes: map[string]any{
				"inspectionRunId":  run.ID,
				"inspectionTaskId": run.TaskID,
				"status":           run.Status,
				"severity":         run.Severity,
			},
		},
	}
	edges := []domaincopilot.AnalysisGraphEdge{{
		ID:       rootID + "->" + runNodeID,
		Source:   rootID,
		Target:   runNodeID,
		Relation: "reviews",
		Severity: run.Severity,
	}}
	for _, item := range evidence {
		findingNodeID := "inspection-finding:" + item.ID
		nodes = appendGraphNode(nodes, domaincopilot.AnalysisGraphNode{
			ID:          findingNodeID,
			Kind:        "inspection_finding",
			Title:       item.Title,
			Subtitle:    item.Summary,
			Severity:    item.Severity,
			EvidenceIDs: []string{item.ID},
			SourceRefs:  []string{"inspection"},
			Attributes:  item.Attributes,
		})
		edges = appendGraphEdge(edges, domaincopilot.AnalysisGraphEdge{
			ID:          runNodeID + "->" + findingNodeID,
			Source:      runNodeID,
			Target:      findingNodeID,
			Relation:    "finds",
			Severity:    item.Severity,
			EvidenceIDs: []string{item.ID},
		})
		recommendation := strings.TrimSpace(fmt.Sprint(item.Attributes["recommendation"]))
		if recommendation == "" {
			continue
		}
		recommendationNodeID := "inspection-recommendation:" + item.ID
		nodes = appendGraphNode(nodes, domaincopilot.AnalysisGraphNode{
			ID:          recommendationNodeID,
			Kind:        "recommendation",
			Title:       recommendation,
			Severity:    item.Severity,
			EvidenceIDs: []string{item.ID},
			SourceRefs:  []string{"inspection"},
		})
		edges = appendGraphEdge(edges, domaincopilot.AnalysisGraphEdge{
			ID:          findingNodeID + "->" + recommendationNodeID,
			Source:      findingNodeID,
			Target:      recommendationNodeID,
			Relation:    "suggests",
			Severity:    item.Severity,
			EvidenceIDs: []string{item.ID},
		})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	return &domaincopilot.AnalysisGraph{
		Layout:      "LR",
		FocusNodeID: rootID,
		Nodes:       nodes,
		Edges:       edges,
	}
}

func (s *Service) CreateInspectionTaskFromSession(ctx context.Context, principal domainidentity.Principal, sessionID string, input domaincopilot.InspectionTaskInput, locale string) (domaincopilot.InspectionTask, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIInspectionManage); err != nil {
		return domaincopilot.InspectionTask{}, err
	}
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincopilot.InspectionTask{}, err
	}
	session, err := s.repo.GetSession(ctx, principal.UserID, strings.TrimSpace(sessionID))
	if err != nil {
		return domaincopilot.InspectionTask{}, err
	}
	metadata := parseSessionMetadata(session.Metadata)
	if strings.TrimSpace(input.Title) == "" {
		input.Title = localize(locale, "从调查生成的巡检任务", "Inspection task from investigation")
	}
	if input.ScopeType == "" {
		if metadata.Scope.Namespace != "" {
			input.ScopeType = "namespace"
		} else if metadata.Scope.ClusterID != "" {
			input.ScopeType = "cluster"
		} else {
			input.ScopeType = "platform"
		}
	}
	if input.ClusterID == "" {
		input.ClusterID = metadata.Scope.ClusterID
	}
	if input.Namespace == "" {
		input.Namespace = metadata.Scope.Namespace
	}
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	input.Metadata["sourceSessionId"] = session.ID
	input.Metadata["sourceSessionMode"] = metadata.Mode
	return s.CreateInspectionTask(ctx, principal, input, locale)
}

func (s *Service) runDueInspectionTasks(ctx context.Context) (int, error) {
	tasks, err := s.repo.ListDueInspectionTasks(ctx, time.Now().UTC(), 20)
	if err != nil {
		return 0, err
	}
	if len(tasks) == 0 {
		return 0, nil
	}

	parallelism := s.inspectionParallelism
	if parallelism <= 0 {
		parallelism = 1
	}
	if parallelism > len(tasks) {
		parallelism = len(tasks)
	}

	jobs := make(chan domaincopilot.InspectionTask)
	errCh := make(chan error, len(tasks)*2)
	var wait sync.WaitGroup

	for i := 0; i < parallelism; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for task := range jobs {
				if _, err := s.executeInspection(ctx, systemPrincipal(), task, "system:inspection", localeFromInspectionMetadata(task.Metadata, "")); err != nil {
					s.logWarn("copilot inspection task failed", zap.String("taskID", task.ID), zap.Error(err))
					errCh <- err
					continue
				}
				if err := s.repo.TouchInspectionTaskRun(ctx, task.ID, time.Now().UTC()); err != nil {
					s.logWarn("copilot inspection task touch failed", zap.String("taskID", task.ID), zap.Error(err))
					errCh <- err
				}
			}
		}()
	}

	for _, task := range tasks {
		select {
		case <-ctx.Done():
			close(jobs)
			wait.Wait()
			close(errCh)
			cycleErr := ctx.Err()
			for err := range errCh {
				cycleErr = errors.Join(cycleErr, err)
			}
			return len(tasks), cycleErr
		case jobs <- task:
		}
	}
	close(jobs)
	wait.Wait()
	close(errCh)

	var cycleErr error
	for err := range errCh {
		cycleErr = errors.Join(cycleErr, err)
	}
	return len(tasks), cycleErr
}

func (s *Service) executeInspection(ctx context.Context, principal domainidentity.Principal, task domaincopilot.InspectionTask, triggeredBy, locale string) (domaincopilot.InspectionRun, error) {
	startedAt := time.Now().UTC()
	if s.metrics != nil {
		s.metrics.RecordStart(runtimeobs.ComponentCopilotInspection, task.ID, 0, 1)
	}
	findings, report := s.collectInspectionFindings(ctx, principal, task, locale)
	severity := summarizeSeverity(findings)
	status := "completed"
	if len(findings) == 0 {
		severity = "info"
	}
	summary := localize(locale, fmt.Sprintf("巡检完成，共发现 %d 项结果。", len(findings)), fmt.Sprintf("Inspection completed with %d findings.", len(findings)))
	if len(findings) == 0 {
		summary = localize(locale, "巡检完成，没有发现需要处理的问题。", "Inspection completed with no actionable findings.")
	}
	completedAt := time.Now().UTC()
	run := domaincopilot.InspectionRun{
		ID:          uuid.NewString(),
		TaskID:      task.ID,
		TriggeredBy: triggeredBy,
		Status:      status,
		Severity:    severity,
		Summary:     summary,
		Findings:    findings,
		Report:      report,
		StartedAt:   startedAt,
		CompletedAt: &completedAt,
		CreatedAt:   startedAt,
	}
	savedRun, err := s.repo.CreateInspectionRun(ctx, run)
	if err != nil {
		if s.metrics != nil {
			s.metrics.RecordFinish(runtimeobs.ComponentCopilotInspection, task.ID, time.Since(startedAt), 0, len(findings), runtimeobs.OutcomeFailed, err)
		}
		return domaincopilot.InspectionRun{}, err
	}
	_ = s.syncInspectionSuggestionSession(ctx, task, savedRun)
	if s.metrics != nil {
		s.metrics.RecordFinish(runtimeobs.ComponentCopilotInspection, task.ID, time.Since(startedAt), 0, len(findings), runtimeobs.OutcomeSucceeded, nil)
	}
	s.logDebug("copilot inspection task completed", zap.String("taskID", task.ID), zap.Int("findings", len(findings)), zap.Duration("duration", time.Since(startedAt)))
	return savedRun, nil
}

func (s *Service) collectInspectionFindings(ctx context.Context, principal domainidentity.Principal, task domaincopilot.InspectionTask, locale string) ([]domaincopilot.InspectionFinding, map[string]any) {
	checks, profile, platformNativeEnabled := s.resolveInspectionChecksProfile(ctx, task)
	if !platformNativeEnabled {
		report := map[string]any{
			"scopeType": task.ScopeType,
			"clusterId": task.ClusterID,
			"namespace": task.Namespace,
			"checks":    checks,
			"message":   localize(locale, "当前巡检 profile 没有启用 platform-native 数据源。", "The selected inspection profile does not enable platform-native sources."),
		}
		return nil, report
	}
	clusters, _ := s.clusters.List(ctx)
	alertSummary, _ := s.alerts.Summary(ctx, principal)
	alerts, _ := s.alerts.ListAlerts(ctx, principal, domainalert.Filter{ClusterID: task.ClusterID, Limit: 20})
	channels, _ := s.alerts.ListChannels(ctx, principal)
	audits, _ := s.audits.List(ctx, domainaudit.Filter{Limit: 20})
	builds, _ := s.builds.List(ctx, domainbuild.Filter{Limit: 20})
	releases, _ := s.releases.List(ctx, domainrelease.Filter{ClusterID: task.ClusterID, Limit: 20})
	apps, _ := s.apps.List(ctx, domainapp.Filter{Limit: 200})

	filteredClusters := clusters
	if task.ClusterID != "" {
		filteredClusters = filterClustersByID(clusters, task.ClusterID)
	}
	if task.Namespace != "" {
		alerts = filterAlertsByNamespace(alerts, task.Namespace)
		releases = filterReleasesByNamespace(releases, task.Namespace)
		audits = filterAuditsByNamespace(audits, task.Namespace)
	}
	if task.ClusterID != "" {
		audits = filterAuditsByCluster(audits, task.ClusterID)
	}

	findings := make([]domaincopilot.InspectionFinding, 0)
	for _, check := range checks {
		switch check {
		case "cluster_health":
			findings = append(findings, evaluateClusterHealth(filteredClusters, locale)...)
		case "alert_pressure":
			findings = append(findings, evaluateAlertPressure(alertSummary, alerts, task, locale)...)
		case "channel_readiness":
			findings = append(findings, evaluateChannelReadiness(channels, alertSummary, locale)...)
		case "release_safety":
			findings = append(findings, evaluateReleaseSafety(releases, locale)...)
		case "build_queue":
			findings = append(findings, evaluateBuildQueue(builds, locale)...)
		case "audit_denials":
			findings = append(findings, evaluateAuditDenials(audits, locale)...)
		}
	}

	report := map[string]any{
		"scopeType":         task.ScopeType,
		"clusterId":         task.ClusterID,
		"namespace":         task.Namespace,
		"checks":            checks,
		"analysisProfileId": profile.ID,
		"clusterCount":      len(filteredClusters),
		"alertCount":        len(alerts),
		"channelCount":      len(channels),
		"buildCount":        len(builds),
		"releaseCount":      len(releases),
		"auditCount":        len(audits),
		"applicationCount":  len(apps),
	}
	return findings, report
}

func (s *Service) resolveInspectionChecksProfile(ctx context.Context, task domaincopilot.InspectionTask) ([]string, domaincopilot.AnalysisProfile, bool) {
	profileID, _ := task.Metadata["analysisProfileId"].(string)
	if strings.TrimSpace(profileID) == "" {
		return normalizeChecks(task.Checks), domaincopilot.AnalysisProfile{}, true
	}
	profile, err := s.repo.GetAnalysisProfile(ctx, strings.TrimSpace(profileID))
	if err != nil {
		return normalizeChecks(task.Checks), domaincopilot.AnalysisProfile{}, true
	}
	checks := mapPlaybooksToInspectionChecks(profile.EnabledPlaybooks)
	if len(checks) == 0 {
		checks = normalizeChecks(task.Checks)
	}
	return checks, profile, sourceEnabled(profile.EnabledSources, "platform-native")
}

func mapPlaybooksToInspectionChecks(playbooks []string) []string {
	if len(playbooks) == 0 {
		return nil
	}
	mapped := make([]string, 0, len(playbooks))
	for _, item := range playbooks {
		switch strings.TrimSpace(item) {
		case "cluster-health":
			mapped = append(mapped, "cluster_health")
		case "alert-pressure":
			mapped = append(mapped, "alert_pressure")
		case "release-correlation", "rollout-risk":
			mapped = append(mapped, "release_safety")
		case "build-queue":
			mapped = append(mapped, "build_queue")
		case "access-drift":
			mapped = append(mapped, "audit_denials")
		}
	}
	return uniqueStrings(nil, mapped)
}

func evaluateClusterHealth(clusters []domaincluster.Summary, locale string) []domaincopilot.InspectionFinding {
	degraded := make([]string, 0)
	for _, cluster := range clusters {
		if cluster.Health.Status != "healthy" && cluster.Health.Status != "ok" {
			degraded = append(degraded, cluster.Name)
		}
	}
	if len(degraded) == 0 {
		return nil
	}
	severity := "warning"
	if len(degraded) > 2 {
		severity = "critical"
	}
	return []domaincopilot.InspectionFinding{{
		ID:             uuid.NewString(),
		Title:          localize(locale, "集群健康需要关注", "Cluster health requires attention"),
		Severity:       severity,
		Summary:        localize(locale, fmt.Sprintf("%d 个集群处于异常状态：%s", len(degraded), strings.Join(degraded, ", ")), fmt.Sprintf("%d clusters are degraded: %s", len(degraded), strings.Join(degraded, ", "))),
		Recommendation: localize(locale, "先检查集群诊断和最近事件流，再继续扩大工作负载变更。", "Review cluster diagnostics and recent event streams before expanding workload changes."),
		Source:         "clusters",
		Data:           map[string]any{"degraded": degraded},
	}}
}

func evaluateAlertPressure(summary domainalert.Summary, alerts []domainalert.Instance, task domaincopilot.InspectionTask, locale string) []domaincopilot.InspectionFinding {
	criticalAlerts := make([]string, 0)
	for _, alert := range alerts {
		if alert.Status == "firing" && alert.Severity == "critical" {
			criticalAlerts = append(criticalAlerts, alert.Title)
		}
	}
	if len(criticalAlerts) == 0 && summary.CriticalCount == 0 && summary.FiringCount == 0 {
		return nil
	}
	severity := "warning"
	count := len(criticalAlerts)
	if count == 0 {
		count = summary.CriticalCount
	}
	if count > 0 {
		severity = "critical"
	}
	scope := task.ScopeType
	if scope == "" {
		scope = "platform"
	}
	return []domaincopilot.InspectionFinding{{
		ID:             uuid.NewString(),
		Title:          localize(locale, "告警压力升高", "Alert pressure is elevated"),
		Severity:       severity,
		Summary:        localize(locale, fmt.Sprintf("当前巡检范围 %s 下有 %d 条触发中的告警，其中 %d 条为严重信号。", localizedScopeLabel(scope, locale), summary.FiringCount, count), fmt.Sprintf("Inspection scope %s currently sees %d firing alerts and %d critical signals.", scope, summary.FiringCount, count)),
		Recommendation: localize(locale, "打开告警中心确认这些事件是否应该阻断发布或进入 AI 会话继续分析。", "Open Alerts and validate whether current incidents should block releases or require follow-up AI analysis."),
		Source:         "alerts",
		Data:           map[string]any{"criticalAlerts": criticalAlerts},
	}}
}

func evaluateChannelReadiness(channels []domainalert.NotificationChannel, summary domainalert.Summary, locale string) []domaincopilot.InspectionFinding {
	if summary.FiringCount == 0 {
		return nil
	}
	enabled := 0
	for _, channel := range channels {
		if channel.Enabled {
			enabled++
		}
	}
	if enabled > 0 {
		return nil
	}
	return []domaincopilot.InspectionFinding{{
		ID:             uuid.NewString(),
		Title:          localize(locale, "告警分发没有启用通道", "Alert delivery has no enabled channels"),
		Severity:       "warning",
		Summary:        localize(locale, "当前有触发中的告警，但没有任何启用的下游通知通道。", "There are firing alerts but no enabled downstream notification channels."),
		Recommendation: localize(locale, "至少登记一个启用通道，并通过告警路由把它接入。", "Register at least one enabled channel and bind it through alert routes."),
		Source:         "monitoring",
	}}
}

func evaluateReleaseSafety(releases []domainrelease.Record, locale string) []domaincopilot.InspectionFinding {
	failed := make([]string, 0)
	for _, item := range releases {
		if item.Status != "deployed" {
			failed = append(failed, item.DeploymentName)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return []domaincopilot.InspectionFinding{{
		ID:             uuid.NewString(),
		Title:          localize(locale, "检测到最近发布失败", "Recent release failures detected"),
		Severity:       "warning",
		Summary:        localize(locale, fmt.Sprintf("%d 条最近发布记录不处于已部署状态。", len(failed)), fmt.Sprintf("%d recent release records are not in deployed state.", len(failed))),
		Recommendation: localize(locale, "在继续推进下一个镜像前，先检查发布记录和集群事件。", "Review release records and cluster events before promoting the next image."),
		Source:         "releases",
		Data:           map[string]any{"failedDeployments": failed},
	}}
}

func evaluateBuildQueue(builds []domainbuild.Record, locale string) []domaincopilot.InspectionFinding {
	queued := 0
	for _, item := range builds {
		if item.Status == "queued" {
			queued++
		}
	}
	if queued < 3 {
		return nil
	}
	return []domaincopilot.InspectionFinding{{
		ID:             uuid.NewString(),
		Title:          localize(locale, "构建队列正在堆积", "Build queue is backing up"),
		Severity:       "info",
		Summary:        localize(locale, fmt.Sprintf("最近仍有 %d 条构建处于排队中。", queued), fmt.Sprintf("%d recent builds are still queued.", queued)),
		Recommendation: localize(locale, "在继续叠加手动构建或发布前，先检查 worker 容量。", "Check worker capacity before stacking more manual builds or release attempts."),
		Source:         "builds",
	}}
}

func evaluateAuditDenials(audits []domainaudit.Entry, locale string) []domaincopilot.InspectionFinding {
	denied := 0
	for _, item := range audits {
		if item.Result == "deny" || item.Result == "failure" {
			denied++
		}
	}
	if denied == 0 {
		return nil
	}
	return []domaincopilot.InspectionFinding{{
		ID:             uuid.NewString(),
		Title:          localize(locale, "检测到最近被拒绝或失败的操作", "Recent denied or failed operations detected"),
		Severity:       "warning",
		Summary:        localize(locale, fmt.Sprintf("最近有 %d 条审计记录以拒绝或失败结束。", denied), fmt.Sprintf("%d recent audit records ended in deny or failure.", denied)),
		Recommendation: localize(locale, "检查访问策略和最近操作，区分预期拒绝与平台阻力。", "Review access policy and recent operator actions to distinguish expected denies from platform friction."),
		Source:         "audit",
	}}
}

func summarizeSeverity(findings []domaincopilot.InspectionFinding) string {
	severityRank := map[string]int{"critical": 3, "warning": 2, "info": 1, "default": 0}
	best := "info"
	bestRank := 0
	for _, finding := range findings {
		rank := severityRank[finding.Severity]
		if rank > bestRank {
			best = finding.Severity
			bestRank = rank
		}
	}
	return best
}

func normalizeChecks(checks []string) []string {
	if len(checks) == 0 {
		return []string{"cluster_health", "alert_pressure", "channel_readiness", "release_safety", "build_queue", "audit_denials"}
	}
	items := make([]string, 0, len(checks))
	for _, check := range checks {
		value := strings.TrimSpace(check)
		if value != "" {
			items = append(items, value)
		}
	}
	if len(items) == 0 {
		return []string{"cluster_health", "alert_pressure", "channel_readiness", "release_safety", "build_queue", "audit_denials"}
	}
	return items
}

func normalizeInterval(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func defaultInspectionMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return map[string]any{}
	}
	return metadata
}

func normalizeInspectionID(value, title string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(value, " ", "-"), "_", "-"))
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return uuid.NewString()
	}
	return strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(title, " ", "-"), "_", "-"))
}

func filterClustersByID(clusters []domaincluster.Summary, clusterID string) []domaincluster.Summary {
	items := make([]domaincluster.Summary, 0)
	for _, item := range clusters {
		if item.ID == clusterID {
			items = append(items, item)
		}
	}
	return items
}

func filterAlertsByNamespace(alerts []domainalert.Instance, namespace string) []domainalert.Instance {
	items := make([]domainalert.Instance, 0)
	for _, item := range alerts {
		if item.Namespace == namespace {
			items = append(items, item)
		}
	}
	return items
}

func filterReleasesByNamespace(releases []domainrelease.Record, namespace string) []domainrelease.Record {
	items := make([]domainrelease.Record, 0)
	for _, item := range releases {
		if item.Namespace == namespace {
			items = append(items, item)
		}
	}
	return items
}

func filterAuditsByNamespace(audits []domainaudit.Entry, namespace string) []domainaudit.Entry {
	items := make([]domainaudit.Entry, 0)
	for _, item := range audits {
		if item.Namespace == namespace {
			items = append(items, item)
		}
	}
	return items
}

func filterAuditsByCluster(audits []domainaudit.Entry, clusterID string) []domainaudit.Entry {
	items := make([]domainaudit.Entry, 0)
	for _, item := range audits {
		if item.ClusterID == clusterID {
			items = append(items, item)
		}
	}
	return items
}

func (s *Service) syncInspectionSuggestionSession(ctx context.Context, task domaincopilot.InspectionTask, run domaincopilot.InspectionRun) error {
	locale := localeFromInspectionMetadata(task.Metadata, "")
	session, err := s.findOrCreateInspectionSession(ctx, task)
	if err != nil {
		return err
	}
	_, err = s.repo.CreateMessage(ctx, domaincopilot.Message{
		ID:        uuid.NewString(),
		SessionID: session.ID,
		Role:      "assistant",
		Content:   formatInspectionSuggestionMessage(task, run, locale),
		Metadata: map[string]any{
			"mode":             "inspection",
			"inspectionTaskId": task.ID,
			"inspectionRunId":  run.ID,
			"source":           "inspection-engine",
		},
		CreatedAt: time.Now().UTC(),
	})
	return err
}

func (s *Service) findOrCreateInspectionSession(ctx context.Context, task domaincopilot.InspectionTask) (domaincopilot.Session, error) {
	locale := localeFromInspectionMetadata(task.Metadata, "")
	sessions, err := s.repo.ListSessions(ctx, task.CreatedBy, 100)
	if err != nil {
		return domaincopilot.Session{}, err
	}
	for _, session := range sessions {
		if mode, _ := session.Metadata["mode"].(string); mode == "inspection" {
			if taskID, _ := session.Metadata["inspectionTaskId"].(string); taskID == task.ID {
				return session, nil
			}
		}
	}
	now := time.Now().UTC()
	return s.repo.CreateSession(ctx, domaincopilot.Session{
		ID:        uuid.NewString(),
		Title:     fmt.Sprintf("%s · %s", localize(locale, "巡检", "Inspection"), task.Title),
		CreatedBy: task.CreatedBy,
		Metadata: map[string]any{
			"mode":             "inspection",
			"inspectionTaskId": task.ID,
			"scopeType":        task.ScopeType,
			"clusterId":        task.ClusterID,
			"namespace":        task.Namespace,
			"locale":           locale,
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func formatInspectionSuggestionMessage(task domaincopilot.InspectionTask, run domaincopilot.InspectionRun, locale string) string {
	lines := []string{
		fmt.Sprintf("%s: %s", localize(locale, "巡检任务", "Inspection task"), task.Title),
		fmt.Sprintf("%s: %s", localize(locale, "摘要", "Summary"), run.Summary),
		fmt.Sprintf("%s: %s", localize(locale, "严重级别", "Severity"), run.Severity),
	}
	if task.ScopeType != "" {
		scope := localizedScopeLabel(task.ScopeType, locale)
		if task.ClusterID != "" {
			scope += " / " + task.ClusterID
		}
		if task.Namespace != "" {
			scope += " / " + task.Namespace
		}
		lines = append(lines, fmt.Sprintf("%s: %s", localize(locale, "范围", "Scope"), scope))
	}
	if len(run.Findings) == 0 {
		lines = append(lines, localize(locale, "本次执行没有发现需要处理的问题。", "No actionable findings were detected in this run."))
		return strings.Join(lines, "\n")
	}
	lines = append(lines, "", localize(locale, "重点结果：", "Top findings:"))
	for index, finding := range run.Findings {
		if index == 3 {
			break
		}
		lines = append(lines, fmt.Sprintf("%d. %s [%s]", index+1, finding.Title, finding.Severity))
		lines = append(lines, "   "+finding.Summary)
		if strings.TrimSpace(finding.Recommendation) != "" {
			lines = append(lines, "   "+localize(locale, "建议", "Recommendation")+": "+finding.Recommendation)
		}
	}
	return strings.Join(lines, "\n")
}
