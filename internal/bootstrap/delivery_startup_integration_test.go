package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appdelivery "github.com/opensoha/soha/internal/application/delivery"
	apptemplateupdate "github.com/opensoha/soha/internal/application/templateupdate"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	dbinfra "github.com/opensoha/soha/internal/infrastructure/db"
	resourcebackendinfra "github.com/opensoha/soha/internal/infrastructure/resourcebackend"
	"github.com/opensoha/soha/internal/platform/apperrors"
	userrepo "github.com/opensoha/soha/internal/repository/user"
	repoworkflow "github.com/opensoha/soha/internal/repository/workflow"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	deliveryStartupPhaseEnv = "TEST_DELIVERY_STARTUP_PHASE"
	deliveryStartupReport   = "DELIVERY_STARTUP_REPORT "
	deliveryStartupBinding  = "startup-binding"
	deliveryStartupApp      = "startup-app"
	deliveryStartupActor    = "64bb4bcf-304d-4855-afbf-995712a82924"
)

type deliveryStartupResult struct {
	PID          int
	RunID        string
	BatchID      string
	CandidateID  string
	Starts       [6]int
	Claims       int32
	ConfigWrites int32
	RunnerCalls  int32
	FirstReady   bool
	StartupError string
}

func TestDeliveryStartupWithPostgres(t *testing.T) {
	if phase := os.Getenv(deliveryStartupPhaseEnv); phase != "" {
		runDeliveryStartupChild(t, phase)
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	store, database := deliveryStartupTestStore(t, ctx)

	configFile := deliveryStartupConfigFile(t, database)
	pids := map[int]bool{os.Getpid(): true}
	runChild := func(t *testing.T, phase string, seed deliveryStartupResult) deliveryStartupResult {
		t.Helper()
		result := invokeDeliveryStartupChild(t, ctx, phase, configFile, database, seed)
		require.Positive(t, result.PID)
		require.False(t, pids[result.PID], "phase %q reused a process", phase)
		pids[result.PID] = true
		require.Zero(t, result.RunnerCalls)
		return result
	}

	seed := runChild(t, "seed", deliveryStartupResult{})
	require.NotEmpty(t, seed.RunID)
	require.NotEmpty(t, seed.BatchID)
	require.NotEmpty(t, seed.CandidateID)
	repo := repoworkflow.New(store.DB())
	queued, err := repo.Get(ctx, seed.RunID)
	require.NoError(t, err)
	require.Equal(t, "queued", queued.Status)
	require.Zero(t, queued.FencingToken)
	requireDeliveryStartupNodes(t, queued.NodeRuns, [3]string{"pending", "pending", "pending"}, [3]string{})

	heldResult := runChild(t, "hold", seed)
	requireDeliveryStartupReady(t, heldResult)
	require.EqualValues(t, 3, heldResult.Claims)
	require.Zero(t, heldResult.ConfigWrites)
	held, err := repo.Get(ctx, seed.RunID)
	require.NoError(t, err)
	require.Equal(t, seed.BatchID, held.DeliveryBatchID)
	require.Equal(t, "running", held.Status)
	require.EqualValues(t, 3, held.FencingToken)
	require.NotEmpty(t, held.LeaseOwner)
	require.NotNil(t, held.LeaseUntil)
	require.True(t, held.LeaseUntil.After(time.Now()), "shutdown must leave the outstanding lease to expire")
	var planID string
	for _, node := range held.NodeRuns {
		if node.Stage == "plan" {
			require.Equal(t, "completed", node.Status)
			planID = node.DeliveryPlanID
		}
	}
	require.NotEmpty(t, planID, "the Plan stage must have persisted its configuration Plan")
	requireDeliveryStartupNodes(t, held.NodeRuns, [3]string{"completed", "pending", "completed"}, [3]string{planID, "", ""})
	_, err = repo.ClaimManagedRun(ctx, "before-expiration", time.Minute)
	require.ErrorIs(t, err, apperrors.ErrNotFound)

	// Simulate the exact lease reaching its existing expiry boundary. The
	// blocked processes now face eligible work, and the replacement must use
	// production claim SQL to take ownership.
	expired := store.DB().WithContext(ctx).Exec(`UPDATE workflow_runs SET lease_until=NOW()-INTERVAL '1 second'
		WHERE id=? AND lease_owner=? AND fencing_token=? AND version=?`, held.ID, held.LeaseOwner, held.FencingToken, held.Version)
	require.NoError(t, expired.Error)
	require.EqualValues(t, 1, expired.RowsAffected)
	heldState := deliveryStartupState(t, ctx, store.DB())

	for _, test := range []struct{ phase, errorText string }{
		{"missing_template_updates", "delivery runtime requires template updates"},
		{"typed_nil_template_updates", "delivery runtime requires template updates"},
		{"missing_principals", "delivery workflow requires principals"},
	} {
		t.Run(test.phase, func(t *testing.T) {
			result := runChild(t, test.phase, seed)
			require.Contains(t, result.StartupError, "start delivery workers: "+test.errorText)
			require.Equal(t, [6]int{}, result.Starts)
			require.Zero(t, result.Claims)
			require.Zero(t, result.ConfigWrites)
			require.Equal(t, heldState, deliveryStartupState(t, ctx, store.DB()))
		})
	}

	t.Run("restored_dependencies_resume_same_run", func(t *testing.T) {
		result := runChild(t, "recover", seed)
		requireDeliveryStartupReady(t, result)
		require.EqualValues(t, 1, result.ConfigWrites)
		requireDeliveryStartupCompleted(t, ctx, store.DB(), seed, held, planID)
	})

	t.Run("completed_receipt_does_not_replay", func(t *testing.T) {
		completed := deliveryStartupState(t, ctx, store.DB())
		result := runChild(t, "replay", seed)
		requireDeliveryStartupReady(t, result)
		require.GreaterOrEqual(t, result.Claims, int32(2), "wait for one complete terminal-state poll")
		require.Zero(t, result.ConfigWrites)
		require.Equal(t, completed, deliveryStartupState(t, ctx, store.DB()))
	})
}

func requireDeliveryStartupCompleted(t *testing.T, ctx context.Context, db *gorm.DB, seed deliveryStartupResult, held domainworkflow.Run, planID string) {
	t.Helper()
	finished, err := repoworkflow.New(db).Get(ctx, seed.RunID)
	require.NoError(t, err)
	require.Equal(t, "completed", finished.Status)
	require.Equal(t, seed.BatchID, finished.DeliveryBatchID)
	require.Greater(t, finished.FencingToken, held.FencingToken)
	require.Greater(t, finished.Version, held.Version)
	require.Empty(t, finished.LeaseOwner)
	require.Nil(t, finished.LeaseUntil)
	requireDeliveryStartupNodes(t, finished.NodeRuns, [3]string{"completed", "completed", "completed"}, [3]string{planID, planID, ""})

	var receipt, status string
	db = db.WithContext(ctx)
	require.NoError(t, db.Raw(`SELECT c.applied_plan_id,p.status FROM template_update_candidates c
		JOIN delivery_plans p ON p.id=c.applied_plan_id AND p.template_candidate_id=c.id
		WHERE c.id=? AND p.confirmed_at IS NOT NULL`, seed.CandidateID).Row().Scan(&receipt, &status))
	require.Equal(t, planID, receipt)
	require.Equal(t, string(domaindelivery.DeliveryPlanStatusConfirmed), status)

	var version int64
	require.NoError(t, db.Raw(`SELECT workflow_template_version FROM application_environments WHERE id=?`, deliveryStartupBinding).Row().Scan(&version))
	require.EqualValues(t, 2, version)
	for table, want := range map[string]int64{"delivery_batches": 1, "workflow_runs": 1, "delivery_plans": 1, "template_update_candidates": 1, "execution_tasks": 0} {
		var count int64
		require.NoError(t, db.Table(table).Count(&count).Error)
		require.Equal(t, want, count, table)
	}
}

func requireDeliveryStartupNodes(t *testing.T, nodes []domainworkflow.NodeRun, statuses, planIDs [3]string) {
	t.Helper()
	// Build-all batches retain their build barrier even for config-only targets.
	// The barrier runs first and has no delivery Plan or execution task.
	expected := []struct{ id, target, stage string }{
		{"adopt-1:plan", "adopt-1", "plan"},
		{"adopt-1:deploy", "adopt-1", "deploy"},
		{"builds:barrier", "", "barrier"},
	}
	require.Len(t, nodes, len(expected))
	for index, want := range expected {
		node := nodes[index]
		require.Equal(t, want.id, node.NodeID)
		require.Equal(t, want.id, node.Name)
		require.Equal(t, "delivery_stage", node.Type)
		require.Equal(t, want.target, node.TargetID)
		require.Equal(t, want.stage, node.Stage)
		require.Equal(t, statuses[index], node.Status, node.NodeID)
		require.Equal(t, planIDs[index], node.DeliveryPlanID, node.NodeID)
		require.Empty(t, node.ExecutionTaskID, node.NodeID)
	}
}

func requireDeliveryStartupReady(t *testing.T, result deliveryStartupResult) {
	t.Helper()
	require.Empty(t, result.StartupError)
	require.Equal(t, [6]int{1, 1, 1, 1, 1, 1}, result.Starts)
	require.Positive(t, result.Claims)
	require.True(t, result.FirstReady, "the actual first claim did not see the bound runtime")
}

func deliveryStartupTestStore(t *testing.T, ctx context.Context) (*dbinfra.Store, cfgpkg.DatabaseConfig) {
	t.Helper()
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_CATALOG_TEST_POSTGRES_PORT to this run's isolated PostgreSQL")
	}
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)

	database := deliveryStartupDatabase(port, "postgres")
	admin, err := dbinfra.New(database, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, admin.Close()) })
	database.Name = "soha_delivery_startup_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NoError(t, admin.Exec(ctx, "CREATE DATABASE "+database.Name))
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		require.NoError(t, admin.Exec(cleanup, "DROP DATABASE "+database.Name))
	})
	store, err := dbinfra.New(database, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.NoError(t, store.MigrateFromFile(ctx, filepath.Join("..", "..", "migrations", "postgres")))

	return store, database
}

