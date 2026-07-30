package manifest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func (r *Repository) CreateDeliveryIntent(ctx context.Context, item domainmanifest.DeliveryIntent) (domainmanifest.DeliveryIntent, error) {
	files, _ := json.Marshal(item.Files)
	evidence, _ := json.Marshal(item.EvidenceRefs)
	validation, _ := json.Marshal(item.Validation)
	err := r.db.WithContext(ctx).Exec(`
		INSERT INTO manifest_delivery_intents (
			id, package_id, binding_id, status, files, provider, model,
			prompt_template_version, request_id, evidence_digest, evidence_refs,
			proposal_digest, rationale, risk, validation, created_by, created_at, updated_at
		) VALUES (?, ?, NULLIF(?, ''), ?, ?::jsonb, ?, ?, ?, NULLIF(?, ''), ?, ?::jsonb, ?, ?, ?, ?::jsonb, ?, ?, ?)
	`, item.ID, item.PackageID, item.BindingID, item.Status, string(files), item.Provider,
		item.Model, item.PromptTemplateVersion, item.RequestID, item.EvidenceDigest,
		string(evidence), item.ProposalDigest, item.Rationale, item.Risk,
		string(validation), item.CreatedBy, item.CreatedAt, item.UpdatedAt).Error
	if err != nil {
		return domainmanifest.DeliveryIntent{}, fmt.Errorf("create manifest delivery intent: %w", err)
	}
	return r.GetDeliveryIntent(ctx, item.ID)
}

func (r *Repository) ListDeliveryIntents(ctx context.Context, packageID string) ([]domainmanifest.DeliveryIntent, error) {
	rows, err := r.db.WithContext(ctx).Raw(deliveryIntentSelect+` WHERE package_id=? ORDER BY created_at DESC`, strings.TrimSpace(packageID)).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]domainmanifest.DeliveryIntent, 0)
	for rows.Next() {
		item, err := scanDeliveryIntent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetDeliveryIntent(ctx context.Context, id string) (domainmanifest.DeliveryIntent, error) {
	item, err := scanDeliveryIntent(r.db.WithContext(ctx).Raw(deliveryIntentSelect+` WHERE id=? LIMIT 1`, strings.TrimSpace(id)).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return domainmanifest.DeliveryIntent{}, apperrors.ErrNotFound
	}
	return item, err
}

func (r *Repository) DecideDeliveryIntent(ctx context.Context, item domainmanifest.DeliveryIntent, input domainmanifest.DeliveryIntentDecisionInput) (domainmanifest.DeliveryIntent, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var status string
		if err := tx.Raw(`SELECT status FROM manifest_delivery_intents WHERE id=? FOR UPDATE`, item.ID).Row().Scan(&status); err != nil {
			return err
		}
		if status != domainmanifest.IntentStatusDraft {
			return apperrors.ErrConflict
		}
		var currentRevision int
		var packageUpdatedAt time.Time
		var sourceMode string
		if err := tx.Raw(`
			SELECT package.current_revision, package.updated_at, source.mode
			FROM manifest_packages package
			JOIN manifest_sources source ON source.package_id=package.id
			WHERE package.id=? AND package.archived_at IS NULL
			FOR UPDATE OF package, source
		`, item.PackageID).Row().Scan(&currentRevision, &packageUpdatedAt, &sourceMode); err != nil {
			return err
		}
		if currentRevision != input.ExpectedCurrentRevision || !packageUpdatedAt.Equal(input.ExpectedPackageUpdatedAt) {
			return apperrors.ErrConflict
		}
		if item.Status == domainmanifest.IntentStatusAccepted {
			if sourceMode != domainmanifest.SourceModeSohaManaged {
				return apperrors.ErrConflict
			}
			files, _ := json.Marshal(item.Files)
			result := tx.Exec(`UPDATE manifest_packages SET files=?::jsonb, status='draft', updated_by=?, updated_at=? WHERE id=? AND current_revision=? AND updated_at=?`, string(files), item.DecidedBy, item.DecidedAt, item.PackageID, currentRevision, packageUpdatedAt)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return apperrors.ErrConflict
			}
		}
		result := tx.Exec(`UPDATE manifest_delivery_intents SET status=?, decision_comment=?, decided_by=?, decided_at=?, updated_at=? WHERE id=? AND status='draft'`, item.Status, item.DecisionComment, item.DecidedBy, item.DecidedAt, item.UpdatedAt, item.ID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return apperrors.ErrConflict
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, apperrors.ErrConflict) {
			return domainmanifest.DeliveryIntent{}, fmt.Errorf("%w: manifest delivery intent or package changed", apperrors.ErrConflict)
		}
		return domainmanifest.DeliveryIntent{}, err
	}
	return r.GetDeliveryIntent(ctx, item.ID)
}

const deliveryIntentSelect = `
	SELECT id, package_id, COALESCE(binding_id, ''), status, files, provider, model,
		prompt_template_version, COALESCE(request_id, ''), evidence_digest, evidence_refs,
		proposal_digest, rationale, risk, validation, COALESCE(decision_comment, ''),
		created_by, COALESCE(decided_by, ''), decided_at, created_at, updated_at
	FROM manifest_delivery_intents
`

func scanDeliveryIntent(source scanner) (domainmanifest.DeliveryIntent, error) {
	var item domainmanifest.DeliveryIntent
	var files, evidence, validation []byte
	if err := source.Scan(&item.ID, &item.PackageID, &item.BindingID, &item.Status,
		&files, &item.Provider, &item.Model, &item.PromptTemplateVersion, &item.RequestID,
		&item.EvidenceDigest, &evidence, &item.ProposalDigest, &item.Rationale, &item.Risk,
		&validation, &item.DecisionComment, &item.CreatedBy, &item.DecidedBy,
		&item.DecidedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domainmanifest.DeliveryIntent{}, err
	}
	if err := json.Unmarshal(files, &item.Files); err != nil {
		return domainmanifest.DeliveryIntent{}, err
	}
	if err := json.Unmarshal(evidence, &item.EvidenceRefs); err != nil {
		return domainmanifest.DeliveryIntent{}, err
	}
	if err := json.Unmarshal(validation, &item.Validation); err != nil {
		return domainmanifest.DeliveryIntent{}, err
	}
	return item, nil
}
