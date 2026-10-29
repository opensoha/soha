package contentdlp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/contentdlp"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
	"time"
)

const appealJSON = `CASE WHEN a.event_id IS NULL THEN NULL ELSE jsonb_build_object('eventId',a.event_id,'projectId',a.project_id,'actorId',a.actor_id,'reasonCode',a.reason_code,'status',a.status,'revision',a.revision,'reviewerId',COALESCE(a.reviewer_id,''),'reviewReason',COALESCE(a.review_reason,''),'createdAt',a.created_at,'updatedAt',a.updated_at) END`

func (r *Repository) Within(ctx context.Context, fn func(context.Context) error) error {
	return dbtx.Within(ctx, r.db, fn)
}
func (r *Repository) Event(ctx context.Context, project, id string) (domain.EventRecord, error) {
	var record domain.EventRecord
	var metadata, appeal []byte
	var expires, updated sql.NullTime
	err := dbtx.DB(ctx, r.db).Raw(`SELECT e.metadata,b.binding_key,b.binding_tag,b.expires_at,b.project_updated_at,`+appealJSON+` FROM docker_volume_dlp_events e LEFT JOIN docker_volume_dlp_bindings b ON b.event_id=e.id LEFT JOIN docker_volume_dlp_appeals a ON a.event_id=e.id WHERE e.project_id=? AND e.id=?`, project, id).Row().Scan(&metadata, &record.Binding.Key, &record.Binding.Tag, &expires, &updated, &appeal)
	if errors.Is(err, sql.ErrNoRows) {
		return record, apperrors.ErrNotFound
	}
	if err != nil {
		return record, err
	}
	if err := json.Unmarshal(metadata, &record.Event); err != nil {
		return record, err
	}
	record.Binding.ExpiresAt = expires.Time
	record.Binding.ProjectUpdatedAt = updated.Time
	if len(appeal) > 0 {
		record.Appeal = &domain.Appeal{}
		if err := json.Unmarshal(appeal, record.Appeal); err != nil {
			return record, err
		}
	}
	return record, nil
}
func (r *Repository) Proof(ctx context.Context, hash string) (*domain.Proof, error) {
	var event, project, kind string
	err := dbtx.DB(ctx, r.db).Raw(`SELECT event_id,project_id,kind FROM docker_volume_dlp_authorizations WHERE token_hash=? AND consumed_at IS NULL AND expires_at>CURRENT_TIMESTAMP`, hash).Row().Scan(&event, &project, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	record, err := r.Event(ctx, project, event)
	if err != nil {
		return nil, err
	}
	return &domain.Proof{TokenHash: hash, Kind: kind, Record: record}, nil
}
func workflowEvent(tx *gorm.DB, original domain.Event, actor, action string, revision int64, now time.Time) error {
	e := domain.Event{ID: uuid.NewString(), ProjectID: original.ProjectID, ActorID: actor, Action: action, Revision: revision, CreatedAt: now, Decision: domain.Decision{Mode: original.Mode, Status: original.Status, Allowed: false, PolicyVersion: original.PolicyVersion, DetectorVersion: domain.DetectorVersion, MatchedRuleIDs: []string{}, OriginEventID: original.ID}}
	e.EventID = e.ID
	return addEvent(tx, e)
}
func (r *Repository) IssueAuthorization(ctx context.Context, record domain.EventRecord, actor, kind, hash string, now time.Time) (time.Time, error) {
	expires := now.Add(time.Minute)
	err := r.Within(ctx, func(ctx context.Context) error {
		tx := dbtx.DB(ctx, r.db)
		updated, err := lockProject(tx, record.Event.ProjectID)
		if err != nil {
			return err
		}
		fresh, err := r.Event(ctx, record.Event.ProjectID, record.Event.ID)
		if err != nil {
			return err
		}
		p, err := (&Repository{db: tx}).Policy(ctx, record.Event.ProjectID)
		if err != nil {
			return err
		}
		if err := authorizationEligible(fresh, p, updated, actor, kind, now, expires); err != nil {
			return err
		}

		var exists bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM docker_volume_dlp_authorizations WHERE event_id=?)`, fresh.Event.ID).Scan(&exists).Error; err != nil {
			return err
		}
		if exists {
			return apperrors.ErrConflict
		}
		if err := tx.Exec(`INSERT INTO docker_volume_dlp_authorizations(event_id,actor_id,project_id,policy_version,token_hash,kind,expires_at) VALUES(?,?,?,?,?,?,?)`, fresh.Event.ID, actor, fresh.Event.ProjectID, fresh.Event.PolicyVersion, hash, kind, expires).Error; err != nil {
			return err
		}
		action := "warning_acknowledged"
		if kind == "approved_appeal" {
			action = "appeal_authorization_issued"
		}
		return workflowEvent(tx, fresh.Event, actor, action, fresh.Event.Revision, now)
	})
	return expires, err
}
func (r *Repository) SubmitAppeal(ctx context.Context, record domain.EventRecord, actor, reason string, now time.Time) (domain.Appeal, error) {
	var result domain.Appeal
	err := r.Within(ctx, func(ctx context.Context) error {
		tx := dbtx.DB(ctx, r.db)
		if _, err := lockProject(tx, record.Event.ProjectID); err != nil {
			return err
		}
		fresh, err := r.Event(ctx, record.Event.ProjectID, record.Event.ID)
		if err != nil {
			return err
		}
		if fresh.Event.ActorID != actor || fresh.Event.Allowed || fresh.Event.Status != "matched" || fresh.Event.Action != "transfer" {
			return apperrors.ErrAccessDenied
		}
		if fresh.Appeal != nil {
			if fresh.Appeal.ReasonCode != reason {
				return apperrors.ErrConflict
			}
			result = *fresh.Appeal
			return nil
		}
		var count int64
		if err := tx.Raw(`SELECT COUNT(*) FROM docker_volume_dlp_appeals WHERE project_id=? AND actor_id=? AND status='pending'`, fresh.Event.ProjectID, actor).Scan(&count).Error; err != nil {
			return err
		}
		if count >= 20 {
			return apperrors.ErrConflict
		}
		result = domain.Appeal{EventID: fresh.Event.ID, ProjectID: fresh.Event.ProjectID, ActorID: actor, ReasonCode: reason, Status: "pending", Revision: 1, CreatedAt: now, UpdatedAt: now}
		if err := tx.Exec(`INSERT INTO docker_volume_dlp_appeals(event_id,project_id,actor_id,reason_code,status,revision,created_at,updated_at) VALUES(?,?,?,?,'pending',1,?,?)`, result.EventID, result.ProjectID, actor, reason, now, now).Error; err != nil {
			return err
		}
		return workflowEvent(tx, fresh.Event, actor, "appeal_submitted", 1, now)
	})
	return result, err
}
func (r *Repository) ResolveAppeal(ctx context.Context, record domain.EventRecord, reviewer string, input domain.ReviewInput, projectUpdated time.Time, now time.Time) (domain.Appeal, error) {
	var result domain.Appeal
	err := r.Within(ctx, func(ctx context.Context) error {
		tx := dbtx.DB(ctx, r.db)
		updated, err := lockProject(tx, record.Event.ProjectID)
		if err != nil {
			return err
		}
		if !updated.Equal(projectUpdated) {
			return apperrors.ErrConflict
		}
		fresh, err := r.Event(ctx, record.Event.ProjectID, record.Event.ID)
		if err != nil {
			return err
		}
		if fresh.Event.ActorID == reviewer {
			return apperrors.ErrAccessDenied
		}
		if fresh.Appeal == nil || fresh.Appeal.Status != "pending" || fresh.Appeal.Revision != input.ExpectedRevision {
			return apperrors.ErrConflict
		}
		q := tx.Exec(`UPDATE docker_volume_dlp_appeals SET status=?,revision=revision+1,reviewer_id=?,review_reason=?,updated_at=? WHERE event_id=? AND revision=? AND status='pending'`, input.Resolution, reviewer, input.ReasonCode, now, fresh.Event.ID, input.ExpectedRevision)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		result = *fresh.Appeal
		result.Status = input.Resolution
		result.Revision++
		result.ReviewerID = reviewer
		result.ReviewReason = input.ReasonCode
		result.UpdatedAt = now
		return workflowEvent(tx, fresh.Event, reviewer, "appeal_reviewed", result.Revision, now)
	})
	return result, err
}
func (r *Repository) Cases(ctx context.Context, project, actor string) ([]domain.Case, error) {
	query := `SELECT e.metadata,` + appealJSON + ` FROM docker_volume_dlp_events e LEFT JOIN docker_volume_dlp_appeals a ON a.event_id=e.id WHERE e.project_id=?`
	args := []any{project}
	if actor != "" {
		query += ` AND e.actor_id=?`
		args = append(args, actor)
	}
	query += ` ORDER BY e.created_at DESC,e.id DESC LIMIT 100`
	rows, err := dbtx.DB(ctx, r.db).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.Case{}
	for rows.Next() {
		var meta, appeal []byte
		var c domain.Case
		if err := rows.Scan(&meta, &appeal); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(meta, &c.Event); err != nil {
			return nil, err
		}
		if len(appeal) > 0 {
			c.Appeal = &domain.Appeal{}
			if err := json.Unmarshal(appeal, c.Appeal); err != nil {
				return nil, err
			}
		}
		items = append(items, c)
	}
	return items, rows.Err()
}
func (r *Repository) CommitTransfer(ctx context.Context, event domain.Event, binding domain.Binding, proof *domain.Proof, projectUpdated time.Time) (domain.Decision, error) {
	d := event.Decision
	err := r.Within(ctx, func(ctx context.Context) error {
		tx := dbtx.DB(ctx, r.db)
		updated, err := lockProject(tx, event.ProjectID)
		if err != nil {
			return err
		}
		if !updated.Equal(projectUpdated) {
			return apperrors.ErrConflict
		}
		p, err := (&Repository{db: tx}).Policy(ctx, event.ProjectID)
		if err != nil {
			return err
		}
		if p.PublishedVersion != event.PolicyVersion {
			return apperrors.ErrConflict
		}
		if proof != nil && event.Status == "matched" {
			q := tx.Exec(`UPDATE docker_volume_dlp_authorizations SET consumed_at=clock_timestamp() WHERE token_hash=? AND event_id=? AND actor_id=? AND project_id=? AND policy_version=? AND kind=? AND consumed_at IS NULL AND expires_at>clock_timestamp()`, proof.TokenHash, proof.Record.Event.ID, event.ActorID, event.ProjectID, event.PolicyVersion, proof.Kind)
			if q.Error != nil {
				return q.Error
			}
			if q.RowsAffected == 1 {
				d.Allowed = true
				d.RequiredAction = ""
				d.AuthorizationKind = proof.Kind
				d.OriginEventID = proof.Record.Event.ID
			}
		}
		event.Decision = d
		if err := addEvent(tx, event); err != nil {
			return err
		}
		if !d.Allowed && d.Status == "matched" {
			return tx.Exec(`INSERT INTO docker_volume_dlp_bindings(event_id,binding_key,binding_tag,project_updated_at,expires_at) VALUES(?,?,?,?,?)`, event.ID, binding.Key, binding.Tag, projectUpdated, binding.ExpiresAt).Error
		}
		return nil
	})
	return d, err
}
func (r *Repository) AttachHold(ctx context.Context, eventID, holdID string) error {
	return dbtx.DB(ctx, r.db).Exec(`UPDATE docker_volume_dlp_events SET metadata=jsonb_set(metadata,'{networkHoldId}',to_jsonb(?::text)) WHERE id=?`, holdID, eventID).Error
}

func authorizationEligible(fresh domain.EventRecord, p domain.Policy, updated time.Time, actor, kind string, now, expires time.Time) error {
	if !updated.Equal(fresh.Binding.ProjectUpdatedAt) || p.PublishedVersion != fresh.Event.PolicyVersion || !fresh.Binding.ExpiresAt.After(expires) || fresh.Event.ActorID != actor || fresh.Event.Allowed || fresh.Event.Status != "matched" {
		return apperrors.ErrConflict
	}
	if kind == "acknowledge" {
		if fresh.Event.Mode != "warn" || fresh.Event.CreatedAt.Before(now.Add(-2*time.Minute)) {
			return apperrors.ErrConflict
		}
	} else if kind != "approved_appeal" || fresh.Appeal == nil || fresh.Appeal.Status != "approved" || fresh.Appeal.ReviewerID == actor {
		return apperrors.ErrConflict
	}
	return nil
}