func deliveryStartupDatabase(port int, name string) cfgpkg.DatabaseConfig {
	return cfgpkg.DatabaseConfig{
		Driver: "postgres", Host: "127.0.0.1", Port: port, Name: name, User: "pgsql",
		Password: "test-only", SSLMode: "disable", MaxOpenConns: 8, MaxIdleConns: 4,
	}
}

// JSON is also valid YAML. Provide the isolated runtime inputs before Load so
// the normal config validation and keyring initialization see the same values
// that the production constructors will consume.
func deliveryStartupConfigFile(t *testing.T, database cfgpkg.DatabaseConfig) string {
	t.Helper()
	dir := t.TempDir()
	body, err := json.Marshal(map[string]any{
		"auth": map[string]any{
			"enable_dev_auth": false,
			"jwt":             map[string]any{"secret": "startup-test-jwt-secret-01234567890123456789"}, // #nosec G101 -- Synthetic key used only by this isolated startup fixture.
			"oidc":            map[string]any{"enabled": false},
		},
		"runtime": map[string]any{
			"workflow_workers":       1,
			"execution_runner_token": "startup-test-runner-token-01234567890123456789",
		},
		"monitoring": map[string]any{
			"webhook_token": "startup-test-webhook-token-01234567890123456789",
		},
		"security": map[string]any{ // #nosec G101 -- Synthetic encryption key used only by this isolated startup fixture.
			"credential_encryption_key": "startup-test-encryption-key-01234567890123456789",
			"webauthn_rp_id":            "localhost",
			"webauthn_origins":          []string{"http://localhost:8080"},
		},
		"database": map[string]any{
			"driver": database.Driver, "host": database.Host, "port": database.Port,
			"name": database.Name, "user": database.User, "password": database.Password,
			"sslmode": database.SSLMode, "max_open_conns": database.MaxOpenConns,
			"max_idle_conns": database.MaxIdleConns, "auto_migrate": false,
		},
		"bootstrap":  map[string]any{"seed_defaults": false},
		"kubernetes": map[string]any{"clusters": []any{}},
		"modules": map[string]any{
			"delivery":       map[string]any{"enabled": true},
			"ai":             map[string]any{"enabled": false},
			"virtualization": map[string]any{"enabled": false},
			"monitoring":     map[string]any{"enabled": false},
			"docker":         map[string]any{"enabled": false},
		},
		"plugins": map[string]any{
			"companion": map[string]any{"storage_dir": filepath.Join(dir, "companion")},
		},
		"ai_gateway": map[string]any{
			"relay": map[string]any{"health_check_enabled": false},
		},
	})
	require.NoError(t, err)
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, body, 0o600))
	return path
}

