package networkproxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	accessdomain "github.com/opensoha/soha/internal/domain/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/apperrors"
	accessrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"slices"
	"time"
)

func lockedInstance(tx *gorm.DB, id string) (instanceRow, error) {
	var row instanceRow
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = 'default' AND workspace_id = 'default'", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = apperrors.ErrNotFound
	}
	return row, err
}

func (r *Repository) ResolveBinding(ctx context.Context, value domain.Binding) (domain.Binding, error) {
	repo := accessrepo.New(r.db)
	site, err := repo.GetSite(ctx, value.SiteID)
	if err != nil {
		return domain.Binding{}, err
	}
	device, err := repo.GetDevice(ctx, value.DeviceID)
	if err != nil {
		return domain.Binding{}, err
	}
	if site.Status != accessdomain.StatusActive || device.Status != accessdomain.DeviceStatusActive || device.SiteID != site.ID {
		return domain.Binding{}, apperrors.ErrConflict
	}
	value.OwnerUserID = device.OwnerUserID
	value.ResourceSpaceIDs = map[string]string{}
	for _, id := range value.ResourceIDs {
		resource, err := repo.GetResource(ctx, id)
		if err != nil {
			return domain.Binding{}, err
		}
		space, err := repo.GetSpace(ctx, resource.SpaceID)
		if err != nil {
			return domain.Binding{}, err
		}
		if !resource.Protected || space.SiteID != site.ID || space.Status != accessdomain.StatusActive {
			return domain.Binding{}, apperrors.ErrConflict
		}
		value.ResourceSpaceIDs[id] = space.ID
	}
	raw, _ := json.Marshal(struct {
		SiteID, DeviceID, OwnerUserID string
		ResourceSpaceIDs              map[string]string
	}{value.SiteID, value.DeviceID, value.OwnerUserID, value.ResourceSpaceIDs})
	value.AuthorizationHash = fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
	return value, nil
}

func lockBinding(tx *gorm.DB, value domain.Binding) error {
	queries := []struct {
		sql  string
		args []any
	}{
		{`SELECT id FROM network_access_sites WHERE tenant_id='default' AND workspace_id='default' AND id=? FOR SHARE`, []any{value.SiteID}},
		{`SELECT id FROM network_access_devices WHERE tenant_id='default' AND workspace_id='default' AND id=? FOR SHARE`, []any{value.DeviceID}},
	}
	if len(value.ResourceIDs) > 0 {
		queries = append(queries,
			struct {
				sql  string
				args []any
			}{`SELECT id FROM network_access_resources WHERE tenant_id='default' AND workspace_id='default' AND id IN ? ORDER BY id FOR SHARE`, []any{value.ResourceIDs}},
			struct {
				sql  string
				args []any
			}{`SELECT id FROM network_access_spaces WHERE tenant_id='default' AND workspace_id='default' AND id IN (SELECT space_id FROM network_access_resources WHERE tenant_id='default' AND workspace_id='default' AND id IN ?) ORDER BY id FOR SHARE`, []any{value.ResourceIDs}},
		)
	}
	for _, query := range queries {
		var ids []string
		if err := tx.Raw(query.sql, query.args...).Scan(&ids).Error; err != nil {
			return err
		}
	}
	return nil
}

// Policy publication and proxy configuration use one revision stream. A binding
// is administrative placement; all published protected targets are still denied.
func (r *Repository) PrepareConfiguration(ctx context.Context, id string, now time.Time) (domain.Instance, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-access-policy-snapshot:default:default").Error; err != nil {
			return err
		}
		row, err := lockedInstance(tx, id)
		if err != nil {
			return err
		}
		item := row.domain()
		if !row.Enabled || row.DesiredRevision == 0 {
			return nil
		}
		if item.Binding == nil {
			return apperrors.ErrConflict
		}
		if err := lockBinding(tx, *item.Binding); err != nil {
			return err
		}
		if _, err := New(tx).ResolveBinding(ctx, *item.Binding); err != nil {
			return err
		}
		if item.Binding != nil {
			binding, err := New(tx).ResolveBinding(ctx, *item.Binding)
			if err != nil {
				return err
			}
			var restricted bool
			if err := tx.Raw(`SELECT network_security_subject_restricted(?)`, binding.OwnerUserID).Scan(&restricted).Error; err != nil {
				return err
			}
			if restricted != row.SecurityRestricted {
				if err := tx.Model(&row).Updates(map[string]any{"security_restricted": restricted, "desired_revision": gorm.Expr("desired_revision+1"), "updated_at": now}).Error; err != nil {
					return err
				}
			}
		}

		repo := accessrepo.New(tx)
		snapshot, err := repo.GetPolicySnapshot(ctx)
		if err != nil {
			return err
		}
		for _, resourceID := range item.Binding.ResourceIDs {
			if !slices.Contains(snapshot.ProtectedResourceIDs, resourceID) {
				return apperrors.ErrConflict
			}
		}
		protection, err := repo.CompileProtectedDestinations(ctx, snapshot.PolicyVersion, snapshot.ProtectedResourceIDs)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(protection)
		if err != nil {
			return err
		}
		old, _ := json.Marshal(item.Protection)
		if bytes.Equal(raw, old) {
			return nil
		}
		return tx.Model(&row).Updates(map[string]any{"desired_protection": gorm.Expr("?::jsonb", string(raw)), "desired_revision": gorm.Expr("desired_revision + 1"), "updated_at": now}).Error
	})
	if err != nil {
		diagnosticErr := r.db.WithContext(ctx).Model(&instanceRow{}).Where("id=? AND tenant_id='default' AND workspace_id='default'", id).Updates(map[string]any{"health": "degraded", "reason_code": "protection_unavailable"}).Error
		return domain.Instance{}, errors.Join(err, diagnosticErr)
	}
	return r.Get(ctx, id)
}
