package networkaccess

import (
	"context"
	"encoding/json"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type vpnDocument struct {
	ID                string
	Revision          int
	PublishedRevision int
	Configuration     []byte
	Published         []byte
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

const vpnDocumentColumns = "id, revision, published_revision, configuration, published_configuration, created_at, updated_at"

func scanVPNDocument(row scanner) (vpnDocument, error) {
	var item vpnDocument
	err := row.Scan(&item.ID, &item.Revision, &item.PublishedRevision, &item.Configuration, &item.Published, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func (r *Repository) getVPNDocument(ctx context.Context, kind, id string) (vpnDocument, error) {
	return scanOne(r.db.WithContext(ctx).Raw("SELECT "+vpnDocumentColumns+" FROM network_vpn_documents WHERE tenant_id = 'default' AND workspace_id = 'default' AND kind = ? AND id = ? AND deleted_at IS NULL", kind, id).Row(), scanVPNDocument, "VPN document")
}

func (r *Repository) listVPNDocuments(ctx context.Context, kind string, filter domain.VPNDocumentFilter) ([]vpnDocument, error) {
	query := "SELECT " + vpnDocumentColumns + " FROM network_vpn_documents WHERE tenant_id = 'default' AND workspace_id = 'default' AND kind = ? AND deleted_at IS NULL AND id > ?"
	args := []any{kind, filter.AfterID}
	query, args = addSearch(query, args, filter.Search, "configuration->>'name'")
	query += " ORDER BY id LIMIT ?"
	args = append(args, min(200, max(1, filter.Limit)))
	return queryMany(ctx, r.db, query, args, scanVPNDocument)
}

func (r *Repository) saveVPNDocument(ctx context.Context, kind, id string, expected int, configuration any, actor string, now time.Time) error {
	raw, err := json.Marshal(configuration)
	if err != nil {
		return err
	}
	return normalizeDatabaseError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if kind == "profile" {
			if err := lockVPNPolicyReference(tx, raw, false); err != nil {
				return err
			}
		}
		if expected == 0 {
			if err := tx.Exec(`INSERT INTO network_vpn_documents (id, kind, revision, configuration, created_at, updated_at) VALUES (?, ?, 1, ?::jsonb, ?, ?)`, id, kind, string(raw), now, now).Error; err != nil {
				return err
			}
		} else {
			result := tx.Exec(`UPDATE network_vpn_documents SET revision = revision + 1, configuration = ?::jsonb, updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND kind = ? AND revision = ? AND deleted_at IS NULL`, string(raw), now, id, kind, expected)
			if err := vpnRevisionMutation(result); err != nil {
				return err
			}
		}
		return tx.Exec(`INSERT INTO network_vpn_document_revisions (document_id, revision, configuration, created_at, created_by) VALUES (?, ?, ?::jsonb, ?, ?)`, id, expected+1, string(raw), now, actor).Error
	}))
}

func vpnRevisionMutation(result *gorm.DB) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return apperrors.NewBusiness(apperrors.ErrConflict, "vpn_revision_conflict", "The VPN configuration changed. Reload it before saving.", "VPN 配置已变化，请刷新后重试。")
	}
	return nil
}

