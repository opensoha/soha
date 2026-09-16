package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

const inspectionTaskColumns = `id,title,scope_type,cluster_id,namespace,checks,enabled,interval_minutes,metadata,created_by,last_run_at,created_at,updated_at,capability_config,revision,execution_token_id`
const inspectionRunColumns = `id,task_id,triggered_by,status,severity,summary,findings,report,started_at,completed_at,created_at`

// Due slots are claimed and recorded together. Missed intervals coalesce into one
// receipt, and an active capability goal suppresses overlapping runs.
func (r *Repository) QueueDueInspectionRuns(ctx context.Context, now time.Time, limit int) error {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		rows, err := db.Raw(`SELECT `+inspectionTaskColumns+` FROM ai_inspection_tasks
            WHERE enabled AND interval_minutes > 0
              AND COALESCE(capability_config->'trigger'->>'kind','schedule') = 'schedule'
              AND (last_run_at IS NULL OR last_run_at + make_interval(mins => interval_minutes) <= ?)
              AND NOT soha_inspection_has_active(id)
            ORDER BY last_run_at ASC NULLS FIRST,id LIMIT ? FOR UPDATE SKIP LOCKED`, now, limit).Rows()
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		tasks := []domain.InspectionTask{}
		for rows.Next() {
			task, err := scanInspectionTask(rows)
			if err != nil {
				return err
			}
			tasks = append(tasks, task)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
		for _, task := range tasks {
			if _, err := r.queueInspectionRun(ctx, task, "schedule", "", now); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repository) queueInspectionRun(ctx context.Context, task domain.InspectionTask, trigger, key string, now time.Time) (domain.InspectionRun, error) {
	run := domain.InspectionRun{ID: uuid.NewString(), TaskID: task.ID, TriggeredBy: trigger, Status: "queued", Severity: "info", Summary: "Awaiting inspection handoff", Findings: []domain.InspectionFinding{}, Report: map[string]any{"registrationRevision": task.Revision}, StartedAt: now, CreatedAt: now}
	if key != "" {
		run.Report["idempotencyKey"] = key
	}
	stored, err := r.CreateInspectionRun(ctx, run)
	if err != nil {
		return stored, err
	}
	err = dbtx.DB(ctx, r.db).Exec(`UPDATE ai_inspection_tasks SET last_run_at = ? WHERE id = ?`, now, task.ID).Error
	return stored, err
}

func (r *Repository) QueueManualInspectionRun(ctx context.Context, owner, taskID, key string, revision int64) (domain.InspectionRun, error) {
	var run domain.InspectionRun
	err := dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		task, err := scanInspectionTaskRow(db.Raw(`SELECT `+inspectionTaskColumns+` FROM ai_inspection_tasks WHERE id = ? AND created_by = ? FOR UPDATE`, taskID, owner).Row(), taskID)
		if err != nil {
			return err
		}
		if key == "" || revision < 1 {
			return apperrors.ErrInvalidArgument
		}
		var existing string
		if err := db.Raw(`SELECT id FROM ai_inspection_runs WHERE task_id = ? AND report->>'idempotencyKey' = ?`, taskID, key).Scan(&existing).Error; err != nil {
			return err
		}
		if existing != "" {
			run, err = r.inspectionRun(ctx, existing)
			if err == nil && fmt.Sprint(run.Report["registrationRevision"]) != fmt.Sprint(revision) {
				return apperrors.ErrConflict
			}
			return err
		}
		if task.Revision != revision {
			return apperrors.ErrConflict
		}
		var active bool
		if err := db.Raw(`SELECT soha_inspection_has_active(?)`, task.ID).Scan(&active).Error; err != nil {
			return err
		}
		if active {
			return fmt.Errorf("%w: an inspection is already active", apperrors.ErrConflict)
		}
		run, err = r.queueInspectionRun(ctx, task, "manual", key, time.Now().UTC())
		return err
	})
	return run, err
}

func (r *Repository) PendingInspectionRunIDs(ctx context.Context, limit int) ([]string, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	ids := []string{}
	err := dbtx.DB(ctx, r.db).Raw(`SELECT id FROM ai_inspection_runs WHERE status = 'queued' ORDER BY created_at,id LIMIT ?`, limit).Scan(&ids).Error
	return ids, err
}

func (r *Repository) inspectionRun(ctx context.Context, id string) (domain.InspectionRun, error) {
	var run domain.InspectionRun
	var findings, report []byte
	err := dbtx.DB(ctx, r.db).Raw(`SELECT `+inspectionRunColumns+` FROM ai_inspection_runs WHERE id = ?`, id).Row().Scan(&run.ID, &run.TaskID, &run.TriggeredBy, &run.Status, &run.Severity, &run.Summary, &findings, &report, &run.StartedAt, &run.CompletedAt, &run.CreatedAt)
	if err != nil {
		return run, err
	}
	if err = json.Unmarshal(report, &run.Report); err != nil {
		return run, err
	}
	if err = json.Unmarshal(findings, &run.Findings); err != nil {
		return run, err
	}
	return run, nil
}

