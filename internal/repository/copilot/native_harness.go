package copilot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

func (r *Repository) CreateNativeRun(ctx context.Context, run domaincopilot.AgentRun, message domaincopilot.Message, key, hash string) (domaincopilot.AgentRun, error) {
	var saved domaincopilot.AgentRun
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := &Repository{db: tx, storageKeys: r.storageKeys}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, run.CreatedBy+":"+run.SessionID+":"+key).Error; err != nil {
			return err
		}
		var existing struct{ ID, NativeRequestHash string }
		found := tx.Raw(`SELECT id,native_request_hash FROM ai_agent_runs WHERE created_by=? AND session_id=? AND native_request_id=? AND provider_kind IN ('native','dsh')`, run.CreatedBy, run.SessionID, key).Scan(&existing)
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
		if run.ProviderKind != "dsh" {
			return apperrors.ErrUnsupportedOperation
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
	return domaincopilot.NativeLease{}, apperrors.ErrUnsupportedOperation
}

func (r *Repository) ClaimDSHRun(ctx context.Context, worker string) (domaincopilot.NativeLease, error) {
	if !strings.HasPrefix(worker, "dsh-core:") {
		return domaincopilot.NativeLease{}, apperrors.ErrInvalidArgument
	}
	return r.claimGovernedRun(ctx, worker, "dsh", "dsh-core")
}

func (r *Repository) claimGovernedRun(ctx context.Context, worker, kind, owner string) (domaincopilot.NativeLease, error) {
	var lease domaincopilot.NativeLease
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var id string
		if err := tx.Raw(`SELECT id FROM ai_agent_runs WHERE provider_kind=? AND COALESCE(input->>'_sohaExecutionOwner','')=? AND status IN ('queued','running')
   AND (native_phase <> 'paused' OR (provider_kind='dsh' AND output->>'stopReason'='waiting_input'))
   AND (native_lease_until IS NULL OR native_lease_until <= now())
   ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED`, kind, owner).Row().Scan(&id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return copilotNotFound("native run", "")
			}
			return fmt.Errorf("claim native run: %w", err)
		}
		// A successor only reconciles a submitted dsh attempt. Fence child
		// dispatch in this transaction, before the new worker reads its receipt.
		if err := tx.Exec(`UPDATE ai_agent_runs SET status='running',claimed_by_agent_id=?,started_at=COALESCE(started_at,now()),last_heartbeat_at=now(),
   native_phase=CASE WHEN native_fence>0 THEN 'cancelling' ELSE native_phase END,
   output=CASE WHEN native_fence>0 THEN jsonb_set(output::jsonb,'{phase}','"cancelling"')::json ELSE output END,
   native_fence=native_fence+1,native_version=native_version+1,native_lease_until=now()+interval '30 seconds',updated_at=now() WHERE id=?`, worker, id).Error; err != nil {
			return err
		}
		var err error
		lease, err = (&Repository{db: tx, storageKeys: r.storageKeys}).ReadNativeLease(ctx, "", id)
		return err
	})
	return lease, err
}

func (r *Repository) ReadNativeLease(ctx context.Context, user, id string) (domaincopilot.NativeLease, error) {
	// Run output and the CAS version must be read from the same MVCC snapshot.
	query := `SELECT ` + agentRunColumns + `, COALESCE(native_request_hash,''),native_version,native_fence,native_phase,native_event_sequence,
		native_checkpoint,native_lease_until,native_cancel_at,budget.run_id IS NOT NULL,
		COALESCE(work_calls,0),COALESCE(cleanup_calls,0),COALESCE(work_limit,0),COALESCE(cleanup_limit,0)
		FROM ai_agent_runs
		LEFT JOIN (SELECT run_id,work_calls,cleanup_calls,work_limit,cleanup_limit FROM ai_native_tool_budget) budget ON budget.run_id=ai_agent_runs.id
		WHERE id = ?`
	args := []any{strings.TrimSpace(id)}
	if user = strings.TrimSpace(user); user != "" {
		query += ` AND created_by = ?`
		args = append(args, user)
	}
	var lease domaincopilot.NativeLease
	var checkpoint []byte
	var cancelAt *time.Time
	var hasBudget bool
	usage := &lease.ToolUsage
	row := dbtx.DB(ctx, r.db).Raw(query, args...).Row()
	run, err := scanAgentRunRow(row, id, &lease.RequestHash, &lease.Version, &lease.Fence, &lease.Phase, &lease.Sequence,
		&checkpoint, &lease.LeaseUntil, &cancelAt, &hasBudget, &usage.WorkCalls, &usage.CleanupCalls, &usage.WorkLimit, &usage.CleanupLimit)
	if err != nil {
		return domaincopilot.NativeLease{}, err
	}
	if run.ProviderKind != "native" && !(run.ProviderKind == "dsh" && run.Input["_sohaExecutionOwner"] == "dsh-core") {
		return domaincopilot.NativeLease{}, fmt.Errorf("%w: run is owned by an external runtime", apperrors.ErrUnsupportedOperation)
	}
	if !hasBudget {
		return domaincopilot.NativeLease{}, nativeBudgetConflict()
	}
	lease.Checkpoint, err = r.decodeNativeCheckpoint(run, checkpoint)
	if err != nil {
		return domaincopilot.NativeLease{}, err
	}
	lease.Run = run
	lease.CancelRequested = cancelAt != nil
	return lease, nil
}

