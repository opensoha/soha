package copilot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func (r *Repository) CreateNativeRun(ctx context.Context, run domaincopilot.AgentRun, message domaincopilot.Message, key, hash string) (domaincopilot.AgentRun, error) {
	var saved domaincopilot.AgentRun
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := &Repository{db: tx}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, run.CreatedBy+":"+run.SessionID+":"+key).Error; err != nil {
			return err
		}
		var existing struct{ ID, NativeRequestHash string }
		found := tx.Raw(`SELECT id,native_request_hash FROM ai_agent_runs WHERE created_by=? AND session_id=? AND native_request_id=? AND provider_kind='native'`, run.CreatedBy, run.SessionID, key).Scan(&existing)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected > 0 {
			if existing.NativeRequestHash != hash {
				return fmt.Errorf("%w: request key belongs to different content", apperrors.ErrConflict)
			}
			var err error
			saved, err = repo.GetAgentRun(ctx, run.CreatedBy, existing.ID)
			return err
		}
		if _, err := repo.CreateAgentRun(ctx, run); err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE ai_agent_runs SET native_request_id=?,native_request_hash=?,native_phase='queued' WHERE id=?`, key, hash, run.ID).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO ai_native_tool_budget(run_id) VALUES (?)`, run.ID).Error; err != nil {
			return err
		}
		if _, err := repo.CreateMessage(ctx, message); err != nil {
			return err
		}
		saved = run
		return nil
	})
	return saved, err
}

func (r *Repository) ClaimNativeRun(ctx context.Context, worker string) (domaincopilot.NativeLease, error) {
	var lease domaincopilot.NativeLease
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var id string
		if err := tx.Raw(`SELECT id FROM ai_agent_runs WHERE provider_kind='native' AND status IN ('queued','running')
   AND native_phase <> 'paused' AND (native_lease_until IS NULL OR native_lease_until <= now())
   ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED`).Row().Scan(&id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return copilotNotFound("native run", "")
			}
			return fmt.Errorf("claim native run: %w", err)
		}
		if err := tx.Exec(`UPDATE ai_agent_runs SET status='running',claimed_by_agent_id=?,started_at=COALESCE(started_at,now()),last_heartbeat_at=now(),
   native_fence=native_fence+1,native_version=native_version+1,native_lease_until=now()+interval '30 seconds',updated_at=now() WHERE id=?`, worker, id).Error; err != nil {
			return err
		}
		var err error
		lease, err = (&Repository{db: tx}).ReadNativeLease(ctx, "", id)
		return err
	})
	return lease, err
}

func (r *Repository) ReadNativeLease(ctx context.Context, user, id string) (domaincopilot.NativeLease, error) {
	run, err := r.GetAgentRun(ctx, user, id)
	if err != nil {
		return domaincopilot.NativeLease{}, err
	}
	if run.ProviderKind != "native" {
		return domaincopilot.NativeLease{}, fmt.Errorf("%w: run is owned by an external runtime", apperrors.ErrUnsupportedOperation)
	}
	var row struct {
		NativeRequestHash                                string
		NativeVersion, NativeFence, NativeEventSequence  int64
		NativePhase                                      string
		NativeCheckpoint                                 []byte
		NativeLeaseUntil, NativeCancelAt                 *time.Time
		WorkCalls, CleanupCalls, WorkLimit, CleanupLimit int
	}
	result := r.db.WithContext(ctx).Raw(`SELECT native_request_hash,native_version,native_fence,native_phase,native_event_sequence,native_checkpoint,native_lease_until,native_cancel_at,work_calls,cleanup_calls,work_limit,cleanup_limit FROM ai_agent_runs JOIN ai_native_tool_budget ON run_id=ai_agent_runs.id WHERE ai_agent_runs.id=?`, id).Scan(&row)
	if result.Error != nil {
		return domaincopilot.NativeLease{}, result.Error
	}
	if result.RowsAffected != 1 {
		return domaincopilot.NativeLease{}, nativeBudgetConflict()
	}
	return domaincopilot.NativeLease{Run: run, Version: row.NativeVersion, Fence: row.NativeFence, Phase: row.NativePhase, Sequence: row.NativeEventSequence, Checkpoint: row.NativeCheckpoint, RequestHash: row.NativeRequestHash, LeaseUntil: row.NativeLeaseUntil, CancelRequested: row.NativeCancelAt != nil, ToolUsage: domaincopilot.NativeToolUsage{WorkCalls: row.WorkCalls, CleanupCalls: row.CleanupCalls, WorkLimit: row.WorkLimit, CleanupLimit: row.CleanupLimit}}, nil
}

