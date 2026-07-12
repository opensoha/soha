package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appresource "github.com/opensoha/soha/internal/application/resource"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainevent "github.com/opensoha/soha/internal/domain/event"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmcp "github.com/opensoha/soha/internal/domain/mcp"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type stubPlatformResourceService struct {
	*appresource.Workloads
	*appresource.Configuration
	*appresource.Network
	*appresource.Storage
	*appresource.RBAC
	*appresource.CustomResources
	*appresource.Helm
	*appresource.Inventory
	*appresource.GenericResources
	*appresource.PortForwards
	*appresource.Events

	listPodsClusterID              string
	listPodsNamespace              string
	listPodsPrincipal              domainidentity.Principal
	listPodsCalled                 bool
	listPodsErr                    error
	podLogsNamespace               string
	podLogsContainer               string
	podLogsTailLines               int64
	podLogsSince                   int64
	podLogsPrevious                bool
	applyPodYAMLCalled             bool
	applyPodYAMLBody               string
	serviceListErr                 error
	statefulSetMetricsCalled       bool
	statefulSetMetricsClusterID    string
	statefulSetMetricsNamespace    string
	statefulSetMetricsName         string
	statefulSetMetricsRangeMinutes int
	statefulSetMetricsStepSeconds  int
	daemonSetMetricsCalled         bool
	daemonSetMetricsClusterID      string
	daemonSetMetricsNamespace      string
	daemonSetMetricsName           string
	daemonSetMetricsRangeMinutes   int
	daemonSetMetricsStepSeconds    int
	crdCreateNamespace             string
	crdCreateContent               string
	helmInstallInput               domainresource.HelmChartInstallInput
}

func newStubPlatformResourceService() *stubPlatformResourceService {
	service := appresource.New(appresource.Dependencies{})
	return &stubPlatformResourceService{
		Workloads: service.Workloads(), Configuration: service.Configuration(),
		Network: service.Network(), Storage: service.Storage(), RBAC: service.RBAC(),
		CustomResources: service.CustomResources(), Helm: service.Helm(), Inventory: service.Inventory(),
		GenericResources: service.GenericResources(), PortForwards: service.PortForwards(), Events: service.Events(),
	}
}

func newTestPlatformHandler(
	clusters ClusterService,
	resources *stubPlatformResourceService,
	audit AuditService,
	events EventService,
	operations OperationService,
	integration IntegrationService,
) *PlatformHandler {
	base := newStubPlatformResourceService()
	if resources != nil {
		base = resources
	}
	resourceServices := completeResourceServices(base)
	if clusters == nil {
		clusters = &stubPlatformClusterService{}
	}
	if audit == nil {
		audit = &stubPlatformAuditService{}
	}
	if events == nil {
		events = &stubPlatformEventService{}
	}
	if operations == nil {
		operations = &stubPlatformOperationService{}
	}
	if integration == nil {
		integration = stubPlatformIntegrationService{}
	}
	handler, err := NewPlatformHandlerWithResources(PlatformDependencies{
		Clusters:    clusters,
		Resources:   resourceServices,
		Audit:       audit,
		Events:      events,
		Operations:  operations,
		Integration: integration,
	})
	if err != nil {
		panic(err)
	}
	return handler
}

func completeResourceServices(resources *stubPlatformResourceService) ResourceServices {
	return ResourceServices{
		PodReader: resources, PodEditor: resources, PodDiagnostics: resources, PodStreams: resources,
		DeploymentReader: resources, DeploymentEditor: resources,
		StatefulSetReader: resources, StatefulSetEditor: resources,
		DaemonSetReader: resources, DaemonSetEditor: resources,
		Jobs: resources, CronJobs: resources, WorkloadInventory: resources,
		Creator: resources, ConfigMaps: resources, Secrets: resources, ConfigurationInventory: resources,
		NetworkOverview: resources, NetworkInventory: resources, GatewayRouting: resources, GatewayPolicy: resources,
		PersistentVolumeClaims: resources, PersistentVolumes: resources, StorageClasses: resources,
		NamespacedRBAC: resources, ClusterRBAC: resources,
		CRDReader: resources, CRDEditor: resources,
		Helm: resources, HelmReleaseReader: resources, HelmReleaseEditor: resources,
		Namespaces: resources, NodeReader: resources, NodeEditor: resources,
		Generic: resources, Events: resources, PortForwards: resources,
	}
}

type stubPlatformClusterService struct{ matrixCalled bool }

type stubPlatformEventService struct {
	validateErr    error
	validatedToken string
	ingestInput    domainevent.ConnectorEventIngestInput
	ingestCalled   bool
}