// Both resources keep drafts and immutable revisions; rollback creates a new revision.
func (r *Repository) publishVPNDocument(ctx context.Context, kind, id string, expected, target int, actor string, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('network-wireguard-mutations'))`).Error; err != nil {
			return err
		}
		item, err := scanOne(tx.Raw("SELECT "+vpnDocumentColumns+" FROM network_vpn_documents WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND kind = ? AND deleted_at IS NULL FOR UPDATE", id, kind).Row(), scanVPNDocument, "VPN document")
		if err != nil {
			return err
		}
		if item.Revision != expected {
			return apperrors.ErrConflict
		}
		revision, raw := item.Revision, item.Configuration
		if target != 0 {
			var history []byte
			if err := tx.Raw(`SELECT configuration FROM network_vpn_document_revisions WHERE document_id = ? AND revision = ?`, id, target).Row().Scan(&history); err != nil {
				return err
			}
			revision, raw = item.Revision+1, history
			if err := tx.Exec(`INSERT INTO network_vpn_document_revisions (document_id, revision, configuration, created_at, created_by) VALUES (?, ?, ?::jsonb, ?, ?)`, id, revision, string(raw), now, actor).Error; err != nil {
				return err
			}
		}
		if kind == "profile" {
			if err := lockVPNPolicyReference(tx, raw, true); err != nil {
				return err
			}
			if vpnProfileScopeChanged(item.Published, raw) {
				if err := revokeVPNProfileSessions(tx, id, now); err != nil {
					return err
				}
			}
		}
		return tx.Exec(`UPDATE network_vpn_documents SET revision = ?, configuration = ?::jsonb, published_revision = ?, published_configuration = ?::jsonb, updated_at = ? WHERE id = ? AND kind = ? AND tenant_id = 'default' AND workspace_id = 'default'`, revision, string(raw), revision, string(raw), now, id, kind).Error
	})
}

func (r *Repository) deleteVPNDocument(ctx context.Context, kind, id string, expected int, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('network-wireguard-mutations'))`).Error; err != nil {
			return err
		}
		var locked string
		if err := tx.Raw(`SELECT id FROM network_vpn_documents WHERE id = ? AND kind = ? AND revision = ? AND deleted_at IS NULL AND tenant_id = 'default' AND workspace_id = 'default' FOR UPDATE`, id, kind, expected).Row().Scan(&locked); err != nil {
			return apperrors.ErrConflict
		}
		if kind == "profile" {
			if err := revokeVPNProfileSessions(tx, id, now); err != nil {
				return err
			}
		}

		if kind == "selection-policy" {
			var count int64
			if err := tx.Raw(`SELECT count(*) FROM network_vpn_documents WHERE kind = 'profile' AND deleted_at IS NULL AND tenant_id = 'default' AND workspace_id = 'default' AND (configuration->>'selectionPolicyId' = ? OR published_configuration->>'selectionPolicyId' = ?)`, id, id).Row().Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				return apperrors.NewBusiness(apperrors.ErrConflict, "vpn_policy_in_use", "The selection policy is referenced by a VPN profile.", "该选择策略仍被 VPN 连接方案引用。")
			}
		}
		return vpnRevisionMutation(tx.Exec(`UPDATE network_vpn_documents SET deleted_at = ?, updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND kind = ? AND revision = ? AND deleted_at IS NULL`, now, now, id, kind, expected))
	})
}