func (r *Repository) RenewNativeLease(ctx context.Context, lease domaincopilot.NativeLease) (bool, error) {
	result := r.db.WithContext(ctx).Exec(`UPDATE ai_agent_runs SET native_lease_until=now()+interval '30 seconds',last_heartbeat_at=now()
  WHERE id=? AND native_fence=? AND claimed_by_agent_id=? AND native_lease_until>now() AND native_cancel_at IS NULL AND status='running'`, lease.Run.ID, lease.Fence, lease.Run.ClaimedByAgentID)
	return result.RowsAffected == 1, result.Error
}

func (r *Repository) AdvanceNativeRun(ctx context.Context, lease domaincopilot.NativeLease, next domaincopilot.NativeAdvance) (domaincopilot.NativeLease, error) {
	checkpoint, err := r.encodeNativeCheckpoint(lease.Run, next.Checkpoint)
	if err != nil {
		return domaincopilot.NativeLease{}, err
	}
	var saved domaincopilot.NativeLease
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
   WHERE id=? AND native_version=? AND native_fence=? AND claimed_by_agent_id=? AND native_lease_until>now() AND status='running'
    AND (native_cancel_at IS NULL OR ?='canceled' OR (?='cancelling' AND ?='running') OR (provider_kind='dsh' AND ?='paused' AND ?='unknown_outcome'))`,
			next.Phase, string(checkpoint), seq, leaseUntil, next.Run.Status, string(outputJSON), string(tools), string(artifacts), next.Run.ErrorMessage, completedAt, now, lease.Run.ID, lease.Version, lease.Fence, lease.Run.ClaimedByAgentID, next.Run.Status, next.Phase, next.Run.Status, next.Phase, next.StopReason)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: stale native worker", apperrors.ErrConflict)
		}
		if err = tx.Exec(`INSERT INTO ai_agent_run_steps(run_id,sequence,attempt,phase,checkpoint) VALUES (?,?,?,?,?)`, lease.Run.ID, lease.Version+1, lease.Fence, next.Phase, string(checkpoint)).Error; err != nil {
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
		saved, readErr = (&Repository{db: tx, storageKeys: r.storageKeys}).ReadNativeLease(ctx, "", lease.Run.ID)
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
	result := r.db.WithContext(ctx).Exec(`UPDATE ai_agent_runs SET native_cancel_at=now(),native_version=native_version+1,native_phase='cancelling',
  native_lease_until=CASE WHEN provider_kind='dsh' AND native_phase='paused' AND output->>'stopReason'='unknown_outcome' THEN NULL ELSE native_lease_until END,
  output=jsonb_set(output::jsonb,'{phase}','"cancelling"')::json,updated_at=now()
  WHERE id=? AND created_by=? AND native_cancel_at IS NULL AND status IN ('queued','running')`, id, user)
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
	var data map[string]json.RawMessage
	if err := json.Unmarshal(lease.Checkpoint, &data); err != nil || data == nil {
		return domaincopilot.AgentRun{}, fmt.Errorf("%w: native checkpoint unavailable", apperrors.ErrConflict)
	}
	data["userInput"], err = json.Marshal(input)
	if err != nil {
		return domaincopilot.AgentRun{}, err
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return domaincopilot.AgentRun{}, err
	}
	checkpoint, err := r.encodeNativeCheckpoint(lease.Run, raw)
	if err != nil {
		return domaincopilot.AgentRun{}, err
	}
	result := r.db.WithContext(ctx).Exec(`UPDATE ai_agent_runs SET native_phase='preparing',native_checkpoint=?::jsonb,native_version=native_version+1,native_lease_until=CASE WHEN provider_kind='dsh' THEN native_lease_until ELSE NULL END,
  output=(jsonb_set(jsonb_set(output::jsonb,'{phase}','"preparing"'),'{stopReason}','""')-'pendingQuestion')::json,updated_at=now()
  WHERE id=? AND created_by=? AND native_version=? AND native_phase='paused' AND output->>'stopReason'='waiting_input' AND native_cancel_at IS NULL`, string(checkpoint), id, user, lease.Version)
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