// Registration -> receipt is the lock order also used by the alert trigger.
// The callback may only read evidence or persist a Workflow handoff via dbtx;
// provider writes remain in the existing domain workers after commit.
func (r *Repository) WithInspectionRun(ctx context.Context, id string, apply func(context.Context, domain.InspectionTask, domain.InspectionRun) (domain.InspectionRun, error)) (domain.InspectionRun, error) {
	var run domain.InspectionRun
	err := dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		db := dbtx.DB(ctx, r.db)
		task, err := scanInspectionTaskRow(db.Raw(`SELECT `+inspectionTaskColumns+` FROM ai_inspection_tasks
            WHERE id = (SELECT task_id FROM ai_inspection_runs WHERE id = ?) FOR UPDATE`, id).Row(), id)
		if err != nil {
			return err
		}
		var lockedID string
		if err = db.Raw(`SELECT id FROM ai_inspection_runs WHERE id = ? FOR UPDATE`, id).Scan(&lockedID).Error; err != nil {
			return err
		}
		run, err = r.inspectionRun(ctx, id)
		if err != nil || run.Status != "queued" {
			return err
		}
		reason, err := r.inspectionInvalidReason(ctx, task, run)
		if err != nil {
			return err
		}
		if reason != "" {
			run.Status, run.Summary = "blocked", reason
		} else {
			candidate := run
			// Roll back the entire handoff, including a Workflow created just
			// before its final visibility check failed.
			err = dbtx.Within(ctx, r.db, func(ctx context.Context) error {
				var applyErr error
				candidate, applyErr = apply(ctx, task, run)
				return applyErr
			})
			if err != nil {
				if !inspectionRefused(err) {
					return err
				}
				run.Status, run.Summary = "blocked", "Current identity, permissions, or registered plan no longer allow this inspection"
			} else {
				run = candidate
			}
		}
		now := time.Now().UTC()
		run.CompletedAt = &now
		findings, err := json.Marshal(run.Findings)
		if err != nil {
			return err
		}
		report, err := json.Marshal(run.Report)
		if err != nil {
			return err
		}
		return db.Exec(`UPDATE ai_inspection_runs SET status=?,severity=?,summary=?,findings=?::json,report=?::json,completed_at=? WHERE id=?`, run.Status, run.Severity, run.Summary, string(findings), string(report), now, id).Error
	})
	return run, err
}

func (r *Repository) inspectionInvalidReason(ctx context.Context, task domain.InspectionTask, run domain.InspectionRun) (string, error) {
	if fmt.Sprint(run.Report["registrationRevision"]) != fmt.Sprint(task.Revision) {
		return "Inspection registration changed before handoff", nil
	}
	if !task.Enabled && run.TriggeredBy != "manual" {
		return "Inspection registration disabled before handoff", nil
	}
	if run.TriggeredBy != "alert" {
		return "", nil
	}
	if task.Trigger == nil || task.Trigger.Kind != "alert" {
		return "Alert registration unavailable", nil
	}
	maxAge := task.Trigger.MaxEventAgeSeconds
	if maxAge == 0 {
		maxAge = 3600
	}
	var current bool
	err := dbtx.DB(ctx, r.db).Raw(`SELECT EXISTS(SELECT 1 FROM alert_events WHERE id = ? AND inspection_occurrence = ?
        AND source_type = 'internal_rule' AND rule_id = ? AND status = 'firing' AND COALESCE(current_state,'') <> 'acknowledged'
        AND inspection_occurrence_at >= NOW() - make_interval(secs => ?) AND inspection_occurrence_at <= NOW()
        AND (? = '' OR cluster_id = ?) AND (? = '' OR namespace = ?))`, run.Report["eventId"], run.Report["eventOccurrence"], task.Trigger.AlertRuleID, maxAge, task.ClusterID, task.ClusterID, task.Namespace, task.Namespace).Scan(&current).Error
	if err != nil {
		return "", err
	}
	if !current {
		return "Alert occurrence resolved, acknowledged, replaced, or expired before handoff", nil
	}
	return "", nil
}

func inspectionRefused(err error) bool {
	return errors.Is(err, apperrors.ErrUnauthorized) || errors.Is(err, apperrors.ErrAccessDenied) || errors.Is(err, apperrors.ErrInvalidArgument) || errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, apperrors.ErrConflict)
}
