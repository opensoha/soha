package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func (r *Repository) withLockedAgentRun(ctx context.Context, runID string, mutate func(*Repository, domaincopilot.AgentRun) (domaincopilot.AgentRun, error)) (domaincopilot.AgentRun, error) {
	var result domaincopilot.AgentRun
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := scanAgentRunRow(tx.Raw(agentRunSelect()+" WHERE id = ? FOR UPDATE", runID).Row(), runID)
		if err != nil {
			return err
		}
		result, err = mutate(New(tx), current)
		return err
	})
	return result, err
}

// The row lock covers only the reservation, never the remote tool execution.
func (r *Repository) BeginAgentToolCall(ctx context.Context, input domaincopilot.AgentRunCallbackInput) (domaincopilot.AgentRun, error) {
	return r.withLockedAgentRun(ctx, input.RunID, func(repo *Repository, current domaincopilot.AgentRun) (domaincopilot.AgentRun, error) {
		if current.Status != domaincopilot.AgentRunStatusRunning || current.ClaimedByAgentID == "" || current.ClaimedByAgentID != input.AgentID || current.CallbackToken == "" || current.CallbackToken != input.CallbackToken {
			return domaincopilot.AgentRun{}, fmt.Errorf("%w: run is not active for this tool caller", apperrors.ErrAccessDenied)
		}
		if len(current.ToolExecutions) >= 8 || len(input.ToolExecutions) != 1 {
			return domaincopilot.AgentRun{}, fmt.Errorf("%w: tool call budget exhausted", apperrors.ErrInvalidArgument)
		}
		for _, tool := range current.ToolExecutions {
			if tool.Status == "running" {
				return domaincopilot.AgentRun{}, fmt.Errorf("%w: a tool call is already active", apperrors.ErrConflict)
			}
		}
		if input.ToolExecutions[0].ToolName == "artifact.preview" && len(current.AnalysisArtifacts) >= 4 {
			return domaincopilot.AgentRun{}, fmt.Errorf("%w: artifact budget exhausted", apperrors.ErrInvalidArgument)
		}
		return repo.updateAgentRunCallback(ctx, current, input)
	})
}

// Creation and parent cancellation serialize on the parent row. The deterministic
// child ID makes repeated delegate requests return the original run.
func (r *Repository) CreateDelegatedAgentRun(ctx context.Context, parentID string, child domaincopilot.AgentRun) (domaincopilot.AgentRun, error) {
	return r.withLockedAgentRun(ctx, parentID, func(repo *Repository, parent domaincopilot.AgentRun) (domaincopilot.AgentRun, error) {
		if parent.Status != domaincopilot.AgentRunStatusRunning || parent.ParentRunID != "" || child.ParentRunID != parent.ID || child.ID != parent.ID+":specialist" || child.CreatedBy != parent.CreatedBy || child.SessionID != parent.SessionID {
			return domaincopilot.AgentRun{}, fmt.Errorf("%w: specialist parent is not active", apperrors.ErrAccessDenied)
		}
		existing, err := repo.GetAgentRun(ctx, parent.CreatedBy, child.ID)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, apperrors.ErrNotFound) {
			return domaincopilot.AgentRun{}, err
		}
		return repo.CreateAgentRun(ctx, child)
	})
}

func finishInterruptedAgentTools(tools []domaincopilot.ToolExecution, now time.Time) []domaincopilot.ToolExecution {
	tools = append([]domaincopilot.ToolExecution(nil), tools...)
	for index := range tools {
		if tools[index].Status == "running" {
			tools[index].Status = "failed"
			tools[index].Summary = "Task ended before this tool returned a result."
			tools[index].CompletedAt = &now
		}
	}
	return tools
}

// A cancellation acknowledgement may update its metadata, never its terminal state.
func (r *Repository) acknowledgeAgentCancellation(ctx context.Context, current domaincopilot.AgentRun, input domaincopilot.AgentRunCallbackInput) (domaincopilot.AgentRun, error) {
	if current.Status != domaincopilot.AgentRunStatusCanceled || normalizeAgentRunStatus(input.Status) != domaincopilot.AgentRunStatusCanceled || current.Output["cancellationPending"] != true || input.Payload["cancellationAcknowledged"] != true || current.ClaimedByAgentID == "" || current.ClaimedByAgentID != input.AgentID {
		return current, nil
	}
	now := time.Now().UTC()
	output := mergeAgentRunOutput(current.Output, map[string]any{"cancellationPending": false, "cancellationAcknowledgedAt": now.Format(time.RFC3339Nano)})
	payload, err := json.Marshal(output)
	if err != nil {
		return domaincopilot.AgentRun{}, err
	}
	if err := r.db.WithContext(ctx).Exec("UPDATE ai_agent_runs SET output = ?, updated_at = ? WHERE id = ? AND status = ?", string(payload), now, current.ID, domaincopilot.AgentRunStatusCanceled).Error; err != nil {
		return domaincopilot.AgentRun{}, err
	}
	updated, err := r.GetAgentRun(ctx, "", current.ID)
	updated.CallbackTransition = domaincopilot.AgentRunCallbackTransitionTerminal
	return updated, err
}
