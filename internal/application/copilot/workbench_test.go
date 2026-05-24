package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
	domaincopilot "github.com/kubecrux/kubecrux/internal/domain/copilot"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainsettings "github.com/kubecrux/kubecrux/internal/domain/settings"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
)

func TestToolsetAllowsToolHonorsAdapterSelectionAndDisabledTools(t *testing.T) {
	toolset := domaincopilot.SessionToolset{
		EnabledAdapterIDs: []string{"metrics.v1"},
		DisabledToolNames: []string{"metrics.v1.metrics.anomaly_summary"},
	}

	if toolsetAllowsTool(toolset, "traces.v1", "traces.find_slow_spans") {
		t.Fatalf("expected trace tool to be blocked when traces adapter is not enabled")
	}
	if toolsetAllowsTool(toolset, "metrics.v1", "metrics.anomaly_summary") {
		t.Fatalf("expected disabled metrics tool to be blocked")
	}
	if !toolsetAllowsTool(toolset, "metrics.v1", "metrics.list") {
		t.Fatalf("expected other metrics tools to remain available")
	}
}

func TestBuildInspectionReviewArtifactCreatesEvidenceAndGraph(t *testing.T) {
	startedAt := time.Date(2026, 5, 24, 10, 30, 0, 0, time.UTC)
	run := domaincopilot.InspectionRun{
		ID:       "run-1",
		TaskID:   "task-1",
		Status:   "completed",
		Severity: "critical",
		Summary:  "Inspection completed with 1 finding.",
		Findings: []domaincopilot.InspectionFinding{{
			ID:             "finding-1",
			Title:          "Alert pressure is elevated",
			Severity:       "critical",
			Summary:        "There are critical firing alerts.",
			Recommendation: "Open Alerts and validate whether incidents should block releases.",
			Source:         "alerts",
			Data:           map[string]any{"criticalAlerts": []string{"api-latency"}},
		}},
		Report:    map[string]any{"alertCount": 1},
		StartedAt: startedAt,
	}
	scope := domaincopilot.SessionScope{ClusterID: "cluster-a", Namespace: "default"}

	artifact := buildInspectionReviewArtifact(scope, run, "en-US")

	if artifact.Kind != "inspection_review" || artifact.RunID != run.ID {
		t.Fatalf("unexpected artifact identity: kind=%s runID=%s", artifact.Kind, artifact.RunID)
	}
	if len(artifact.Evidence) != 1 {
		t.Fatalf("expected 1 evidence item, got %d", len(artifact.Evidence))
	}
	if artifact.Evidence[0].Kind != "inspection.finding" || artifact.Evidence[0].Attributes["source"] != "alerts" {
		t.Fatalf("unexpected evidence payload: %#v", artifact.Evidence[0])
	}
	if len(artifact.Recommendations) != 1 {
		t.Fatalf("expected finding recommendation to be promoted, got %d", len(artifact.Recommendations))
	}
	if artifact.DataSourceSnapshot["alertCount"] != 1 {
		t.Fatalf("expected report fields in data source snapshot: %#v", artifact.DataSourceSnapshot)
	}
	if artifact.Graph == nil {
		t.Fatalf("expected review graph")
	}
	if !graphHasNode(artifact.Graph, "inspection-finding:finding-1") {
		t.Fatalf("expected finding node in graph: %#v", artifact.Graph.Nodes)
	}
	if !graphHasEdge(artifact.Graph, "inspection-run:run-1->inspection-finding:finding-1") {
		t.Fatalf("expected run-to-finding edge in graph: %#v", artifact.Graph.Edges)
	}
}

func TestWithinCooldownWindowHonorsConfiguredSeconds(t *testing.T) {
	recentRuns := []domaincopilot.RootCauseRun{{CreatedAt: time.Now().UTC().Add(-2 * time.Minute)}}

	if !withinCooldownWindow(recentRuns, 300) {
		t.Fatalf("expected recent automation run to be inside the cooldown window")
	}
	if withinCooldownWindow(recentRuns, 60) {
		t.Fatalf("expected old automation run to be outside the shorter cooldown window")
	}
	if withinCooldownWindow(recentRuns, 0) {
		t.Fatalf("expected zero cooldown to disable policy-level cooldown")
	}
}

