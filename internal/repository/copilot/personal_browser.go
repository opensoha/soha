package copilot

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

// The owning environment row is locked by the caller before changing control.
func (r *Repository) ReservePersonalBrowser(ctx context.Context, env domain.PersonalEnvironmentRecord, requested guest.BrowserAction) (guest.BrowserAction, error) {
	tx := dbtx.DB(ctx, r.db)
	var current domain.PersonalBrowserControl
	err := tx.Raw(`SELECT assistant_id,owner_id,control_id,generation,owner_mode,task_id,instance_id,expires_at FROM ai_personal_browser_controls WHERE assistant_id=? AND owner_id=? FOR UPDATE`, env.AssistantID, env.CreatedBy).Row().Scan(&current.AssistantID, &current.OwnerID, &current.ID, &current.Generation, &current.Owner, &current.TaskID, &current.InstanceID, &current.ExpiresAt)
	found := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return requested, err
	}
	now := time.Now().UTC()
	requested, err = nextPersonalBrowserControl(current, found, requested, now)
	if err != nil {
		return requested, err
	}
	expires := now.Add(5 * time.Minute)
	if env.Lease.ExpiresAt.Before(expires) {
		expires = env.Lease.ExpiresAt
	}
	owner := requested.Owner
	if requested.Action == "release" {
		owner = "released"
	}
	if err := tx.Exec(`INSERT INTO ai_personal_browser_controls(assistant_id,owner_id,control_id,generation,owner_mode,task_id,expires_at)
 VALUES(?,?,?,?,?,?,?) ON CONFLICT(assistant_id) DO UPDATE SET control_id=excluded.control_id,generation=excluded.generation,owner_mode=excluded.owner_mode,task_id=excluded.task_id,expires_at=excluded.expires_at,updated_at=now() WHERE ai_personal_browser_controls.owner_id=excluded.owner_id`, env.AssistantID, env.CreatedBy, requested.ControlID, requested.Generation, owner, requested.TaskID, expires).Error; err != nil {
		return requested, err
	}
	return requested, nil
}

func (r *Repository) CheckPersonalBrowserOperation(ctx context.Context, op guest.RuntimeOperation) error {
	if op.Browser == nil {
		return nil
	}
	b := op.Browser
	owner := b.Owner
	if b.Action == "release" {
		owner = "released"
	}
	var valid bool
	err := dbtx.DB(ctx, r.db).Raw(`SELECT EXISTS(SELECT 1 FROM ai_personal_browser_controls WHERE assistant_id=? AND owner_id=? AND control_id=? AND generation=? AND owner_mode=? AND task_id=? AND expires_at>now())`, op.Binding.AssistantID, op.Binding.OwnerID, b.ControlID, b.Generation, owner, b.TaskID).Row().Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return apperrors.ErrAccessDenied
	}
	return nil
}

func nextPersonalBrowserControl(current domain.PersonalBrowserControl, found bool, requested guest.BrowserAction, now time.Time) (guest.BrowserAction, error) {
	if requested.Action == "acquire" {
		if found && (requested.ControlID != current.ID || requested.Generation != current.Generation || current.Owner == "agent" && now.Before(current.ExpiresAt)) {
			return requested, apperrors.ErrConflict
		}
		id := current.ID
		if !found {
			id = uuid.NewString()
		}
		requested.ControlID, requested.Generation = id, current.Generation+1
	} else {
		if !found || requested.ControlID != current.ID || requested.Generation != current.Generation || !now.Before(current.ExpiresAt) {
			return requested, apperrors.ErrConflict
		}
		if requested.Action == "takeover" || requested.Action == "return" {
			if requested.Action == "return" && (requested.Owner != "agent" || requested.TaskID == "") {
				return requested, apperrors.ErrConflict
			}
			requested.Generation++
		} else if current.Owner != requested.Owner {
			return requested, apperrors.ErrAccessDenied
		}
	}
	if requested.Generation > guest.MaxCounter {
		return requested, apperrors.ErrConflict
	}
	return requested, nil
}
