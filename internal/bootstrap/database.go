package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	accessapp "github.com/kubecrux/kubecrux/internal/application/access"
	cfgpkg "github.com/kubecrux/kubecrux/internal/infrastructure/config"
	dbinfra "github.com/kubecrux/kubecrux/internal/infrastructure/db"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type menuSeed struct {
	ID        string
	ParentID  string
	Path      string
	LabelZH   string
	LabelEN   string
	IconKey   string
	Section   string
	SortOrder int
	Enabled   bool
	Roles     []string
}

type policySeed struct {
	ID         string
	Name       string
	Effect     string
	Priority   int
	Subjects   string
	Targets    string
	Actions    string
	Conditions string
	Reason     string
}

type roleSeed struct {
	ID             string
	Name           string
	Capabilities   string
	PermissionKeys string
}

type environmentSeed struct {
	ID               string
	Key              string
	Name             string
	Tier             string
	StageLevel       int
	SortOrder        int
	IsProduction     bool
	RequiresApproval bool
	Enabled          bool
}

type clusterSeed struct {
	ID          string
	Name        string
	Region      string
	Environment string
	Labels      string
}

type clusterCredentialSeed struct {
	ID        string
	ClusterID string
	SourceRef string
	Metadata  string
}

// bootstrapSeedVersion identifies the current set of built-in seeds. When the
// code introduces new menus/roles/policies or changes structural defaults, bump
// this string so the next startup replays the idempotent seed helpers:
//   - roles/policies keep using UPSERT so code changes propagate
//   - menus/environments keep using ON CONFLICT DO NOTHING so user edits survive
//
// While the stored version matches this constant, the static seed block is
// skipped entirely. Config-driven sync (admin user, clusters) runs separately
// during startup so runtime config updates do not depend on replaying defaults.
const bootstrapSeedVersion = "2026-05-12-2"

const bootstrapSeedVersionKey = "bootstrap.seed_version"

func seedDefaults(ctx context.Context, store *dbinfra.Store, cfg cfgpkg.Config) error {
	storedVersion, err := readBootstrapSeedVersion(ctx, store.DB())
	if err != nil {
		return err
	}
	if storedVersion == bootstrapSeedVersion {
		return nil
	}

	return store.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		storedVersion, err := readBootstrapSeedVersion(ctx, tx)
		if err != nil {
			return err
		}
		if storedVersion == bootstrapSeedVersion {
			return nil
		}
		if err := seedRoles(ctx, tx); err != nil {
			return err
		}
		if err := seedMenus(ctx, tx, cfg.Modules); err != nil {
			return err
		}
		if err := seedPolicies(ctx, tx); err != nil {
			return err
		}
		if err := seedDeliveryCatalog(ctx, tx); err != nil {
			return err
		}
		if err := seedWorkflowTemplates(ctx, tx); err != nil {
			return err
		}
		if err := writeBootstrapSeedVersion(ctx, tx, bootstrapSeedVersion); err != nil {
			return err
		}
		return nil
	})
}

func syncBootstrapRuntime(ctx context.Context, store *dbinfra.Store, cfg cfgpkg.Config) error {
	return store.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := seedUser(ctx, tx, cfg); err != nil {
			return err
		}
		if err := pruneDemoClusters(ctx, tx, cfg.Kubernetes.Clusters); err != nil {
			return err
		}
		if err := seedClusters(ctx, tx, cfg.Kubernetes.Clusters); err != nil {
			return err
		}
		return nil
	})
}

func readBootstrapSeedVersion(ctx context.Context, db *gorm.DB) (string, error) {
	var value string
	row := db.WithContext(ctx).Raw(
		`SELECT value #>> '{version}' FROM app_settings WHERE setting_key = ?`,
		bootstrapSeedVersionKey,
	).Row()
	if err := row.Scan(&value); err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return "", nil
		}
		return "", err
	}
	return value, nil
}

func writeBootstrapSeedVersion(ctx context.Context, db *gorm.DB, version string) error {
	payload, err := json.Marshal(map[string]string{"version": version})
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Exec(`
		INSERT INTO app_settings (setting_key, category, value, updated_by, created_at, updated_at)
		VALUES (?, 'bootstrap', ?::jsonb, 'system', NOW(), NOW())
		ON CONFLICT (setting_key) DO UPDATE SET
			value = EXCLUDED.value,
			updated_at = EXCLUDED.updated_at
	`, bootstrapSeedVersionKey, string(payload)).Error
}

