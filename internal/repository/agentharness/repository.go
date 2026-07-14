package agentharness

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	appagentharness "github.com/opensoha/soha/internal/application/agentharness"
	"gorm.io/gorm"
)

const providerCatalogStateID = "current"

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) LoadProviderState(ctx context.Context, acknowledgementLimit int) (appagentharness.ProviderPersistedState, error) {
	state := appagentharness.ProviderPersistedState{Acknowledgements: []appagentharness.RegistryAcknowledgement{}}
	var catalogJSON []byte
	err := r.db.WithContext(ctx).Raw(`SELECT catalog FROM ai_agent_provider_catalog_state WHERE id=?`, providerCatalogStateID).Row().Scan(&catalogJSON)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return state, fmt.Errorf("load agent provider catalog: %w", err)
	}
	if err == nil {
		var catalog appagentharness.ProviderCatalog
		if err := json.Unmarshal(catalogJSON, &catalog); err != nil {
			return state, fmt.Errorf("decode agent provider catalog: %w", err)
		}
		state.Catalog = &catalog
	}
	if acknowledgementLimit <= 0 {
		return state, nil
	}
	rows, err := r.db.WithContext(ctx).Raw(`SELECT acknowledgement FROM ai_agent_provider_registry_acks ORDER BY observed_at DESC, runner_id ASC LIMIT ?`, acknowledgementLimit).Rows()
	if err != nil {
		return state, fmt.Errorf("load agent provider acknowledgements: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return state, fmt.Errorf("scan agent provider acknowledgement: %w", err)
		}
		var acknowledgement appagentharness.RegistryAcknowledgement
		if err := json.Unmarshal(raw, &acknowledgement); err != nil {
			return state, fmt.Errorf("decode agent provider acknowledgement: %w", err)
		}
		state.Acknowledgements = append(state.Acknowledgements, acknowledgement)
	}
	if err := rows.Err(); err != nil {
		return state, fmt.Errorf("iterate agent provider acknowledgements: %w", err)
	}
	return state, nil
}

func (r *Repository) SaveProviderCatalog(ctx context.Context, catalog appagentharness.ProviderCatalog) error {
	raw, err := json.Marshal(catalog)
	if err != nil {
		return fmt.Errorf("encode agent provider catalog: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_agent_provider_catalog_state(id,revision,digest,catalog,created_at,updated_at)
		VALUES(?,?,?,?,?,CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET
			revision=EXCLUDED.revision,digest=EXCLUDED.digest,catalog=EXCLUDED.catalog,
			created_at=EXCLUDED.created_at,updated_at=CURRENT_TIMESTAMP
		WHERE EXCLUDED.revision > ai_agent_provider_catalog_state.revision
		   OR (EXCLUDED.revision = ai_agent_provider_catalog_state.revision
		       AND EXCLUDED.digest = ai_agent_provider_catalog_state.digest)`,
		providerCatalogStateID, catalog.Revision, catalog.Digest, raw, catalog.CreatedAt)
	if result.Error != nil {
		return fmt.Errorf("save agent provider catalog: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("save agent provider catalog: persisted revision is newer or has a conflicting digest")
	}
	return nil
}

func (r *Repository) SaveRegistryAcknowledgement(ctx context.Context, acknowledgement appagentharness.RegistryAcknowledgement, limit int) error {
	raw, err := json.Marshal(acknowledgement)
	if err != nil {
		return fmt.Errorf("encode agent provider acknowledgement: %w", err)
	}
	if limit <= 0 {
		return fmt.Errorf("agent provider acknowledgement limit must be positive")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`
			INSERT INTO ai_agent_provider_registry_acks(runner_id,revision,active_revision,accepted,reason,acknowledgement,observed_at,updated_at)
			VALUES(?,?,?,?,?,?,?,CURRENT_TIMESTAMP)
			ON CONFLICT (runner_id) DO UPDATE SET
				revision=EXCLUDED.revision,active_revision=EXCLUDED.active_revision,
				accepted=EXCLUDED.accepted,reason=EXCLUDED.reason,
				acknowledgement=EXCLUDED.acknowledgement,observed_at=EXCLUDED.observed_at,
				updated_at=CURRENT_TIMESTAMP
			WHERE EXCLUDED.revision > ai_agent_provider_registry_acks.revision
			   OR (EXCLUDED.revision = ai_agent_provider_registry_acks.revision
			       AND EXCLUDED.observed_at > ai_agent_provider_registry_acks.observed_at)`,
			acknowledgement.RunnerID, acknowledgement.Revision, acknowledgement.ActiveRevision,
			acknowledgement.Accepted, acknowledgement.Reason, raw, acknowledgement.ObservedAt)
		if result.Error != nil {
			return fmt.Errorf("upsert agent provider acknowledgement: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("upsert agent provider acknowledgement: persisted acknowledgement is newer")
		}
		if err := tx.Exec(`DELETE FROM ai_agent_provider_registry_acks WHERE runner_id IN (
			SELECT runner_id FROM ai_agent_provider_registry_acks
			ORDER BY observed_at DESC, runner_id ASC OFFSET ?
		)`, limit).Error; err != nil {
			return fmt.Errorf("prune agent provider acknowledgements: %w", err)
		}
		return nil
	})
}
