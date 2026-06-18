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
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainevent "github.com/opensoha/soha/internal/domain/event"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type stubPlatformResourceService struct {
	ResourceService

	listPodsClusterID  string
	listPodsNamespace  string
	listPodsPrincipal  domainidentity.Principal
	listPodsCalled     bool
	listPodsErr        error
	podLogsNamespace   string
	podLogsContainer   string
	podLogsTailLines   int64
	podLogsSince       int64
	podLogsPrevious    bool
	applyPodYAMLCalled bool
	applyPodYAMLBody   string
	serviceListErr     error
	crdCreateNamespace string
	crdCreateContent   string
	helmInstallInput   domainresource.HelmChartInstallInput
}

type stubPlatformClusterService struct {
	ClusterService

	matrixCalled bool
}

type stubPlatformEventService struct {
	EventService

	validateErr    error
	validatedToken string
	ingestInput    domainevent.ConnectorEventIngestInput
	ingestCalled   bool
}

type stubPlatformAuditService struct {
	AuditService

	filter        domainaudit.Filter
	summaryCalled bool
	exportCalled  bool
}

type stubPlatformOperationService struct {
	OperationService

	filter        domainoperation.Filter
	summaryCalled bool
	exportCalled  bool
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
	gin.SetMode(gin.TestMode)

	resources := &stubPlatformResourceService{}
	handler := NewPlatformHandler(nil, resources, nil, nil, nil, nil)
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

func TestPlatformClusterCapabilityMatrixReturnsItems(t *testing.T) {
	gin.SetMode(gin.TestMode)

	clusters := &stubPlatformClusterService{}
	handler := NewPlatformHandler(clusters, nil, nil, nil, nil, nil)
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
	gin.SetMode(gin.TestMode)

	audit := &stubPlatformAuditService{}
	handler := NewPlatformHandler(nil, nil, audit, nil, nil, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/audit/logs?actorId=user-1&actorName=Operator&clusterId=cluster-a&namespace=prod&resourceKind=Deployment&resourceName=api&action=platform.deployment.restart&result=success&requestID=req-1&requestPath=/api/v1/restart&method=post&sourceIP=127.0.0.1&approvalRequestId=approval-1&agentRunId=agent-run-1&rootCauseRunId=root-cause-1&metadataKey=usageSnapshot.templateId&metadataValue=tpl-1&from=2026-06-12T08:00:00Z&to=2026-06-12T09:00:00Z&limit=25", "", nil)

	handler.ListAuditLogs(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if audit.filter.ActorID != "user-1" || audit.filter.ActorName != "Operator" || audit.filter.ClusterID != "cluster-a" || audit.filter.Namespace != "prod" {
		t.Fatalf("audit scope filter = %#v", audit.filter)
	}
	if audit.filter.ResourceKind != "Deployment" || audit.filter.ResourceName != "api" || audit.filter.Action != "platform.deployment.restart" || audit.filter.Result != "success" {
		t.Fatalf("audit resource filter = %#v", audit.filter)
	}
	if audit.filter.RequestID != "req-1" || audit.filter.RequestPath != "/api/v1/restart" || audit.filter.RequestMethod != "post" || audit.filter.SourceIP != "127.0.0.1" {
		t.Fatalf("audit request filter = %#v", audit.filter)
	}
	if audit.filter.ApprovalRequestID != "approval-1" || audit.filter.AgentRunID != "agent-run-1" || audit.filter.RootCauseRunID != "root-cause-1" {
		t.Fatalf("audit correlation filter = %#v", audit.filter)
	}
	if audit.filter.MetadataKey != "usageSnapshot.templateId" || audit.filter.MetadataValue != "tpl-1" {
		t.Fatalf("audit metadata filter = %#v", audit.filter)
	}
	from := time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	if audit.filter.From == nil || !audit.filter.From.Equal(from) || audit.filter.To == nil || !audit.filter.To.Equal(to) || audit.filter.Limit != 25 {
		t.Fatalf("audit time/limit filter = %#v", audit.filter)
	}
}

func TestPlatformAuditSummaryBindsFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	audit := &stubPlatformAuditService{}
	handler := NewPlatformHandler(nil, nil, audit, nil, nil, nil)
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
	gin.SetMode(gin.TestMode)

	audit := &stubPlatformAuditService{}
	handler := NewPlatformHandler(nil, nil, audit, nil, nil, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/audit/logs/export?actorId=user-1&limit=25", "", nil)

	handler.ExportAuditLogs(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !audit.exportCalled || audit.filter.ActorID != "user-1" || audit.filter.Limit != 25 {
		t.Fatalf("export filter = %#v called=%v", audit.filter, audit.exportCalled)
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); !strings.Contains(got, "audit-logs-20260612T100000Z.csv") {
		t.Fatalf("content disposition = %q", got)
	}
	if got := recorder.Header().Get("X-Soha-Audit-Export-Count"); got != "1" {
		t.Fatalf("export count header = %q", got)
	}
	if !strings.Contains(recorder.Body.String(), "audit-1,user-1") {
		t.Fatalf("expected CSV body, got %q", recorder.Body.String())
	}
}

func TestPlatformListOperationLogsBindsExpandedFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	operations := &stubPlatformOperationService{}
	handler := NewPlatformHandler(nil, nil, nil, nil, operations, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/operations/logs?type=ai_gateway.tool.invoke&actor=user-1&clusterId=cluster-a&namespace=prod&resourceKind=Pod&resourceName=api-0&result=failure&requestId=req-1&requestPath=/api/v1/invoke&requestMethod=POST&sourceIp=127.0.0.1&approvalRequestId=approval-1&agentRunId=agent-run-1&rootCauseRunId=root-cause-1&metadataKey=usageSnapshot.riskLevel&metadataValue=high&createdAtFrom=2026-06-12T08:00:00Z&createdAtTo=2026-06-12T09:00:00Z&limit=25", "", nil)

	handler.ListOperationLogs(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if operations.filter.OperationType != "ai_gateway.tool.invoke" || operations.filter.ActorID != "user-1" || operations.filter.Result != "failure" {
		t.Fatalf("operation identity filter = %#v", operations.filter)
	}
	if operations.filter.ClusterID != "cluster-a" || operations.filter.Namespace != "prod" || operations.filter.ResourceKind != "Pod" || operations.filter.ResourceName != "api-0" {
		t.Fatalf("operation scope filter = %#v", operations.filter)
	}
	if operations.filter.RequestID != "req-1" || operations.filter.RequestPath != "/api/v1/invoke" || operations.filter.RequestMethod != "POST" || operations.filter.SourceIP != "127.0.0.1" {
		t.Fatalf("operation request filter = %#v", operations.filter)
	}
	if operations.filter.ApprovalRequestID != "approval-1" || operations.filter.AgentRunID != "agent-run-1" || operations.filter.RootCauseRunID != "root-cause-1" {
		t.Fatalf("operation correlation filter = %#v", operations.filter)
	}
	if operations.filter.MetadataKey != "usageSnapshot.riskLevel" || operations.filter.MetadataValue != "high" {
		t.Fatalf("operation metadata filter = %#v", operations.filter)
	}
	from := time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	if operations.filter.From == nil || !operations.filter.From.Equal(from) || operations.filter.To == nil || !operations.filter.To.Equal(to) || operations.filter.Limit != 25 {
		t.Fatalf("operation time/limit filter = %#v", operations.filter)
	}
}

func TestPlatformOperationSummaryBindsFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	operations := &stubPlatformOperationService{}
	handler := NewPlatformHandler(nil, nil, nil, nil, operations, nil)
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
	gin.SetMode(gin.TestMode)

	operations := &stubPlatformOperationService{}
	handler := NewPlatformHandler(nil, nil, nil, nil, operations, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/operations/logs/export?actor=user-1&limit=25", "", nil)

	handler.ExportOperationLogs(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !operations.exportCalled || operations.filter.ActorID != "user-1" || operations.filter.Limit != 25 {
		t.Fatalf("export filter = %#v called=%v", operations.filter, operations.exportCalled)
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); !strings.Contains(got, "operation-logs-20260612T100000Z.csv") {
		t.Fatalf("content disposition = %q", got)
	}
	if got := recorder.Header().Get("X-Soha-Operation-Export-Count"); got != "1" {
		t.Fatalf("export count header = %q", got)
	}
	if !strings.Contains(recorder.Body.String(), "op-1,user-1") {
		t.Fatalf("expected CSV body, got %q", recorder.Body.String())
	}
}

func TestPlatformGetPodLogsBindsDefaultsAndQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)

	resources := &stubPlatformResourceService{}
	handler := NewPlatformHandler(nil, resources, nil, nil, nil, nil)
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
	gin.SetMode(gin.TestMode)

	resources := &stubPlatformResourceService{}
	handler := NewPlatformHandler(nil, resources, nil, nil, nil, nil)
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
	gin.SetMode(gin.TestMode)

	resources := &stubPlatformResourceService{}
	handler := NewPlatformHandler(nil, resources, nil, nil, nil, nil)
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
	gin.SetMode(gin.TestMode)

	resources := &stubPlatformResourceService{}
	handler := NewPlatformHandler(nil, resources, nil, nil, nil, nil)
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
	gin.SetMode(gin.TestMode)

	resources := &stubPlatformResourceService{serviceListErr: fmt.Errorf("%w: no access", apperrors.ErrAccessDenied)}
	handler := NewPlatformHandler(nil, resources, nil, nil, nil, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/clusters/cluster-a/network/services", "", gin.Params{{Key: "clusterID", Value: "cluster-a"}})

	handler.ListServices(ctx)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Code != "access_denied" {
		t.Fatalf("error.code = %q, want access_denied", payload.Error.Code)
	}
}

func TestPlatformHandlerMapsUnsupportedOperations(t *testing.T) {
	gin.SetMode(gin.TestMode)

	resources := &stubPlatformResourceService{serviceListErr: fmt.Errorf("%w: streaming pod logs are not supported for agent-connected clusters yet", apperrors.ErrUnsupportedOperation)}
	handler := NewPlatformHandler(nil, resources, nil, nil, nil, nil)
	ctx, recorder := newPlatformTestContext(http.MethodGet, "/api/v1/clusters/cluster-a/network/services", "", gin.Params{{Key: "clusterID", Value: "cluster-a"}})

	handler.ListServices(ctx)

	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusNotImplemented, recorder.Body.String())
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Error.Code != "unsupported_operation" {
		t.Fatalf("error.code = %q, want unsupported_operation", payload.Error.Code)
	}
}

func TestPlatformIngestConnectorEventsAcceptsStaticBearerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	events := &stubPlatformEventService{}
	recorder := postConnectorEventSinkRequest(NewPlatformHandler(nil, nil, nil, events, nil, nil), `{
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
	gin.SetMode(gin.TestMode)

	events := &stubPlatformEventService{validateErr: fmt.Errorf("%w: static token should not be used", apperrors.ErrUnauthorized)}
	recorder := postConnectorEventSinkRequest(NewPlatformHandler(nil, nil, nil, events, nil, nil), `{
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
	gin.SetMode(gin.TestMode)

	events := &stubPlatformEventService{validateErr: fmt.Errorf("%w: invalid connector event sink token", apperrors.ErrUnauthorized)}
	recorder := postConnectorEventSinkRequest(NewPlatformHandler(nil, nil, nil, events, nil, nil), `{
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