func pruneDemoClusters(ctx context.Context, db *gorm.DB, clusters []cfgpkg.ClusterConfig) error {
	if len(clusters) > 0 {
		return nil
	}
	demoIDs := []string{"local", "direct-demo", "agent-demo", "agent-audit-check", "agent-audit-pass"}
	if err := db.WithContext(ctx).Exec(`DELETE FROM cluster_credentials_meta WHERE cluster_id IN ?`, demoIDs).Error; err != nil {
		return err
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM clusters WHERE id IN ?`, demoIDs).Error; err != nil {
		return err
	}
	return nil
}

func defaultMenuSeeds() []menuSeed {
	return []menuSeed{
		{ID: "dashboard", Path: "/", LabelZH: "总览", LabelEN: "Dashboard", IconKey: "gauge", Section: "platform", SortOrder: 10, Enabled: true},
		{ID: "cluster-resources-nodes", Path: "/cluster-resources/nodes", LabelZH: "节点", LabelEN: "Nodes", IconKey: "server", Section: "platform", SortOrder: 20, Enabled: true},
		{ID: "extensions", Path: "/extensions", LabelZH: "CRD", LabelEN: "CRD", IconKey: "puzzle", Section: "platform", SortOrder: 90, Enabled: true},
		{ID: "helm", Path: "/helm", LabelZH: "Helm", LabelEN: "Helm", IconKey: "puzzle", Section: "platform", SortOrder: 80, Enabled: true},
		{ID: "helm-releases", ParentID: "helm", Path: "/helm/releases", LabelZH: "Releases", LabelEN: "Releases", IconKey: "puzzle", Section: "platform", SortOrder: 20, Enabled: true},
		{ID: "helm-charts", ParentID: "helm", Path: "/helm/charts", LabelZH: "Charts", LabelEN: "Charts", IconKey: "puzzle", Section: "platform", SortOrder: 21, Enabled: true},
		{ID: "platform-access-control", Path: "/platform-access-control", LabelZH: "RBAC", LabelEN: "RBAC", IconKey: "shield", Section: "platform", SortOrder: 70, Enabled: true, Roles: []string{"admin", "ops", "developer", "readonly"}},
		{ID: "platform-access-control-serviceaccounts", ParentID: "platform-access-control", Path: "/platform-access-control/serviceaccounts", LabelZH: "ServiceAccounts", LabelEN: "ServiceAccounts", IconKey: "shield", Section: "platform", SortOrder: 23, Enabled: true, Roles: []string{"admin", "ops", "developer", "readonly"}},
		{ID: "platform-access-control-clusterroles", ParentID: "platform-access-control", Path: "/platform-access-control/clusterroles", LabelZH: "ClusterRoles", LabelEN: "ClusterRoles", IconKey: "shield", Section: "platform", SortOrder: 24, Enabled: true, Roles: []string{"admin", "ops", "developer", "readonly"}},
		{ID: "platform-access-control-roles", ParentID: "platform-access-control", Path: "/platform-access-control/roles", LabelZH: "Roles", LabelEN: "Roles", IconKey: "shield", Section: "platform", SortOrder: 25, Enabled: true, Roles: []string{"admin", "ops", "developer", "readonly"}},
		{ID: "platform-access-control-clusterrolebindings", ParentID: "platform-access-control", Path: "/platform-access-control/clusterrolebindings", LabelZH: "ClusterRoleBindings", LabelEN: "ClusterRoleBindings", IconKey: "shield", Section: "platform", SortOrder: 26, Enabled: true, Roles: []string{"admin", "ops", "developer", "readonly"}},
		{ID: "platform-access-control-rolebindings", ParentID: "platform-access-control", Path: "/platform-access-control/rolebindings", LabelZH: "RoleBindings", LabelEN: "RoleBindings", IconKey: "shield", Section: "platform", SortOrder: 27, Enabled: true, Roles: []string{"admin", "ops", "developer", "readonly"}},
		{ID: "workloads", Path: "/workloads", LabelZH: "工作负载", LabelEN: "Workloads", IconKey: "boxes", Section: "platform", SortOrder: 30, Enabled: true},
		{ID: "workloads-overview", ParentID: "workloads", Path: "/workloads/overview", LabelZH: "概览", LabelEN: "Overview", IconKey: "boxes", Section: "platform", SortOrder: 31, Enabled: true},
		{ID: "workloads-deployments", ParentID: "workloads", Path: "/workloads/deployments", LabelZH: "Deployments", LabelEN: "Deployments", IconKey: "boxes", Section: "platform", SortOrder: 32, Enabled: true},
		{ID: "workloads-pods", ParentID: "workloads", Path: "/workloads/pods", LabelZH: "Pods", LabelEN: "Pods", IconKey: "boxes", Section: "platform", SortOrder: 33, Enabled: true},
		{ID: "workloads-statefulsets", ParentID: "workloads", Path: "/workloads/statefulsets", LabelZH: "StatefulSets", LabelEN: "StatefulSets", IconKey: "boxes", Section: "platform", SortOrder: 34, Enabled: true},
		{ID: "workloads-daemonsets", ParentID: "workloads", Path: "/workloads/daemonsets", LabelZH: "DaemonSets", LabelEN: "DaemonSets", IconKey: "boxes", Section: "platform", SortOrder: 35, Enabled: true},
		{ID: "workloads-jobs", ParentID: "workloads", Path: "/workloads/jobs", LabelZH: "Jobs", LabelEN: "Jobs", IconKey: "boxes", Section: "platform", SortOrder: 36, Enabled: true},
		{ID: "workloads-cronjobs", ParentID: "workloads", Path: "/workloads/cronjobs", LabelZH: "CronJobs", LabelEN: "CronJobs", IconKey: "boxes", Section: "platform", SortOrder: 37, Enabled: true},
		{ID: "workloads-replicasets", ParentID: "workloads", Path: "/workloads/replicasets", LabelZH: "ReplicaSets", LabelEN: "ReplicaSets", IconKey: "boxes", Section: "platform", SortOrder: 38, Enabled: true},
		{ID: "workloads-replicationcontrollers", ParentID: "workloads", Path: "/workloads/replicationcontrollers", LabelZH: "ReplicationControllers", LabelEN: "ReplicationControllers", IconKey: "boxes", Section: "platform", SortOrder: 39, Enabled: true},
		{ID: "configuration", Path: "/configuration", LabelZH: "配置", LabelEN: "Configuration", IconKey: "cog", Section: "platform", SortOrder: 40, Enabled: true},
		{ID: "configuration-configmaps", ParentID: "configuration", Path: "/configuration/configmaps", LabelZH: "ConfigMaps", LabelEN: "ConfigMaps", IconKey: "cog", Section: "platform", SortOrder: 41, Enabled: true},
		{ID: "configuration-secrets", ParentID: "configuration", Path: "/configuration/secrets", LabelZH: "Secrets", LabelEN: "Secrets", IconKey: "cog", Section: "platform", SortOrder: 42, Enabled: true},
		{ID: "configuration-resourcequotas", ParentID: "configuration", Path: "/configuration/resourcequotas", LabelZH: "ResourceQuotas", LabelEN: "ResourceQuotas", IconKey: "cog", Section: "platform", SortOrder: 43, Enabled: true},
		{ID: "configuration-limitranges", ParentID: "configuration", Path: "/configuration/limitranges", LabelZH: "LimitRanges", LabelEN: "LimitRanges", IconKey: "cog", Section: "platform", SortOrder: 44, Enabled: true},
		{ID: "configuration-hpas", ParentID: "configuration", Path: "/configuration/hpas", LabelZH: "HorizontalPodAutoscalers", LabelEN: "HorizontalPodAutoscalers", IconKey: "cog", Section: "platform", SortOrder: 45, Enabled: true},
		{ID: "configuration-poddisruptionbudgets", ParentID: "configuration", Path: "/configuration/poddisruptionbudgets", LabelZH: "PodDisruptionBudgets", LabelEN: "PodDisruptionBudgets", IconKey: "cog", Section: "platform", SortOrder: 46, Enabled: true},
		{ID: "configuration-priorityclasses", ParentID: "configuration", Path: "/configuration/priorityclasses", LabelZH: "PriorityClasses", LabelEN: "PriorityClasses", IconKey: "cog", Section: "platform", SortOrder: 47, Enabled: true},
		{ID: "configuration-runtimeclasses", ParentID: "configuration", Path: "/configuration/runtimeclasses", LabelZH: "RuntimeClasses", LabelEN: "RuntimeClasses", IconKey: "cog", Section: "platform", SortOrder: 48, Enabled: true},
		{ID: "configuration-leases", ParentID: "configuration", Path: "/configuration/leases", LabelZH: "Leases", LabelEN: "Leases", IconKey: "cog", Section: "platform", SortOrder: 49, Enabled: true},
		{ID: "configuration-mutatingwebhookconfigurations", ParentID: "configuration", Path: "/configuration/mutatingwebhookconfigurations", LabelZH: "MutatingWebhookConfigurations", LabelEN: "MutatingWebhookConfigurations", IconKey: "cog", Section: "platform", SortOrder: 50, Enabled: true},
		{ID: "configuration-validatingwebhookconfigurations", ParentID: "configuration", Path: "/configuration/validatingwebhookconfigurations", LabelZH: "ValidatingWebhookConfigurations", LabelEN: "ValidatingWebhookConfigurations", IconKey: "cog", Section: "platform", SortOrder: 51, Enabled: true},
		{ID: "network", Path: "/network", LabelZH: "网络", LabelEN: "Network", IconKey: "network", Section: "platform", SortOrder: 50, Enabled: true},
		{ID: "network-topology", ParentID: "network", Path: "/network/topology", LabelZH: "网络拓扑", LabelEN: "Network Topology", IconKey: "network", Section: "platform", SortOrder: 40, Enabled: true},
		{ID: "network-services", ParentID: "network", Path: "/network/services", LabelZH: "Services", LabelEN: "Services", IconKey: "network", Section: "platform", SortOrder: 41, Enabled: true},
		{ID: "network-ingresses", ParentID: "network", Path: "/network/ingresses", LabelZH: "Ingresses", LabelEN: "Ingresses", IconKey: "network", Section: "platform", SortOrder: 42, Enabled: true},
		{ID: "network-gateways", ParentID: "network", Path: "/network/gateways", LabelZH: "Gateways", LabelEN: "Gateways", IconKey: "network", Section: "platform", SortOrder: 43, Enabled: true},
		{ID: "network-endpointslices", ParentID: "network", Path: "/network/endpointslices", LabelZH: "EndpointSlices", LabelEN: "EndpointSlices", IconKey: "network", Section: "platform", SortOrder: 53, Enabled: true},
		{ID: "network-ingressclasses", ParentID: "network", Path: "/network/ingressclasses", LabelZH: "IngressClasses", LabelEN: "IngressClasses", IconKey: "network", Section: "platform", SortOrder: 54, Enabled: true},
		{ID: "network-networkpolicies", ParentID: "network", Path: "/network/networkpolicies", LabelZH: "NetworkPolicies", LabelEN: "NetworkPolicies", IconKey: "network", Section: "platform", SortOrder: 55, Enabled: true},
		{ID: "network-port-forward", ParentID: "network", Path: "/network/port-forward", LabelZH: "端口转发", LabelEN: "Port Forward", IconKey: "network", Section: "platform", SortOrder: 56, Enabled: true},
		{ID: "storage", Path: "/storage", LabelZH: "存储", LabelEN: "Storage", IconKey: "waves", Section: "platform", SortOrder: 60, Enabled: true},
		{ID: "storage-pvc", ParentID: "storage", Path: "/storage/persistentvolumeclaims", LabelZH: "PVC", LabelEN: "PVC", IconKey: "waves", Section: "platform", SortOrder: 51, Enabled: true},
		{ID: "storage-pv", ParentID: "storage", Path: "/storage/persistentvolumes", LabelZH: "PV", LabelEN: "PV", IconKey: "waves", Section: "platform", SortOrder: 52, Enabled: true},
		{ID: "storage-classes", ParentID: "storage", Path: "/storage/storageclasses", LabelZH: "StorageClasses", LabelEN: "StorageClasses", IconKey: "waves", Section: "platform", SortOrder: 53, Enabled: true},
		{ID: "clusters", Path: "/clusters", LabelZH: "集群", LabelEN: "Clusters", IconKey: "globe", Section: "platform", SortOrder: 99, Enabled: true},
		{ID: "monitoring-workbench", Path: "/monitoring-workbench", LabelZH: "监控工作台", LabelEN: "Monitoring Workbench", IconKey: "gauge", Section: "ops", SortOrder: 60, Enabled: true},
		{ID: "monitoring-workbench-overview", ParentID: "monitoring-workbench", Path: "/monitoring-workbench/overview", LabelZH: "工作台概览", LabelEN: "Overview", IconKey: "gauge", Section: "ops", SortOrder: 61, Enabled: true},
		{ID: "monitoring-workbench-rules", ParentID: "monitoring-workbench", Path: "/monitoring-workbench/rules", LabelZH: "告警规则", LabelEN: "Alert Rules", IconKey: "siren", Section: "ops", SortOrder: 62, Enabled: true},
		{ID: "monitoring-workbench-alerts", ParentID: "monitoring-workbench", Path: "/monitoring-workbench/alerts", LabelZH: "活跃告警", LabelEN: "Active Alerts", IconKey: "siren", Section: "ops", SortOrder: 63, Enabled: true},
		{ID: "monitoring-workbench-notifications", ParentID: "monitoring-workbench", Path: "/monitoring-workbench/notifications", LabelZH: "通知策略", LabelEN: "Notification Policies", IconKey: "bell", Section: "ops", SortOrder: 64, Enabled: true},
		{ID: "monitoring-workbench-healing", ParentID: "monitoring-workbench", Path: "/monitoring-workbench/healing", LabelZH: "自愈中心", LabelEN: "Healing Center", IconKey: "activity", Section: "ops", SortOrder: 65, Enabled: true},
		{ID: "monitoring-workbench-oncall", ParentID: "monitoring-workbench", Path: "/monitoring-workbench/oncall", LabelZH: "值班协同", LabelEN: "On-Call Coordination", IconKey: "users", Section: "ops", SortOrder: 66, Enabled: true},
		{ID: "monitoring-workbench-events", ParentID: "monitoring-workbench", Path: "/monitoring-workbench/events", LabelZH: "事件流", LabelEN: "Events", IconKey: "bell", Section: "ops", SortOrder: 67, Enabled: true},
		{ID: "ai-workbench", Path: "/ai-workbench", LabelZH: "AI工作台", LabelEN: "AI Workbench", IconKey: "bot", Section: "ops", SortOrder: 15, Enabled: true},
		{ID: "ai-workbench-investigation", ParentID: "ai-workbench", Path: "/ai-workbench/investigation", LabelZH: "调查工作台", LabelEN: "Investigation Workbench", IconKey: "bot", Section: "ops", SortOrder: 16, Enabled: true},
		{ID: "ai-workbench-operations", ParentID: "ai-workbench", Path: "/ai-workbench/automation", LabelZH: "巡检与自动化", LabelEN: "Automation", IconKey: "bot", Section: "ops", SortOrder: 17, Enabled: true},
		{ID: "ai-workbench-tools", ParentID: "ai-workbench", Path: "/ai-workbench/tools", LabelZH: "工具与技能", LabelEN: "Tools & Skills", IconKey: "bot", Section: "ops", SortOrder: 18, Enabled: true},
		{ID: "builds", Path: "/applications", LabelZH: "应用中心", LabelEN: "Application Center", IconKey: "blocks", Section: "deliver", SortOrder: 110, Enabled: true, Roles: []string{"admin", "ops", "developer"}},
		{ID: "application-management", Path: "/application-management", LabelZH: "应用管理", LabelEN: "Application Management", IconKey: "blocks", Section: "deliver", SortOrder: 111, Enabled: true, Roles: []string{"admin", "ops", "developer"}},
		{ID: "build-templates", Path: "/build-templates", LabelZH: "构建模板", LabelEN: "Build Templates", IconKey: "code", Section: "deliver", SortOrder: 112, Enabled: true, Roles: []string{"admin", "ops"}},
		{ID: "delivery-blueprints", Path: "/delivery/blueprints", LabelZH: "交付蓝图", LabelEN: "Delivery Blueprints", IconKey: "code", Section: "deliver", SortOrder: 112, Enabled: true, Roles: []string{"admin", "ops", "developer"}},
		{ID: "release-bundles", Path: "/delivery/release-bundles", LabelZH: "版本包", LabelEN: "Release Bundles", IconKey: "blocks", Section: "deliver", SortOrder: 113, Enabled: true, Roles: []string{"admin", "ops", "developer"}},
		{ID: "execution-tasks", Path: "/delivery/execution-tasks", LabelZH: "执行任务", LabelEN: "Execution Tasks", IconKey: "activity", Section: "deliver", SortOrder: 114, Enabled: true, Roles: []string{"admin", "ops", "developer"}},
		{ID: "approval-policies", Path: "/delivery/approval-policies", LabelZH: "审批策略", LabelEN: "Approval Policies", IconKey: "shield", Section: "deliver", SortOrder: 115, Enabled: true, Roles: []string{"admin", "ops"}},
		{ID: "workflow-templates", Path: "/workflow-templates", LabelZH: "发布流程模板", LabelEN: "Workflow Templates", IconKey: "activity", Section: "deliver", SortOrder: 116, Enabled: true, Roles: []string{"admin", "ops"}},
		{ID: "release-board", Path: "/release-board", LabelZH: "发布看板", LabelEN: "Release Board", IconKey: "activity", Section: "deliver", SortOrder: 117, Enabled: true, Roles: []string{"admin", "ops", "developer"}},
		{ID: "business-lines", Path: "/business-lines", LabelZH: "业务线管理", LabelEN: "Business Lines", IconKey: "blocks", Section: "catalog", SortOrder: 210, Enabled: true, Roles: []string{"admin", "ops"}},
		{ID: "delivery-environments", Path: "/delivery-environments", LabelZH: "环境管理", LabelEN: "Environments", IconKey: "blocks", Section: "catalog", SortOrder: 220, Enabled: true, Roles: []string{"admin", "ops"}},
		{ID: "application-environments", Path: "/application-environments", LabelZH: "应用环境绑定", LabelEN: "Application Environment Bindings", IconKey: "blocks", Section: "deliver", SortOrder: 111, Enabled: true, Roles: []string{"admin", "ops", "developer"}},
		{ID: "workflows", Path: "/workflows", LabelZH: "工作流", LabelEN: "Workflows", IconKey: "activity", Section: "deliver", SortOrder: 118, Enabled: true, Roles: []string{"admin", "ops", "developer"}},
		{ID: "releases", Path: "/releases", LabelZH: "发布", LabelEN: "Releases", IconKey: "activity", Section: "deliver", SortOrder: 120, Enabled: true, Roles: []string{"admin", "ops", "developer"}},
		{ID: "system", Path: "/system", LabelZH: "系统", LabelEN: "System", IconKey: "panels-top-left", Section: "admin", SortOrder: 225, Enabled: true},
		{ID: "announcements", ParentID: "system", Path: "/system/announcements", LabelZH: "通知公告", LabelEN: "Announcements", IconKey: "megaphone", Section: "admin", SortOrder: 230, Enabled: true, Roles: []string{"admin"}},
		{ID: "access", Path: "/access", LabelZH: "访问控制", LabelEN: "Access Control", IconKey: "shield", Section: "admin", SortOrder: 240, Enabled: true, Roles: []string{"admin"}},
		{ID: "access-users", ParentID: "access", Path: "/access/users", LabelZH: "用户", LabelEN: "Users", IconKey: "shield", Section: "admin", SortOrder: 241, Enabled: true, Roles: []string{"admin"}},
		{ID: "access-roles", ParentID: "access", Path: "/access/roles", LabelZH: "角色", LabelEN: "Roles", IconKey: "shield", Section: "admin", SortOrder: 242, Enabled: true, Roles: []string{"admin"}},
		{ID: "access-teams", ParentID: "access", Path: "/access/teams", LabelZH: "用户组", LabelEN: "User Groups", IconKey: "shield", Section: "admin", SortOrder: 243, Enabled: true, Roles: []string{"admin"}},
		{ID: "access-policies", ParentID: "access", Path: "/access/policies", LabelZH: "策略", LabelEN: "Policies", IconKey: "shield", Section: "admin", SortOrder: 244, Enabled: true, Roles: []string{"admin"}},
		{ID: "menus", ParentID: "system", Path: "/system/menus", LabelZH: "菜单管理", LabelEN: "Menu Management", IconKey: "menu-square", Section: "admin", SortOrder: 250, Enabled: true, Roles: []string{"admin"}},
		{ID: "system-online-users", ParentID: "system", Path: "/system/online-users", LabelZH: "在线用户", LabelEN: "Online Users", IconKey: "users", Section: "admin", SortOrder: 256, Enabled: true, Roles: []string{"admin"}},
		{ID: "operations", ParentID: "system", Path: "/system/operations", LabelZH: "操作", LabelEN: "Operations", IconKey: "clipboard-list", Section: "admin", SortOrder: 257, Enabled: true},
		{ID: "audit", ParentID: "system", Path: "/system/audit", LabelZH: "审计", LabelEN: "Audit", IconKey: "file-clock", Section: "admin", SortOrder: 258, Enabled: true},
		{ID: "registries", Path: "/registries", LabelZH: "镜像仓库", LabelEN: "Registry Connections", IconKey: "menu-square", Section: "deliver", SortOrder: 121, Enabled: true, Roles: []string{"admin", "ops"}},
		{ID: "settings", Path: "/settings", LabelZH: "设置", LabelEN: "Settings", IconKey: "cog", Section: "admin", SortOrder: 260, Enabled: true, Roles: []string{"admin"}},
	}
}

func deprecatedMenuIDs() []string {
	return []string{
		"assistant-root-cause",
		"assistant-performance",
		"assistant-chat",
		"assistant-inspection",
		"network-http-routes",
		"observability",
		"monitoring",
		"rules",
		"alerts",
		"notifications",
		"healing",
		"oncall",
		"assistant",
		"assistant-workbench",
		"assistant-operations",
		"assistant-tools",
		"events",
	}
}

func validateMenuSeeds(items []menuSeed) error {
	ids := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, exists := ids[item.ID]; exists {
			return fmt.Errorf("duplicate menu seed id %q", item.ID)
		}
		ids[item.ID] = struct{}{}
	}
	for _, item := range items {
		if item.ParentID == "" {
			continue
		}
		if _, exists := ids[item.ParentID]; !exists {
			return fmt.Errorf("menu seed %q references missing parent %q", item.ID, item.ParentID)
		}
	}
	return nil
}

func seedMenus(ctx context.Context, db *gorm.DB, modules cfgpkg.ModulesConfig) error {
	now := time.Now().UTC()
	items := defaultMenuSeeds()
	if err := validateMenuSeeds(items); err != nil {
		return err
	}
	allItems := append([]menuSeed(nil), items...)
	items = filterSeedMenusByModules(items, modules)
	if err := upsertMenus(ctx, db, items, now); err != nil {
		return err
	}
	if err := deleteDisabledModuleMenus(ctx, db, allItems, modules); err != nil {
		return err
	}
	menuIDs := make([]string, 0, len(items))
	roleBindingValues := make([][]string, 0)
	for _, item := range items {
		menuIDs = append(menuIDs, item.ID)
		for _, roleID := range item.Roles {
			roleBindingValues = append(roleBindingValues, []string{fmt.Sprintf("%s:%s", item.ID, roleID), item.ID, roleID})
		}
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM menu_role_bindings WHERE menu_id IN ?`, menuIDs).Error; err != nil {
		return err
	}
	deprecatedIDs := deprecatedMenuIDs()
	if err := db.WithContext(ctx).Exec(`DELETE FROM menu_role_bindings WHERE menu_id IN ?`, deprecatedIDs).Error; err != nil {
		return err
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM menus WHERE id IN ?`, deprecatedIDs).Error; err != nil {
		return err
	}
	if len(roleBindingValues) > 0 {
		if err := insertMenuRoleBindings(ctx, db, roleBindingValues, now); err != nil {
			return err
		}
	}
	return nil
}

func nullableMenu(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func filterSeedMenusByModules(items []menuSeed, modules cfgpkg.ModulesConfig) []menuSeed {
	filtered := make([]menuSeed, 0, len(items))
	for _, item := range items {
		switch {
		case !modules.Delivery.Enabled && isDeliveryMenuSeed(item):
			continue
		case !modules.Monitoring.Enabled && isMonitoringMenuSeed(item):
			continue
		case !modules.AI.Enabled && isAIMenuSeed(item):
			continue
		default:
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func deleteDisabledModuleMenus(ctx context.Context, db *gorm.DB, items []menuSeed, modules cfgpkg.ModulesConfig) error {
	menuIDs := make([]string, 0)
	for _, item := range items {
		switch {
		case !modules.Delivery.Enabled && isDeliveryMenuSeed(item):
			menuIDs = append(menuIDs, item.ID)
		case !modules.Monitoring.Enabled && isMonitoringMenuSeed(item):
			menuIDs = append(menuIDs, item.ID)
		case !modules.AI.Enabled && isAIMenuSeed(item):
			menuIDs = append(menuIDs, item.ID)
		}
	}
	if len(menuIDs) == 0 {
		return nil
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM menu_role_bindings WHERE menu_id IN ?`, menuIDs).Error; err != nil {
		return err
	}
	return db.WithContext(ctx).Exec(`DELETE FROM menus WHERE id IN ?`, menuIDs).Error
}

