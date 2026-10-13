package copilot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

const personalCommandColumns = `id,assistant_id,created_by,request_hash,task_id,lease,env_operation_id,state,version,output,output_digest,observed_at,operation_until,updated_at`

func scanPersonalCommand(row *sql.Row) (domain.PersonalCommandRecord, error) {
	var item domain.PersonalCommandRecord
	var lease []byte
	err := row.Scan(&item.ID, &item.AssistantID, &item.CreatedBy, &item.RequestHash, &item.TaskID, &lease, &item.EnvOperationID, &item.State, &item.Version, &item.Output, &item.OutputDigest, &item.ObservedAt, &item.OperationUntil, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, apperrors.ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(lease, &item.Lease)
	}
	return item, err
}

func (r *Repository) GetPersonalCommand(ctx context.Context, owner, id string) (domain.PersonalCommandRecord, error) {
	return scanPersonalCommand(r.db.WithContext(ctx).Raw(`SELECT `+personalCommandColumns+` FROM ai_personal_commands WHERE created_by=? AND id=?`, owner, id).Row())
}

// This autonomous transaction commits both the once-only command and the
// personal-environment reservation before any network dispatch.
func (r *Repository) ReservePersonalCommand(ctx context.Context, previous domain.PersonalEnvironmentRecord, candidate domain.PersonalCommandRecord) (environment domain.PersonalEnvironmentRecord, command domain.PersonalCommandRecord, fresh bool, err error) {
	if candidate.AssistantID != previous.AssistantID || candidate.CreatedBy != previous.CreatedBy || candidate.Lease.OwnerID != previous.AssistantID || candidate.TaskID == "" {
		return environment, command, false, apperrors.ErrInvalidArgument
	}
	if id, parseErr := uuid.Parse(candidate.ID); parseErr != nil || id.String() != candidate.ID {
		return environment, command, false, apperrors.ErrInvalidArgument
	}
	lease, err := json.Marshal(candidate.Lease)
	if err != nil {
		return environment, command, false, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := New(tx)
		insert := tx.Exec(`INSERT INTO ai_personal_commands
 (id,assistant_id,created_by,request_hash,task_id,lease,env_operation_id,state,operation_until)
 SELECT ?,assistant_id,created_by,?,?,lease,?,'dispatching',now()+interval '70 seconds'
 FROM ai_personal_environments WHERE assistant_id=? AND created_by=? AND version=?
 AND state IN ('ready','quota_exceeded') AND lease=CAST(? AS jsonb)
 ON CONFLICT (id) DO NOTHING`, candidate.ID, candidate.RequestHash, candidate.TaskID, candidate.EnvOperationID, previous.AssistantID, previous.CreatedBy, previous.Version, string(lease))
		if insert.Error != nil {
			return insert.Error
		}
		command, err = repo.GetPersonalCommand(ctx, candidate.CreatedBy, candidate.ID)
		if err != nil {
			if errors.Is(err, apperrors.ErrNotFound) {
				return apperrors.ErrConflict
			}
			return err
		}
		if command.RequestHash != candidate.RequestHash || command.AssistantID != candidate.AssistantID || command.TaskID != candidate.TaskID {
			return apperrors.ErrConflict
		}
		fresh = insert.RowsAffected == 1
		if !fresh {
			return nil
		}
		// Hold one native task for the whole personal sandbox, not just one RPC.
		// Another task cannot lose its temporary work to this task's cancellation.
		slot := tx.Exec(`UPDATE ai_personal_environments e SET active_task_id=?
 WHERE assistant_id=? AND created_by=? AND version=?
 AND (active_task_id='' OR active_task_id=?)
 AND NOT EXISTS (SELECT 1 FROM ai_personal_commands c WHERE c.assistant_id=e.assistant_id
 AND c.created_by=e.created_by AND c.lease->>'id'=e.lease->>'id' AND c.id<>?
 AND c.state IN ('dispatching','unknown','cancelling','cancellation_unknown'))`, candidate.TaskID, previous.AssistantID, previous.CreatedBy, previous.Version, candidate.TaskID, candidate.ID)
		if slot.Error != nil {
			return slot.Error
		}
		if slot.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		environment, err = repo.ReservePersonalEnvironment(ctx, previous, candidate.Lease, candidate.EnvOperationID, false)
		return err
	})
	return environment, command, fresh && err == nil, err
}

