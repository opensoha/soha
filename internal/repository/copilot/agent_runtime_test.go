package copilot

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestMergeAgentRunToolExecutionsAppendsAndReplacesByID(t *testing.T) {
	current := []domaincopilot.ToolExecution{{
		ID:       "tool:events",
		ToolName: "events.query",
		Status:   "success",
	}, {
		ID:       "tool:stale",
		ToolName: "logs.query",
		Status:   "running",
	}}
	patch := []domaincopilot.ToolExecution{{
		ID:       "tool:stale",
		ToolName: "logs.query",
		Status:   "failed",
	}, {
		ID:       "tool:agent-analysis",
		ToolName: "hermes.analysis",
		Status:   "completed",
	}}

	merged := mergeAgentRunToolExecutions(current, patch)

	if len(merged) != 3 {
		t.Fatalf("expected 3 merged executions, got %#v", merged)
	}
	if merged[0].ID != "tool:events" || merged[1].ID != "tool:stale" || merged[1].Status != "failed" || merged[2].ID != "tool:agent-analysis" {
		t.Fatalf("unexpected merge result: %#v", merged)
	}
}

func TestMergeAgentRunAnalysisArtifactsAppendsAndReplacesByStableKey(t *testing.T) {
	current := []domaincopilot.AnalysisArtifact{{
		Kind:    "root_cause",
		RunID:   "agent:run-1",
		Title:   "Root cause",
		Summary: "old root cause",
	}, {
		Kind:    "trace",
		RunID:   "agent:run-1",
		Title:   "Trace",
		Summary: "trace summary",
	}, {
		Kind:    "inspection_review",
		Title:   "Nightly review",
		Summary: "old review",
	}}
	patch := []domaincopilot.AnalysisArtifact{{
		Kind:    "root_cause",
		RunID:   "agent:run-1",
		Title:   "Root cause",
		Summary: "updated root cause",
	}, {
		Kind:    "root_cause",
		RunID:   "agent:run-1",
		Title:   "Mitigation plan",
		Summary: "new mitigation",
	}, {
		Kind:    "inspection_review",
		Title:   "Nightly review",
		Summary: "updated review",
	}}

	merged := mergeAgentRunAnalysisArtifacts(current, patch)

	if len(merged) != 4 {
		t.Fatalf("expected 4 merged artifacts, got %#v", merged)
	}
	if merged[0].Kind != "root_cause" || merged[0].Summary != "updated root cause" {
		t.Fatalf("expected first artifact to be replaced, got %#v", merged)
	}
	if merged[1].Kind != "trace" || merged[1].Summary != "trace summary" {
		t.Fatalf("expected trace artifact to remain in place, got %#v", merged)
	}
	if merged[2].Kind != "inspection_review" || merged[2].Summary != "updated review" {
		t.Fatalf("expected no-run artifact to be replaced by kind/title, got %#v", merged)
	}
	if merged[3].Title != "Mitigation plan" || merged[3].Summary != "new mitigation" {
		t.Fatalf("expected new artifact to be appended, got %#v", merged)
	}
}

func TestMergeAgentRunWorkbenchEventsNormalizesFiltersAndLimits(t *testing.T) {
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	run := domaincopilot.AgentRun{
		ID:             "agent:run-events",
		ProviderID:     "hermes",
		ProviderKind:   "hermes",
		CapabilityID:   "root_cause",
		SessionID:      "session-1",
		RootCauseRunID: "rca:run-1",
	}
	normalized := normalizeAgentRunCallbackEvents(run, []domaincopilot.WorkbenchStreamEvent{{
		Type:      "thinking.delta",
		TextDelta: "valid agent event",
	}, {
		Type:    "message.done",
		RunID:   "rca:run-1",
		Content: "valid root cause event",
	}, {
		Type:    "message.done",
		RunID:   "other-run",
		Content: "filtered",
	}}, now)
	if len(normalized) != 2 {
		t.Fatalf("expected invalid run event to be filtered, got %#v", normalized)
	}
	merged := mergeAgentRunWorkbenchEventSnapshot([]domaincopilot.WorkbenchStreamEvent{{
		ID:        "evt:old-1",
		Type:      "thinking.delta",
		SessionID: "session-1",
		RunID:     "agent:run-events",
		Sequence:  7,
		CreatedAt: now.Add(-2 * time.Minute),
	}, {
		ID:        "evt:old-2",
		Type:      "tool.completed",
		SessionID: "session-1",
		RunID:     "agent:run-events",
		Sequence:  8,
		CreatedAt: now.Add(-time.Minute),
	}}, normalized, 3)

	if len(merged) != 3 {
		t.Fatalf("expected recent 3 event snapshot, got %#v", merged)
	}
	if merged[0].ID != "evt:old-2" {
		t.Fatalf("expected oldest event to be trimmed, got %#v", merged)
	}
	for index, event := range merged {
		if event.Sequence != index+1 || event.SessionID != "session-1" || event.ID == "" || event.CreatedAt.IsZero() {
			t.Fatalf("event[%d] not normalized: %#v", index, event)
		}
		if event.RunID == "other-run" {
			t.Fatalf("filtered run leaked into snapshot: %#v", merged)
		}
	}
}

