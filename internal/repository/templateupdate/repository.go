package templateupdate

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"github.com/opensoha/soha/internal/repository/servicereferencelock"
	repoworkflow "github.com/opensoha/soha/internal/repository/workflow"
	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) SavePreview(ctx context.Context, preview sohaapi.TemplateUpdatePreview, candidates []domainupdate.FrozenCandidate) error {
	return dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		body, err := json.Marshal(preview)
		if err != nil {
			return err
		}
		if err := dbtx.DB(ctx, r.db).Exec(`INSERT INTO template_update_previews(id,snapshot,expires_at,created_at) VALUES(?,?::jsonb,?,?)`, preview.ID, string(body), preview.ExpiresAt, preview.CreatedAt).Error; err != nil {
			return err
		}
		for _, candidate := range candidates {
			body, err := json.Marshal(candidate)
			if err != nil {
				return err
			}
			if err := dbtx.DB(ctx, r.db).Exec(`INSERT INTO template_update_candidates(id,preview_id,target_id,frozen) VALUES(?,?,?,?::jsonb)`, candidate.Candidate.ID, preview.ID, candidate.Candidate.Target.ID, string(body)).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repository) GetPreview(ctx context.Context, id string) (sohaapi.TemplateUpdatePreview, error) {
	var body []byte
	var preview sohaapi.TemplateUpdatePreview
	err := dbtx.DB(ctx, r.db).Raw(`SELECT snapshot FROM template_update_previews WHERE id=?`, id).Row().Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return preview, apperrors.ErrNotFound
	}
	if err != nil {
		return preview, err
	}
	err = json.Unmarshal(body, &preview)
	return preview, err
}

func (r *Repository) GetCandidate(ctx context.Context, id string) (domainupdate.FrozenCandidate, error) {
	var body []byte
	var candidate domainupdate.FrozenCandidate
	var planID string
	err := dbtx.DB(ctx, r.db).Raw(`SELECT frozen,applied_plan_id FROM template_update_candidates WHERE id=?`, id).Row().Scan(&body, &planID)
	if errors.Is(err, sql.ErrNoRows) {
		return candidate, apperrors.ErrNotFound
	}
	if err != nil {
		return candidate, err
	}
	err = json.Unmarshal(body, &candidate)
	candidate.AppliedPlanID = planID
	return candidate, err
}

func (r *Repository) ApplyCandidate(ctx context.Context, id, planID string, apply func(context.Context, domainupdate.FrozenCandidate) error) error {
	return dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		tx := dbtx.DB(ctx, r.db)
		discovered, err := lockCandidateApplications(tx, id)
		if err != nil {
			return err
		}
		var body []byte
		var appliedPlanID string
		if err := tx.Raw(`SELECT frozen,applied_plan_id FROM template_update_candidates WHERE id=? FOR UPDATE`, id).Row().Scan(&body, &appliedPlanID); err != nil {
			return err
		}
		if !bytes.Equal(discovered, body) {
			return fmt.Errorf("%w: template adoption candidate changed", apperrors.ErrConflict)
		}
		if appliedPlanID != "" {
			if appliedPlanID == planID {
				return nil
			}
			return fmt.Errorf("%w: candidate was adopted by another Plan", apperrors.ErrConflict)
		}
		var frozen domainupdate.FrozenCandidate
		if err := json.Unmarshal(body, &frozen); err != nil {
			return err
		}
		if err := repoworkflow.LockDeliveryNode(ctx, tx, frozen.Candidate.Target.ApplicationID, frozen.Candidate.Target.ApplicationEnvironmentID); err != nil {
			return err
		}
		if err := apply(ctx, frozen); err != nil {
			return err
		}
		return tx.Exec(`UPDATE template_update_candidates SET applied_plan_id=? WHERE id=? AND applied_plan_id=''`, planID, id).Error
	})
}

func lockCandidateApplications(tx *gorm.DB, id string) ([]byte, error) {
	var discovered []byte
	err := servicereferencelock.Resolve(tx, func() ([]string, error) {
		var raw []byte
		var frozen domainupdate.FrozenCandidate
		if err := tx.Raw(`SELECT frozen FROM template_update_candidates WHERE id=?`, id).Row().Scan(&raw); err != nil {
			return nil, err
		}
		if discovered != nil && !bytes.Equal(discovered, raw) {
			return nil, fmt.Errorf("%w: template adoption candidate changed", apperrors.ErrConflict)
		}
		discovered = raw
		if err := json.Unmarshal(raw, &frozen); err != nil {
			return nil, err
		}
		var applications []string
		err := tx.Raw(`
			SELECT application_id FROM application_environments WHERE id = ?
			UNION
			SELECT e.application_id FROM application_environments e
			JOIN manifest_bindings b ON b.application_environment_id = e.id WHERE b.id = ?`,
			frozen.Candidate.Target.ApplicationEnvironmentID, frozen.Binding.ManifestBinding.ID).Scan(&applications).Error
		return append(applications, frozen.Candidate.Target.ApplicationID), err
	})
	return discovered, err
}