// Cancellation may fence this command's still-active reservation, never another
// lease or an unrelated environment operation. The late command reply then loses
// both its environment fence and its command version.
func (r *Repository) ReservePersonalCommandCancellation(ctx context.Context, previous domain.PersonalEnvironmentRecord, current domain.PersonalCommandRecord, operationID string) (environment domain.PersonalEnvironmentRecord, command domain.PersonalCommandRecord, err error) {
	if current.AssistantID != previous.AssistantID || current.CreatedBy != previous.CreatedBy {
		return environment, command, apperrors.ErrAccessDenied
	}
	if id, parseErr := uuid.Parse(operationID); parseErr != nil || id == uuid.Nil {
		return environment, command, apperrors.ErrInvalidArgument
	}
	lease, err := json.Marshal(current.Lease)
	if err != nil {
		return environment, command, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := tx.Raw(`UPDATE ai_personal_environments SET state='pending',version=version+1,
 operation_id=?,operation_until=now()+interval '70 seconds',updated_at=now()
 WHERE assistant_id=? AND created_by=? AND version=? AND lease=CAST(? AS jsonb) AND active_task_id=?
 AND (operation_id=? OR operation_until IS NULL OR operation_until<=now())
 RETURNING `+personalEnvironmentColumns, operationID, previous.AssistantID, previous.CreatedBy, previous.Version, string(lease), current.TaskID, current.EnvOperationID).Row()
		environment, err = scanPersonalEnvironment(row)
		if err != nil {
			return err
		}
		row = tx.Raw(`UPDATE ai_personal_commands SET state='cancelling',version=version+1,
 env_operation_id=?,operation_until=?,updated_at=now()
 WHERE id=? AND created_by=? AND assistant_id=? AND version=?
 AND state IN ('dispatching','unknown','cancelling','cancellation_unknown')
 RETURNING `+personalCommandColumns, operationID, environment.OperationUntil, current.ID, current.CreatedBy, current.AssistantID, current.Version).Row()
		command, err = scanPersonalCommand(row)
		return err
	})
	if errors.Is(err, apperrors.ErrNotFound) {
		err = apperrors.ErrConflict
	}
	return environment, command, err
}

func (r *Repository) CompletePersonalCommand(ctx context.Context, previous, reserved domain.PersonalEnvironmentRecord, command domain.PersonalCommandRecord, notDispatched bool) (completed domain.PersonalCommandRecord, err error) {
	if len(command.Output) > 64<<10 || command.CreatedBy != reserved.CreatedBy || command.AssistantID != reserved.AssistantID || command.EnvOperationID != reserved.OperationID {
		return completed, apperrors.ErrInvalidArgument
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := New(tx)
		if notDispatched {
			_, err = repo.AbortPersonalEnvironment(ctx, reserved, previous)
		} else {
			_, err = repo.CompletePersonalEnvironment(ctx, reserved)
		}
		if err != nil {
			return err
		}
		row := tx.Raw(`UPDATE ai_personal_commands SET state=?,version=version+1,output=?,output_digest=?,observed_at=?,updated_at=now()
 WHERE id=? AND created_by=? AND assistant_id=? AND version=? AND env_operation_id=?
 AND state IN ('dispatching','cancelling')
 RETURNING `+personalCommandColumns, command.State, command.Output, command.OutputDigest, command.ObservedAt, command.ID, command.CreatedBy, command.AssistantID, command.Version, command.EnvOperationID).Row()
		completed, err = scanPersonalCommand(row)
		return err
	})
	if errors.Is(err, apperrors.ErrNotFound) {
		err = apperrors.ErrConflict
	}
	return completed, err
}