type stubPlatformAuditService struct {
	filter        domainaudit.Filter
	summaryCalled bool
	exportCalled  bool
}

type stubPlatformOperationService struct {
	filter        domainoperation.Filter
	summaryCalled bool
	exportCalled  bool
}

type stubPlatformIntegrationService struct{}

func (stubPlatformIntegrationService) ListCapabilities(context.Context) ([]domainmcp.Capability, error) {
	return nil, nil
}

func (s *stubPlatformClusterService) List(context.Context) ([]domaincluster.Summary, error) {
	return nil, nil
}
func (s *stubPlatformClusterService) ListAccessible(context.Context, domainidentity.Principal) ([]domaincluster.Summary, error) {
	return nil, nil
}
func (s *stubPlatformClusterService) Describe(context.Context, domainidentity.Principal, string) (domaincluster.Detail, error) {
	return domaincluster.Detail{}, nil
}
func (s *stubPlatformClusterService) Register(context.Context, domainidentity.Principal, domaincluster.RegisterInput) (domaincluster.Summary, error) {
	return domaincluster.Summary{}, nil
}
func (s *stubPlatformClusterService) Update(context.Context, domainidentity.Principal, string, domaincluster.UpdateInput) (domaincluster.Summary, error) {
	return domaincluster.Summary{}, nil
}
func (s *stubPlatformClusterService) Delete(context.Context, domainidentity.Principal, string) error {
	return nil
}

func (s *stubPlatformAuditService) ListAuthorized(_ context.Context, _ domainidentity.Principal, filter domainaudit.Filter) ([]domainaudit.Entry, error) {
	s.filter = filter
	return []domainaudit.Entry{{ID: "audit-1"}}, nil
}

func (s *stubPlatformAuditService) SummaryAuthorized(_ context.Context, _ domainidentity.Principal, filter domainaudit.Filter) (domainaudit.Summary, error) {
	s.summaryCalled = true
	s.filter = filter
	total := int64(2)
	return domainaudit.Summary{Total: total, RetentionDays: 90, ExpiredEntryCount: 1, ExportRecommended: true, RecommendedNextAction: "export_then_purge_expired_audit_logs"}, nil
}

func (s *stubPlatformAuditService) ExportCSVAuthorized(_ context.Context, _ domainidentity.Principal, filter domainaudit.Filter) (domainaudit.Export, error) {
	s.exportCalled = true
	s.filter = filter
	return domainaudit.Export{
		Filename:    "audit-logs-20260612T100000Z.csv",
		Content:     []byte("id,actorId\n audit-1,user-1\n"),
		ContentType: "text/csv; charset=utf-8",
		Count:       1,
		GeneratedAt: time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC),
	}, nil
}

func (s *stubPlatformOperationService) ListAuthorized(_ context.Context, _ domainidentity.Principal, filter domainoperation.Filter) ([]domainoperation.Entry, error) {
	s.filter = filter
	return []domainoperation.Entry{{ID: "op-1"}}, nil
}

func (s *stubPlatformOperationService) SummaryAuthorized(_ context.Context, _ domainidentity.Principal, filter domainoperation.Filter) (domainoperation.Summary, error) {
	s.summaryCalled = true
	s.filter = filter
	return domainoperation.Summary{Total: 2, FailureCount: 1, RetentionDays: 90, ExportRecommended: true, RecommendedNextAction: "inspect_failed_operations"}, nil
}

func (s *stubPlatformOperationService) ExportCSVAuthorized(_ context.Context, _ domainidentity.Principal, filter domainoperation.Filter) (domainoperation.Export, error) {
	s.exportCalled = true
	s.filter = filter
	return domainoperation.Export{
		Filename:    "operation-logs-20260612T100000Z.csv",
		Content:     []byte("id,actorId\nop-1,user-1\n"),
		ContentType: "text/csv; charset=utf-8",
		Count:       1,
		GeneratedAt: time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC),
	}, nil
}

func (s *stubPlatformEventService) List(context.Context, int) ([]domainevent.Envelope, error) {
	return nil, nil
}

func (s *stubPlatformEventService) Get(context.Context, string) (domainevent.Envelope, error) {
	return domainevent.Envelope{}, nil
}

func (s *stubPlatformEventService) ValidateConnectorEventSinkToken(token string) error {
	s.validatedToken = token
	return s.validateErr
}

func (s *stubPlatformEventService) IngestConnectorEvents(_ context.Context, input domainevent.ConnectorEventIngestInput) (int, error) {
	s.ingestCalled = true
	s.ingestInput = input
	return len(input.Events), nil
}

