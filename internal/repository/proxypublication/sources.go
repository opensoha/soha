package proxypublication

import (
	"context"
	"encoding/json"
	"errors"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

type sourceRow struct {
	InstanceID         string `gorm:"primaryKey"`
	SourceID           string `gorm:"primaryKey"`
	Revision           int64
	Public             []byte `gorm:"type:jsonb"`
	EncryptedInput     string
	EncryptedIR        string
	NodeMaterial       []byte `gorm:"type:jsonb"`
	DeletedAt          *time.Time
	CompatibilityFloor int64
}

func (sourceRow) TableName() string { return "network_proxy_sources" }
func policyLock(tx *gorm.DB) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "network-access-policy-snapshot:default:default").Error
}
func decodeSource(row sourceRow) (domain.SourceRecord, error) {
	r := domain.SourceRecord{InstanceID: row.InstanceID, EncryptedInput: row.EncryptedInput, EncryptedIR: row.EncryptedIR, Deleted: row.DeletedAt != nil, CompatibilityFloor: row.CompatibilityFloor}
	if json.Unmarshal(row.Public, &r.Public) != nil || json.Unmarshal(row.NodeMaterial, &r.NodeMaterial) != nil {
		return r, apperrors.ErrServiceUnavailable
	}
	return r, nil
}
func sourceByID(tx *gorm.DB, instance, id string, locked bool) (sourceRow, error) {
	var row sourceRow
	query := tx.Where("instance_id=? AND source_id=?", instance, id)
	if locked {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := query.Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, apperrors.ErrNotFound
	}
	return row, err
}
func (r *Repository) GetSource(ctx context.Context, instance, id string) (domain.SourceRecord, error) {
	row, err := sourceByID(r.db.WithContext(ctx), instance, id, false)
	if err != nil {
		return domain.SourceRecord{}, err
	}
	if row.DeletedAt != nil {
		return domain.SourceRecord{}, apperrors.ErrGone
	}
	return decodeSource(row)
}
func (r *Repository) ListSources(ctx context.Context, instance string) ([]domain.SourceRecord, error) {
	var rows []sourceRow
	if err := r.db.WithContext(ctx).Where("instance_id=? AND deleted_at IS NULL", instance).Order("source_id").Limit(256).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := []domain.SourceRecord{}
	for _, row := range rows {
		item, err := decodeSource(row)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}
func (r *Repository) SaveSource(ctx context.Context, record domain.SourceRecord, expected int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := policyLock(tx); err != nil {
			return err
		}
		if err := saveSource(tx, record, expected); err != nil {
			return err
		}
		return narrowSourceCredentials(tx, record.InstanceID, record.Public.SourceID)
	})
}
func saveSource(tx *gorm.DB, record domain.SourceRecord, expected int64) error {
	if len(record.NodeMaterial) == 0 || record.Public.Revision != expected+1 {
		return apperrors.ErrInvalidArgument
	}
	public, err := json.Marshal(record.Public)
	if err != nil {
		return err
	}
	material, err := json.Marshal(record.NodeMaterial)
	if err != nil {
		return err
	}
	if expected == 0 {
		result := tx.Exec("INSERT INTO network_proxy_sources(instance_id,source_id,revision,public,encrypted_input,encrypted_ir,node_material,compatibility_floor) VALUES(?,?,?,?::jsonb,?,?,?::jsonb,1) ON CONFLICT DO NOTHING", record.InstanceID, record.Public.SourceID, record.Public.Revision, string(public), record.EncryptedInput, record.EncryptedIR, string(material))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		return nil
	}
	old, err := sourceByID(tx, record.InstanceID, record.Public.SourceID, true)
	if err != nil {
		return err
	}
	if old.DeletedAt != nil || old.Revision != expected {
		return apperrors.ErrConflict
	}
	var before map[string]string
	if json.Unmarshal(old.NodeMaterial, &before) != nil {
		return apperrors.ErrServiceUnavailable
	}
	if err = insertInvalidations(tx, record.InstanceID, record.Public.SourceID, record.Public.Revision, domain.InvalidatedNodes(before, record.NodeMaterial)); err != nil {
		return err
	}
	result := tx.Model(&sourceRow{}).Where("instance_id=? AND source_id=? AND revision=? AND deleted_at IS NULL", record.InstanceID, record.Public.SourceID, expected).Updates(map[string]any{"revision": record.Public.Revision, "public": string(public), "encrypted_input": record.EncryptedInput, "encrypted_ir": record.EncryptedIR, "node_material": string(material)})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return apperrors.ErrConflict
	}
	return nil
}
func insertInvalidations(tx *gorm.DB, instance, source string, revision int64, nodes []string) error {
	for _, id := range nodes {
		if err := tx.Exec("INSERT INTO network_proxy_node_invalidations(instance_id,source_id,node_id,invalidated_source_revision) VALUES(?,?,?,?) ON CONFLICT DO NOTHING", instance, source, id, revision).Error; err != nil {
			return err
		}
	}
	return nil
}
func (r *Repository) DeleteSource(ctx context.Context, instance, id string, expected int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := policyLock(tx); err != nil {
			return err
		}
		old, err := sourceByID(tx, instance, id, true)
		if err != nil {
			return err
		}
		if old.DeletedAt != nil || old.Revision != expected {
			return apperrors.ErrConflict
		}
		item, err := decodeSource(old)
		if err != nil {
			return err
		}
		if err = insertInvalidations(tx, instance, id, expected+1, domain.InvalidatedNodes(item.NodeMaterial, map[string]string{})); err != nil {
			return err
		}
		item.Public.Revision = expected + 1
		item.Public.Configured = false
		item.Public.NodeCount = 0
		item.Public.UpdatedAt = time.Now().UTC()
		raw, _ := json.Marshal(item.Public)
		if err := tx.Exec("DELETE FROM network_proxy_employee_credentials c USING network_proxy_bundles b WHERE c.instance_id=b.instance_id AND c.bundle_id=b.bundle_id AND b.instance_id=? AND b.record->'Public'->>'sourceId'=?", instance, id).Error; err != nil {
			return err
		}
		if err := tx.Exec("UPDATE network_proxy_bundles SET version=version+1,record=jsonb_set(jsonb_set(record,'{EncryptedIR}','\"\"'::jsonb),'{Public,state}','\"revoked\"'::jsonb) WHERE instance_id=? AND record->'Public'->>'sourceId'=?", instance, id).Error; err != nil {
			return err
		}
		return tx.Model(&sourceRow{}).Where("instance_id=? AND source_id=? AND revision=?", instance, id, expected).Updates(map[string]any{"revision": expected + 1, "public": string(raw), "deleted_at": time.Now().UTC(), "encrypted_input": "", "encrypted_ir": "", "node_material": "{}"}).Error
	})
}
func (r *Repository) AllowedSourceNodes(ctx context.Context, instance, source string, revision int64, original []string) ([]string, error) {
	return allowedSourceNodes(r.db.WithContext(ctx), instance, source, revision, original)
}
func allowedSourceNodes(tx *gorm.DB, instance, source string, revision int64, original []string) ([]string, error) {
	row, err := sourceByID(tx, instance, source, false)
	if err != nil {
		return nil, err
	}
	if row.DeletedAt != nil || revision < row.CompatibilityFloor || revision > row.Revision || len(original) == 0 {
		return nil, apperrors.ErrAccessDenied
	}
	var current map[string]string
	if json.Unmarshal(row.NodeMaterial, &current) != nil {
		return nil, apperrors.ErrServiceUnavailable
	}
	var invalid []struct{ NodeID string }
	if err = tx.Table("network_proxy_node_invalidations").Select("node_id").Where("instance_id=? AND source_id=? AND node_id IN ? AND invalidated_source_revision>?", instance, source, original, revision).Find(&invalid).Error; err != nil {
		return nil, err
	}
	denied := map[string]bool{}
	for _, node := range invalid {
		denied[node.NodeID] = true
	}
	out := []string{}
	for _, node := range original {
		if _, present := current[node]; present && !denied[node] {
			out = append(out, node)
		}
	}
	return out, nil
}