func TestAgentRunCallbackPayloadWorkbenchEventsAreReservedAndNormalized(t *testing.T) {
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	run := domaincopilot.AgentRun{
		ID:             "agent:run-events",
		ProviderID:     "hermes",
		ProviderKind:   "hermes",
		CapabilityID:   "performance",
		SessionID:      "session-1",
		RootCauseRunID: "session-1",
	}
	payload := map[string]any{
		"summary": "ok",
		"workbenchEvents": []domaincopilot.WorkbenchStreamEvent{{
			ID:        "runner-custom-id",
			Type:      "message.done",
			RunID:     "agent:run-events",
			Sequence:  99,
			CreatedAt: now.Add(-time.Hour),
			Content:   "valid agent event",
		}, {
			Type:    "message.done",
			RunID:   "session-1",
			Content: "session id must not be allowed",
		}, {
			Type:    "thinking.delta",
			RunID:   "foreign-run",
			Content: "foreign run must be filtered",
		}},
	}

	output := mergeAgentRunOutput(map[string]any{}, payload)
	if _, ok := output["workbenchEvents"]; ok {
		t.Fatalf("payload workbenchEvents should be reserved, got %#v", output)
	}
	normalized := normalizeAgentRunCallbackEvents(run, agentRunCallbackPayloadWorkbenchEvents(payload), now)
	if len(normalized) != 1 {
		t.Fatalf("expected only the agent run event to survive, got %#v", normalized)
	}
	if normalized[0].ID != "" || normalized[0].Sequence != 0 || !normalized[0].CreatedAt.Equal(now) || normalized[0].SessionID != "session-1" {
		t.Fatalf("payload event was not normalized before snapshot merge: %#v", normalized[0])
	}
	merged := mergeAgentRunWorkbenchEventSnapshot([]domaincopilot.WorkbenchStreamEvent{{
		ID:        "evt:old",
		Type:      "thinking.delta",
		SessionID: "session-1",
		RunID:     "agent:run-events",
		Sequence:  7,
		CreatedAt: now.Add(-time.Minute),
	}}, normalized, 1)
	if len(merged) != 1 || merged[0].ID == "runner-custom-id" || merged[0].Sequence != 1 || merged[0].Content != "valid agent event" {
		t.Fatalf("snapshot did not rewrite/cap payload events: %#v", merged)
	}
}

