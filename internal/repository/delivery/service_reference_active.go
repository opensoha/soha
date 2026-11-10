package delivery

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"github.com/opensoha/soha/internal/repository/servicereferencelock"
)

const activeReferenceSelect = `SELECT id,application_id,consumer_binding_id,service_id,source_binding_id,revision,preview,source_identity_revision,control_revision,consumer_configuration_revision,created_at,updated_at FROM environment_service_references `

type referenceScanner interface{ Scan(...any) error }

func scanActiveReference(row referenceScanner) (domaindelivery.ServiceReferenceRecord, error) {
	var item domaindelivery.ServiceReferenceRecord
	var raw []byte
	ref := &item.Reference
	err := row.Scan(&ref.ID, &item.ApplicationID, &item.ConsumerBindingID, &item.ServiceID, &item.SourceBindingID, &ref.Revision, &raw, &ref.SourceIdentityRevision, &item.ControlRevision, &item.ConsumerConfigurationRevision, &ref.CreatedAt, &ref.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	if err != nil {
		return item, fmt.Errorf("read active service reference: %w", err)
	}
	if err = json.Unmarshal(raw, &ref.Preview); err != nil {
		return item, fmt.Errorf("decode active service reference: %w", err)
	}
	ref.State = "active"
	ref.CurrentSourceRevision = ref.Preview.SourceRevision
	item.SourceTargetIDs = make([]string, 0, len(ref.Preview.Targets))
	for _, target := range ref.Preview.Targets {
		item.SourceTargetIDs = append(item.SourceTargetIDs, target.TargetID)
	}
	return item, nil
}

func (r *Repository) GetServiceReference(ctx context.Context, consumerID, serviceID string) (domaindelivery.ServiceReferenceRecord, error) {
	return scanActiveReference(dbtx.DB(ctx, r.db).Raw(activeReferenceSelect+`WHERE consumer_binding_id=? AND service_id=?`, consumerID, serviceID).Row())
}

// This complete bounded set is private; public consumer filtering happens after authorization.
func (r *Repository) ListSourceServiceReferences(ctx context.Context, sourceID, serviceID string) ([]domaindelivery.ServiceReferenceRecord, error) {
	rows, err := dbtx.DB(ctx, r.db).Raw(activeReferenceSelect+`WHERE source_binding_id=? AND (?='' OR service_id=?) ORDER BY id LIMIT 1001`, sourceID, serviceID, serviceID).Rows()
	if err != nil {
		return nil, fmt.Errorf("list source service references: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]domaindelivery.ServiceReferenceRecord, 0)
	for rows.Next() {
		item, err := scanActiveReference(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(items) > 1000 {
		return nil, fmt.Errorf("%w: source consumer set exceeds 1000", apperrors.ErrUnsupportedOperation)
	}
	if err := r.enrichReferenceConsumerConfigurations(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) enrichReferenceConsumerConfigurations(ctx context.Context, items []domaindelivery.ServiceReferenceRecord) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]string, len(items))
	positions := make(map[string]int, len(items))
	for i, item := range items {
		ids[i] = item.Reference.ID
		positions[item.Reference.ID] = i
	}
	rows, err := dbtx.DB(ctx, r.db).Raw(`SELECT r.id,jsonb_build_object('binding',to_jsonb(b)-'created_at'-'updated_at','environment',to_jsonb(e)-'created_at'-'updated_at','service',to_jsonb(s)-'created_at'-'updated_at'), e.id,e.environment_key,COALESCE(e.tier,''),e.enabled,e.is_production,e.stage_level,e.requires_approval FROM environment_service_references r JOIN application_environments b ON b.id=r.consumer_binding_id JOIN delivery_environments e ON e.id=b.environment_id JOIN application_services s ON s.id=r.service_id AND s.application_id=r.application_id WHERE r.id IN ? ORDER BY r.id`, ids).Rows()
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	seen := 0
	for rows.Next() {
		var id string
		var raw []byte
		var environment domaincatalog.Environment
		if err := rows.Scan(&id, &raw, &environment.ID, &environment.Key, &environment.Tier, &environment.Enabled, &environment.IsProduction, &environment.StageLevel, &environment.RequiresApproval); err != nil {
			return err
		}
		digest := sha256.Sum256(raw)
		items[positions[id]].ConsumerConfigurationRevision = hex.EncodeToString(digest[:])
		items[positions[id]].ConsumerEnvironment = environment
		seen++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if seen != len(items) {
		return fmt.Errorf("%w: consumer configuration is no longer available", apperrors.ErrConflict)
	}
	return nil
}

func (r *Repository) WithServiceReferenceScope(ctx context.Context, applicationID string, apply func(context.Context) error) error {
	if applicationID == "" || apply == nil {
		return fmt.Errorf("%w: explicit application scope is required", apperrors.ErrInvalidArgument)
	}
	err := dbtx.Within(ctx, r.db, func(txctx context.Context) error {
		if err := servicereferencelock.Applications(dbtx.DB(txctx, r.db), applicationID); err != nil {
			return err
		}
		return apply(txctx)
	})
	return referenceConstraintError(err)
}

func referenceConstraintError(err error) error {
	var conflict *pgconn.PgError
	if errors.As(err, &conflict) {
		switch conflict.Code {
		case "23001", "23503", "23505", "23514", "40001", "40P01":
			return fmt.Errorf("%w: service reference configuration changed or is in use", apperrors.ErrConflict)
		}
	}
	return err
}

func (r *Repository) SaveServiceReference(ctx context.Context, input domaindelivery.ServiceReferenceWrite, revalidate func(context.Context) (domaindelivery.ServiceReferenceConfiguration, error)) (domaindelivery.ServiceReferenceRecord, error) {
	var saved domaindelivery.ServiceReferenceRecord
	if revalidate == nil || input.ExpectedRevision < 0 || input.ConfigurationRevision == "" {
		return saved, fmt.Errorf("%w: reference context and reauthorization required", apperrors.ErrInvalidArgument)
	}
	err := r.WithServiceReferenceScope(ctx, input.Record.ApplicationID, func(txctx context.Context) error {
		if err := r.lockReferenceCandidateConfiguration(txctx, input.Record.Reference.Preview); err != nil {
			return err
		}
		current, err := revalidate(txctx)
		if err != nil {
			return err
		}
		revision, err := current.Revision()
		if err != nil {
			return err
		}
		if revision != input.ConfigurationRevision {
			return fmt.Errorf("%w: source or consumer configuration changed", apperrors.ErrConflict)
		}
		candidate, err := r.GetServiceReferenceCandidate(txctx, input.Record.ConsumerBindingID, input.Record.ServiceID)
		if err != nil {
			return err
		}
		if candidate.ID != input.ExpectedCandidateID || candidate.Revision != input.ExpectedCandidateRevision || candidate.ContextRevision != revision || candidate.SourceIdentityRevision != input.Record.Reference.SourceIdentityRevision || candidate.Preview.SourceRevision != input.Record.Reference.Preview.SourceRevision {
			return fmt.Errorf("%w: saved candidate changed; review and save again", apperrors.ErrConflict)
		}
		saved, err = r.writeActiveReference(txctx, input)
		return err
	})
	return saved, err
}

func (r *Repository) writeActiveReference(ctx context.Context, input domaindelivery.ServiceReferenceWrite) (domaindelivery.ServiceReferenceRecord, error) {
	item := input.Record
	old, err := r.GetServiceReference(ctx, item.ConsumerBindingID, item.ServiceID)
	create := errors.Is(err, ErrNotFound)
	if err != nil && !create {
		return item, err
	}
	if (create && (input.ExpectedRevision != 0 || input.ExpectedReferenceID != "")) || (!create && (old.Reference.ID != input.ExpectedReferenceID || old.Reference.Revision != input.ExpectedRevision)) {
		return item, fmt.Errorf("%w: active reference changed", apperrors.ErrConflict)
	}
	now := time.Now().UTC()
	ref := &item.Reference
	ref.State = "active"
	ref.UpdatedAt = now
	if create {
		ref.ID = uuid.NewString()
		ref.Revision = 1
		ref.CreatedAt = now
	} else {
		ref.ID = old.Reference.ID
		ref.Revision = old.Reference.Revision + 1
		ref.CreatedAt = old.Reference.CreatedAt
	}
	raw, err := json.Marshal(ref.Preview)
	if err != nil {
		return item, err
	}
	tx := dbtx.DB(ctx, r.db)
	if create {
		err = tx.Exec(`INSERT INTO environment_service_references(id,application_id,consumer_binding_id,service_id,source_binding_id,revision,preview,source_identity_revision,control_revision,consumer_configuration_revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?::jsonb,?,?,?,?,?)`, ref.ID, item.ApplicationID, item.ConsumerBindingID, item.ServiceID, item.SourceBindingID, ref.Revision, string(raw), ref.SourceIdentityRevision, item.ControlRevision, item.ConsumerConfigurationRevision, ref.CreatedAt, ref.UpdatedAt).Error
	} else {
		result := tx.Exec(`UPDATE environment_service_references SET source_binding_id=?,revision=?,preview=?::jsonb,source_identity_revision=?,control_revision=?,consumer_configuration_revision=?,updated_at=? WHERE id=? AND revision=?`, item.SourceBindingID, ref.Revision, string(raw), ref.SourceIdentityRevision, item.ControlRevision, item.ConsumerConfigurationRevision, now, ref.ID, input.ExpectedRevision)
		err = result.Error
		if err == nil && result.RowsAffected != 1 {
			err = apperrors.ErrConflict
		}
	}
	if err != nil {
		return domaindelivery.ServiceReferenceRecord{}, referenceConstraintError(err)
	}
	return r.GetServiceReference(ctx, item.ConsumerBindingID, item.ServiceID)
}

func (r *Repository) DeleteServiceReference(ctx context.Context, consumerID, serviceID string, input domaindelivery.ServiceReferenceDeleteInput, authorize func(context.Context) error) error {
	if authorize == nil {
		return fmt.Errorf("%w: consumer authorization required", apperrors.ErrInvalidArgument)
	}
	item, err := r.GetServiceReference(ctx, consumerID, serviceID)
	if err != nil {
		return err
	}
	return r.WithServiceReferenceScope(ctx, item.ApplicationID, func(txctx context.Context) error {
		if err := lockImportBinding(dbtx.DB(txctx, r.db), consumerID); err != nil {
			return err
		}
		var updatedAt time.Time
		if err := dbtx.DB(txctx, r.db).Raw(`SELECT updated_at FROM application_environments WHERE id=?`, consumerID).Row().Scan(&updatedAt); err != nil {
			return err
		}
		if !updatedAt.Equal(input.ExpectedConsumerUpdatedAt) {
			return fmt.Errorf("%w: consumer configuration changed", apperrors.ErrConflict)
		}
		if err := authorize(txctx); err != nil {
			return err
		}
		result := dbtx.DB(txctx, r.db).Exec(`DELETE FROM environment_service_references WHERE consumer_binding_id=? AND service_id=? AND id=? AND revision=?`, consumerID, serviceID, input.ExpectedReferenceID, input.ExpectedRevision)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: active reference changed", apperrors.ErrConflict)
		}
		return nil
	})
}

func (r *Repository) ListServiceReferences(ctx context.Context, consumerID, applicationID, cursor string, limit int) (domaindelivery.ServiceReferencePage, error) {
	page := domaindelivery.ServiceReferencePage{Items: make([]domaindelivery.ServiceReferenceSummary, 0)}
	if limit < 1 || limit > 100 {
		return page, fmt.Errorf("%w: invalid reference page limit", apperrors.ErrInvalidArgument)
	}
	rows, err := dbtx.DB(ctx, r.db).Raw(`SELECT id,service_id,revision,created_at,updated_at FROM environment_service_references WHERE consumer_binding_id=? AND application_id=? AND service_id>? ORDER BY service_id LIMIT ?`, consumerID, applicationID, cursor, limit+1).Rows()
	if err != nil {
		return page, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var item domaindelivery.ServiceReferenceSummary
		if err := rows.Scan(&item.ID, &item.ServiceID, &item.Revision, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return page, err
		}
		item.State = "active"
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