func isDeliveryMenuSeed(item menuSeed) bool {
	return strings.HasPrefix(item.Path, "/applications") ||
		strings.HasPrefix(item.Path, "/application-management") ||
		strings.HasPrefix(item.Path, "/business-lines") ||
		strings.HasPrefix(item.Path, "/delivery-environments") ||
		strings.HasPrefix(item.Path, "/application-environments") ||
		strings.HasPrefix(item.Path, "/build-templates") ||
		strings.HasPrefix(item.Path, "/delivery/blueprints") ||
		strings.HasPrefix(item.Path, "/delivery/release-bundles") ||
		strings.HasPrefix(item.Path, "/delivery/execution-tasks") ||
		strings.HasPrefix(item.Path, "/delivery/approval-policies") ||
		strings.HasPrefix(item.Path, "/workflow-templates") ||
		strings.HasPrefix(item.Path, "/release-board") ||
		strings.HasPrefix(item.Path, "/workflows") ||
		strings.HasPrefix(item.Path, "/releases") ||
		strings.HasPrefix(item.Path, "/registries")
}

func isMonitoringMenuSeed(item menuSeed) bool {
	return item.ID == "monitoring-workbench" ||
		strings.HasPrefix(item.Path, "/monitoring-workbench")
}

func isAIMenuSeed(item menuSeed) bool {
	return item.ID == "ai-workbench" ||
		strings.HasPrefix(item.Path, "/ai-workbench")
}