func TestCancelAgentRunUpdatesNonTerminalRun(t *testing.T) {
	repo, mock := newAgentRunRepository(t)
	queuedAt := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	expectGetAgentRun(mock, domaincopilot.AgentRun{
		ID:             "agent:cancel",
		ProviderID:     "hermes",
		ProviderKind:   "hermes",
		CapabilityID:   "delivery_failure",
		CreatedBy:      "user-1",
		Status:         domaincopilot.AgentRunStatusRunning,
		CallbackToken:  "callback-token",
		TimeoutSeconds: 600,
		QueuedAt:       queuedAt,
		CreatedAt:      queuedAt,
		UpdatedAt:      queuedAt,
	})
	mock.ExpectExec(`(?s)UPDATE ai_agent_runs\s+SET status = \$1, output = \$2, error_message = \$3, completed_at = \$4, updated_at = \$5\s+WHERE id = \$6 AND status NOT IN \(\$7, \$8, \$9, \$10\)`).
		WithArgs(
			domaincopilot.AgentRunStatusCanceled,
			sqlmock.AnyArg(),
			"operator canceled",
			sqlmock.AnyArg(),
			sqlmock.AnyArg(),
			"agent:cancel",
			domaincopilot.AgentRunStatusCompleted,
			domaincopilot.AgentRunStatusFailed,
			domaincopilot.AgentRunStatusCanceled,
			domaincopilot.AgentRunStatusCallbackTimeout,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	expectGetAgentRun(mock, domaincopilot.AgentRun{
		ID:             "agent:cancel",
		ProviderID:     "hermes",
		ProviderKind:   "hermes",
		CapabilityID:   "delivery_failure",
		CreatedBy:      "user-1",
		Status:         domaincopilot.AgentRunStatusCanceled,
		Output:         map[string]any{"cancelReason": "operator canceled"},
		CallbackToken:  "callback-token",
		ErrorMessage:   "operator canceled",
		TimeoutSeconds: 600,
		QueuedAt:       queuedAt,
		CompletedAt:    &queuedAt,
		CreatedAt:      queuedAt,
		UpdatedAt:      queuedAt,
	})

	run, err := repo.CancelAgentRun(context.Background(), domaincopilot.AgentRunCancelInput{
		RunID:       "agent:cancel",
		RequestedBy: "user-1",
		Reason:      "operator canceled",
	})
	if err != nil {
		t.Fatalf("cancel agent run: %v", err)
	}
	if run.Status != domaincopilot.AgentRunStatusCanceled || run.ErrorMessage != "operator canceled" {
		t.Fatalf("unexpected canceled run: %#v", run)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestUpdateAgentRunCallbackIgnoresLateCallbackForTerminalRun(t *testing.T) {
	repo, mock := newAgentRunRepository(t)
	completedAt := time.Date(2026, 6, 12, 10, 3, 0, 0, time.UTC)
	expectGetAgentRun(mock, domaincopilot.AgentRun{
		ID:             "agent:late",
		ProviderID:     "hermes",
		ProviderKind:   "hermes",
		CapabilityID:   "delivery_failure",
		CreatedBy:      "user-1",
		Status:         domaincopilot.AgentRunStatusCanceled,
		Output:         map[string]any{"cancelReason": "operator canceled"},
		CallbackToken:  "callback-token",
		ErrorMessage:   "operator canceled",
		TimeoutSeconds: 600,
		QueuedAt:       completedAt.Add(-3 * time.Minute),
		CompletedAt:    &completedAt,
		CreatedAt:      completedAt.Add(-3 * time.Minute),
		UpdatedAt:      completedAt,
	})

	run, err := repo.UpdateAgentRunCallback(context.Background(), domaincopilot.AgentRunCallbackInput{
		RunID:         "agent:late",
		CallbackToken: "callback-token",
		Status:        domaincopilot.AgentRunStatusCompleted,
		Payload:       map[string]any{"summary": "late result"},
	})
	if err != nil {
		t.Fatalf("late callback: %v", err)
	}
	if run.Status != domaincopilot.AgentRunStatusCanceled || run.Output["summary"] != nil {
		t.Fatalf("late callback should not override terminal state, got %#v", run)
	}
	if run.CallbackTransition != domaincopilot.AgentRunCallbackTransitionNoopTerminal {
		t.Fatalf("expected late callback noop transition, got %#v", run.CallbackTransition)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func newAgentRunRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm postgres mock: %v", err)
	}
	return New(db), mock
}

func expectGetAgentRun(mock sqlmock.Sqlmock, run domaincopilot.AgentRun) {
	rows := sqlmock.NewRows([]string{
		"id",
		"provider_id",
		"provider_kind",
		"capability_id",
		"skill_ids",
		"session_id",
		"root_cause_run_id",
		"created_by",
		"status",
		"scope",
		"toolset",
		"tool_bindings",
		"skill_bindings",
		"input",
		"output",
		"tool_executions",
		"analysis_artifacts",
		"callback_token",
		"claimed_by_agent_id",
		"external_run_id",
		"error_message",
		"timeout_seconds",
		"queued_at",
		"started_at",
		"last_heartbeat_at",
		"completed_at",
		"created_at",
		"updated_at",
	}).AddRow(
		run.ID,
		run.ProviderID,
		run.ProviderKind,
		run.CapabilityID,
		`[]`,
		nullableSQLString(run.SessionID),
		nullableSQLString(run.RootCauseRunID),
		run.CreatedBy,
		run.Status,
		`{}`,
		`{}`,
		`[]`,
		`[]`,
		mustJSON(run.Input),
		mustJSON(run.Output),
		`[]`,
		`[]`,
		run.CallbackToken,
		nullableSQLString(run.ClaimedByAgentID),
		nullableSQLString(run.ExternalRunID),
		nullableSQLString(run.ErrorMessage),
		run.TimeoutSeconds,
		run.QueuedAt,
		nullableSQLTime(run.StartedAt),
		nullableSQLTime(run.LastHeartbeatAt),
		nullableSQLTime(run.CompletedAt),
		run.CreatedAt,
		run.UpdatedAt,
	)
	mock.ExpectQuery(`(?s)SELECT id, provider_id, provider_kind, capability_id, skill_ids, session_id, root_cause_run_id, created_by, status, scope, toolset, tool_bindings, skill_bindings, input, output,\s+tool_executions, analysis_artifacts, callback_token, claimed_by_agent_id, external_run_id, error_message, timeout_seconds,\s+queued_at, started_at, last_heartbeat_at, completed_at, created_at, updated_at\s+FROM ai_agent_runs\s+WHERE id = \$1 LIMIT 1`).
		WithArgs(run.ID).
		WillReturnRows(rows)
}

func nullableSQLString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableSQLTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return *value
}

func mustJSON(value map[string]any) string {
	if value == nil {
		return `{}`
	}
	bytes, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(bytes)
}