func TestBuildAlertAutomationDedupPrefix(t *testing.T) {
	if got := buildAlertAutomationDedupPrefix(" policy:one "); got != "policy:one:" {
		t.Fatalf("unexpected dedup prefix: %q", got)
	}
	if got := buildAlertAutomationDedupPrefix(" "); got != "" {
		t.Fatalf("expected empty prefix for blank policy id, got %q", got)
	}
}

func TestSessionAnalysisRunPersistenceUsesAutomationContext(t *testing.T) {
	createdBy, triggerType, dedupKey := sessionAnalysisRunPersistence("automation:policy-1", domaincopilot.SessionMetadata{
		PinnedContext: map[string]any{
			"triggerType": "alert_webhook",
			"dedupKey":    "policy-1:fingerprint:cluster-a:default",
		},
	})

	if createdBy != automationRootCauseCreatedBy {
		t.Fatalf("expected automation runs to use automation owner, got %q", createdBy)
	}
	if triggerType != "alert_webhook" {
		t.Fatalf("expected pinned trigger type, got %q", triggerType)
	}
	if dedupKey != "policy-1:fingerprint:cluster-a:default" {
		t.Fatalf("expected pinned dedup key, got %q", dedupKey)
	}
}

func TestApplySessionAnalysisPatchPersistsExplicitAnalysisMode(t *testing.T) {
	metadata := applySessionAnalysisPatch(domaincopilot.SessionMetadata{
		Mode:    "root_cause",
		Summary: "old summary",
		AnalysisRunRefs: []domaincopilot.AnalysisRunRef{{
			ID:     "run-old",
			Kind:   "root_cause",
			Status: "completed",
		}},
	}, map[string]any{
		"mode":    " trace ",
		"summary": " slow spans detected ",
		"analysisRunRefs": []domaincopilot.AnalysisRunRef{{
			ID:     "trace:run-1",
			Kind:   "trace",
			Status: "completed",
		}},
	})

	if metadata.Mode != "trace" {
		t.Fatalf("expected explicit analysis mode to become session mode, got %q", metadata.Mode)
	}
	if metadata.Summary != "slow spans detected" {
		t.Fatalf("expected trimmed summary, got %q", metadata.Summary)
	}
	if len(metadata.AnalysisRunRefs) != 1 || metadata.AnalysisRunRefs[0].Kind != "trace" {
		t.Fatalf("expected trace run refs, got %#v", metadata.AnalysisRunRefs)
	}
}

func TestRunnableSessionAnalysisModeRejectsNonArtifactModes(t *testing.T) {
	for _, mode := range []string{"root_cause", "performance", "trace"} {
		if !isRunnableSessionAnalysisMode(mode) {
			t.Fatalf("expected %q to be runnable", mode)
		}
	}
	for _, mode := range []string{"", "general", "inspection_review", "unknown"} {
		if isRunnableSessionAnalysisMode(mode) {
			t.Fatalf("expected %q to be rejected for explicit analysis", mode)
		}
	}
}

func TestInspectionSessionHandoffRequiresChatAndViewPermissions(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, repo := newInspectionAuthzTestService(map[string][]string{
		"partial": {appaccess.PermObserveAIChatUse},
	})

	_, err := service.CreateSessionFromInspectionRun(context.Background(), domainidentity.Principal{
		UserID: "user-1",
		Roles:  []string{"partial"},
	}, "run-1", "en-US")

	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("expected access denied for missing observe.ai.view, got %v", err)
	}
	if repo.listInspectionRunsCalled {
		t.Fatalf("expected handoff to fail before reading inspection runs")
	}
}

func TestSessionToInspectionHandoffRequiresChatAndManagePermissions(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, repo := newInspectionAuthzTestService(map[string][]string{
		"partial": {appaccess.PermObserveAIInspectionManage},
	})

	_, err := service.CreateInspectionTaskFromSession(context.Background(), domainidentity.Principal{
		UserID: "user-1",
		Roles:  []string{"partial"},
	}, "session-1", domaincopilot.InspectionTaskInput{Title: "Generated inspection"}, "en-US")

	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("expected access denied for missing observe.ai.chat, got %v", err)
	}
	if repo.getSessionCalled {
		t.Fatalf("expected handoff to fail before reading the session")
	}
}