func (r *Repository) RenewNativeLease(ctx context.Context, lease domaincopilot.NativeLease) (bool, error) {
	result := r.db.WithContext(ctx).Exec(`UPDATE ai_agent_runs SET native_lease_until=now()+interval '30 seconds',last_heartbeat_at=now()
  WHERE id=? AND native_fence=? AND claimed_by_agent_id=? AND native_lease_until>now() AND native_cancel_at IS NULL AND status='running'`, lease.Run.ID, lease.Fence, lease.Run.ClaimedByAgentID)
	return result.RowsAffected == 1, result.Error
}

func (r *Repository) AdvanceNativeRun(ctx context.Context, lease domaincopilot.NativeLease, next domaincopilot.NativeAdvance) (domaincopilot.NativeLease, error) {
	var saved domaincopilot.NativeLease
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		seq := lease.Sequence
		now := time.Now().UTC()
		for i := range next.Events {
			seq++
			event := &next.Events[i]
			event.Sequence = int(seq)
			event.ID = fmt.Sprintf("evt:%s:%06d", lease.Run.ID, seq)
			event.RunID = lease.Run.ID
			event.SessionID = lease.Run.SessionID
			event.CreatedAt = now
			raw, err := json.Marshal(event)
			if err != nil {
				return err
			}
			if err = tx.Exec(`INSERT INTO ai_agent_run_events(run_id,sequence,event_id,payload) VALUES (?,?,?,?)`, lease.Run.ID, seq, event.ID, string(raw)).Error; err != nil {
				return err
			}
		}
		output := mergeAgentRunOutput(next.Run.Output, map[string]any{"phase": next.Phase, "stopReason": next.StopReason, "eventSequence": seq})
		legacy := append(workbenchEventsFromRepositoryValue(lease.Run.Output["workbenchEvents"]), next.Events...)
		if len(legacy) > maxAgentRunWorkbenchEvents {
			legacy = legacy[len(legacy)-maxAgentRunWorkbenchEvents:]
		}
		output["workbenchEvents"] = legacy
		outputJSON, err := json.Marshal(output)
		if err != nil {
			return err
		}
		tools, err := json.Marshal(next.Run.ToolExecutions)
		if err != nil {
			return err
		}
		artifacts, err := json.Marshal(next.Run.AnalysisArtifacts)
		if err != nil {
			return err
		}
		var completedAt *time.Time
		if agentRunTerminal(next.Run.Status) {
			completedAt = &now
		}
		var leaseUntil any = now.Add(30 * time.Second)
		if next.Release {
			leaseUntil = now.Add(5 * time.Second)
		}
		result := tx.Exec(`UPDATE ai_agent_runs SET native_version=native_version+1,native_phase=?,native_checkpoint=?,native_event_sequence=?,native_lease_until=?,
   status=?,output=?,tool_executions=?,analysis_artifacts=?,error_message=?,completed_at=?,updated_at=?
   WHERE id=? AND native_version=? AND native_fence=? AND native_lease_until>now() AND status='running' AND (native_cancel_at IS NULL OR ?='canceled' OR (?='cancelling' AND ?='running'))`,
			next.Phase, string(next.Checkpoint), seq, leaseUntil, next.Run.Status, string(outputJSON), string(tools), string(artifacts), next.Run.ErrorMessage, completedAt, now, lease.Run.ID, lease.Version, lease.Fence, next.Run.Status, next.Phase, next.Run.Status)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: stale native worker", apperrors.ErrConflict)
		}
		if err = tx.Exec(`INSERT INTO ai_agent_run_steps(run_id,sequence,attempt,phase,checkpoint) VALUES (?,?,?,?,?)`, lease.Run.ID, lease.Version+1, lease.Fence, next.Phase, string(next.Checkpoint)).Error; err != nil {
			return err
		}
		if next.FinalMessage != nil {
			message := *next.FinalMessage
			meta, err := json.Marshal(message.Metadata)
			if err != nil {
				return err
			}
			if message.SessionID != lease.Run.SessionID || message.ID != lease.Run.ID+":reply" || !agentRunTerminal(next.Run.Status) {
				return fmt.Errorf("%w: invalid native result projection", apperrors.ErrInvalidArgument)
			}
			if err = tx.Exec(`INSERT INTO ai_messages(id,session_id,role,content,metadata,created_at) VALUES (?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, message.ID, message.SessionID, message.Role, message.Content, string(meta), now).Error; err != nil {
				return err
			}
		}
		var readErr error
		saved, readErr = (&Repository{db: tx}).ReadNativeLease(ctx, "", lease.Run.ID)
		return readErr
	})
	return saved, err
}

func (r *Repository) RequestNativeCancel(ctx context.Context, user, id string) (domaincopilot.AgentRun, error) {
	lease, err := r.ReadNativeLease(ctx, user, id)
	if err != nil {
		return domaincopilot.AgentRun{}, err
	}
	if agentRunTerminal(lease.Run.Status) {
		return lease.Run, nil
	}
	result := r.db.WithContext(ctx).Exec(`UPDATE ai_agent_runs SET native_cancel_at=COALESCE(native_cancel_at,now()),native_phase='cancelling',output=jsonb_set(output::jsonb,'{phase}','"cancelling"')::json,updated_at=now() WHERE id=? AND created_by=? AND status IN ('queued','running')`, id, user)
	if result.Error != nil {
		return domaincopilot.AgentRun{}, result.Error
	}
	return r.GetAgentRun(ctx, user, id)
}

func (r *Repository) ResumeNativeRun(ctx context.Context, user, id, input string) (domaincopilot.AgentRun, error) {
	lease, err := r.ReadNativeLease(ctx, user, id)
	if err != nil {
		return domaincopilot.AgentRun{}, err
	}
	if lease.Run.Status != "running" || lease.CancelRequested {
		return domaincopilot.AgentRun{}, fmt.Errorf("%w: run cannot resume", apperrors.ErrConflict)
	}
	if lease.Phase != "paused" {
		return lease.Run, nil
	}
	if lease.Run.StopReason != "waiting_input" {
		return domaincopilot.AgentRun{}, fmt.Errorf("%w: only a task waiting for user input can resume here", apperrors.ErrConflict)
	}
	result := r.db.WithContext(ctx).Exec(`UPDATE ai_agent_runs SET native_phase='preparing',native_checkpoint=jsonb_set(native_checkpoint,'{userInput}',to_jsonb(?::text)),native_version=native_version+1,native_lease_until=NULL,
  output=(jsonb_set(jsonb_set(output::jsonb,'{phase}','"preparing"'),'{stopReason}','""')-'pendingQuestion')::json,updated_at=now()
  WHERE id=? AND created_by=? AND native_version=? AND native_phase='paused' AND output->>'stopReason'='waiting_input' AND native_cancel_at IS NULL`, input, id, user, lease.Version)
	if result.Error != nil {
		return domaincopilot.AgentRun{}, result.Error
	}
	if result.RowsAffected != 1 {
		return domaincopilot.AgentRun{}, fmt.Errorf("%w: native run changed", apperrors.ErrConflict)
	}
	return r.GetAgentRun(ctx, user, id)
}

func (r *Repository) ListNativeEvents(ctx context.Context, user, id string, after int64) (page domaincopilot.NativeEventPage, resultErr error) {
	lease, err := r.ReadNativeLease(ctx, user, id)
	if err != nil {
		return domaincopilot.NativeEventPage{}, err
	}
	page = domaincopilot.NativeEventPage{Items: []domaincopilot.WorkbenchStreamEvent{}, NextSequence: after}
	if after < 0 || after > lease.Sequence {
		page.ResyncRequired = true
		page.NextSequence = lease.Sequence
		return page, nil
	}
	rows, err := r.db.WithContext(ctx).Raw(`SELECT payload FROM ai_agent_run_events WHERE run_id=? AND sequence>? ORDER BY sequence LIMIT 101`, id, after).Rows()
	if err != nil {
		return page, err
	}
	defer func() {
		if err := rows.Close(); resultErr == nil && err != nil {
			resultErr = err
		}
	}()
	for rows.Next() {
		var raw []byte
		var event domaincopilot.WorkbenchStreamEvent
		if err = rows.Scan(&raw); err != nil {
			return page, err
		}
		if err = json.Unmarshal(raw, &event); err != nil {
			return page, err
		}
		if len(page.Items) == 100 {
			page.HasMore = true
			break
		}
		page.Items = append(page.Items, event)
		page.NextSequence = int64(event.Sequence)
	}
	return page, rows.Err()
}