func (s *stubPlatformClusterService) CapabilityMatrix(context.Context, domainidentity.Principal) ([]domaincluster.CapabilityMatrixEntry, error) {
	s.matrixCalled = true
	return []domaincluster.CapabilityMatrixEntry{
		{
			Key:              "resource.yaml.apply",
			Label:            "YAML apply and delete",
			Category:         "configuration",
			RequiredScopes:   []string{"cluster", "namespace"},
			RiskLevel:        domaincluster.CapabilityRiskMutate,
			RequiresApproval: true,
			DocsURL:          "/operations/agent-runtime",
			Direct:           domaincluster.CapabilityModeSupport{Status: domaincluster.CapabilityStatusAvailable},
			Agent: domaincluster.CapabilityModeSupport{
				Status: domaincluster.CapabilityStatusUnsupported,
				Reason: "YAML apply and delete are not supported for agent-connected clusters yet",
				Notes:  []string{"YAML apply and delete are not supported for agent-connected clusters yet"},
			},
		},
	}, nil
}

func (s *stubPlatformResourceService) ListPods(_ context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.PodView, error) {
	s.listPodsCalled = true
	s.listPodsPrincipal = principal
	s.listPodsClusterID = clusterID
	s.listPodsNamespace = namespace
	if s.listPodsErr != nil {
		return nil, s.listPodsErr
	}
	return []domainresource.PodView{{Name: "api-0", Namespace: namespace, Phase: "Running"}}, nil
}

func (s *stubPlatformResourceService) GetPodLogs(_ context.Context, _ domainidentity.Principal, _, namespace, podName, container string, tailLines, sinceSeconds int64, previous bool) (domainresource.PodLogsView, error) {
	s.podLogsNamespace = namespace
	s.podLogsContainer = container
	s.podLogsTailLines = tailLines
	s.podLogsSince = sinceSeconds
	s.podLogsPrevious = previous
	return domainresource.PodLogsView{PodName: podName, Namespace: namespace, Container: container, TailLines: tailLines, Previous: previous}, nil
}

func (s *stubPlatformResourceService) ApplyPodYAML(_ context.Context, _ domainidentity.Principal, _, namespace, podName, content string) (domainresource.ResourceYAMLView, error) {
	s.applyPodYAMLCalled = true
	s.applyPodYAMLBody = content
	return domainresource.ResourceYAMLView{Kind: "Pod", Name: podName, Namespace: namespace, Content: content}, nil
}

func (s *stubPlatformResourceService) ListServices(context.Context, domainidentity.Principal, string, string) ([]domainresource.ServiceView, error) {
	return nil, s.serviceListErr
}

func (s *stubPlatformResourceService) GetStatefulSetMetrics(_ context.Context, _ domainidentity.Principal, clusterID, namespace, statefulSetName string, rangeMinutes, stepSeconds int) (domainresource.ResourceMetricsView, error) {
	s.statefulSetMetricsCalled = true
	s.statefulSetMetricsClusterID = clusterID
	s.statefulSetMetricsNamespace = namespace
	s.statefulSetMetricsName = statefulSetName
	s.statefulSetMetricsRangeMinutes = rangeMinutes
	s.statefulSetMetricsStepSeconds = stepSeconds
	return domainresource.ResourceMetricsView{
		ResourceKind: "StatefulSet",
		ResourceName: statefulSetName,
		Namespace:    namespace,
		RangeMinutes: rangeMinutes,
		StepSeconds:  stepSeconds,
	}, nil
}

func (s *stubPlatformResourceService) GetDaemonSetMetrics(_ context.Context, _ domainidentity.Principal, clusterID, namespace, daemonSetName string, rangeMinutes, stepSeconds int) (domainresource.ResourceMetricsView, error) {
	s.daemonSetMetricsCalled = true
	s.daemonSetMetricsClusterID = clusterID
	s.daemonSetMetricsNamespace = namespace
	s.daemonSetMetricsName = daemonSetName
	s.daemonSetMetricsRangeMinutes = rangeMinutes
	s.daemonSetMetricsStepSeconds = stepSeconds
	return domainresource.ResourceMetricsView{
		ResourceKind: "DaemonSet",
		ResourceName: daemonSetName,
		Namespace:    namespace,
		RangeMinutes: rangeMinutes,
		StepSeconds:  stepSeconds,
	}, nil
}

func (s *stubPlatformResourceService) CreateCRDResourceFromYAML(_ context.Context, _ domainidentity.Principal, _, _, namespace, content string) (domainresource.ResourceYAMLView, error) {
	s.crdCreateNamespace = namespace
	s.crdCreateContent = content
	return domainresource.ResourceYAMLView{Kind: "Widget", Name: "sample", Namespace: namespace, Content: content}, nil
}

