package audit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaingovernance "github.com/opensoha/soha/internal/domain/governance"
)

type captureAuditRepository struct {
	filter        domainaudit.Filter
	summaryFilter domainaudit.Filter
	retentionDays int
	items         []domainaudit.Entry
	created       domainaudit.Entry
}

type captureAuditAlertSink struct {
	input  domaingovernance.AlertInput
	called bool
	err    error
}

func (s *captureAuditAlertSink) RecordGovernanceAlert(_ context.Context, input domaingovernance.AlertInput) error {
	s.called = true
	s.input = input
	return s.err
}

func (r *captureAuditRepository) Create(_ context.Context, entry domainaudit.Entry) error {
	r.created = entry
	return nil
}

func (r *captureAuditRepository) List(_ context.Context, filter domainaudit.Filter) ([]domainaudit.Entry, error) {
	r.filter = filter
	return r.items, nil
}

func (r *captureAuditRepository) Summary(_ context.Context, filter domainaudit.Filter, retentionDays int) (domainaudit.Summary, error) {
	r.summaryFilter = filter
	r.retentionDays = retentionDays
	return domainaudit.Summary{Total: 1, RetentionDays: retentionDays}, nil
}

func TestRecordRedactsAuditSensitiveFields(t *testing.T) {
	repo := &captureAuditRepository{}
	service := New(repo, nil)

	if err := service.Record(context.Background(), domainaudit.Entry{
		ActorID:  "user-1",
		Action:   "ai_gateway.tool.invoke",
		Result:   "success",
		Summary:  "invoked tool token=raw-token password: raw-password",
		Metadata: map[string]any{"apiKey": "raw-api-key", "nested": map[string]any{"note": "authorization: Bearer raw-bearer"}},
	}); err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	if strings.Contains(repo.created.Summary, "raw-token") || strings.Contains(repo.created.Summary, "raw-password") {
		t.Fatalf("summary was not redacted: %q", repo.created.Summary)
	}
	if repo.created.Metadata["apiKey"] != "[REDACTED]" {
		t.Fatalf("apiKey metadata was not redacted: %#v", repo.created.Metadata)
	}
	nested := repo.created.Metadata["nested"].(map[string]any)
	if strings.Contains(nested["note"].(string), "raw-bearer") {
		t.Fatalf("nested metadata text was not redacted: %#v", nested)
	}
}

func TestRecordAuditFailureCreatesGovernanceAlert(t *testing.T) {
	repo := &captureAuditRepository{}
	alerts := &captureAuditAlertSink{}
	service := New(repo, nil)
	service.SetAlertSink(alerts)

	if err := service.Record(context.Background(), domainaudit.Entry{
		ID:            "audit-1",
		ActorID:       "user-1",
		ActorName:     "Operator",
		ClusterID:     "cluster-a",
		Namespace:     "prod",
		ResourceKind:  "Pod",
		ResourceName:  "api-0",
		Action:        "delete",
		Result:        "failure",
		Summary:       "delete denied token=raw-token",
		RequestID:     "req-1",
		RequestPath:   "/api/v1/pods/api-0",
		RequestMethod: "DELETE",
		SourceIP:      "127.0.0.1",
	}); err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	if !alerts.called {
		t.Fatal("expected governance alert")
	}
	if alerts.input.Source != "audit" || alerts.input.EventID != "audit-1" || alerts.input.Action != "delete" || alerts.input.ClusterID != "cluster-a" {
		t.Fatalf("unexpected alert input: %#v", alerts.input)
	}
	if strings.Contains(alerts.input.Summary, "raw-token") {
		t.Fatalf("alert summary was not redacted: %q", alerts.input.Summary)
	}
}

func TestRecordAuditIgnoresGovernanceAlertSinkError(t *testing.T) {
	repo := &captureAuditRepository{}
	service := New(repo, nil)
	service.SetAlertSink(&captureAuditAlertSink{err: errors.New("sink unavailable")})

	if err := service.Record(context.Background(), domainaudit.Entry{
		ID:      "audit-1",
		Action:  "delete",
		Result:  "failure",
		Summary: "delete denied",
	}); err != nil {
		t.Fatalf("Record returned error: %v", err)
	}
	if repo.created.ID != "audit-1" {
		t.Fatalf("audit record was not persisted: %#v", repo.created)
	}
}