func invokeDeliveryStartupChild(t *testing.T, ctx context.Context, phase, configFile string, db cfgpkg.DatabaseConfig, seed deliveryStartupResult) deliveryStartupResult {
	t.Helper()
	binary, err := os.Executable()
	require.NoError(t, err)
	// #nosec G204 -- Re-execute this test binary with fixed test flags and an isolated environment, without a shell.
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestDeliveryStartupWithPostgres$", "-test.v")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "SOHA_") && !strings.HasPrefix(entry, "TEST_DELIVERY_STARTUP_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env,
		deliveryStartupPhaseEnv+"="+phase, "SOHA_CONFIG_FILE="+configFile,
		"TEST_DELIVERY_STARTUP_PORT="+strconv.Itoa(db.Port), "TEST_DELIVERY_STARTUP_DATABASE="+db.Name,
		"TEST_DELIVERY_STARTUP_RUN="+seed.RunID, "TEST_DELIVERY_STARTUP_BATCH="+seed.BatchID,
		"TEST_DELIVERY_STARTUP_CANDIDATE="+seed.CandidateID,
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "phase %s:\n%s", phase, output)
	for _, line := range strings.Split(string(output), "\n") {
		if payload, ok := strings.CutPrefix(line, deliveryStartupReport); ok {
			var report deliveryStartupResult
			require.NoError(t, json.Unmarshal([]byte(payload), &report))
			t.Logf("%s: %s", phase, payload)
			return report
		}
	}
	t.Fatalf("phase %s returned no report:\n%s", phase, output)
	return deliveryStartupResult{}
}