func upsertMenus(ctx context.Context, db *gorm.DB, items []menuSeed, now time.Time) error {
	if len(items) == 0 {
		return nil
	}
	var builder strings.Builder
	args := make([]any, 0, len(items)*11)
	builder.WriteString(`
		INSERT INTO menus (id, parent_id, path, label_zh, label_en, icon_key, section, sort_order, enabled, created_at, updated_at)
		VALUES
	`)
	for index, item := range items {
		if index > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(" (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(args, item.ID, nullableMenu(item.ParentID), item.Path, item.LabelZH, item.LabelEN, item.IconKey, item.Section, item.SortOrder, item.Enabled, now, now)
	}
	builder.WriteString(`
		ON CONFLICT (id) DO NOTHING
	`)
	return db.WithContext(ctx).Exec(builder.String(), args...).Error
}

func insertMenuRoleBindings(ctx context.Context, db *gorm.DB, values [][]string, now time.Time) error {
	var builder strings.Builder
	args := make([]any, 0, len(values)*5)
	builder.WriteString(`
		INSERT INTO menu_role_bindings (id, menu_id, role_id, created_at, updated_at)
		VALUES
	`)
	for index, value := range values {
		if index > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(" (?, ?, ?, ?, ?)")
		args = append(args, value[0], value[1], value[2], now, now)
	}
	builder.WriteString(`
		ON CONFLICT (menu_id, role_id) DO UPDATE SET updated_at = EXCLUDED.updated_at
	`)
	return db.WithContext(ctx).Exec(builder.String(), args...).Error
}

func upsertPolicies(ctx context.Context, db *gorm.DB, items []policySeed, now time.Time) error {
	if len(items) == 0 {
		return nil
	}
	var builder strings.Builder
	args := make([]any, 0, len(items)*11)
	builder.WriteString(`
		INSERT INTO policies (id, name, effect, priority, subjects, targets, actions, conditions, reason, created_at, updated_at)
		VALUES
	`)
	for index, item := range items {
		if index > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(" (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(args, item.ID, item.Name, item.Effect, item.Priority, item.Subjects, item.Targets, item.Actions, item.Conditions, item.Reason, now, now)
	}
	builder.WriteString(`
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			effect = EXCLUDED.effect,
			priority = EXCLUDED.priority,
			subjects = EXCLUDED.subjects,
			targets = EXCLUDED.targets,
			actions = EXCLUDED.actions,
			conditions = EXCLUDED.conditions,
			reason = EXCLUDED.reason,
			updated_at = EXCLUDED.updated_at
	`)
	return db.WithContext(ctx).Exec(builder.String(), args...).Error
}

func insertUserRoleBindings(ctx context.Context, db *gorm.DB, values [][]string, now time.Time) error {
	if len(values) == 0 {
		return nil
	}
	var builder strings.Builder
	args := make([]any, 0, len(values)*5)
	builder.WriteString(`
		INSERT INTO user_role_bindings (id, user_id, role_id, scope, created_at, updated_at)
		VALUES
	`)
	for index, value := range values {
		if index > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(" (?, ?, ?, '{}', ?, ?)")
		args = append(args, value[0], value[1], value[2], now, now)
	}
	builder.WriteString(`
		ON CONFLICT (user_id, role_id) DO UPDATE SET updated_at = EXCLUDED.updated_at
	`)
	return db.WithContext(ctx).Exec(builder.String(), args...).Error
}

func upsertRoles(ctx context.Context, db *gorm.DB, items []roleSeed, now time.Time) error {
	if len(items) == 0 {
		return nil
	}
	var builder strings.Builder
	args := make([]any, 0, len(items)*6)
	builder.WriteString(`
		INSERT INTO roles (id, name, scope, capabilities, permission_keys, created_at, updated_at)
		VALUES
	`)
	for index, item := range items {
		if index > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(" (?, ?, 'system', ?, ?, ?, ?)")
		args = append(args, item.ID, item.Name, item.Capabilities, item.PermissionKeys, now, now)
	}
	builder.WriteString(`
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			capabilities = EXCLUDED.capabilities,
			permission_keys = EXCLUDED.permission_keys,
			updated_at = EXCLUDED.updated_at
	`)
	return db.WithContext(ctx).Exec(builder.String(), args...).Error
}

func upsertDeliveryEnvironments(ctx context.Context, db *gorm.DB, items []environmentSeed, now time.Time) error {
	if len(items) == 0 {
		return nil
	}
	var builder strings.Builder
	args := make([]any, 0, len(items)*11)
	builder.WriteString(`
		INSERT INTO delivery_environments (id, environment_key, name, tier, stage_level, sort_order, is_production, requires_approval, enabled, created_at, updated_at)
		VALUES
	`)
	for index, item := range items {
		if index > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(" (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(args, item.ID, item.Key, item.Name, item.Tier, item.StageLevel, item.SortOrder, item.IsProduction, item.RequiresApproval, item.Enabled, now, now)
	}
	builder.WriteString(`
		ON CONFLICT (id) DO UPDATE SET
			environment_key = EXCLUDED.environment_key,
			name = EXCLUDED.name,
			tier = EXCLUDED.tier,
			stage_level = EXCLUDED.stage_level,
			sort_order = EXCLUDED.sort_order,
			is_production = EXCLUDED.is_production,
			requires_approval = EXCLUDED.requires_approval,
			enabled = EXCLUDED.enabled,
			updated_at = EXCLUDED.updated_at
	`)
	return db.WithContext(ctx).Exec(builder.String(), args...).Error
}

func upsertClusters(ctx context.Context, db *gorm.DB, items []clusterSeed, now time.Time) error {
	if len(items) == 0 {
		return nil
	}
	var builder strings.Builder
	args := make([]any, 0, len(items)*7)
	builder.WriteString(`
		INSERT INTO clusters (id, name, region, environment, labels, connection_mode, version, capabilities, health_snapshot, created_at, updated_at)
		VALUES
	`)
	for index, item := range items {
		if index > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(" (?, ?, ?, ?, ?, 'direct_kubeconfig', '', '[]', '{}', ?, ?)")
		args = append(args, item.ID, item.Name, item.Region, item.Environment, item.Labels, now, now)
	}
	builder.WriteString(`
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			region = EXCLUDED.region,
			environment = EXCLUDED.environment,
			labels = EXCLUDED.labels,
			connection_mode = EXCLUDED.connection_mode,
			updated_at = EXCLUDED.updated_at
	`)
	return db.WithContext(ctx).Exec(builder.String(), args...).Error
}

func upsertClusterCredentials(ctx context.Context, db *gorm.DB, items []clusterCredentialSeed, now time.Time) error {
	if len(items) == 0 {
		return nil
	}
	var builder strings.Builder
	args := make([]any, 0, len(items)*6)
	builder.WriteString(`
		INSERT INTO cluster_credentials_meta (id, cluster_id, credential_type, source_type, source_ref, metadata, created_at, updated_at)
		VALUES
	`)
	for index, item := range items {
		if index > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(" (?, ?, 'kubeconfig', 'config', ?, ?, ?, ?)")
		args = append(args, item.ID, item.ClusterID, item.SourceRef, item.Metadata, now, now)
	}
	builder.WriteString(`
		ON CONFLICT (id) DO UPDATE SET
			credential_type = EXCLUDED.credential_type,
			source_type = EXCLUDED.source_type,
			source_ref = EXCLUDED.source_ref,
			metadata = EXCLUDED.metadata,
			updated_at = EXCLUDED.updated_at
	`)
	return db.WithContext(ctx).Exec(builder.String(), args...).Error
}

func seedUser(ctx context.Context, db *gorm.DB, cfg cfgpkg.Config) error {
	if strings.TrimSpace(cfg.Auth.DevPrincipal.UserID) == "" {
		return nil
	}
	now := time.Now().UTC()
	if err := db.WithContext(ctx).Exec(`
		INSERT INTO users (id, username, email, display_name, status, tags, preferences, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'active', ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			username = EXCLUDED.username,
			email = EXCLUDED.email,
			display_name = EXCLUDED.display_name,
			updated_at = EXCLUDED.updated_at
	`,
		cfg.Auth.DevPrincipal.UserID,
		cfg.Auth.DevPrincipal.UserID,
		cfg.Auth.DevPrincipal.Email,
		cfg.Auth.DevPrincipal.Name,
		`[]`,
		`{}`,
		now,
		now,
	).Error; err != nil {
		return err
	}

	if strings.TrimSpace(cfg.Auth.DevPrincipal.Password) != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(cfg.Auth.DevPrincipal.Password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash bootstrap password: %w", err)
		}
		if err := db.WithContext(ctx).Exec(`
			INSERT INTO user_password_credentials (user_id, password_hash, password_updated_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (user_id) DO UPDATE SET
				password_hash = EXCLUDED.password_hash,
				password_updated_at = EXCLUDED.password_updated_at,
				updated_at = EXCLUDED.updated_at
		`, cfg.Auth.DevPrincipal.UserID, string(hash), now, now, now).Error; err != nil {
			return err
		}
	}

	if len(cfg.Auth.DevPrincipal.Roles) > 0 {
		roleBindingValues := make([][]string, 0, len(cfg.Auth.DevPrincipal.Roles))
		for _, roleID := range cfg.Auth.DevPrincipal.Roles {
			roleBindingValues = append(roleBindingValues, []string{
				fmt.Sprintf("%s:%s", cfg.Auth.DevPrincipal.UserID, roleID),
				cfg.Auth.DevPrincipal.UserID,
				roleID,
			})
		}
		if err := insertUserRoleBindings(ctx, db, roleBindingValues, now); err != nil {
			return err
		}
	}
	return nil
}

func seedRoles(ctx context.Context, db *gorm.DB) error {
	now := time.Now().UTC()
	items := make([]roleSeed, 0, len(accessapp.RoleMatrix()))
	for name, actions := range accessapp.RoleMatrix() {
		capabilities, err := json.Marshal(actions)
		if err != nil {
			return fmt.Errorf("marshal capabilities for role %s: %w", name, err)
		}
		permissionKeys, err := json.Marshal(accessapp.PermissionKeysForRoles([]string{name}))
		if err != nil {
			return fmt.Errorf("marshal permission keys for role %s: %w", name, err)
		}
		items = append(items, roleSeed{ID: name, Name: name, Capabilities: string(capabilities), PermissionKeys: string(permissionKeys)})
	}
	return upsertRoles(ctx, db, items, now)
}

func seedPolicies(ctx context.Context, db *gorm.DB) error {
	now := time.Now().UTC()
	items := make([]policySeed, 0, len(accessapp.DefaultPolicies()))
	for _, policy := range accessapp.DefaultPolicies() {
		subjects, _ := json.Marshal(policy.Subjects)
		targets, _ := json.Marshal(map[string]any{"clusters": policy.Clusters, "namespaces": policy.Namespaces, "resources": policy.Resources})
		actions, _ := json.Marshal(policy.Actions)
		conditions, _ := json.Marshal(policy.Conditions)
		items = append(items, policySeed{
			ID:         policy.ID,
			Name:       policy.Name,
			Effect:     string(policy.Effect),
			Priority:   policy.Priority,
			Subjects:   string(subjects),
			Targets:    string(targets),
			Actions:    string(actions),
			Conditions: string(conditions),
			Reason:     policy.Reason,
		})
	}
	return upsertPolicies(ctx, db, items, now)
}

func seedDeliveryCatalog(ctx context.Context, db *gorm.DB) error {
	now := time.Now().UTC()
	items := []environmentSeed{
		{ID: "env-dev", Key: "dev", Name: "开发", Tier: "dev", StageLevel: 10, SortOrder: 10, Enabled: true},
		{ID: "env-test", Key: "test", Name: "测试", Tier: "test", StageLevel: 20, SortOrder: 20, Enabled: true},
		{ID: "env-pre", Key: "pre", Name: "预发", Tier: "pre", StageLevel: 30, SortOrder: 30, RequiresApproval: true, Enabled: true},
		{ID: "env-prod", Key: "prod", Name: "生产", Tier: "prod", StageLevel: 40, SortOrder: 40, IsProduction: true, RequiresApproval: true, Enabled: true},
		{ID: "env-local-prod", Key: "local-prod", Name: "本地生产", Tier: "local-prod", StageLevel: 50, SortOrder: 50, IsProduction: true, RequiresApproval: true, Enabled: true},
	}
	return upsertDeliveryEnvironments(ctx, db, items, now)
}

func seedWorkflowTemplates(ctx context.Context, db *gorm.DB) error {
	now := time.Now().UTC()
	definition, _ := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mode":          "release_dag",
		"nodes": []map[string]any{
			{
				"id":                "approval",
				"name":              "审批",
				"type":              "manual_approval",
				"timeoutSeconds":    300,
				"continueOnFailure": false,
				"position":          map[string]any{"x": 120, "y": 120},
				"config":            map[string]any{"approverRoles": []string{"release-manager"}, "required": true},
			},
			{
				"id":                "deploy",
				"name":              "更新镜像",
				"type":              "deploy_update_image",
				"timeoutSeconds":    300,
				"continueOnFailure": false,
				"position":          map[string]any{"x": 420, "y": 120},
				"config":            map[string]any{"targetRef": "primary", "imageTagSource": "workflow_input"},
			},
			{
				"id":                "rollout",
				"name":              "等待 Rollout",
				"type":              "wait_rollout",
				"timeoutSeconds":    300,
				"continueOnFailure": false,
				"position":          map[string]any{"x": 720, "y": 120},
				"config":            map[string]any{"timeoutSeconds": 300},
			},
			{
				"id":                "verify",
				"name":              "HTTP 检查",
				"type":              "check_http",
				"timeoutSeconds":    300,
				"continueOnFailure": false,
				"position":          map[string]any{"x": 1020, "y": 120},
				"config":            map[string]any{"url": "", "expectedStatus": 200},
			},
			{
				"id":                "rollback",
				"name":              "失败回滚",
				"type":              "rollback_to_previous",
				"timeoutSeconds":    300,
				"continueOnFailure": false,
				"position":          map[string]any{"x": 720, "y": 360},
				"config":            map[string]any{},
			},
			{
				"id":                "notify",
				"name":              "发送通知",
				"type":              "notify",
				"timeoutSeconds":    60,
				"continueOnFailure": true,
				"position":          map[string]any{"x": 1020, "y": 360},
				"config":            map[string]any{"channel": "", "template": "release-result"},
			},
		},
		"edges": []map[string]any{
			{"id": "edge-approval-deploy", "source": "approval", "target": "deploy", "condition": "success"},
			{"id": "edge-deploy-rollout", "source": "deploy", "target": "rollout", "condition": "success"},
			{"id": "edge-rollout-verify", "source": "rollout", "target": "verify", "condition": "success"},
			{"id": "edge-rollout-rollback", "source": "rollout", "target": "rollback", "condition": "failure"},
			{"id": "edge-verify-notify", "source": "verify", "target": "notify", "condition": "success"},
			{"id": "edge-rollback-notify", "source": "rollback", "target": "notify", "condition": "always"},
		},
	})
	return db.WithContext(ctx).Exec(`
		INSERT INTO workflow_templates (id, template_key, name, description, category, definition, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			template_key = EXCLUDED.template_key,
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			category = EXCLUDED.category,
			definition = EXCLUDED.definition,
			enabled = EXCLUDED.enabled,
			updated_at = EXCLUDED.updated_at
	`, "wf-build-release-verify", "build-release-verify", "Build Release Verify", "默认的构建-发布-校验模板", "default", string(definition), true, now, now).Error
}