func TestWorkbenchCatalogAllowsChatUsersWithoutExposingProviderSettings(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, _ := newInspectionAuthzTestService(map[string][]string{
		"chat": {appaccess.PermObserveAIChatUse},
	})
	service.settings = inspectionAuthzSettingsResolver{settings: domainsettings.AISettings{
		Provider: domainsettings.AIProviderSettings{
			APIKey: "secret-key",
			Model:  "gpt-test",
		},
		SkillsRegistry: []domainsettings.SkillDefinition{{
			ID:             "skill-1",
			Name:           "Skill One",
			Description:    "Safe skill summary",
			Enabled:        true,
			InputSchema:    map[string]any{"secret": "not exported"},
			OutputSchema:   map[string]any{"secret": "not exported"},
			CapabilityRefs: []string{"logs.query"},
		}},
	}}

	catalog, err := service.GetWorkbenchCatalog(context.Background(), domainidentity.Principal{
		UserID: "user-1",
		Roles:  []string{"chat"},
	})

	if err != nil {
		t.Fatalf("expected chat user to read workbench catalog, got %v", err)
	}
	if len(catalog.DataSources) != 1 {
		t.Fatalf("expected one data source summary, got %#v", catalog.DataSources)
	}
	dataSourcePayload, err := json.Marshal(catalog.DataSources[0])
	if err != nil {
		t.Fatalf("marshal data source summary: %v", err)
	}
	if strings.Contains(string(dataSourcePayload), "credential") {
		t.Fatalf("expected safe data source summary, got %#v", catalog.DataSources)
	}
	if len(catalog.AnalysisProfiles) != 1 || catalog.AnalysisProfiles[0].ID != "profile:inspection" {
		t.Fatalf("expected profile summaries, got %#v", catalog.AnalysisProfiles)
	}
	if len(catalog.SkillsRegistry) != 1 || catalog.SkillsRegistry[0].ID != "skill-1" {
		t.Fatalf("expected safe skill summary, got %#v", catalog.SkillsRegistry)
	}
}

func TestWorkbenchCatalogRejectsUsersWithoutAIWorkbenchPermissions(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, repo := newInspectionAuthzTestService(map[string][]string{
		"none": {},
	})

	_, err := service.GetWorkbenchCatalog(context.Background(), domainidentity.Principal{
		UserID: "user-1",
		Roles:  []string{"none"},
	})

	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("expected access denied without AI permissions, got %v", err)
	}
	if repo.listDataSourcesCalled {
		t.Fatalf("expected catalog to fail before reading data sources")
	}
}

func TestNormalizeAutomationPolicyInputRequiresAlertWebhookTrigger(t *testing.T) {
	_, err := normalizeAutomationPolicyInput(domaincopilot.AutomationPolicyInput{
		Name:              "Manual policy",
		TriggerType:       "manual",
		AnalysisProfileID: "profile:root",
		AnalysisKinds:     []string{"root_cause"},
	})

	if err == nil {
		t.Fatalf("expected unsupported trigger type to be rejected")
	}
}

func TestNormalizeAutomationPolicyInputRejectsUnsupportedAnalysisKind(t *testing.T) {
	_, err := normalizeAutomationPolicyInput(domaincopilot.AutomationPolicyInput{
		Name:              "Inspection policy",
		TriggerType:       "alert_webhook",
		AnalysisProfileID: "profile:root",
		AnalysisKinds:     []string{"inspection_review"},
	})

	if err == nil {
		t.Fatalf("expected unsupported automation analysis kind to be rejected")
	}
}

func TestNormalizeAutomationPolicyInputDeduplicatesSupportedAnalysisKinds(t *testing.T) {
	input, err := normalizeAutomationPolicyInput(domaincopilot.AutomationPolicyInput{
		Name:              "Alert policy",
		TriggerType:       " alert_webhook ",
		AnalysisProfileID: " profile:root ",
		AnalysisKinds:     []string{"root_cause", " performance ", "root_cause", ""},
	})
	if err != nil {
		t.Fatalf("expected valid automation policy input, got %v", err)
	}

	expected := []string{"root_cause", "performance"}
	if len(input.AnalysisKinds) != len(expected) {
		t.Fatalf("expected analysis kinds %v, got %v", expected, input.AnalysisKinds)
	}
	for i, item := range expected {
		if input.AnalysisKinds[i] != item {
			t.Fatalf("expected analysis kind %q at index %d, got %q", item, i, input.AnalysisKinds[i])
		}
	}
}