type scanner interface{ Scan(...any) error }

const policySelect = `SELECT snapshot,application_id,subject_id,token_id,version,attempt_version,attempt_preview_id FROM template_update_policies`

func scanPolicy(row scanner) (domainupdate.Policy, error) {
	var item domainupdate.Policy
	var body []byte
	err := row.Scan(&body, &item.ApplicationID, &item.SubjectID, &item.TokenID, &item.Value.Version, &item.AttemptVersion, &item.AttemptPreviewID)
	if errors.Is(err, sql.ErrNoRows) {
		return item, apperrors.ErrNotFound
	}
	if err != nil {
		return item, err
	}
	version := item.Value.Version
	err = json.Unmarshal(body, &item.Value)
	item.Value.Version = version
	return item, err
}

func (r *Repository) ListPolicies(ctx context.Context, f domainupdate.Filter) ([]domainupdate.Policy, error) {
	rows, err := dbtx.DB(ctx, r.db).Raw(policySelect+` WHERE kind=? AND template_id=? AND (?='' OR application_id=?) ORDER BY id LIMIT 2001`, f.Kind, f.TemplateID, f.ApplicationID, f.ApplicationID).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []domainupdate.Policy{}
	for rows.Next() {
		item, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if len(items) > 2000 {
		return nil, fmt.Errorf("%w: too many adoption policies", apperrors.ErrInvalidArgument)
	}
	return items, rows.Err()
}

func (r *Repository) SavePolicy(ctx context.Context, item domainupdate.Policy, expected int64) (domainupdate.Policy, error) {
	item.Value.Version = expected + 1
	body, err := json.Marshal(item.Value)
	if err != nil {
		return item, err
	}
	var result *gorm.DB
	if expected == 0 {
		result = dbtx.DB(ctx, r.db).Exec(`INSERT INTO template_update_policies(id,kind,template_id,target_id,application_id,subject_id,token_id,snapshot) VALUES(?,?,?,?,?,?,?,?::jsonb) ON CONFLICT DO NOTHING`, item.Value.ID, item.Value.Kind, item.Value.TemplateID, item.Value.TargetID, item.ApplicationID, item.SubjectID, item.TokenID, string(body))
	} else {
		result = dbtx.DB(ctx, r.db).Exec(`UPDATE template_update_policies SET subject_id=?,token_id=?,snapshot=jsonb_set(?::jsonb,'{lastBatchId}',COALESCE(snapshot->'lastBatchId','""'::jsonb)),version=version+1,attempt_version=0,attempt_preview_id='',next_check_at=now() WHERE kind=? AND template_id=? AND target_id=? AND version=?`, item.SubjectID, item.TokenID, string(body), item.Value.Kind, item.Value.TemplateID, item.Value.TargetID, expected)
	}
	if result.Error != nil {
		return item, result.Error
	}
	if result.RowsAffected != 1 {
		return item, apperrors.ErrConflict
	}
	return scanPolicy(dbtx.DB(ctx, r.db).Raw(policySelect+` WHERE kind=? AND template_id=? AND target_id=?`, item.Value.Kind, item.Value.TemplateID, item.Value.TargetID).Row())
}

func (r *Repository) ListDuePolicies(ctx context.Context, limit int) ([]domainupdate.Policy, error) {
	rows, err := dbtx.DB(ctx, r.db).Raw(policySelect+` WHERE next_check_at<=now() AND snapshot->>'mode' IN ('automatic','approval') AND snapshot->>'paused'='false' ORDER BY next_check_at,id LIMIT ?`, min(max(limit, 1), 32)).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []domainupdate.Policy{}
	for rows.Next() {
		item, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) WithPolicy(ctx context.Context, id string, expected int64, apply func(context.Context, *domainupdate.Policy) error) error {
	return dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		item, err := scanPolicy(dbtx.DB(ctx, r.db).Raw(policySelect+` WHERE id=? AND version=? FOR UPDATE SKIP LOCKED`, id, expected).Row())
		if errors.Is(err, apperrors.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := apply(ctx, &item); err != nil {
			return err
		}
		body, err := json.Marshal(item.Value)
		if err != nil {
			return err
		}
		return dbtx.DB(ctx, r.db).Exec(`UPDATE template_update_policies SET snapshot=?::jsonb,attempt_version=?,attempt_preview_id=?,next_check_at=now()+interval '30 seconds' WHERE id=? AND version=?`, string(body), item.AttemptVersion, item.AttemptPreviewID, id, expected).Error
	})
}
