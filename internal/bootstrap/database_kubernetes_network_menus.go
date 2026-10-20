package bootstrap

import (
	"context"
	"time"

	"gorm.io/gorm"
)

const flatKubernetesNetworkMenuVersionKey = "bootstrap.kubernetes_network_flat_version"

// Preserve menu identities, custom labels and role bindings while removing the two legacy groups.
func upgradeFlatKubernetesNetworkMenus(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		version, err := readSeedVersion(ctx, tx, flatKubernetesNetworkMenuVersionKey)
		if err != nil || version != "" {
			return err
		}
		now := time.Now().UTC()
		groups := []string{"network-routing", "network-backends"}
		// Move custom children too, so disabling a legacy parent cannot hide them.
		if err := tx.Exec(`UPDATE menus SET parent_id='network', updated_at=? WHERE parent_id IN ?`, now, groups).Error; err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE menus SET enabled=FALSE, updated_at=? WHERE id IN ? AND enabled=TRUE`, now, groups).Error; err != nil {
			return err
		}
		return writeSeedVersion(ctx, tx, flatKubernetesNetworkMenuVersionKey, "2026-10-19-task113-flat-network")
	})
}