func newInspectionAuthzTestService(matrix map[string][]string) (*Service, *inspectionAuthzTestRepository) {
	repo := &inspectionAuthzTestRepository{}
	return New(repo, nil, nil, nil, nil, nil, nil, nil, nil, appaccess.NewPermissionResolver(inspectionAuthzRoleReader{matrix: matrix})), repo
}

type inspectionAuthzRoleReader struct {
	matrix map[string][]string
}

func (r inspectionAuthzRoleReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return r.matrix, nil
}

type inspectionAuthzTestRepository struct {
	listInspectionRunsCalled bool
	getSessionCalled         bool
	listDataSourcesCalled    bool
}

func (r *inspectionAuthzTestRepository) ListSessions(context.Context, string, int) ([]domaincopilot.Session, error) {
	return nil, nil
}

func (r *inspectionAuthzTestRepository) GetSession(context.Context, string, string) (domaincopilot.Session, error) {
	r.getSessionCalled = true
	return domaincopilot.Session{}, errors.New("unexpected session read")
}

func (r *inspectionAuthzTestRepository) CreateSession(context.Context, domaincopilot.Session) (domaincopilot.Session, error) {
	return domaincopilot.Session{}, errors.New("unexpected session create")
}

func (r *inspectionAuthzTestRepository) UpdateSession(context.Context, string, string, domaincopilot.Session) (domaincopilot.Session, error) {
	return domaincopilot.Session{}, errors.New("unexpected session update")
}

func (r *inspectionAuthzTestRepository) DeleteSession(context.Context, string, string) error {
	return errors.New("unexpected session delete")
}

func (r *inspectionAuthzTestRepository) ListMessages(context.Context, string, int) ([]domaincopilot.Message, error) {
	return nil, nil
}

func (r *inspectionAuthzTestRepository) CreateMessage(context.Context, domaincopilot.Message) (domaincopilot.Message, error) {
	return domaincopilot.Message{}, errors.New("unexpected message create")
}

func (r *inspectionAuthzTestRepository) ListDataSources(context.Context) ([]domaincopilot.DataSource, error) {
	r.listDataSourcesCalled = true
	return []domaincopilot.DataSource{{
		ID:                "ds-1",
		Name:              "Prometheus",
		SourceKind:        "metrics",
		BackendType:       "prometheus",
		Enabled:           true,
		CredentialRef:     "secret/ref",
		MCPAdapter:        "metrics.v1",
		ValidationStatus:  "success",
		ValidationMessage: "ready",
	}}, nil
}

func (r *inspectionAuthzTestRepository) GetDataSource(context.Context, string) (domaincopilot.DataSource, error) {
	return domaincopilot.DataSource{}, nil
}

func (r *inspectionAuthzTestRepository) CreateDataSource(context.Context, domaincopilot.DataSource) (domaincopilot.DataSource, error) {
	return domaincopilot.DataSource{}, nil
}

func (r *inspectionAuthzTestRepository) UpdateDataSource(context.Context, string, domaincopilot.DataSourceInput) (domaincopilot.DataSource, error) {
	return domaincopilot.DataSource{}, nil
}

func (r *inspectionAuthzTestRepository) UpdateDataSourceValidation(context.Context, string, string, string, time.Time) (domaincopilot.DataSource, error) {
	return domaincopilot.DataSource{}, nil
}

func (r *inspectionAuthzTestRepository) ListAnalysisProfiles(context.Context) ([]domaincopilot.AnalysisProfile, error) {
	return []domaincopilot.AnalysisProfile{{
		ID:      "profile:inspection",
		Name:    "Inspection",
		Mode:    "inspection",
		Enabled: true,
	}}, nil
}

func (r *inspectionAuthzTestRepository) GetAnalysisProfile(context.Context, string) (domaincopilot.AnalysisProfile, error) {
	return domaincopilot.AnalysisProfile{}, nil
}

func (r *inspectionAuthzTestRepository) CreateAnalysisProfile(context.Context, domaincopilot.AnalysisProfile) (domaincopilot.AnalysisProfile, error) {
	return domaincopilot.AnalysisProfile{}, nil
}

