package bootstrap

import (
	"context"
	"time"

	"gorm.io/gorm"
)

const networkNavigationSeedVersion = "2026-10-18-intranet-navigation"
const networkNavigationSeedVersionKey = "bootstrap.network_navigation_version"

// Apply this menu-only upgrade once without replaying unrelated defaults or bindings.
func upgradeNetworkAccessNavigationSeeds(ctx context.Context, db *gorm.DB) error {
	version, err := readSeedVersion(ctx, db, networkNavigationSeedVersionKey)
	if err != nil {
		return err
	}
	if version != "" {
		return nil
	}
	now := time.Now().UTC()
	var groups []menuSeed
	for _, item := range defaultMenuSeeds() {
		switch item.ID {
		case "network-access-infrastructure", "network-access-controls", "network-access-vpn", "network-access-monitoring":
			groups = append(groups, item)
		}
	}
	if err := upsertMenus(ctx, db, groups, now); err != nil {
		return err
	}
	if err := syncNetworkAccessMenuSeedUpgrades(ctx, db, now); err != nil {
		return err
	}
	if err := syncNetworkAccessNavigationGroups(ctx, db, now); err != nil {
		return err
	}
	return writeSeedVersion(ctx, db, networkNavigationSeedVersionKey, networkNavigationSeedVersion)
}

func syncNetworkAccessNavigationGroups(ctx context.Context, db *gorm.DB, now time.Time) error {
	items := []struct {
		id, path, labelZH, labelEN, icon, section string
		order                                     int
		parent, newZH, newEN                      string
		newOrder                                  int
	}{
		{id: "network-access-sites", path: "/network-access/sites", labelZH: "站点", labelEN: "Sites", icon: "globe", section: "vpn", order: 10, parent: "network-access-infrastructure", newZH: "站点", newEN: "Sites", newOrder: 10},
		{id: "network-access-spaces", path: "/network-access/spaces", labelZH: "网络空间", labelEN: "Network Spaces", icon: "network", section: "vpn", order: 20, parent: "network-access-infrastructure", newZH: "网络空间", newEN: "Network Spaces", newOrder: 20},
		{id: "network-access-gateways", path: "/network-access/gateways", labelZH: "网关", labelEN: "Gateways", icon: "network", section: "vpn", order: 40, parent: "network-access-infrastructure", newZH: "网关", newEN: "Gateways", newOrder: 30},
		{id: "network-access-resources", path: "/network-access/resources", labelZH: "资源", labelEN: "Resources", icon: "blocks", section: "vpn", order: 30, parent: "identity", newZH: "网络资源", newEN: "Network Resources", newOrder: 20},
		{id: "network-access-policy", path: "/network-access/policy", labelZH: "策略", labelEN: "Policies", icon: "shield", section: "vpn", order: 70, parent: "network-access-controls", newZH: "策略", newEN: "Policies", newOrder: 10},
		{id: "network-access-access-grants", path: "/network-access/access-grants", labelZH: "访问授权", labelEN: "Access Grants", icon: "shield", section: "vpn", order: 60, parent: "network-access-controls", newZH: "访问授权", newEN: "Access Grants", newOrder: 20},
		{id: "network-access-site-profile-bindings", path: "/network-access/site-profile-bindings", labelZH: "接入等级", labelEN: "Access Levels", icon: "shield", section: "vpn", order: 50, parent: "network-access-controls", newZH: "接入等级", newEN: "Access Levels", newOrder: 30},
		{id: "network-access-vpn-profiles", path: "/network-access/vpn/profiles", labelZH: "VPN 连接方案", labelEN: "VPN Profiles", icon: "network", section: "vpn", order: 110, parent: "network-access-vpn", newZH: "VPN 连接方案", newEN: "VPN Profiles", newOrder: 10},
		{id: "network-access-vpn-selection-policies", path: "/network-access/vpn/selection-policies", labelZH: "VPN Auto 策略", labelEN: "VPN Auto Policies", icon: "network", section: "vpn", order: 120, parent: "network-access-vpn", newZH: "VPN Auto 策略", newEN: "VPN Auto Policies", newOrder: 20},
		{id: "network-access-sessions", path: "/network-access/sessions", labelZH: "会话", labelEN: "Sessions", icon: "activity", section: "vpn", order: 80, parent: "network-access-monitoring", newZH: "会话", newEN: "Sessions", newOrder: 10},
		{id: "network-access-telemetry", path: "/network-access/telemetry", labelZH: "遥测", labelEN: "Telemetry", icon: "gauge", section: "vpn", order: 100, parent: "network-access-monitoring", newZH: "遥测", newEN: "Telemetry", newOrder: 20},
		{id: "network-access-vpn-dashboard", path: "/network-access/vpn/dashboard", labelZH: "VPN Dashboard", labelEN: "VPN Dashboard", icon: "network", section: "vpn", order: 130, parent: "network-access-monitoring", newZH: "VPN 看板", newEN: "VPN Dashboard", newOrder: 30},
		{id: "network-access-enrollments", path: "/network-access/enrollments", labelZH: "运行时注册", labelEN: "Runtime Enrollment", icon: "key", section: "vpn", order: 90, parent: "identity", newZH: "运行时接入", newEN: "Runtime Access", newOrder: 60},
		{id: "network-access-settings", path: "/network-access/settings", labelZH: "入网设置", labelEN: "Admission Settings", icon: "settings", section: "network", order: 20, parent: "identity", newZH: "入网配置", newEN: "Admission Configuration", newOrder: 20},
	}
	for _, item := range items {
		if err := db.WithContext(ctx).Exec(`
			UPDATE menus
			SET parent_id = ?, label_zh = ?, label_en = ?, sort_order = ?, updated_at = ?
			WHERE id = ? AND parent_id = 'identity' AND path = ? AND label_zh = ? AND label_en = ?
				AND icon_key = ? AND section = ? AND sort_order = ? AND enabled = TRUE
		`, item.parent, item.newZH, item.newEN, item.newOrder, now,
			item.id, item.path, item.labelZH, item.labelEN, item.icon, item.section, item.order).Error; err != nil {
			return err
		}
	}
	return nil
}