// Read the durable rows as canonical JSON so the blocked and replay cases
// compare the entire run (including its node attempts and fencing fields),
// Plan, candidate receipt and owning configuration, not just their statuses.
func deliveryStartupState(t *testing.T, ctx context.Context, db *gorm.DB) string {
	t.Helper()
	var snapshot string
	err := db.WithContext(ctx).Raw(`SELECT jsonb_build_object(
		'run', (SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM workflow_runs r),
		'batch', (SELECT jsonb_agg(to_jsonb(b) ORDER BY b.id) FROM delivery_batches b),
		'plan', (SELECT jsonb_agg(to_jsonb(p) ORDER BY p.id) FROM delivery_plans p),
		'candidate', (SELECT jsonb_agg(to_jsonb(c) ORDER BY c.id) FROM template_update_candidates c),
		'config', (SELECT to_jsonb(e) FROM application_environments e WHERE e.id=?)
	)::text`, deliveryStartupBinding).Row().Scan(&snapshot)
	require.NoError(t, err)
	return snapshot
}

func deliveryStartupChildConfig(t *testing.T) cfgpkg.Config {
	t.Helper()
	port, err := strconv.Atoi(os.Getenv("TEST_DELIVERY_STARTUP_PORT"))
	require.NoError(t, err)
	name := os.Getenv("TEST_DELIVERY_STARTUP_DATABASE")
	require.True(t, strings.HasPrefix(name, "soha_delivery_startup_"))
	cfg, err := cfgpkg.Load()
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", cfg.Database.Host)
	require.Equal(t, port, cfg.Database.Port)
	require.Equal(t, name, cfg.Database.Name)
	require.False(t, cfg.Database.AutoMigrate)
	require.False(t, cfg.Auth.EnableDevAuth)
	require.False(t, cfg.Bootstrap.SeedDefaults)
	require.Empty(t, cfg.Kubernetes.Clusters)
	require.False(t, cfg.Modules.AI.Enabled)
	require.False(t, cfg.Modules.Virtualization.Enabled)
	require.False(t, cfg.Modules.Monitoring.Enabled)
	require.False(t, cfg.Modules.Docker.Enabled)
	require.Equal(t, 1, cfg.Runtime.WorkflowWorkers)
	require.False(t, cfg.AIGateway.Relay.HealthCheckEnabled)
	require.True(t, cfg.Modules.Delivery.Enabled)

	return cfg
}

