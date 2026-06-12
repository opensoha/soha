package auditlog

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRepositoryListAppliesExpandedFilters(t *testing.T) {
	repo, mock := newAuditLogRepository(t)
	from := time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	filter := domainaudit.Filter{
		ActorID:           "user-1",
		ActorName:         "Operator",
		ClusterID:         "cluster-a",
		Namespace:         "prod",
		ResourceKind:      "Deployment",
		ResourceName:      "api",
		Action:            "platform.deployment.restart",
		Result:            "success",
		RequestID:         "req-1",
		RequestPath:       "/api/v1/clusters/cluster-a/workloads/deployments/api/restart",
		RequestMethod:     "POST",
		SourceIP:          "127.0.0.1",
		ApprovalRequestID: "approval-1",
		AgentRunID:        "agent-run-1",
		RootCauseRunID:    "root-cause-1",
		MetadataKey:       "connectorId",
		MetadataValue:     "feishu",
		From:              &from,
		To:                &to,
		Limit:             10,
	}

	rows := sqlmock.NewRows([]string{
		"id", "actor_id", "actor_name", "roles", "teams", "cluster_id", "namespace", "resource_kind",
		"resource_name", "action", "result", "summary", "request_path", "request_method",
		"request_id", "source_ip", "metadata", "created_at",
	}).AddRow(
		"audit-1",
		filter.ActorID,
		filter.ActorName,
		[]byte(`["admin"]`),
		[]byte(`["sre"]`),
		filter.ClusterID,
		filter.Namespace,
		filter.ResourceKind,
		filter.ResourceName,
		filter.Action,
		filter.Result,
		"restarted deployment",
		filter.RequestPath,
		filter.RequestMethod,
		filter.RequestID,
		filter.SourceIP,
		[]byte(`{"approvalRequestId":"approval-1"}`),
		to,
	)
	mock.ExpectQuery("SELECT id, actor_id, actor_name, roles, teams, cluster_id, namespace, resource_kind").
		WithArgs(
			filter.ActorID, filter.ActorID,
			filter.ActorName, filter.ActorName,
			filter.ClusterID, filter.ClusterID,
			filter.Namespace, filter.Namespace,
			filter.ResourceKind, filter.ResourceKind,
			filter.ResourceName, filter.ResourceName,
			filter.Action, filter.Action,
			filter.Result, filter.Result,
			filter.RequestID, filter.RequestID,
			filter.RequestPath, filter.RequestPath,
			filter.RequestMethod, filter.RequestMethod,
			filter.SourceIP, filter.SourceIP,
			filter.ApprovalRequestID, filter.ApprovalRequestID, filter.ApprovalRequestID,
			filter.AgentRunID, filter.AgentRunID, filter.AgentRunID, filter.AgentRunID,
			filter.RootCauseRunID, filter.RootCauseRunID, filter.RootCauseRunID,
			filter.MetadataKey, filter.MetadataKey, filter.MetadataValue,
			from, from,
			to, to,
			filter.Limit,
		).
		WillReturnRows(rows)

	items, err := repo.List(context.Background(), filter)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(items) != 1 || items[0].ID != "audit-1" || items[0].Metadata["approvalRequestId"] != "approval-1" {
		t.Fatalf("items = %#v, want scanned audit entry", items)
	}
	if len(items[0].Roles) != 1 || items[0].Roles[0] != "admin" || len(items[0].Teams) != 1 || items[0].Teams[0] != "sre" {
		t.Fatalf("roles/teams were not scanned: %#v", items[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestRepositorySummaryAppliesFiltersAndRetention(t *testing.T) {
	repo, mock := newAuditLogRepository(t)
	from := time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	oldest := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	newest := time.Date(2026, 6, 12, 8, 30, 0, 0, time.UTC)
	filter := domainaudit.Filter{
		ActorID:           "user-1",
		ActorName:         "Operator",
		ClusterID:         "cluster-a",
		Namespace:         "prod",
		ResourceKind:      "Deployment",
		ResourceName:      "api",
		Action:            "platform.deployment.restart",
		Result:            "success",
		RequestID:         "req-1",
		RequestPath:       "/api/v1/restart",
		RequestMethod:     "POST",
		SourceIP:          "127.0.0.1",
		ApprovalRequestID: "approval-1",
		AgentRunID:        "agent-run-1",
		RootCauseRunID:    "root-cause-1",
		MetadataKey:       "connectorId",
		MetadataValue:     "feishu",
		From:              &from,
		To:                &to,
	}
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\), MIN\(created_at\), MAX\(created_at\),\s+COUNT\(\*\) FILTER \(WHERE created_at < \$1\)\s+FROM audit_logs`).
		WithArgs(
			sqlmock.AnyArg(),
			filter.ActorID, filter.ActorID,
			filter.ActorName, filter.ActorName,
			filter.ClusterID, filter.ClusterID,
			filter.Namespace, filter.Namespace,
			filter.ResourceKind, filter.ResourceKind,
			filter.ResourceName, filter.ResourceName,
			filter.Action, filter.Action,
			filter.Result, filter.Result,
			filter.RequestID, filter.RequestID,
			filter.RequestPath, filter.RequestPath,
			filter.RequestMethod, filter.RequestMethod,
			filter.SourceIP, filter.SourceIP,
			filter.ApprovalRequestID, filter.ApprovalRequestID, filter.ApprovalRequestID,
			filter.AgentRunID, filter.AgentRunID, filter.AgentRunID, filter.AgentRunID,
			filter.RootCauseRunID, filter.RootCauseRunID, filter.RootCauseRunID,
			filter.MetadataKey, filter.MetadataKey, filter.MetadataValue,
			from, from,
			to, to,
		).
		WillReturnRows(sqlmock.NewRows([]string{"count", "min", "max", "expired"}).AddRow(int64(2), oldest, newest, int64(1)))

	summary, err := repo.Summary(context.Background(), filter, 30)
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}
	if summary.Total != 2 || summary.RetentionDays != 30 || summary.ExpiredEntryCount != 1 || !summary.ExportRecommended {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if summary.OldestEntryAt == nil || !summary.OldestEntryAt.Equal(oldest) || summary.NewestEntryAt == nil || !summary.NewestEntryAt.Equal(newest) {
		t.Fatalf("unexpected summary time bounds: %#v", summary)
	}
	if summary.RecommendedNextAction != "export_then_purge_expired_audit_logs" {
		t.Fatalf("recommended next action = %q", summary.RecommendedNextAction)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func newAuditLogRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm postgres mock: %v", err)
	}
	return New(db), mock
}
