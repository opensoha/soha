package virtualization

import (
	"context"
	"fmt"
	"slices"
	"time"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

type capacityPolicyRow struct {
	SourceConfirmed bool
	SourceID        string
	Revision        int
	Mode            string
	CPUEnabled      bool
	CPUPercent      int
	MemoryEnabled   bool
	MemoryPercent   int
	UpdatedBy       string
	UpdatedAt       time.Time
}

func (row capacityPolicyRow) policy() domain.CapacityPolicy {
	return domain.CapacityPolicy{SourceConfirmed: row.SourceConfirmed, SourceID: row.SourceID, Revision: row.Revision, Mode: row.Mode, CPU: domain.OvercommitPolicy{Enabled: row.CPUEnabled, Percent: row.CPUPercent}, Memory: domain.OvercommitPolicy{Enabled: row.MemoryEnabled, Percent: row.MemoryPercent}, UpdatedBy: row.UpdatedBy, UpdatedAt: &row.UpdatedAt}
}

func (r *Repository) GetCapacityPolicy(ctx context.Context, connectionID, fallback string) (domain.CapacityPolicy, error) {
	var row capacityPolicyRow
	err := dbtx.DB(ctx, r.db).Raw(`SELECT p.* FROM virtualization_capacity_policies p JOIN virtualization_capacity_sources s ON s.source_id=p.source_id WHERE s.connection_id=?`, connectionID).Scan(&row).Error
	if err != nil {
		return domain.CapacityPolicy{}, err
	}
	if row.SourceID == "" {
		return domain.DefaultCapacityPolicy(fallback), nil
	}
	return row.policy(), nil
}

func (r *Repository) SaveCapacityPolicy(ctx context.Context, connectionID, previousSource string, policy domain.CapacityPolicy, expected int) (domain.CapacityPolicy, error) {
	var saved domain.CapacityPolicy
	err := dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "virtualization-capacity-connection/"+connectionID).Error; err != nil {
			return err
		}
		locks := []string{previousSource, policy.SourceID}
		slices.Sort(locks)
		for _, source := range slices.Compact(locks) {
			if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "virtualization-capacity/"+source).Error; err != nil {
				return err
			}
		}
		var existingSource string
		if err := tx.Raw(`SELECT source_id FROM virtualization_capacity_sources WHERE connection_id=? FOR UPDATE`, connectionID).Scan(&existingSource).Error; err != nil {
			return err
		}
		previous, err := capacityPolicyRecord(tx, existingSource)
		if err != nil {
			return err
		}
		if existingSource != "" && (previous.Revision != expected || previous.SourceConfirmed && existingSource != policy.SourceID) {
			return fmt.Errorf("%w: confirmed source or policy revision changed", apperrors.ErrConflict)
		}
		if existingSource == "" && expected != 0 {
			return fmt.Errorf("%w: initial policy revision must be zero", apperrors.ErrConflict)
		}
		target, err := capacityPolicyRecord(tx, policy.SourceID)
		if err != nil {
			return err
		}
		// Joining another confirmed source inherits its policy; editing it requires its returned revision.
		inherit := target.SourceConfirmed && existingSource != policy.SourceID
		if !inherit {
			if err := writeCapacityPolicy(tx, policy, target.Revision+1); err != nil {
				return err
			}
		}
		if err := bindCapacitySource(tx, connectionID, previousSource, policy.SourceID); err != nil {
			return err
		}
		saved, err = (&Repository{db: tx}).GetCapacityPolicy(ctx, connectionID, previousSource)
		return err
	})
	return saved, err
}

func capacityPolicyRecord(tx *gorm.DB, source string) (capacityPolicyRow, error) {
	var row capacityPolicyRow
	err := tx.Raw(`SELECT * FROM virtualization_capacity_policies WHERE source_id=? FOR UPDATE`, source).Scan(&row).Error
	return row, err
}