func runDeliveryStartupChild(t *testing.T, phase string) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cfg := deliveryStartupChildConfig(t)

	// Use the constructors and order used by New, stopping at delivery startup.
	// No HTTP listener or external integration is configured for this fixture.
	infra, err := newInfrastructure(ctx, &cfg)
	require.NoError(t, err)
	app := &App{cancel: infra.cancel, Informers: infra.informers, Database: infra.databaseStore}
	stopped := false
	shutdown := func() {
		t.Helper()
		if !stopped {
			stopped = true
			shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			require.NoError(t, app.Shutdown(shutdownCtx))
		}
	}
	t.Cleanup(shutdown)
	db := infra.databaseStore.DB()
	report := deliveryStartupResult{
		PID: os.Getpid(), RunID: os.Getenv("TEST_DELIVERY_STARTUP_RUN"),
		BatchID: os.Getenv("TEST_DELIVERY_STARTUP_BATCH"), CandidateID: os.Getenv("TEST_DELIVERY_STARTUP_CANDIDATE"),
	}
	var core *coreServices
	var routes deliveryRouteServices
	var probe *deliveryStartupProbe
	if phase != "seed" {
		// GORM callback registration must precede Core construction, whose
		// unrelated directory and cluster schedulers can already read the DB.
		// The closure inspects the finished graph only at the real claim SQL.
		probe = watchDeliveryStartup(t, db, phase, func(ctx context.Context) error {
			err := errors.Join(routes.workers.helms.ValidateRuntimeDependencies(), routes.workers.workflows.ValidateDeliveryRuntime())
			if err != nil {
				return err
			}
			principal, err := core.identityService.CurrentExecutionPrincipal(ctx, deliveryStartupActor, "")
			if err != nil {
				return err
			}
			_, err = routes.templateUpdates.ReadCandidate(ctx, principal, report.CandidateID)
			return err
		})
	}
	repos := newRepositories(cfg, infra.databaseStore)
	core, err = newCoreServices(ctx, cfg, infra, repos)
	require.NoError(t, err)
	delivery := newDeliveryServices(infra.lifecycleCtx, cfg, infra, repos, core)
	app.WorkflowService = delivery.workflowService
	app.VirtualizationService = delivery.virtualizationService
	gateway, err := newGatewayServices(infra.lifecycleCtx, cfg, repos, core, delivery)
	require.NoError(t, err)
	app.RateLimitBackend = gateway.rateLimitBackend
	routes, err = newDeliveryRouteServices(cfg, infra, repos, core, delivery)
	require.NoError(t, err)

	runner := &deliveryStartupRunner{}
	delivery.deliveryService.SetHelmDelivery(appdelivery.HelmDeliveryDependencies{
		Charts: resourcebackendinfra.HelmChartLoader{}, Runtime: runner, Secrets: core.secretService,
		Keys: cfg.Security.CredentialEncryptionKeys,
	})
	if phase == "seed" {
		seedDeliveryStartup(t, ctx, db, core, routes.templateUpdates, &report)
	} else {
		var previousOwner domainworkflow.Run
		if phase == "recover" {
			previousOwner, err = repos.workflowRepository.Get(ctx, report.RunID)
			require.NoError(t, err)
			require.NotEmpty(t, previousOwner.LeaseOwner)
			require.NotNil(t, previousOwner.LeaseUntil)
			require.True(t, previousOwner.LeaseUntil.Before(time.Now()))
		}
		switch phase {
		case "missing_template_updates":
			delivery.deliveryService.SetTemplateUpdates(nil)
		case "typed_nil_template_updates":
			delivery.deliveryService.SetTemplateUpdates((*apptemplateupdate.Service)(nil))
		case "missing_principals":
			delivery.workflowService.SetDeliveryRuntime(delivery.deliveryService, nil)
		}
		routes.workers = countDeliveryStartupStarts(routes.workers, &report.Starts)
		err := routes.startWorkers(cfg.Modules.Delivery.Enabled, infra.lifecycleCtx)
		if err != nil {
			report.StartupError = err.Error()
		} else {
			require.NoError(t, awaitDeliveryStartup(ctx, probe.firstClaim))
			report.FirstReady = true
			verifyDeliveryStartupProgress(t, ctx, repos.workflowRepository, phase, probe, previousOwner)
		}
		shutdown()
		report.Claims = probe.claims.Load()
		report.ConfigWrites = probe.configWrites.Load()
	}
	shutdown()
	report.RunnerCalls = runner.calls.Load()
	payload, err := json.Marshal(report)
	require.NoError(t, err)
	fmt.Printf("%s%s\n", deliveryStartupReport, payload)
}

