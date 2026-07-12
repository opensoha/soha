package operation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domaingovernance "github.com/opensoha/soha/internal/domain/governance"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
)

type captureOperationRepository struct {
	filter        domainoperation.Filter
	summaryFilter domainoperation.Filter
	retentionDays int
	items         []domainoperation.Entry
	created       domainoperation.Entry
}

type captureOperationAlertSink struct {
	input  domaingovernance.AlertInput
	called bool
	err    error
}

func (s *captureOperationAlertSink) RecordGovernanceAlert(_ context.Context, input domaingovernance.AlertInput) error {
	s.called = true
	s.input = input
	return s.err
}

func (r *captureOperationRepository) Create(_ context.Context, entry domainoperation.Entry) error {
	r.created = entry
	return nil
}

func (r *captureOperationRepository) List(_ context.Context, filter domainoperation.Filter) ([]domainoperation.Entry, error) {
	r.filter = filter
	return r.items, nil
}

func (r *captureOperationRepository) Summary(_ context.Context, filter domainoperation.Filter, retentionDays int) (domainoperation.Summary, error) {
	r.summaryFilter = filter
	r.retentionDays = retentionDays
	return domainoperation.Summary{Total: 1, FailureCount: 1, RetentionDays: retentionDays}, nil
}

func TestRecordRedactsOperationSensitiveFields(t *testing.T) {
	repo := &captureOperationRepository{}
	service := New(repo, nil)

	if err := service.Record(context.Background(), domainoperation.Entry{
		ActorID:       "user-1",
		OperationType: "ai_gateway.tool.invoke",
		TargetScope:   map[string]any{"clusterId": "cluster-a", "kubeconfig": "raw-kubeconfig"},
		Result:        "failure",
		Summary:       "operation failed token=raw-token",
		Metadata:      map[string]any{"authorization": "Bearer raw-bearer", "nested": map[string]any{"password": "raw-password"}},
	}); err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	if strings.Contains(repo.created.Summary, "raw-token") {
		t.Fatalf("summary was not redacted: %q", repo.created.Summary)
	}
	if repo.created.TargetScope["kubeconfig"] != "[REDACTED]" {
		t.Fatalf("target scope kubeconfig was not redacted: %#v", repo.created.TargetScope)
	}
	if repo.created.Metadata["authorization"] != "[REDACTED]" {
		t.Fatalf("authorization metadata was not redacted: %#v", repo.created.Metadata)
	}
	nested := mustOperationValue[map[string]any](t, repo.created.Metadata["nested"])
	if nested["password"] != "[REDACTED]" {
		t.Fatalf("nested metadata was not redacted: %#v", nested)
	}
}

func TestRecordOperationFailureCreatesGovernanceAlert(t *testing.T) {
	repo := &captureOperationRepository{}
	alerts := &captureOperationAlertSink{}
	service := New(repo, nil)
	service.SetAlertSink(alerts)

	if err := service.Record(context.Background(), domainoperation.Entry{
		ID:            "op-1",
		ActorID:       "user-1",
		ActorName:     "Operator",
		OperationType: "platform.pod.delete",
		TargetScope:   map[string]any{"clusterId": "cluster-a", "namespace": "prod", "resourceKind": "Pod", "resourceName": "api-0"},
		Result:        "failure",
		Summary:       "delete denied token=raw-token",
		RequestID:     "req-1",
		RequestPath:   "/api/v1/pods/api-0",
		RequestMethod: "DELETE",
		SourceIP:      "127.0.0.1",
		Metadata:      map[string]any{"approvalRequestId": "approval-1"},
	}); err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	if !alerts.called {
		t.Fatal("expected governance alert")
	}
	if alerts.input.Source != "operation" || alerts.input.EventID != "op-1" || alerts.input.OperationType != "platform.pod.delete" || alerts.input.ClusterID != "cluster-a" {
		t.Fatalf("unexpected alert input: %#v", alerts.input)
	}
	if alerts.input.Annotations["approvalRequestId"] != "approval-1" {
		t.Fatalf("approval annotation missing: %#v", alerts.input.Annotations)
	}
	if strings.Contains(alerts.input.Summary, "raw-token") {
		t.Fatalf("alert summary was not redacted: %q", alerts.input.Summary)
	}
}

func TestRecordOperationIgnoresGovernanceAlertSinkError(t *testing.T) {
	repo := &captureOperationRepository{}
	service := New(repo, nil)
	service.SetAlertSink(&captureOperationAlertSink{err: errors.New("sink unavailable")})

	if err := service.Record(context.Background(), domainoperation.Entry{
		ID:            "op-1",
		OperationType: "platform.pod.delete",
		Result:        "failure",
		Summary:       "delete denied",
	}); err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	if repo.created.ID != "op-1" {
		t.Fatalf("operation record was not persisted: %#v", repo.created)
	}
}

func TestListRedactsOperationSensitiveFields(t *testing.T) {
	repo := &captureOperationRepository{items: []domainoperation.Entry{{
		ID:          "op-1",
		TargetScope: map[string]any{"apiKey": "raw-api-key"},
		Summary:     "authorization: Bearer raw-bearer",
		Metadata:    map[string]any{"note": "secret=raw-secret"},
	}}}
	service := New(repo, nil)

	items, err := service.List(context.Background(), domainoperation.Filter{})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	content := items[0].Summary + mustOperationValue[string](t, items[0].TargetScope["apiKey"]) + mustOperationValue[string](t, items[0].Metadata["note"])
	for _, leaked := range []string{"raw-api-key", "raw-bearer", "raw-secret"} {
		if strings.Contains(content, leaked) {
			t.Fatalf("list leaked %q in %#v", leaked, items[0])
		}
	}
}