func writeCapacityPolicy(tx *gorm.DB, policy domain.CapacityPolicy, revision int) error {
	return tx.Exec(`INSERT INTO virtualization_capacity_policies(source_id,source_confirmed,revision,mode,cpu_enabled,cpu_percent,memory_enabled,memory_percent,updated_by) VALUES(?,TRUE,?,?,?,?,?,?,?) ON CONFLICT(source_id) DO UPDATE SET source_confirmed=TRUE,revision=EXCLUDED.revision,mode=EXCLUDED.mode,cpu_enabled=EXCLUDED.cpu_enabled,cpu_percent=EXCLUDED.cpu_percent,memory_enabled=EXCLUDED.memory_enabled,memory_percent=EXCLUDED.memory_percent,updated_by=EXCLUDED.updated_by,updated_at=now()`, policy.SourceID, revision, policy.Mode, policy.CPU.Enabled, policy.CPU.Percent, policy.Memory.Enabled, policy.Memory.Percent, policy.UpdatedBy).Error
}

func bindCapacitySource(tx *gorm.DB, connectionID, previous, source string) error {
	if previous != source {
		if err := tx.Exec(`UPDATE virtualization_capacity_reservations SET source_id=? WHERE source_id=?`, source, previous).Error; err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE virtualization_tasks SET payload=jsonb_set(payload,'{providerSourceId}',to_jsonb(?::text)) WHERE provider='pve' AND payload->>'providerSourceId'=?`, source, previous).Error; err != nil {
			return err
		}
	}
	return tx.Exec(`INSERT INTO virtualization_capacity_sources(connection_id,source_id)
 SELECT c.id,? FROM virtualization_connections c WHERE c.provider='pve' AND
 (c.id=? OR CASE WHEN NULLIF(trim(c.config->>'capacitySourceId'),'') IS NOT NULL THEN 'pve/'||trim(c.config->>'capacitySourceId') ELSE 'pve/'||lower(rtrim(c.endpoint,'/')) END=?)
 AND NOT EXISTS(SELECT 1 FROM virtualization_capacity_sources s WHERE s.connection_id=c.id AND s.source_id NOT IN (?,?))
 ON CONFLICT(connection_id) DO UPDATE SET source_id=EXCLUDED.source_id`, source, connectionID, previous, previous, source).Error
}

func initializeCapacityPolicy(tx *gorm.DB, connection domain.Connection) error {
	var source string
	// A new connection to the same confirmed endpoint is an alias of that source.
	if err := tx.Raw(`SELECT s.source_id FROM virtualization_capacity_sources s JOIN virtualization_connections c ON c.id=s.connection_id JOIN virtualization_capacity_policies p ON p.source_id=s.source_id WHERE c.provider='pve' AND lower(rtrim(c.endpoint,'/'))=lower(rtrim(?,'/')) AND p.source_confirmed=TRUE LIMIT 1`, connection.Endpoint).Scan(&source).Error; err != nil {
		return err
	}
	if source == "" {
		source = "pve/unconfirmed-" + connection.ID
		if err := tx.Exec(`INSERT INTO virtualization_capacity_policies(source_id,source_confirmed,revision,mode,updated_by) VALUES(?,FALSE,1,'enforced','system')`, source).Error; err != nil {
			return err
		}
	}
	return tx.Exec(`INSERT INTO virtualization_capacity_sources(connection_id,source_id) VALUES(?,?)`, connection.ID, source).Error
}

func checkCapacityPolicyRevision(tx *gorm.DB, snapshot domain.CapacitySnapshot) error {
	if snapshot.ConnectionID == "" && snapshot.Policy.Revision == 0 {
		return nil
	}
	var row capacityPolicyRow
	if err := tx.Raw(`SELECT p.source_id,p.revision FROM virtualization_capacity_policies p JOIN virtualization_capacity_sources s ON s.source_id=p.source_id WHERE s.connection_id=?`, snapshot.ConnectionID).Scan(&row).Error; err != nil {
		return err
	}
	if row.Revision != snapshot.Policy.Revision || row.Revision > 0 && row.SourceID != snapshot.SourceID {
		return fmt.Errorf("%w: capacity policy changed; repeat admission", apperrors.ErrConflict)
	}
	return nil
}