func seedDeliveryStartup(t *testing.T, ctx context.Context, db *gorm.DB, core *coreServices, updates *apptemplateupdate.Service, result *deliveryStartupResult) {
	t.Helper()
	require.NoError(t, seedRoles(ctx, db))
	require.NoError(t, seedPolicies(ctx, db))
	accounts := userrepo.New(db)
	_, err := accounts.CreateUser(ctx, domainaccess.UserInput{
		ID: deliveryStartupActor, Username: "startup-admin", Email: "startup-admin@example.test",
		DisplayName: "Startup administrator", Status: "active", RoleIDs: []string{"admin"},
		Tags: []string{}, Preferences: map[string]any{}, Password: "startup-test-password",
	})
	require.NoError(t, err)
	// Confirm the complete account can be restored through the real reader.
	// Omitting display_name in raw SQL leaves NULL, which scanUser rejects.
	account, err := accounts.GetByID(ctx, deliveryStartupActor)
	require.NoError(t, err)
	require.Equal(t, "active", account.Status)
	require.Equal(t, "Startup administrator", account.DisplayName)
	state, err := accounts.GetAuthzState(ctx, deliveryStartupActor)
	require.NoError(t, err)
	require.Equal(t, "active", state.Status)
	require.Positive(t, state.AuthzVersion)
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO applications(id,name,app_key,app_group,language) VALUES (?,'Startup application',?,'test','configuration')`, []any{deliveryStartupApp, deliveryStartupApp}},
		{`INSERT INTO delivery_environments(id,environment_key,name,tier,stage_level) VALUES ('startup-env','startup-env','Startup environment','test',20)`, nil},
	} {
		require.NoError(t, db.WithContext(ctx).Exec(statement.query, statement.args...).Error)
	}
	principal, err := core.identityService.CurrentExecutionPrincipal(ctx, deliveryStartupActor, "")
	require.NoError(t, err)
	require.Equal(t, deliveryStartupActor, principal.UserID)
	require.Equal(t, []string{"admin"}, principal.Roles)
	// These persisted role permissions cover preview/batch creation and the
	// current identity restored by each production delivery stage after restart.
	for _, permission := range []string{
		appaccess.PermDeliveryApplicationsView,
		appaccess.PermDeliveryApplicationsUpdate,
		appaccess.PermDeliveryApplicationEnvView,
		appaccess.ManagedActionPermission(appaccess.PermDeliveryApplicationEnvManage, "create"),
		appaccess.ManagedActionPermission(appaccess.PermDeliveryApplicationEnvManage, "update"),
		appaccess.PermDeliveryWorkflowTemplatesView,
		appaccess.ManagedActionPermission(appaccess.PermDeliveryWorkflowTemplatesManage, "create"),
		appaccess.ManagedActionPermission(appaccess.PermDeliveryWorkflowTemplatesManage, "update"),
		appaccess.PermDeliveryWorkflowsView,
		appaccess.PermDeliveryWorkflowsTrigger,
	} {
		require.NoError(t, core.permissionResolver.Authorize(ctx, principal, permission), permission)
	}

	input := domaincatalog.WorkflowTemplateInput{Key: "startup-workflow", Name: "Startup workflow", Enabled: true}
	initial, err := core.catalogService.CreateWorkflowTemplate(ctx, principal, input)
	require.NoError(t, err)
	require.EqualValues(t, 1, initial.PublishedVersion)
	_, err = core.catalogService.CreateApplicationEnvironment(ctx, principal, domaincatalog.ApplicationEnvironmentInput{
		ID: deliveryStartupBinding, ApplicationID: deliveryStartupApp, EnvironmentID: "startup-env",
		WorkflowTemplateID: initial.ID, WorkflowTemplateVersion: initial.PublishedVersion,
	})
	require.NoError(t, err)
	input.ExpectedRevision = &initial.Revision
	input.Name = "Startup workflow updated"
	current, err := core.catalogService.UpdateWorkflowTemplate(ctx, principal, initial.ID, input)
	require.NoError(t, err)
	require.EqualValues(t, 2, current.PublishedVersion)

	targets, err := updates.ListTargets(ctx, principal, domainupdate.Filter{Kind: "workflow", TemplateID: initial.ID, ApplicationID: deliveryStartupApp})
	require.NoError(t, err)
	require.Len(t, targets.Items, 1)
	previewInput := sohaapi.TemplateUpdatePreviewInput{
		Kind: "workflow", TemplateID: initial.ID, ToVersion: current.PublishedVersion,
		Targets: []sohaapi.TemplateUpdateSelection{{TargetID: targets.Items[0].ID}},
	}
	previewInput.ApplicationID = deliveryStartupApp
	preview, err := updates.CreatePreview(ctx, principal, previewInput)
	require.NoError(t, err)
	require.Len(t, preview.Candidates, 1)
	candidate := preview.Candidates[0]
	require.True(t, candidate.Eligible, candidate.Reason)
	require.False(t, candidate.RequiresApproval)
	batch, err := updates.CreateBatch(ctx, principal, preview.ID, sohaapi.TemplateUpdateBatchInput{
		PreviewDigest: preview.Digest, CandidateIDs: []string{candidate.ID}, WaveSize: 1,
		StopOnFailure: true, IdempotencyKey: "startup-batch",
	})
	require.NoError(t, err)
	result.RunID, result.BatchID, result.CandidateID = batch.RootRunID, batch.ID, candidate.ID
}

type deliveryStartupProbe struct {
	claims       atomic.Int32
	configWrites atomic.Int32
	loadHeld     atomic.Bool
	firstClaim   chan error
	secondClaim  chan struct{}
	loadReached  chan struct{}
	resume       chan struct{}
}

func watchDeliveryStartup(t *testing.T, db *gorm.DB, phase string, readiness func(context.Context) error) *deliveryStartupProbe {
	t.Helper()
	probe := &deliveryStartupProbe{
		firstClaim: make(chan error, 1), secondClaim: make(chan struct{}, 1),
		loadReached: make(chan struct{}, 1), resume: make(chan struct{}),
	}
	// Observe the repository's real Row callback before it executes the lease
	// update. The worker and repository are the same instances production uses.
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register("delivery_startup:watch_claim", func(tx *gorm.DB) {
		ctx := tx.Statement.Context
		if ctx.Err() != nil {
			return
		}
		query := strings.TrimSpace(tx.Statement.SQL.String())
		if strings.HasPrefix(query, "WITH candidate AS (") && strings.Contains(query, "UPDATE workflow_runs SET lease_owner") {
			switch probe.claims.Add(1) {
			case 1:
				probe.firstClaim <- readiness(ctx)
			case 2:
				probe.secondClaim <- struct{}{}
			}
		}
		claims := probe.claims.Load()
		if strings.HasPrefix(query, "SELECT snapshot FROM delivery_batches WHERE id =") &&
			((phase == "hold" && claims == 3) || (phase == "recover" && claims == 1)) &&
			probe.loadHeld.CompareAndSwap(false, true) {
			// The successful claim has already returned. Hold the workflow at
			// its next batch read, before it can write any stage outcome.
			probe.loadReached <- struct{}{}
			select {
			case <-ctx.Done():
			case <-probe.resume:
			}
		}
	}))
	require.NoError(t, db.Callback().Raw().After("gorm:raw").Register("delivery_startup:count_adoption", func(tx *gorm.DB) {
		query := strings.TrimSpace(tx.Statement.SQL.String())
		if strings.HasPrefix(query, "UPDATE application_environments") && strings.Contains(query, "workflow_template_version =") && tx.Error == nil && tx.RowsAffected == 1 {
			probe.configWrites.Add(1)
		}
	}))
	return probe
}

func verifyDeliveryStartupProgress(t *testing.T, ctx context.Context, repo *repoworkflow.Repository, phase string, probe *deliveryStartupProbe, previousOwner domainworkflow.Run) {
	t.Helper()
	switch phase {
	case "hold":
		require.NoError(t, awaitDeliveryStartup(ctx, probe.loadReached))
	case "recover":
		require.NoError(t, awaitDeliveryStartup(ctx, probe.loadReached))
		active, err := repo.Get(ctx, previousOwner.ID)
		require.NoError(t, err)
		require.NotEmpty(t, active.LeaseOwner)
		require.NotEqual(t, previousOwner.LeaseOwner, active.LeaseOwner)
		require.Equal(t, previousOwner.FencingToken+1, active.FencingToken)
		require.Equal(t, previousOwner.Version+1, active.Version)
		// Try the actual prior claim while its replacement holds an active
		// lease, using the old owner's own version and fencing token.
		_, err = repo.SaveManagedRun(ctx, previousOwner, true)
		require.ErrorIs(t, err, apperrors.ErrConflict)
		close(probe.resume)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			run, err := repo.Get(ctx, previousOwner.ID)
			require.NoError(t, err)
			if run.Status == "completed" {
				return
			}
			require.Equal(t, "running", run.Status, "unexpected persisted workflow state: %+v", run.NodeRuns)
			require.NoError(t, awaitDeliveryStartup(ctx, ticker.C))
		}
	case "replay":
		require.NoError(t, awaitDeliveryStartup(ctx, probe.secondClaim))
	default:
		t.Fatalf("phase %q unexpectedly started its delivery workers", phase)
	}
}

func awaitDeliveryStartup[T any](ctx context.Context, signal <-chan T) error {
	select {
	case value := <-signal:
		if err, ok := any(value).(error); ok {
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type deliveryStartupWorker func(context.Context)

func (start deliveryStartupWorker) Start(ctx context.Context) { start(ctx) }

type deliveryStartupHelmWorker struct {
	deliveryHelmWorkers
	start deliveryStartupWorker
}

func (worker deliveryStartupHelmWorker) StartHelmDelivery(ctx context.Context) { worker.start(ctx) }

type deliveryStartupWorkflowWorker struct {
	deliveryWorkflowWorkers
	start deliveryStartupWorker
}

func (worker deliveryStartupWorkflowWorker) Start(ctx context.Context) { worker.start(ctx) }

func countDeliveryStartupStarts(w deliveryWorkers, counts *[6]int) deliveryWorkers {
	count := func(index int, start func(context.Context)) deliveryStartupWorker {
		return func(ctx context.Context) {
			counts[index]++
			start(ctx)
		}
	}
	return deliveryWorkers{
		execution: count(0, w.execution.Start),
		manifests: count(1, w.manifests.Start),
		helms:     deliveryStartupHelmWorker{deliveryHelmWorkers: w.helms, start: count(2, w.helms.StartHelmDelivery)},
		workflows: deliveryStartupWorkflowWorker{deliveryWorkflowWorkers: w.workflows, start: count(3, w.workflows.Start)},
		triggers:  count(4, w.triggers.Start),
		updates:   count(5, w.updates.Start),
	}
}

type deliveryStartupRunner struct{ calls atomic.Int32 }

func (r *deliveryStartupRunner) reject() error {
	r.calls.Add(1)
	return errors.New("configuration adoption must not invoke a Helm runner")
}

func (r *deliveryStartupRunner) PrepareHelmDelivery(context.Context, domainidentity.Principal, sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskPayload, error) {
	return sohaapi.HelmExecutionTaskPayload{}, r.reject()
}

func (r *deliveryStartupRunner) ExecuteHelmDelivery(context.Context, domainidentity.Principal, sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskResult, error) {
	return sohaapi.HelmExecutionTaskResult{}, r.reject()
}

func (r *deliveryStartupRunner) AuthorizeHelmDelivery(context.Context, domainidentity.Principal, sohaapi.HelmDeliverySnapshot, bool) (string, error) {
	return "", r.reject()
}
