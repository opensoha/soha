package operationlog

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRepositoryListAppliesExpandedFilters(t *testing.T) {
	repo, mock := newOperationLogRepository(t)
	from := time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	filter := domainoperation.Filter{
		ActorID:           "user-1",
		OperationType:     "ai_gateway.tool.invoke",
		ClusterID:         "cluster-a",
		Namespace:         "prod",
		ResourceKind:      "Pod",
		ResourceName:      "api-0",
		Result:            "failure",
		RequestID:         "req-1",
		RequestPath:       "/api/v1/ai-gateway/tools/k8s.pods.delete/invoke",
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
		"id", "actor_id", "actor_name", "operation_type", "target_scope", "result", "summary",
		"request_path", "request_method", "request_id", "source_ip", "metadata", "created_at",
	}).AddRow(
		"op-1",
		filter.ActorID,
		"Operator",
		filter.OperationType,
		[]byte(`{"clusterId":"cluster-a","namespace":"prod"}`),
		filter.Result,
		"pod delete denied",
		filter.RequestPath,
		filter.RequestMethod,
		filter.RequestID,
		filter.SourceIP,
		[]byte(`{"approvalRequestId":"approval-1"}`),
		to,
	)
	mock.ExpectQuery("SELECT id, actor_id, actor_name, operation_type, target_scope, result, summary").
		WithArgs(
			filter.ActorID, filter.ActorID,
			filter.OperationType, filter.OperationType,
			filter.ClusterID, filter.ClusterID, filter.ClusterID, filter.ClusterID,
			filter.Namespace, filter.Namespace,
			filter.ResourceKind, filter.ResourceKind, filter.ResourceKind,
			filter.ResourceName, filter.ResourceName, filter.ResourceName,
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
	if len(items) != 1 || items[0].ID != "op-1" || items[0].TargetScope["clusterId"] != "cluster-a" {
		t.Fatalf("items = %#v, want scanned operation entry", items)
	}
	if items[0].Metadata["approvalRequestId"] != "approval-1" {
		t.Fatalf("metadata was not scanned: %#v", items[0].Metadata)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestRepositorySummaryAppliesFiltersAndRetention(t *testing.T) {
	repo, mock := newOperationLogRepository(t)
	from := time.Date(2026, 6, 12, 8, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	oldest := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	newest := time.Date(2026, 6, 12, 8, 30, 0, 0, time.UTC)
	filter := domainoperation.Filter{
		ActorID:           "user-1",
		OperationType:     "ai_gateway.tool.invoke",
		ClusterID:         "cluster-a",
		Namespace:         "prod",
		ResourceKind:      "Pod",
		ResourceName:      "api-0",
		Result:            "failure",
		RequestID:         "req-1",
		RequestPath:       "/api/v1/invoke",
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
	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\), MIN\(created_at\), MAX\(created_at\),\s+COUNT\(\*\) FILTER \(WHERE created_at < \$1\),\s+COUNT\(\*\) FILTER \(WHERE result = 'failure'\)\s+FROM operation_logs`).
		WithArgs(
			sqlmock.AnyArg(),
			filter.ActorID, filter.ActorID,
			filter.OperationType, filter.OperationType,
			filter.ClusterID, filter.ClusterID, filter.ClusterID, filter.ClusterID,
			filter.Namespace, filter.Namespace,
			filter.ResourceKind, filter.ResourceKind, filter.ResourceKind,
			filter.ResourceName, filter.ResourceName, filter.ResourceName,
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
		WillReturnRows(sqlmock.NewRows([]string{"count", "min", "max", "expired", "failure"}).AddRow(int64(3), oldest, newest, int64(1), int64(2)))

	summary, err := repo.Summary(context.Background(), filter, 30)
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}
	if summary.Total != 3 || summary.RetentionDays != 30 || summary.ExpiredEntryCount != 1 || summary.FailureCount != 2 || !summary.ExportRecommended {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if summary.OldestEntryAt == nil || !summary.OldestEntryAt.Equal(oldest) || summary.NewestEntryAt == nil || !summary.NewestEntryAt.Equal(newest) {
		t.Fatalf("unexpected summary time bounds: %#v", summary)
	}
	if summary.RecommendedNextAction != "export_then_purge_expired_operation_logs" {
		t.Fatalf("recommended next action = %q", summary.RecommendedNextAction)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func newOperationLogRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
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