func (s *stubPlatformResourceService) InstallHelmChart(_ context.Context, _ domainidentity.Principal, _ string, input domainresource.HelmChartInstallInput) (domainresource.HelmChartInstallResult, error) {
	s.helmInstallInput = input
	return domainresource.HelmChartInstallResult{Name: input.ReleaseName, Namespace: input.Namespace, Status: "deployed"}, nil
}

func newPlatformTestContext(method, target, body string, params gin.Params) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = params
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1", UserName: "operator", Roles: []string{"platform-admin"}})
	return ctx, recorder
}

func TestPlatformListPodsPassesScopeAndPrincipal(t *testing.T) {
	resources := newStubPlatformResourceService()
	handler := newTestPlatformHandler(nil, resources, nil, nil, nil, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/clusters/cluster-a/workloads/pods?namespace=team-a", "", gin.Params{{Key: "clusterID", Value: "cluster-a"}})

	handler.ListPods(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !resources.listPodsCalled {
		t.Fatal("ListPods was not called")
	}
	if resources.listPodsClusterID != "cluster-a" || resources.listPodsNamespace != "team-a" {
		t.Fatalf("scope = %s/%s, want cluster-a/team-a", resources.listPodsClusterID, resources.listPodsNamespace)
	}
	if resources.listPodsPrincipal.UserID != "u-1" {
		t.Fatalf("principal.UserID = %q, want u-1", resources.listPodsPrincipal.UserID)
	}

	var payload struct {
		Items []domainresource.PodView `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].Name != "api-0" {
		t.Fatalf("items = %#v", payload.Items)
	}
}

func TestPlatformGetStatefulSetMetricsBindsQuery(t *testing.T) {
	resources := newStubPlatformResourceService()
	handler := newTestPlatformHandler(nil, resources, nil, nil, nil, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/clusters/cluster-a/workloads/statefulsets/web/metrics?namespace=team-a&rangeMinutes=30&stepSeconds=15", "", gin.Params{
		{Key: "clusterID", Value: "cluster-a"},
		{Key: "statefulSetName", Value: "web"},
	})

	handler.GetStatefulSetMetrics(ctx)

	assertPlatformMetricsCall(t, recorder, platformMetricsCall{
		called: resources.statefulSetMetricsCalled, clusterID: resources.statefulSetMetricsClusterID,
		namespace: resources.statefulSetMetricsNamespace, name: resources.statefulSetMetricsName,
		rangeMinutes: resources.statefulSetMetricsRangeMinutes, stepSeconds: resources.statefulSetMetricsStepSeconds,
	}, "web")
}

func TestPlatformGetDaemonSetMetricsBindsQuery(t *testing.T) {
	resources := newStubPlatformResourceService()
	handler := newTestPlatformHandler(nil, resources, nil, nil, nil, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/clusters/cluster-a/workloads/daemonsets/node-agent/metrics?namespace=team-a&rangeMinutes=30&stepSeconds=15", "", gin.Params{
		{Key: "clusterID", Value: "cluster-a"},
		{Key: "daemonSetName", Value: "node-agent"},
	})

	handler.GetDaemonSetMetrics(ctx)

	assertPlatformMetricsCall(t, recorder, platformMetricsCall{
		called: resources.daemonSetMetricsCalled, clusterID: resources.daemonSetMetricsClusterID,
		namespace: resources.daemonSetMetricsNamespace, name: resources.daemonSetMetricsName,
		rangeMinutes: resources.daemonSetMetricsRangeMinutes, stepSeconds: resources.daemonSetMetricsStepSeconds,
	}, "node-agent")
}

type platformMetricsCall struct {
	called                    bool
	clusterID, namespace      string
	name                      string
	rangeMinutes, stepSeconds int
}

func assertPlatformMetricsCall(t *testing.T, recorder *httptest.ResponseRecorder, call platformMetricsCall, wantName string) {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !call.called {
		t.Fatal("metrics service was not called")
	}
	if call.clusterID != "cluster-a" || call.namespace != "team-a" || call.name != wantName {
		t.Fatalf("scope = %#v/%#v/%#v, want cluster-a/team-a/%s", call.clusterID, call.namespace, call.name, wantName)
	}
	if call.rangeMinutes != 30 || call.stepSeconds != 15 {
		t.Fatalf("metrics window = %d/%d, want 30/15", call.rangeMinutes, call.stepSeconds)
	}
}

func TestPlatformClusterCapabilityMatrixReturnsItems(t *testing.T) {
	clusters := &stubPlatformClusterService{}
	handler := newTestPlatformHandler(clusters, nil, nil, nil, nil, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/clusters/capabilities", "", nil)

	handler.ClusterCapabilityMatrix(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !clusters.matrixCalled {
		t.Fatal("CapabilityMatrix was not called")
	}
	var payload struct {
		Items []domaincluster.CapabilityMatrixEntry `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].Key != "resource.yaml.apply" {
		t.Fatalf("items = %#v", payload.Items)
	}
	if payload.Items[0].Agent.Status != domaincluster.CapabilityStatusUnsupported {
		t.Fatalf("agent status = %q, want unsupported", payload.Items[0].Agent.Status)
	}
	if payload.Items[0].RiskLevel != domaincluster.CapabilityRiskMutate || !payload.Items[0].RequiresApproval {
		t.Fatalf("contract fields = risk %q approval %v, want mutate approval", payload.Items[0].RiskLevel, payload.Items[0].RequiresApproval)
	}
}

func TestPlatformListAuditLogsBindsExpandedFilter(t *testing.T) {
	audit := &stubPlatformAuditService{}
	handler := newTestPlatformHandler(nil, nil, audit, nil, nil, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/audit/logs?actorId=user-1&actorName=Operator&clusterId=cluster-a&namespace=prod&resourceKind=Deployment&resourceName=api&action=platform.deployment.restart&result=success&requestID=req-1&requestPath=/api/v1/restart&method=post&sourceIP=127.0.0.1&approvalRequestId=approval-1&agentRunId=agent-run-1&rootCauseRunId=root-cause-1&metadataKey=usageSnapshot.templateId&metadataValue=tpl-1&from=2026-06-12T08:00:00Z&to=2026-06-12T09:00:00Z&limit=25", "", nil)

	handler.ListAuditLogs(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	assertAuditFilterScope(t, audit.filter)
	assertAuditFilterCorrelation(t, audit.filter)
}

func TestPlatformAuditSummaryBindsFilter(t *testing.T) {
	audit := &stubPlatformAuditService{}
	handler := newTestPlatformHandler(nil, nil, audit, nil, nil, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/audit/summary?actorId=user-1&result=success&limit=25", "", nil)

	handler.AuditSummary(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !audit.summaryCalled || audit.filter.ActorID != "user-1" || audit.filter.Result != "success" || audit.filter.Limit != 25 {
		t.Fatalf("summary filter = %#v called=%v", audit.filter, audit.summaryCalled)
	}
	if !strings.Contains(recorder.Body.String(), `"exportRecommended":true`) {
		t.Fatalf("expected summary response body, got %s", recorder.Body.String())
	}
}

func TestPlatformExportAuditLogsReturnsCSV(t *testing.T) {
	audit := &stubPlatformAuditService{}
	handler := newTestPlatformHandler(nil, nil, audit, nil, nil, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/audit/logs/export?actorId=user-1&limit=25", "", nil)

	handler.ExportAuditLogs(ctx)

	assertCSVExport(t, recorder, csvExportExpectation{
		called: audit.exportCalled, actorID: audit.filter.ActorID, limit: audit.filter.Limit,
		filename: "audit-logs-20260612T100000Z.csv", countHeader: "X-Soha-Audit-Export-Count",
		bodyFragment: "audit-1,user-1",
	})
}

func TestPlatformListOperationLogsBindsExpandedFilter(t *testing.T) {
	operations := &stubPlatformOperationService{}
	handler := newTestPlatformHandler(nil, nil, nil, nil, operations, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/operations/logs?type=ai_gateway.tool.invoke&actor=user-1&clusterId=cluster-a&namespace=prod&resourceKind=Pod&resourceName=api-0&result=failure&requestId=req-1&requestPath=/api/v1/invoke&requestMethod=POST&sourceIp=127.0.0.1&approvalRequestId=approval-1&agentRunId=agent-run-1&rootCauseRunId=root-cause-1&metadataKey=usageSnapshot.riskLevel&metadataValue=high&createdAtFrom=2026-06-12T08:00:00Z&createdAtTo=2026-06-12T09:00:00Z&limit=25", "", nil)

	handler.ListOperationLogs(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	assertOperationFilterScope(t, operations.filter)
	assertOperationFilterCorrelation(t, operations.filter)
}

func TestPlatformOperationSummaryBindsFilter(t *testing.T) {
	operations := &stubPlatformOperationService{}
	handler := newTestPlatformHandler(nil, nil, nil, nil, operations, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/operations/summary?type=ai_gateway.tool.invoke&actor=user-1&result=failure&limit=25", "", nil)

	handler.OperationSummary(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !operations.summaryCalled || operations.filter.OperationType != "ai_gateway.tool.invoke" || operations.filter.ActorID != "user-1" || operations.filter.Result != "failure" {
		t.Fatalf("summary filter = %#v called=%v", operations.filter, operations.summaryCalled)
	}
	if !strings.Contains(recorder.Body.String(), `"failureCount":1`) {
		t.Fatalf("expected operation summary body, got %s", recorder.Body.String())
	}
}

func TestPlatformExportOperationLogsReturnsCSV(t *testing.T) {
	operations := &stubPlatformOperationService{}
	handler := newTestPlatformHandler(nil, nil, nil, nil, operations, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/operations/logs/export?actor=user-1&limit=25", "", nil)

	handler.ExportOperationLogs(ctx)

	assertCSVExport(t, recorder, csvExportExpectation{
		called: operations.exportCalled, actorID: operations.filter.ActorID, limit: operations.filter.Limit,
		filename: "operation-logs-20260612T100000Z.csv", countHeader: "X-Soha-Operation-Export-Count",
		bodyFragment: "op-1,user-1",
	})
}

func assertAuditFilterScope(t *testing.T, filter domainaudit.Filter) {
	t.Helper()
	if filter.ActorID != "user-1" || filter.ActorName != "Operator" || filter.ClusterID != "cluster-a" || filter.Namespace != "prod" {
		t.Fatalf("audit scope filter = %#v", filter)
	}
	if filter.ResourceKind != "Deployment" || filter.ResourceName != "api" || filter.Action != "platform.deployment.restart" || filter.Result != "success" {
		t.Fatalf("audit resource filter = %#v", filter)
	}
	if filter.RequestID != "req-1" || filter.RequestPath != "/api/v1/restart" || filter.RequestMethod != "post" || filter.SourceIP != "127.0.0.1" {
		t.Fatalf("audit request filter = %#v", filter)
	}
}

func assertAuditFilterCorrelation(t *testing.T, filter domainaudit.Filter) {
	t.Helper()
	if filter.ApprovalRequestID != "approval-1" || filter.AgentRunID != "agent-run-1" || filter.RootCauseRunID != "root-cause-1" {
		t.Fatalf("audit correlation filter = %#v", filter)
	}
	if filter.MetadataKey != "usageSnapshot.templateId" || filter.MetadataValue != "tpl-1" {
		t.Fatalf("audit metadata filter = %#v", filter)
	}
	assertFilterWindow(t, filter.From, filter.To, filter.Limit)
}

func assertOperationFilterScope(t *testing.T, filter domainoperation.Filter) {
	t.Helper()
	if filter.OperationType != "ai_gateway.tool.invoke" || filter.ActorID != "user-1" || filter.Result != "failure" {
		t.Fatalf("operation identity filter = %#v", filter)
	}
	if filter.ClusterID != "cluster-a" || filter.Namespace != "prod" || filter.ResourceKind != "Pod" || filter.ResourceName != "api-0" {
		t.Fatalf("operation scope filter = %#v", filter)
	}
	if filter.RequestID != "req-1" || filter.RequestPath != "/api/v1/invoke" || filter.RequestMethod != "POST" || filter.SourceIP != "127.0.0.1" {
		t.Fatalf("operation request filter = %#v", filter)
	}
}

func assertOperationFilterCorrelation(t *testing.T, filter domainoperation.Filter) {
	t.Helper()
	if filter.ApprovalRequestID != "approval-1" || filter.AgentRunID != "agent-run-1" || filter.RootCauseRunID != "root-cause-1" {
		t.Fatalf("operation correlation filter = %#v", filter)
	}
	if filter.MetadataKey != "usageSnapshot.riskLevel" || filter.MetadataValue != "high" {
		t.Fatalf("operation metadata filter = %#v", filter)
	}
	assertFilterWindow(t, filter.From, filter.To, filter.Limit)
}

func assertFilterWindow(t *testing.T, from, to *time.Time, limit int) {
	t.Helper()
	wantFrom := time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	if from == nil || !from.Equal(wantFrom) || to == nil || !to.Equal(wantTo) || limit != 25 {
		t.Fatalf("time/limit filter = from:%v to:%v limit:%d", from, to, limit)
	}
}

type csvExportExpectation struct {
	called       bool
	actorID      string
	limit        int
	filename     string
	countHeader  string
	bodyFragment string
}

func assertCSVExport(t *testing.T, recorder *httptest.ResponseRecorder, expected csvExportExpectation) {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !expected.called || expected.actorID != "user-1" || expected.limit != 25 {
		t.Fatalf("export called=%v actor=%q limit=%d", expected.called, expected.actorID, expected.limit)
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); !strings.Contains(got, expected.filename) {
		t.Fatalf("content disposition = %q", got)
	}
	if got := recorder.Header().Get(expected.countHeader); got != "1" {
		t.Fatalf("export count header = %q", got)
	}
	if !strings.Contains(recorder.Body.String(), expected.bodyFragment) {
		t.Fatalf("expected CSV body, got %q", recorder.Body.String())
	}
}

func TestPlatformGetPodLogsBindsDefaultsAndQuery(t *testing.T) {
	resources := newStubPlatformResourceService()
	handler := newTestPlatformHandler(nil, resources, nil, nil, nil, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/clusters/cluster-a/workloads/pods/api-0/logs?tailLines=50&sinceSeconds=300&previous=true&container=api", "", gin.Params{{Key: "clusterID", Value: "cluster-a"}, {Key: "podName", Value: "api-0"}})

	handler.GetPodLogs(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if resources.podLogsNamespace != "default" {
		t.Fatalf("namespace = %q, want default", resources.podLogsNamespace)
	}
	if resources.podLogsContainer != "api" || resources.podLogsTailLines != 50 || resources.podLogsSince != 300 || !resources.podLogsPrevious {
		t.Fatalf("log args = container:%q tail:%d since:%d previous:%v", resources.podLogsContainer, resources.podLogsTailLines, resources.podLogsSince, resources.podLogsPrevious)
	}
}

func TestPlatformApplyPodYAMLRejectsInvalidPayload(t *testing.T) {
	resources := newStubPlatformResourceService()
	handler := newTestPlatformHandler(nil, resources, nil, nil, nil, nil)
	ctx, recorder := newPlatformTestContext(http.MethodPut, "/api/v1/clusters/cluster-a/workloads/pods/api-0/yaml", "{", gin.Params{{Key: "clusterID", Value: "cluster-a"}, {Key: "podName", Value: "api-0"}})

	handler.ApplyPodYAML(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if resources.applyPodYAMLCalled {
		t.Fatal("ApplyPodYAML was called for invalid payload")
	}
}

func TestPlatformCreateCRDResourcePrefersBodyNamespace(t *testing.T) {
	resources := newStubPlatformResourceService()
	handler := newTestPlatformHandler(nil, resources, nil, nil, nil, nil)
	body := `{"namespace":"team-b","content":"apiVersion: example.io/v1\nkind: Widget\nmetadata:\n  name: sample\n"}`
	ctx, recorder := newPlatformTestContext(http.MethodPost, "/api/v1/clusters/cluster-a/extensions/crds/widgets.example.io/resources?namespace=team-a", body, gin.Params{{Key: "clusterID", Value: "cluster-a"}, {Key: "crdName", Value: "widgets.example.io"}})

	handler.CreateCRDResource(ctx)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if resources.crdCreateNamespace != "team-b" {
		t.Fatalf("namespace = %q, want team-b", resources.crdCreateNamespace)
	}
	if resources.crdCreateContent == "" {
		t.Fatal("content was not forwarded")
	}
}

func TestPlatformInstallHelmChartBindsPayload(t *testing.T) {
	resources := newStubPlatformResourceService()
	handler := newTestPlatformHandler(nil, resources, nil, nil, nil, nil)
	body := `{"packageId":"pkg-1","name":"nginx","version":"1.2.3","namespace":"apps","releaseName":"edge","values":"replicaCount: 2\n"}`
	ctx, recorder := newPlatformTestContext(http.MethodPost, "/api/v1/clusters/cluster-a/helm/charts/install", body, gin.Params{{Key: "clusterID", Value: "cluster-a"}})

	handler.InstallHelmChart(ctx)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if resources.helmInstallInput.ReleaseName != "edge" || resources.helmInstallInput.Namespace != "apps" || resources.helmInstallInput.Version != "1.2.3" {
		t.Fatalf("helm input = %#v", resources.helmInstallInput)
	}
}

func TestPlatformHandlerMapsApplicationErrors(t *testing.T) {
	resources := newStubPlatformResourceService()
	resources.serviceListErr = fmt.Errorf("%w: no access", apperrors.ErrAccessDenied)
	handler := newTestPlatformHandler(nil, resources, nil, nil, nil, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/clusters/cluster-a/network/services", "", gin.Params{{Key: "clusterID", Value: "cluster-a"}})

	handler.ListServices(ctx)
	assertPlatformErrorResponse(t, recorder, http.StatusForbidden, "access_denied")
}

func TestPlatformHandlerMapsUnsupportedOperations(t *testing.T) {
	resources := newStubPlatformResourceService()
	resources.serviceListErr = fmt.Errorf("%w: streaming pod logs are not supported for agent-connected clusters yet", apperrors.ErrUnsupportedOperation)
	handler := newTestPlatformHandler(nil, resources, nil, nil, nil, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/clusters/cluster-a/network/services", "", gin.Params{{Key: "clusterID", Value: "cluster-a"}})

	handler.ListServices(ctx)
	assertPlatformErrorResponse(t, recorder, http.StatusNotImplemented, "unsupported_operation")
}

func assertPlatformErrorResponse(t *testing.T, recorder *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if recorder.Code != wantStatus {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, wantStatus, recorder.Body.String())
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Code != wantCode {
		t.Fatalf("error.code = %q, want %q", payload.Error.Code, wantCode)
	}
}

func TestPlatformIngestConnectorEventsAcceptsStaticBearerToken(t *testing.T) {
	events := &stubPlatformEventService{}
	recorder := postConnectorEventSinkRequest(newTestPlatformHandler(nil, nil, nil, events, nil, nil), `{
		"connectorId":"feishu",
		"events":[{"id":"event-1","type":"im.message.receive_v1","source":"feishu","occurredAt":"2026-06-11T08:00:00Z","payload":{}}]
	}`, map[string]string{"Authorization": "Bearer sink-token"}, nil)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}
	if events.validatedToken != "sink-token" {
		t.Fatalf("validated token = %q, want sink-token", events.validatedToken)
	}
	if !events.ingestCalled {
		t.Fatal("IngestConnectorEvents was not called")
	}
	if events.ingestInput.RequestPath != "/api/v1/connectors/events" || events.ingestInput.RequestMethod != http.MethodPost || events.ingestInput.RequestID != "request-1" {
		t.Fatalf("unexpected request metadata: %#v", events.ingestInput)
	}
	if events.ingestInput.AuthKind != "static_token" {
		t.Fatalf("AuthKind = %q, want static_token", events.ingestInput.AuthKind)
	}

	var payload struct {
		Accepted int `json:"accepted"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Accepted != 1 {
		t.Fatalf("accepted = %d, want 1", payload.Accepted)
	}
}

func TestPlatformIngestConnectorEventsAcceptsServiceAccountWithGatewayInvoke(t *testing.T) {
	events := &stubPlatformEventService{validateErr: fmt.Errorf("%w: static token should not be used", apperrors.ErrUnauthorized)}
	recorder := postConnectorEventSinkRequest(newTestPlatformHandler(nil, nil, nil, events, nil, nil), `{
		"connectorId":"feishu",
		"events":[{"id":"event-1","type":"im.message.receive_v1","source":"feishu","occurredAt":"2026-06-11T08:00:00Z","payload":{}}]
	}`, nil, func(c *gin.Context) {
		c.Set("principal", domainidentity.Principal{
			UserID:         "service_account:runtime-1",
			UserName:       "feishu-runtime",
			Roles:          []string{"connector-runtime"},
			Teams:          []string{"integrations"},
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke},
		})
		c.Set("access_context", domainidentity.AccessContext{
			TokenKind:   "service_account_token",
			SubjectType: "service_account",
			SubjectID:   "runtime-1",
		})
	})

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusAccepted, recorder.Body.String())
	}
	if events.validatedToken != "" {
		t.Fatalf("static token validator was called with %q", events.validatedToken)
	}
	if events.ingestInput.ActorID != "service_account:runtime-1" || events.ingestInput.ActorName != "feishu-runtime" {
		t.Fatalf("unexpected actor attribution: %#v", events.ingestInput)
	}
	if events.ingestInput.AuthKind != "service_account_token" {
		t.Fatalf("AuthKind = %q, want service_account_token", events.ingestInput.AuthKind)
	}
}

func TestPlatformIngestConnectorEventsRejectsUnauthenticatedRequest(t *testing.T) {
	events := &stubPlatformEventService{validateErr: fmt.Errorf("%w: invalid connector event sink token", apperrors.ErrUnauthorized)}
	recorder := postConnectorEventSinkRequest(newTestPlatformHandler(nil, nil, nil, events, nil, nil), `{
		"connectorId":"feishu",
		"events":[{"id":"event-1","type":"im.message.receive_v1","source":"feishu","occurredAt":"2026-06-11T08:00:00Z","payload":{}}]
	}`, nil, nil)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
	if events.ingestCalled {
		t.Fatal("IngestConnectorEvents was called for unauthenticated request")
	}
}

func postConnectorEventSinkRequest(handler *PlatformHandler, body string, headers map[string]string, setup func(*gin.Context)) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("request_id", "request-1")
		if setup != nil {
			setup(c)
		}
		c.Next()
	})
	router.POST("/api/v1/connectors/events", handler.IngestConnectorEvents)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connectors/events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	router.ServeHTTP(recorder, req)
	return recorder
}