func TestListRedactsAuditSensitiveFields(t *testing.T) {
	repo := &captureAuditRepository{items: []domainaudit.Entry{{
		ID:       "audit-1",
		Summary:  "authorization: Bearer raw-bearer",
		Metadata: map[string]any{"password": "raw-password", "note": "secret=raw-secret"},
	}}}
	service := New(repo, nil)

	items, err := service.List(context.Background(), domainaudit.Filter{})
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	content := items[0].Summary + items[0].Metadata["password"].(string) + items[0].Metadata["note"].(string)
	for _, leaked := range []string{"raw-bearer", "raw-password", "raw-secret"} {
		if strings.Contains(content, leaked) {
			t.Fatalf("list leaked %q in %#v", leaked, items[0])
		}
	}
}

func TestListNormalizesExpandedAuditFilter(t *testing.T) {
	repo := &captureAuditRepository{}
	service := New(repo, nil)

	_, err := service.List(context.Background(), domainaudit.Filter{
		ActorID:           " user-1 ",
		ActorName:         " Operator ",
		ClusterID:         " cluster-a ",
		Namespace:         " prod ",
		ResourceKind:      " Deployment ",
		ResourceName:      " api ",
		Action:            " platform.deployment.restart ",
		Result:            " SUCCESS ",
		RequestID:         " req-1 ",
		RequestPath:       " /api/v1/clusters/cluster-a/workloads/deployments/api/restart ",
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
	if repo.filter.ActorID != "user-1" || repo.filter.ActorName != "Operator" || repo.filter.ClusterID != "cluster-a" || repo.filter.Namespace != "prod" {
		t.Fatalf("scope filter was not normalized: %#v", repo.filter)
	}
	if repo.filter.ResourceKind != "Deployment" || repo.filter.ResourceName != "api" || repo.filter.Action != "platform.deployment.restart" || repo.filter.Result != "success" {
		t.Fatalf("resource/action filter was not normalized: %#v", repo.filter)
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

func TestSummaryUsesNormalizedFilterAndRetention(t *testing.T) {
	repo := &captureAuditRepository{}
	service := New(repo, nil)

	summary, err := service.Summary(context.Background(), domainaudit.Filter{ActorID: " user-1 ", Result: " SUCCESS ", MetadataKey: " ignored "})
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}
	if summary.Total != 1 || repo.retentionDays != 90 {
		t.Fatalf("summary = %#v retention=%d", summary, repo.retentionDays)
	}
	if repo.summaryFilter.ActorID != "user-1" || repo.summaryFilter.Result != "success" {
		t.Fatalf("summary filter was not normalized: %#v", repo.summaryFilter)
	}
	if repo.summaryFilter.MetadataKey != "" {
		t.Fatalf("metadata key without value should be ignored: %#v", repo.summaryFilter)
	}
}

func TestExportCSVUsesExpandedAuditFields(t *testing.T) {
	createdAt := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	repo := &captureAuditRepository{items: []domainaudit.Entry{{
		ID:            "audit-1",
		ActorID:       "user-1",
		ActorName:     "Operator",
		Roles:         []string{"admin", "auditor"},
		Teams:         []string{"sre"},
		ClusterID:     "cluster-a",
		Namespace:     "prod",
		ResourceKind:  "Deployment",
		ResourceName:  "api",
		Action:        "platform.deployment.restart",
		Result:        "success",
		Summary:       "restarted deployment token=raw-token",
		RequestPath:   "/api/v1/restart",
		RequestMethod: "POST",
		RequestID:     "req-1",
		SourceIP:      "127.0.0.1",
		Metadata:      map[string]any{"approvalRequestId": "approval-1", "password": "raw-password"},
		CreatedAt:     createdAt,
	}}}
	service := New(repo, nil)

	export, err := service.ExportCSV(context.Background(), domainaudit.Filter{ActorID: " user-1 ", Limit: 1000})
	if err != nil {
		t.Fatalf("ExportCSV returned error: %v", err)
	}
	if export.ContentType != "text/csv; charset=utf-8" || export.Count != 1 || !strings.HasSuffix(export.Filename, ".csv") {
		t.Fatalf("unexpected export metadata: %#v", export)
	}
	content := string(export.Content)
	for _, want := range []string{"id,actorId,actorName", "audit-1,user-1,Operator", "admin|auditor", "approvalRequestId"} {
		if !strings.Contains(content, want) {
			t.Fatalf("export missing %q in %s", want, content)
		}
	}
	for _, leaked := range []string{"raw-token", "raw-password"} {
		if strings.Contains(content, leaked) {
			t.Fatalf("export leaked %q in %s", leaked, content)
		}
	}
	if repo.filter.Limit != 1000 {
		t.Fatalf("export list limit = %d, want requested export limit", repo.filter.Limit)
	}
}
