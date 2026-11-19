package delivery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

func (r *Repository) WithExecutionCallback(ctx context.Context, token string, apply func(context.Context, domaindelivery.ExecutionTask) error) error {
	return dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		tx := dbtx.DB(ctx, r.db)
		var id, kind string
		err := tx.Raw(`SELECT id, task_kind FROM execution_tasks WHERE callback_token = ? LIMIT 1`, token).Row().Scan(&id, &kind)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("look up callback task: %w", err)
		}
		// SSH sessions acquire the advisory lock before touching the task row.
		if kind == "vm_ssh_command" {
			if err := r.lockSSHExecutionTask(ctx, id); err != nil {
				return err
			}
		}
		task, err := r.getExecutionTaskByCallbackToken(ctx, token, true)
		if err != nil {
			return err
		}
		return apply(ctx, task)
	})
}

func (r *Repository) lockSSHExecutionTask(ctx context.Context, id string) error {
	return dbtx.DB(ctx, r.db).Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "vm-ssh-command/"+id).Error
}

func (r *Repository) FindExecutionCallbackArtifact(ctx context.Context, item domaindelivery.ExecutionArtifact) (domaindelivery.ExecutionArtifact, error) {
	query := artifactSelectColumns + " WHERE execution_task_id = ?"
	args := []any{item.ExecutionTaskID}
	if item.ID != "" {
		query += " AND id = ?"
		args = append(args, item.ID)
	} else {
		query += " AND artifact_kind = ? AND COALESCE(name,'') = ? AND COALESCE(ref,'') = ? AND COALESCE(digest,'') = ? AND COALESCE(path,'') = ?"
		args = append(args, item.Kind, item.Name, item.Ref, item.Digest, item.Path)
	}
	query += " ORDER BY updated_at DESC, id LIMIT 1 FOR UPDATE"
	stored, err := scanExecutionArtifact(dbtx.DB(ctx, r.db).Raw(query, args...).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return domaindelivery.ExecutionArtifact{}, ErrNotFound
	}
	return stored, err
}
