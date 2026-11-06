package networkproxy

import (
	"context"
	"errors"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
	"time"
)

func (r *Repository) ConfigurationHistory(ctx context.Context, id string) ([]domain.ConfigurationRevision, error) {
	items := []domain.ConfigurationRevision{}
	err := r.db.WithContext(ctx).Raw(`SELECT revision,content_hash,created_at FROM network_proxy_configuration_history WHERE tenant_id='default' AND workspace_id='default' AND instance_id=? ORDER BY revision DESC LIMIT 100`, id).Scan(&items).Error
	return items, err
}
func (r *Repository) ConfigurationRevision(ctx context.Context, id string, revision int64) (domain.ConfigurationRevision, error) {
	var row struct {
		Revision         int64
		ContentHash      string
		CreatedAt        time.Time
		ContentEncrypted string
	}
	err := r.db.WithContext(ctx).Table("network_proxy_configuration_history").Where("tenant_id='default' AND workspace_id='default' AND instance_id=? AND revision=?", id, revision).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ConfigurationRevision{}, apperrors.ErrNotFound
	}
	return domain.ConfigurationRevision{Revision: row.Revision, ContentHash: row.ContentHash, CreatedAt: row.CreatedAt, Encrypted: row.ContentEncrypted}, err
}

func saveConfigurationHistory(tx *gorm.DB, id string, revision int64, encrypted, hash string, now time.Time) error {
	if err := tx.Exec(`INSERT INTO network_proxy_configuration_history(tenant_id,workspace_id,instance_id,revision,content_encrypted,content_hash,created_at) VALUES('default','default',?,?,?,?,?)`, id, revision, encrypted, hash, now).Error; err != nil {
		return err
	}
	return tx.Exec(`DELETE FROM network_proxy_configuration_history WHERE tenant_id='default' AND workspace_id='default' AND instance_id=? AND revision NOT IN (SELECT revision FROM network_proxy_configuration_history WHERE tenant_id='default' AND workspace_id='default' AND instance_id=? ORDER BY revision DESC LIMIT 100)`, id, id).Error
}
