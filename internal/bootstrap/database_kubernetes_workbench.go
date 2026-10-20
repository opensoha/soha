package bootstrap

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"
)

const kubernetesWorkbenchSeedVersionKey = "bootstrap.kubernetes_workbench_version"
const kubernetesWorkbenchSeedVersion = "2026-10-19-task113"

// Upgrade only this feature; replaying all defaults would erase menu-role bindings.
func upgradeKubernetesWorkbenchSeeds(ctx context.Context, db *gorm.DB) error {
	version, err := readSeedVersion(ctx, db, kubernetesWorkbenchSeedVersionKey)
	if err != nil {
		return err
	}
	if version != "" {
		return upgradeKubernetesWorkbenchExtensions(ctx, db)
	}
	now := time.Now().UTC()
	var groups []menuSeed
	for _, item := range defaultMenuSeeds() {
		if item.ID == "resource-monitoring" || strings.HasPrefix(item.ID, "resource-monitoring-") || item.ID == "network-routing" || item.ID == "network-backends" {
			groups = append(groups, item)
		}
	}
	if err := upsertMenus(ctx, db, groups, now); err != nil {
		return err
	}
	permissions, err := json.Marshal([]string{
		"platform.resource-monitoring.overview.view", "platform.resource-monitoring.cpu.view",
		"platform.resource-monitoring.memory.view", "platform.resource-monitoring.conntrack.view",
		"platform.resource-monitoring.pods.view",
	})
	if err != nil {
		return err
	}
	if err := db.WithContext(ctx).Exec(`
		UPDATE roles SET permission_keys = (
			SELECT jsonb_agg(DISTINCT permission ORDER BY permission)
			FROM jsonb_array_elements_text(COALESCE(roles.permission_keys::jsonb, '[]'::jsonb) || ?::jsonb) AS added(permission)
		), updated_at = ? WHERE id IN ('admin', 'ops', 'readonly') AND scope = 'system'
	`, string(permissions), now).Error; err != nil {
		return err
	}
	if err := writeSeedVersion(ctx, db, kubernetesWorkbenchSeedVersionKey, kubernetesWorkbenchSeedVersion); err != nil {
		return err
	}
	return upgradeKubernetesWorkbenchExtensions(ctx, db)
}

func upgradeKubernetesWorkbenchExtensions(ctx context.Context, db *gorm.DB) error {
	if err := upgradeResourceMonitoringDashboardSeeds(ctx, db); err != nil {
		return err
	}
	if err := upgradeKubernetesCRUDPermissions(ctx, db); err != nil {
		return err
	}
	return upgradeFlatKubernetesNetworkMenus(ctx, db)
}

// Keep legacy rows and role bindings for old links and customized menus.
func upgradeResourceMonitoringDashboardSeeds(ctx context.Context, db *gorm.DB) error {
	const key = "bootstrap.resource_monitoring_dashboard_version"
	version, err := readSeedVersion(ctx, db, key)
	if err != nil || version != "" {
		return err
	}
	now := time.Now().UTC()
	for _, item := range defaultMenuSeeds() {
		if item.ParentID != "resource-monitoring" {
			continue
		}
		if err := db.WithContext(ctx).Exec(`
			UPDATE menus SET enabled=FALSE,updated_at=?
			WHERE id=? AND parent_id=? AND path=? AND label_zh=? AND label_en=?
				AND icon_key=? AND section=? AND sort_order=? AND enabled=TRUE
		`, now, item.ID, item.ParentID, item.Path, item.LabelZH, item.LabelEN,
			item.IconKey, item.Section, item.SortOrder).Error; err != nil {
			return err
		}
	}
	return writeSeedVersion(ctx, db, key, "2026-10-19-task113-single-page")
}
