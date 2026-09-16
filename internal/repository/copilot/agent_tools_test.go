package copilot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestBeginAgentToolCallChecksLockedState(t *testing.T) {
	for _, name := range []string{"canceled", "busy", "exhausted", "artifacts"} {
		t.Run(name, func(t *testing.T) {
			repo, mock := newAgentRunRepository(t)
			run := domaincopilot.AgentRun{ID: "run", Status: "running", ClaimedByAgentID: "runner", CallbackToken: "token", CreatedAt: time.Now(), UpdatedAt: time.Now()}
			switch name {
			case "canceled":
				run.Status = "canceled"
			case "busy":
				run.ToolExecutions = []domaincopilot.ToolExecution{{ID: "active", Status: "running"}}
			case "exhausted":
				run.ToolExecutions = make([]domaincopilot.ToolExecution, 8)
			case "artifacts":
				run.AnalysisArtifacts = make([]domaincopilot.AnalysisArtifact, 4)
			}
			expectLockedAgentRun(mock, run)
			mock.ExpectRollback()
			_, err := repo.BeginAgentToolCall(context.Background(), domaincopilot.AgentRunCallbackInput{RunID: run.ID, AgentID: "runner", CallbackToken: "token", ToolExecutions: []domaincopilot.ToolExecution{{ID: "next", ToolName: "artifact.preview", Status: "running"}}})
			if err == nil {
				t.Fatal("locked state allowed a new tool")
			}
			if name == "busy" && !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("busy call: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBeginAgentToolCallPersistsReservationBeforeReturning(t *testing.T) {
	repo, mock := newAgentRunRepository(t)
	run := domaincopilot.AgentRun{ID: "run", Status: "running", ClaimedByAgentID: "runner", CallbackToken: "token", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	tool := domaincopilot.ToolExecution{ID: "tool", ToolName: "knowledge.search", Status: "running"}
	expectLockedAgentRun(mock, run)
	mock.ExpectExec(`(?s)UPDATE ai_agent_runs`).WillReturnResult(sqlmock.NewResult(0, 1))
	run.ToolExecutions = []domaincopilot.ToolExecution{tool}
	expectGetAgentRun(mock, run)
	mock.ExpectCommit()
	reserved, err := repo.BeginAgentToolCall(context.Background(), domaincopilot.AgentRunCallbackInput{RunID: run.ID, AgentID: "runner", CallbackToken: "token", ToolExecutions: []domaincopilot.ToolExecution{tool}})
	if err != nil || len(reserved.ToolExecutions) != 1 || reserved.ToolExecutions[0].Status != "running" {
		t.Fatalf("reservation=%+v error=%v", reserved.ToolExecutions, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDelegatedRunCreationChecksLockedParentAndReusesChild(t *testing.T) {
	for _, status := range []string{"canceled", "running"} {
		t.Run(status, func(t *testing.T) {
			repo, mock := newAgentRunRepository(t)
			parent := domaincopilot.AgentRun{ID: "parent", CreatedBy: "owner", SessionID: "session", Status: status, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			child := domaincopilot.AgentRun{ID: "parent:specialist", ParentRunID: "parent", CreatedBy: "owner", SessionID: "session", Status: "completed", Input: map[string]any{"_sohaParentRunId": "parent"}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			expectLockedAgentRun(mock, parent)
			if status == "canceled" {
				mock.ExpectRollback()
			} else {
				mock.ExpectQuery(`(?s)SELECT .* FROM ai_agent_runs.*WHERE id = \$1 AND created_by = \$2 LIMIT 1`).WithArgs(child.ID, parent.CreatedBy).WillReturnRows(agentRunRows(child))
				mock.ExpectCommit()
			}
			result, err := repo.CreateDelegatedAgentRun(context.Background(), parent.ID, child)
			if status == "canceled" && !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("canceled parent: %v", err)
			}
			if status == "running" && (err != nil || result.ID != child.ID || result.ParentRunID != parent.ID || result.Status != "completed") {
				t.Fatalf("duplicate delegation: %+v %v", result, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTerminalRunClosesInterruptedToolsWithoutChangingFinishedResults(t *testing.T) {
	now := time.Now()
	original := []domaincopilot.ToolExecution{{ID: "waiting", Status: "running"}, {ID: "done", Status: "success", Summary: "result"}}
	result := finishInterruptedAgentTools(original, now)
	if result[0].Status != "failed" || result[0].CompletedAt == nil || result[1].Status != "success" || result[1].Summary != "result" || original[0].Status != "running" {
		t.Fatalf("terminal tools: %+v", result)
	}
}

func TestCancellationAcknowledgementOnlyAcceptsClaimedRunner(t *testing.T) {
	for _, name := range []string{"ack", "wrong-runner", "late-completion"} {
		t.Run(name, func(t *testing.T) {
			repo, mock := newAgentRunRepository(t)
			run := domaincopilot.AgentRun{ID: "run", Status: "canceled", ClaimedByAgentID: "runner", CallbackToken: "token", Output: map[string]any{"cancellationPending": true}, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			input := domaincopilot.AgentRunCallbackInput{RunID: run.ID, AgentID: "runner", CallbackToken: "token", Status: "canceled", Payload: map[string]any{"cancellationAcknowledged": true}}
			if name == "wrong-runner" {
				input.AgentID = "other"
			}
			if name == "late-completion" {
				input.Status = "completed"
			}
			expectLockedAgentRun(mock, run)
			if name == "ack" {
				mock.ExpectExec(`UPDATE ai_agent_runs SET output`).WillReturnResult(sqlmock.NewResult(0, 1))
				run.Output = map[string]any{"cancellationPending": false, "cancellationAcknowledgedAt": "server-time"}
				expectGetAgentRun(mock, run)
			}
			mock.ExpectCommit()
			got, err := repo.UpdateAgentRunCallback(context.Background(), input)
			if err != nil || got.Status != "canceled" || (got.Output["cancellationPending"] == false) != (name == "ack") {
				t.Fatalf("result=%+v error=%v", got, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