func (r *inspectionAuthzTestRepository) UpdateAnalysisProfile(context.Context, string, domaincopilot.AnalysisProfileInput) (domaincopilot.AnalysisProfile, error) {
	return domaincopilot.AnalysisProfile{}, nil
}

func (r *inspectionAuthzTestRepository) ListAutomationPolicies(context.Context) ([]domaincopilot.AutomationPolicy, error) {
	return nil, nil
}

func (r *inspectionAuthzTestRepository) CreateAutomationPolicy(context.Context, domaincopilot.AutomationPolicy) (domaincopilot.AutomationPolicy, error) {
	return domaincopilot.AutomationPolicy{}, nil
}

func (r *inspectionAuthzTestRepository) UpdateAutomationPolicy(context.Context, string, domaincopilot.AutomationPolicyInput) (domaincopilot.AutomationPolicy, error) {
	return domaincopilot.AutomationPolicy{}, nil
}

func (r *inspectionAuthzTestRepository) DeleteAutomationPolicy(context.Context, string) error {
	return nil
}

func (r *inspectionAuthzTestRepository) ListRootCauseRuns(context.Context, string, domaincopilot.RootCauseRunFilter) ([]domaincopilot.RootCauseRun, error) {
	return nil, nil
}

func (r *inspectionAuthzTestRepository) GetRootCauseRun(context.Context, string, string) (domaincopilot.RootCauseRun, error) {
	return domaincopilot.RootCauseRun{}, nil
}

func (r *inspectionAuthzTestRepository) CreateRootCauseRun(context.Context, domaincopilot.RootCauseRun) (domaincopilot.RootCauseRun, error) {
	return domaincopilot.RootCauseRun{}, nil
}

func (r *inspectionAuthzTestRepository) ListInspectionTasks(context.Context, string, int) ([]domaincopilot.InspectionTask, error) {
	return nil, nil
}

func (r *inspectionAuthzTestRepository) GetInspectionTask(context.Context, string, string) (domaincopilot.InspectionTask, error) {
	return domaincopilot.InspectionTask{}, nil
}

func (r *inspectionAuthzTestRepository) ListDueInspectionTasks(context.Context, time.Time, int) ([]domaincopilot.InspectionTask, error) {
	return nil, nil
}

func (r *inspectionAuthzTestRepository) CreateInspectionTask(context.Context, domaincopilot.InspectionTask) (domaincopilot.InspectionTask, error) {
	return domaincopilot.InspectionTask{}, errors.New("unexpected inspection task create")
}

func (r *inspectionAuthzTestRepository) UpdateInspectionTask(context.Context, string, string, domaincopilot.InspectionTaskInput) (domaincopilot.InspectionTask, error) {
	return domaincopilot.InspectionTask{}, nil
}

func (r *inspectionAuthzTestRepository) DeleteInspectionTask(context.Context, string, string) error {
	return nil
}

func (r *inspectionAuthzTestRepository) TouchInspectionTaskRun(context.Context, string, time.Time) error {
	return nil
}

func (r *inspectionAuthzTestRepository) ListInspectionRuns(context.Context, string, domaincopilot.InspectionRunFilter) ([]domaincopilot.InspectionRun, error) {
	r.listInspectionRunsCalled = true
	return nil, errors.New("unexpected inspection run list")
}

func (r *inspectionAuthzTestRepository) CreateInspectionRun(context.Context, domaincopilot.InspectionRun) (domaincopilot.InspectionRun, error) {
	return domaincopilot.InspectionRun{}, nil
}

type inspectionAuthzSettingsResolver struct {
	settings domainsettings.AISettings
}

func (r inspectionAuthzSettingsResolver) ResolveAISettings(context.Context) (domainsettings.AISettings, error) {
	return r.settings, nil
}

func graphHasNode(graph *domaincopilot.AnalysisGraph, nodeID string) bool {
	for _, node := range graph.Nodes {
		if node.ID == nodeID {
			return true
		}
	}
	return false
}

func graphHasEdge(graph *domaincopilot.AnalysisGraph, edgeID string) bool {
	for _, edge := range graph.Edges {
		if edge.ID == edgeID {
			return true
		}
	}
	return false
}
