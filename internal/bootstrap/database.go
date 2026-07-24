package bootstrap

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	accessapp "github.com/opensoha/soha/internal/application/access"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	dbinfra "github.com/opensoha/soha/internal/infrastructure/db"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

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
const bootstrapSeedVersion = "2026-07-24-settings-overview-v6"

const bootstrapSeedVersionKey = "bootstrap.seed_version"

func seedDefaults(ctx context.Context, store *dbinfra.Store, cfg cfgpkg.Config) error {
	storedVersion, err := readBootstrapSeedVersion(ctx, store.DB())
	if err != nil {
		return err
	}
	if storedVersion == bootstrapSeedVersion {
		return cleanupDeprecatedMenus(ctx, store.DB(), obsoleteMenuIDsForCleanup())
	}

	return store.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		storedVersion, err := readBootstrapSeedVersion(ctx, tx)
		if err != nil {
			return err
		}
		if storedVersion == bootstrapSeedVersion {
			return cleanupDeprecatedMenus(ctx, tx, obsoleteMenuIDsForCleanup())
		}
		if err := seedRoles(ctx, tx); err != nil {
			return err
		}
		if err := seedMenus(ctx, tx, cfg.Modules); err != nil {
			return err
		}
		if err := syncBuiltinMenuSeedUpgrades(ctx, tx); err != nil {
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

func syncDisabledModuleMenus(ctx context.Context, db *gorm.DB, modules cfgpkg.ModulesConfig) error {
	items := defaultMenuSeeds()
	if err := validateMenuSeeds(items); err != nil {
		return err
	}
	if err := deleteDisabledModuleMenus(ctx, db, items, modules); err != nil {
		return err
	}
	return cleanupDeprecatedMenus(ctx, db, obsoleteMenuIDsForCleanup())
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
		if errors.Is(err, sql.ErrNoRows) {
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
	userID := strings.TrimSpace(cfg.Auth.DevPrincipal.UserID)
	if userID == "" {
		return nil
	}
	username := devPrincipalUsername(cfg)
	email := strings.TrimSpace(cfg.Auth.DevPrincipal.Email)
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
		userID,
		username,
		email,
		cfg.Auth.DevPrincipal.Name,
		`[]`,
		`{}`,
		now,
		now,
	).Error; err != nil {
		return err
	}

	if err := seedUserPassword(ctx, db, cfg, userID, now); err != nil {
		return err
	}

	if len(cfg.Auth.DevPrincipal.Roles) > 0 {
		roleBindingValues := make([][]string, 0, len(cfg.Auth.DevPrincipal.Roles))
		for _, roleID := range cfg.Auth.DevPrincipal.Roles {
			roleBindingValues = append(roleBindingValues, []string{
				fmt.Sprintf("%s:%s", userID, roleID),
				userID,
				roleID,
			})
		}
		if err := insertUserRoleBindings(ctx, db, roleBindingValues, now); err != nil {
			return err
		}
	}
	return nil
}

func seedUserPassword(ctx context.Context, db *gorm.DB, cfg cfgpkg.Config, userID string, now time.Time) error {
	password := strings.TrimSpace(cfg.Auth.DevPrincipal.Password)
	if password == "" {
		return nil
	}

	var existingHash string
	err := db.WithContext(ctx).Raw(`
		SELECT password_hash FROM user_password_credentials WHERE user_id = ? LIMIT 1
	`, userID).Row().Scan(&existingHash)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash bootstrap password: %w", err)
		}
		if err := db.WithContext(ctx).Exec(`
			INSERT INTO user_password_credentials (user_id, password_hash, password_updated_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (user_id) DO NOTHING
		`, userID, string(hash), now, now, now).Error; err != nil {
			return fmt.Errorf("insert bootstrap password: %w", err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("inspect bootstrap password: %w", err)
	default:
		return nil
	}
}

func devPrincipalUsername(cfg cfgpkg.Config) string {
	email := strings.TrimSpace(cfg.Auth.DevPrincipal.Email)
	if local, _, ok := strings.Cut(email, "@"); ok && strings.TrimSpace(local) != "" {
		return strings.TrimSpace(local)
	}
	return strings.TrimSpace(cfg.Auth.DevPrincipal.UserID)
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