func vpnProfile(item vpnDocument) (domain.VPNProfile, error) {
	result := domain.VPNProfile{ID: item.ID, Revision: item.Revision, PublishedRevision: item.PublishedRevision, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
	if err := json.Unmarshal(item.Configuration, &result.Configuration); err != nil {
		return result, err
	}
	if len(item.Published) != 0 {
		if err := json.Unmarshal(item.Published, &result.PublishedConfiguration); err != nil {
			return result, err
		}
	}
	return result, nil
}

func vpnSelectionPolicy(item vpnDocument) (domain.VPNSelectionPolicy, error) {
	result := domain.VPNSelectionPolicy{ID: item.ID, Revision: item.Revision, PublishedRevision: item.PublishedRevision, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
	if err := json.Unmarshal(item.Configuration, &result.Configuration); err != nil {
		return result, err
	}
	if len(item.Published) != 0 {
		if err := json.Unmarshal(item.Published, &result.PublishedConfiguration); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (r *Repository) ListVPNProfiles(ctx context.Context, filter domain.VPNDocumentFilter) ([]domain.VPNProfile, error) {
	rows, err := r.listVPNDocuments(ctx, "profile", filter)
	if err != nil {
		return nil, err
	}
	items := make([]domain.VPNProfile, 0, len(rows))
	for _, row := range rows {
		item, err := vpnProfile(row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *Repository) GetVPNProfile(ctx context.Context, id string) (domain.VPNProfile, error) {
	row, err := r.getVPNDocument(ctx, "profile", id)
	if err != nil {
		return domain.VPNProfile{}, err
	}
	return vpnProfile(row)
}

func (r *Repository) SaveVPNProfile(ctx context.Context, id string, expected int, config domain.VPNProfileConfig, actor string, now time.Time) (domain.VPNProfile, error) {
	if err := r.saveVPNDocument(ctx, "profile", id, expected, config, actor, now); err != nil {
		return domain.VPNProfile{}, err
	}
	return r.GetVPNProfile(ctx, id)
}

func (r *Repository) PublishVPNProfile(ctx context.Context, id string, expected, target int, actor string, now time.Time) (domain.VPNProfile, error) {
	if err := r.publishVPNDocument(ctx, "profile", id, expected, target, actor, now); err != nil {
		return domain.VPNProfile{}, err
	}
	return r.GetVPNProfile(ctx, id)
}

func (r *Repository) DeleteVPNProfile(ctx context.Context, id string, expected int, now time.Time) error {
	return r.deleteVPNDocument(ctx, "profile", id, expected, now)
}

func (r *Repository) ListVPNSelectionPolicies(ctx context.Context, filter domain.VPNDocumentFilter) ([]domain.VPNSelectionPolicy, error) {
	rows, err := r.listVPNDocuments(ctx, "selection-policy", filter)
	if err != nil {
		return nil, err
	}
	items := make([]domain.VPNSelectionPolicy, 0, len(rows))
	for _, row := range rows {
		item, err := vpnSelectionPolicy(row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *Repository) GetVPNSelectionPolicy(ctx context.Context, id string) (domain.VPNSelectionPolicy, error) {
	row, err := r.getVPNDocument(ctx, "selection-policy", id)
	if err != nil {
		return domain.VPNSelectionPolicy{}, err
	}
	return vpnSelectionPolicy(row)
}

func (r *Repository) SaveVPNSelectionPolicy(ctx context.Context, id string, expected int, config domain.VPNSelectionPolicyConfig, actor string, now time.Time) (domain.VPNSelectionPolicy, error) {
	if err := r.saveVPNDocument(ctx, "selection-policy", id, expected, config, actor, now); err != nil {
		return domain.VPNSelectionPolicy{}, err
	}
	return r.GetVPNSelectionPolicy(ctx, id)
}

func (r *Repository) PublishVPNSelectionPolicy(ctx context.Context, id string, expected, target int, actor string, now time.Time) (domain.VPNSelectionPolicy, error) {
	if err := r.publishVPNDocument(ctx, "selection-policy", id, expected, target, actor, now); err != nil {
		return domain.VPNSelectionPolicy{}, err
	}
	return r.GetVPNSelectionPolicy(ctx, id)
}

func (r *Repository) DeleteVPNSelectionPolicy(ctx context.Context, id string, expected int, now time.Time) error {
	return r.deleteVPNDocument(ctx, "selection-policy", id, expected, now)
}

func listVPNRevisions[T any](ctx context.Context, r *Repository, kind, id string) ([]domain.VPNRevision[T], error) {
	if _, err := r.getVPNDocument(ctx, kind, id); err != nil {
		return nil, err
	}
	return queryMany(ctx, r.db, `SELECT revision, configuration, created_at, created_by FROM network_vpn_document_revisions WHERE document_id = ? ORDER BY revision DESC LIMIT 100`, []any{id}, func(row scanner) (domain.VPNRevision[T], error) {
		var item domain.VPNRevision[T]
		var raw []byte
		if err := row.Scan(&item.Revision, &raw, &item.CreatedAt, &item.CreatedBy); err != nil {
			return item, err
		}
		return item, json.Unmarshal(raw, &item.Configuration)
	})
}

func (r *Repository) VPNProfileRevisions(ctx context.Context, id string) ([]domain.VPNRevision[domain.VPNProfileConfig], error) {
	return listVPNRevisions[domain.VPNProfileConfig](ctx, r, "profile", id)
}
func (r *Repository) VPNSelectionPolicyRevisions(ctx context.Context, id string) ([]domain.VPNRevision[domain.VPNSelectionPolicyConfig], error) {
	return listVPNRevisions[domain.VPNSelectionPolicyConfig](ctx, r, "selection-policy", id)
}
