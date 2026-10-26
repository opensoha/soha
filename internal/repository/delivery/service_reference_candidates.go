package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

func (r *Repository) ListServiceReferenceCandidates(ctx context.Context, consumerID, applicationID, cursor string, limit int) (domaindelivery.ServiceReferenceCandidatePage, error) {
	page := domaindelivery.ServiceReferenceCandidatePage{Items: make([]domaindelivery.ServiceReferenceCandidateSummary, 0)}
	if limit < 1 || limit > 100 {
		return page, fmt.Errorf("%w: invalid candidate page limit", apperrors.ErrInvalidArgument)
	}
	rows, err := dbtx.DB(ctx, r.db).Raw(`SELECT id, service_id, revision, created_at, updated_at
 FROM environment_service_reference_candidates WHERE consumer_binding_id = ? AND application_id = ? AND service_id > ?
 ORDER BY service_id LIMIT ?`, consumerID, applicationID, cursor, limit+1).Rows()
	if err != nil {
		return page, fmt.Errorf("list consumer candidate metadata: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var item domaindelivery.ServiceReferenceCandidateSummary
		if err := rows.Scan(&item.ID, &item.ServiceID, &item.Revision, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return page, err
		}
		item.State = "candidate"
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextCursor = page.Items[len(page.Items)-1].ServiceID
	}
	return page, nil
}

func (r *Repository) GetServiceReferenceCandidate(ctx context.Context, consumerID, serviceID string) (domaindelivery.ServiceReferenceCandidate, error) {
	var item domaindelivery.ServiceReferenceCandidate
	var raw []byte
	err := dbtx.DB(ctx, r.db).Raw(`SELECT id, revision, preview, source_identity_revision, context_revision, created_at, updated_at
 FROM environment_service_reference_candidates WHERE consumer_binding_id = ? AND service_id = ?`, consumerID, serviceID).
		Row().Scan(&item.ID, &item.Revision, &raw, &item.SourceIdentityRevision, &item.ContextRevision, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	if err != nil {
		return item, fmt.Errorf("read service reference candidate: %w", err)
	}
	if err := json.Unmarshal(raw, &item.Preview); err != nil {
		return item, fmt.Errorf("decode service reference candidate: %w", err)
	}
	item.State, item.Status = "candidate", "current"
	item.CurrentSourceRevision = item.Preview.SourceRevision
	return item, nil
}

func (r *Repository) SaveServiceReferenceCandidate(ctx context.Context, input domaindelivery.ServiceReferenceCandidateWrite, revalidate func(context.Context) (domaindelivery.ServiceReferenceConfiguration, error)) (domaindelivery.ServiceReferenceCandidate, error) {
	var saved domaindelivery.ServiceReferenceCandidate
	if revalidate == nil || input.ExpectedRevision < 0 || input.Candidate.ContextRevision == "" {
		return saved, fmt.Errorf("%w: candidate context and reauthorization are required", apperrors.ErrInvalidArgument)
	}
	err := dbtx.Within(ctx, r.db, func(txctx context.Context) error {
		if err := r.lockReferenceCandidateConfiguration(txctx, input.Candidate.Preview); err != nil {
			return err
		}
		configuration, err := revalidate(txctx)
		if err != nil {
			return err
		}
		revision, err := configuration.Revision()
		if err != nil {
			return fmt.Errorf("encode locked candidate context: %w", err)
		}
		if revision != input.Candidate.ContextRevision {
			return fmt.Errorf("%w: candidate source or consumer changed; preview again", apperrors.ErrConflict)
		}
		saved, err = r.writeServiceReferenceCandidate(txctx, input)
		return err
	})
	if err != nil {
		return domaindelivery.ServiceReferenceCandidate{}, err
	}
	return saved, nil
}

func (r *Repository) lockReferenceCandidateConfiguration(ctx context.Context, preview domaindelivery.ServiceReferencePreview) error {
	tx := dbtx.DB(ctx, r.db)
	if err := lockCandidateRow(tx, `SELECT id FROM applications WHERE id = ? FOR KEY SHARE`, preview.ApplicationID); err != nil {
		return err
	}
	if err := lockCandidateRow(tx, `SELECT id FROM application_services WHERE id = ? AND application_id = ? AND enabled = true FOR SHARE`, preview.ServiceID, preview.ApplicationID); err != nil {
		return err
	}
	bindings := []string{preview.Consumer.ApplicationEnvironmentID, preview.Source.ApplicationEnvironmentID}
	if bindings[0] == bindings[1] || bindings[0] == "" || bindings[1] == "" {
		return fmt.Errorf("%w: candidate requires distinct explicit bindings", apperrors.ErrInvalidArgument)
	}
	slices.Sort(bindings)
	environments := make([]string, 0, 2)
	for _, id := range bindings {
		if err := lockImportBinding(tx, id); err != nil {
			return err
		}
		var environmentID string
		if err := tx.Raw(`SELECT environment_id FROM application_environments WHERE id = ?`, id).Row().Scan(&environmentID); err != nil {
			return fmt.Errorf("read candidate environment: %w", err)
		}
		environments = append(environments, environmentID)
	}
	slices.Sort(environments)
	for _, id := range slices.Compact(environments) {
		if err := lockCandidateRow(tx, `SELECT id FROM delivery_environments WHERE id = ? FOR SHARE`, id); err != nil {
			return err
		}
	}
	// Also fence cascaded target deletion that does not update the parent binding.
	if err := tx.Exec(`SELECT id FROM release_targets WHERE application_environment_id IN (?, ?) ORDER BY id FOR SHARE`, bindings[0], bindings[1]).Error; err != nil {
		return fmt.Errorf("lock candidate targets: %w", err)
	}
	return nil
}

func lockCandidateRow(tx *gorm.DB, query string, args ...any) error {
	var id string
	err := tx.Raw(query, args...).Row().Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: candidate configuration is no longer available", apperrors.ErrConflict)
	}
	if err != nil {
		return fmt.Errorf("lock candidate configuration: %w", err)
	}
	return nil
}

func (r *Repository) writeServiceReferenceCandidate(ctx context.Context, input domaindelivery.ServiceReferenceCandidateWrite) (domaindelivery.ServiceReferenceCandidate, error) {
	item := input.Candidate
	consumerID, serviceID := item.Preview.Consumer.ApplicationEnvironmentID, item.Preview.ServiceID
	previous, err := r.GetServiceReferenceCandidate(ctx, consumerID, serviceID)
	create := errors.Is(err, ErrNotFound)
	if err != nil && !create {
		return item, err
	}
	if (create && (input.ExpectedRevision != 0 || input.ExpectedCandidateID != "")) || (!create && (input.ExpectedCandidateID != previous.ID || input.ExpectedRevision != previous.Revision)) {
		return item, fmt.Errorf("%w: candidate identity/revision changed; reload before saving", apperrors.ErrConflict)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	item.ID, item.Revision, item.CreatedAt = uuid.NewString(), 1, now
	if !create {
		item.ID, item.Revision, item.CreatedAt = previous.ID, previous.Revision+1, previous.CreatedAt
	}
	item.UpdatedAt, item.State, item.Status = now, "candidate", "current"
	item.CurrentSourceRevision = item.Preview.SourceRevision
	raw, err := json.Marshal(item.Preview)
	if err != nil {
		return item, fmt.Errorf("encode candidate preview: %w", err)
	}
	tx := dbtx.DB(ctx, r.db)
	if create {
		err = tx.Exec(`INSERT INTO environment_service_reference_candidates
 (id, application_id, consumer_binding_id, service_id, revision, preview, source_identity_revision, context_revision, created_at, updated_at)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, item.Preview.ApplicationID, consumerID, serviceID, item.Revision, string(raw), item.SourceIdentityRevision, item.ContextRevision, item.CreatedAt, item.UpdatedAt).Error
	} else {
		result := tx.Exec(`UPDATE environment_service_reference_candidates SET revision = ?, preview = ?, source_identity_revision = ?, context_revision = ?, updated_at = ? WHERE id = ? AND revision = ?`,
			item.Revision, string(raw), item.SourceIdentityRevision, item.ContextRevision, item.UpdatedAt, item.ID, input.ExpectedRevision)
		err = result.Error
		if err == nil && result.RowsAffected != 1 {
			err = fmt.Errorf("%w: candidate changed during save", apperrors.ErrConflict)
		}
	}
	if err != nil {
		return domaindelivery.ServiceReferenceCandidate{}, fmt.Errorf("save inactive reference candidate: %w", err)
	}
	return item, nil
}

func (r *Repository) DeleteServiceReferenceCandidate(ctx context.Context, consumerID, serviceID string, input domaindelivery.ServiceReferenceCandidateDeleteInput, authorize func(context.Context) error) error {
	if authorize == nil {
		return fmt.Errorf("%w: candidate cleanup authorization is required", apperrors.ErrInvalidArgument)
	}
	return dbtx.Within(ctx, r.db, func(txctx context.Context) error {
		tx := dbtx.DB(txctx, r.db)
		if err := lockImportBinding(tx, consumerID); err != nil {
			return err
		}
		var updatedAt time.Time
		if err := tx.Raw(`SELECT updated_at FROM application_environments WHERE id = ?`, consumerID).Row().Scan(&updatedAt); err != nil {
			return err
		}
		if !updatedAt.Equal(input.ExpectedConsumerUpdatedAt) {
			return fmt.Errorf("%w: consumer configuration changed", apperrors.ErrConflict)
		}
		if err := authorize(txctx); err != nil {
			return err
		}
		result := tx.Exec(`DELETE FROM environment_service_reference_candidates WHERE consumer_binding_id = ? AND service_id = ? AND id = ? AND revision = ?`,
			consumerID, serviceID, input.ExpectedCandidateID, input.ExpectedRevision)
		if result.Error != nil {
			return fmt.Errorf("delete reference candidate: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: candidate identity/revision changed", apperrors.ErrConflict)
		}
		return nil
	})
}