func seedClusters(ctx context.Context, db *gorm.DB, clusters []cfgpkg.ClusterConfig) error {
	now := time.Now().UTC()
	clusterItems := make([]clusterSeed, 0, len(clusters))
	credentialItems := make([]clusterCredentialSeed, 0, len(clusters))
	for _, cluster := range clusters {
		labels, _ := json.Marshal(cluster.Labels)
		clusterItems = append(clusterItems, clusterSeed{
			ID:          cluster.ID,
			Name:        cluster.Name,
			Region:      cluster.Region,
			Environment: cluster.Environment,
			Labels:      string(labels),
		})
		metadata, _ := json.Marshal(map[string]any{
			"kubeconfig":               cluster.Kubeconfig,
			"kubeconfig_data":          cluster.KubeconfigData,
			"context":                  cluster.Context,
			"source_ref":               "configs/config.yaml",
			"prometheus_url":           cluster.PrometheusURL,
			"prometheus_bearer_token":  cluster.PrometheusBearerToken,
			"prometheus_cluster_label": cluster.PrometheusClusterLabel,
			"grafana_base_url":         cluster.GrafanaBaseURL,
		})
		credentialItems = append(credentialItems, clusterCredentialSeed{
			ID:        fmt.Sprintf("%s:primary", cluster.ID),
			ClusterID: cluster.ID,
			SourceRef: "configs/config.yaml",
			Metadata:  string(metadata),
		})
	}
	if err := upsertClusters(ctx, db, clusterItems, now); err != nil {
		return err
	}
	if err := upsertClusterCredentials(ctx, db, credentialItems, now); err != nil {
		return err
	}
	return nil
}
