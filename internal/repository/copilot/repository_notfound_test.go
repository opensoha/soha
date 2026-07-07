package copilot

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestRepositoryGetDataSourceWrapsErrNotFound(t *testing.T) {
	repo, mock := newAgentRunRepository(t)

	mock.ExpectQuery(`(?s)SELECT id, name, source_kind, backend_type, enabled, credential_ref, scope, query_budget, redaction_policy, mcp_adapter, config,\s+validation_status, validation_message, last_validated_at, created_at, updated_at\s+FROM ai_data_sources\s+WHERE id = \$1\s+LIMIT 1`).
		WithArgs("missing-data-source").
		WillReturnError(sql.ErrNoRows)

	_, err := repo.GetDataSource(context.Background(), "missing-data-source")
	if err == nil {
		t.Fatal("GetDataSource() error = nil, want ErrNotFound")
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("GetDataSource() error = %v, want ErrNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRepositoryUpdateAgentRunCallbackWrapsErrNotFound(t *testing.T) {
	repo, mock := newAgentRunRepository(t)
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)

	expectGetAgentRun(mock, domaincopilot.AgentRun{
		ID:            "missing-run",
		ProviderID:    "hermes",
		ProviderKind:  "hermes",
		CapabilityID:  "delivery_failure",
		CreatedBy:     "user-1",
		Status:        domaincopilot.AgentRunStatusRunning,
		CallbackToken: "callback-token",
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	mock.ExpectExec(`(?s)UPDATE ai_agent_runs\s+SET status = \$1, output = \$2, tool_executions = \$3, analysis_artifacts = \$4, claimed_by_agent_id = COALESCE\(NULLIF\(\$5, ''\), claimed_by_agent_id\),\s+external_run_id = COALESCE\(NULLIF\(\$6, ''\), external_run_id\), error_message = COALESCE\(NULLIF\(\$7, ''\), error_message\),\s+last_heartbeat_at = \$8, completed_at = \$9, updated_at = \$10\s+WHERE id = \$11 AND callback_token = \$12 AND status NOT IN \(\$13, \$14, \$15, \$16\)`).
		WithArgs(
			domaincopilot.AgentRunStatusRunning,
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			"",
			"",
			"",
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			"missing-run",
			"callback-token",
			domaincopilot.AgentRunStatusCompleted,
			domaincopilot.AgentRunStatusFailed,
			domaincopilot.AgentRunStatusCanceled,
			domaincopilot.AgentRunStatusCallbackTimeout,
		).
		WillReturnResult(sqlmock.NewResult(0, 0))
	expectGetAgentRun(mock, domaincopilot.AgentRun{
		ID:            "missing-run",
		ProviderID:    "hermes",
		ProviderKind:  "hermes",
		CapabilityID:  "delivery_failure",
		CreatedBy:     "user-1",
		Status:        domaincopilot.AgentRunStatusRunning,
		CallbackToken: "callback-token",
		CreatedAt:     now,
		UpdatedAt:     now,
	})

	_, err := repo.UpdateAgentRunCallback(context.Background(), domaincopilot.AgentRunCallbackInput{
		RunID:         "missing-run",
		CallbackToken: "callback-token",
		Status:        domaincopilot.AgentRunStatusRunning,
	})
	if err == nil {
		t.Fatal("UpdateAgentRunCallback() error = nil, want ErrNotFound")
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("UpdateAgentRunCallback() error = %v, want ErrNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