func mustOperationValue[T any](t *testing.T, value any) T {
	t.Helper()
	result, ok := value.(T)
	if !ok {
		t.Fatalf("value has type %T, want %T", value, *new(T))
	}
	return result
}

func TestListNormalizesExpandedOperationFilter(t *testing.T) {
	repo := &captureOperationRepository{}
	service := New(repo, nil)

	_, err := service.List(context.Background(), domainoperation.Filter{
		OperationType:     " ai_gateway.tool.invoke ",
		ActorID:           " user-1 ",
		ClusterID:         " cluster-a ",
		Namespace:         " prod ",
		ResourceKind:      " Pod ",
		ResourceName:      " api-0 ",
		Result:            " FAILURE ",
		RequestID:         " req-1 ",
		RequestPath:       " /api/v1/ai-gateway/tools/k8s.pods.delete/invoke ",
		RequestMethod:     " post ",
		SourceIP:          " 127.0.0.1 ",
		ApprovalRequestID: " approval-1 ",
		AgentRunID:        " agent-run-1 ",
		RootCauseRunID:    " root-cause-1 ",
		MetadataKey:       " connectorId ",
		MetadataValue:     " feishu ",
		Limit:             1000,
	})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if repo.filter.OperationType != "ai_gateway.tool.invoke" || repo.filter.ActorID != "user-1" || repo.filter.Result != "failure" {
		t.Fatalf("operation filter was not normalized: %#v", repo.filter)
	}
	if repo.filter.ClusterID != "cluster-a" || repo.filter.Namespace != "prod" || repo.filter.ResourceKind != "Pod" || repo.filter.ResourceName != "api-0" {
		t.Fatalf("scope filter was not normalized: %#v", repo.filter)
	}
	if repo.filter.RequestID != "req-1" || repo.filter.RequestMethod != "POST" || repo.filter.SourceIP != "127.0.0.1" {
		t.Fatalf("request filter was not normalized: %#v", repo.filter)
	}
	if repo.filter.ApprovalRequestID != "approval-1" || repo.filter.AgentRunID != "agent-run-1" || repo.filter.RootCauseRunID != "root-cause-1" {
		t.Fatalf("correlation filter was not normalized: %#v", repo.filter)
	}
	if repo.filter.MetadataKey != "connectorId" || repo.filter.MetadataValue != "feishu" {
		t.Fatalf("metadata filter was not normalized: %#v", repo.filter)
	}
	if repo.filter.Limit != 500 {
		t.Fatalf("limit = %d, want capped 500", repo.filter.Limit)
	}
}

func TestSummaryUsesNormalizedOperationFilterAndRetention(t *testing.T) {
	repo := &captureOperationRepository{}
	service := New(repo, nil)

	summary, err := service.Summary(context.Background(), domainoperation.Filter{OperationType: " ai_gateway.tool.invoke ", Result: " FAILURE "})
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}
	if summary.Total != 1 || summary.FailureCount != 1 || repo.retentionDays != 90 {
		t.Fatalf("summary = %#v retention=%d", summary, repo.retentionDays)
	}
	if repo.summaryFilter.OperationType != "ai_gateway.tool.invoke" || repo.summaryFilter.Result != "failure" {
		t.Fatalf("summary filter was not normalized: %#v", repo.summaryFilter)
	}
}

func TestExportCSVUsesExpandedOperationFields(t *testing.T) {
	createdAt := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	repo := &captureOperationRepository{items: []domainoperation.Entry{{
		ID:            "op-1",
		ActorID:       "user-1",
		ActorName:     "Operator",
		OperationType: "ai_gateway.tool.invoke",
		TargetScope:   map[string]any{"clusterId": "cluster-a", "namespace": "prod", "kubeconfig": "raw-kubeconfig"},
		Result:        "failure",
		Summary:       "pod delete denied token=raw-token",
		RequestPath:   "/api/v1/invoke",
		RequestMethod: "POST",
		RequestID:     "req-1",
		SourceIP:      "127.0.0.1",
		Metadata:      map[string]any{"approvalRequestId": "approval-1", "password": "raw-password"},
		CreatedAt:     createdAt,
	}}}
	service := New(repo, nil)

	export, err := service.ExportCSV(context.Background(), domainoperation.Filter{ActorID: " user-1 ", Limit: 1000})
	if err != nil {
		t.Fatalf("ExportCSV returned error: %v", err)
	}
	if export.ContentType != "text/csv; charset=utf-8" || export.Count != 1 || !strings.HasSuffix(export.Filename, ".csv") {
		t.Fatalf("unexpected export metadata: %#v", export)
	}
	content := string(export.Content)
	for _, want := range []string{"id,actorId,actorName", "op-1,user-1,Operator", "ai_gateway.tool.invoke", "approvalRequestId"} {
		if !strings.Contains(content, want) {
			t.Fatalf("export missing %q in %s", want, content)
		}
	}
	for _, leaked := range []string{"raw-kubeconfig", "raw-token", "raw-password"} {
		if strings.Contains(content, leaked) {
			t.Fatalf("export leaked %q in %s", leaked, content)
		}
	}
	if repo.filter.Limit != 1000 {
		t.Fatalf("export list limit = %d, want requested export limit", repo.filter.Limit)
	}
}
