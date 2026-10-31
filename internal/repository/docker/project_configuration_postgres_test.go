package docker_test

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appdocker "github.com/opensoha/soha/internal/application/docker"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	dockerrepo "github.com/opensoha/soha/internal/repository/docker"
	testpostgres "github.com/opensoha/soha/internal/testsupport/postgres"
	"go.uber.org/zap"
	gorm "gorm.io/gorm"
)

type projectConfigurationRoles struct{}

func (projectConfigurationRoles) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"fixture": {"docker.projects.create", "docker.projects.update"}}, nil
}

func projectConfigurationStore(t *testing.T) *dbstore.Store {
	t.Helper()
	text := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if text == "" {
		t.Skip("requires this run's disposable PostgreSQL")
	}
	port, err := strconv.Atoi(text)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testpostgres.Configure(t, config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "postgres", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 12, MaxIdleConns: 4})
	admin, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	cfg.Name = "docker_config_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(t.Context(), "CREATE DATABASE "+cfg.Name); err != nil {
		t.Fatal(err)
	}
	store, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
		if err := admin.Exec(context.Background(), "DROP DATABASE "+cfg.Name); err != nil {
			t.Error(err)
		}
	})
	if err := store.MigrateFromFile(t.Context(), "../../../migrations/postgres/0001_init.sql"); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestDockerProjectConfigurationWithPostgres(t *testing.T) {
	store := projectConfigurationStore(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	db := store.DB().WithContext(ctx)
	repo := dockerrepo.New(store.DB())
	service := appdocker.New(repo, appaccess.NewPermissionResolver(projectConfigurationRoles{}), nil)
	principal := domainidentity.Principal{UserID: "configuration-fixture", Roles: []string{"fixture"}}
	must := func(tb *testing.T, err error) { dockerProjectConfigurationWithPostgresPhase1(tb, err) }
	for _, id := range []string{"config-host-a", "config-host-b"} {
		_, err := repo.CreateHost(ctx, domaindocker.HostInput{ID: id, Name: id})
		must(t, err)
	}
	input := func(id string) domaindocker.ProjectInput {
		return domaindocker.ProjectInput{ID: id, HostID: "config-host-a", Name: id, Slug: id, ComposeContent: "services:\n  api:\n    image: fixture:v1\n"}
	}
	project := func(tb *testing.T, id string) domaindocker.Project {
		return dockerProjectConfigurationWithPostgresPhase2(tb, id, &ctx, &repo, &must)
	}
	services := func(tb *testing.T, id string) []domaindocker.Service {
		return dockerProjectConfigurationWithPostgresPhase3(tb, id, &ctx, &repo, &must)
	}
	count := func(tb *testing.T, table, id string) int64 {
		return dockerProjectConfigurationWithPostgresPhase4(tb, table, id, &db, &must)
	}
	seed := func(tb *testing.T, observed bool) (domaindocker.ProjectInput, domaindocker.Service) {
		return dockerProjectConfigurationWithPostgresPhase5(tb, observed, &ctx, &repo, &must, &input, &services)
	}
	rollbackTrigger := func(tb *testing.T) { dockerProjectConfigurationWithPostgresPhase6(tb, &db, &must) }

	t.Run("create_definition_is_not_observation", func(t *testing.T) {
		dockerProjectConfigurationWithPostgresPhase7(t, &ctx, &service, &principal, &must, &input, &services)
	})
	t.Run("configuration_preserves_runtime", func(t *testing.T) {
		dockerProjectConfigurationWithPostgresPhase8(t, &ctx, &repo, &service, &principal, &must, &project, &services, &seed)
	})
	t.Run("explicit_legacy_state_retained", func(t *testing.T) {
		dockerProjectConfigurationWithPostgresPhase9(t, &ctx, &service, &principal, &must, &seed)
	})
	t.Run("create_rolls_back_definitions", func(t *testing.T) {
		dockerProjectConfigurationWithPostgresPhase10(t, &ctx, &service, &principal, &input, &services, &count, &rollbackTrigger)
	})
	t.Run("update_rolls_back_config_revision_and_inventory", func(t *testing.T) {
		dockerProjectConfigurationWithPostgresPhase11(t, &ctx, &repo, &service, &principal, &must, &project, &services, &seed, &rollbackTrigger)
	})
	t.Run("idempotent_create_rolls_back_receipt", func(t *testing.T) {
		dockerProjectConfigurationWithPostgresPhase12(t, &ctx, &repo, &input, &services, &count, &rollbackTrigger)
	})
	t.Run("shared_transaction_rollback", func(t *testing.T) {
		for _, idempotent := range []bool{false, true} {
			in := input(uuid.NewString())
			failure := errors.New("fixture outer rollback")
			err := dbtx.Within(ctx, store.DB(), func(txctx context.Context) error {
				if idempotent {
					_, err := repo.CreateProjectIdempotent(txctx, in, "fixture-digest", []string{"api"})
					must(t, err)
					_, err = repo.FindProjectCreation(txctx, in.ID, "fixture-digest")
					must(t, err)
				} else {
					_, err := repo.CreateProjectConfiguration(txctx, in, []string{"api"})
					must(t, err)
				}
				return failure
			})
			if !errors.Is(err, failure) || count(t, "docker_projects", in.ID) != 0 || count(t, "docker_project_creations", in.ID) != 0 || len(services(t, in.ID)) != 0 {
				t.Fatal("configuration escaped the bound outer transaction")
			}
		}
	})
	t.Run("runtime_update_and_host_fence", func(t *testing.T) { dockerProjectConfigurationWithPostgresPhase13(t, &ctx, &repo, &must, &seed) })
	t.Run("runtime_readback_failure_rolls_back", func(t *testing.T) { dockerProjectConfigurationWithPostgresPhase14(t, &ctx, &db, &repo, &must, &seed) })
	t.Run("cold_host_move_and_observed_move_fence", func(t *testing.T) {
		dockerProjectConfigurationWithPostgresPhase15(t, &ctx, &repo, &service, &principal, &must, &project, &services, &seed)
	})
	t.Run("legacy_definition_evidence_retained", func(t *testing.T) {
		dockerProjectConfigurationWithPostgresPhase16(t, &ctx, &db, &service, &principal, &must, &seed)
	})
	t.Run("bound_ports_and_operations_block_move", func(t *testing.T) {
		assertBoundReferencesPreventHostMove(t, ctx, repo, service, principal, must, seed)
	})
	t.Run("stale_reference_writes_rejected", func(t *testing.T) { dockerProjectConfigurationWithPostgresPhase17(t, &ctx, &db, &repo, &must, &seed) })
	t.Run("terminal_history_requires_relocation", func(t *testing.T) {
		for _, status := range []string{"completed", "failed", "canceled", "callback_timeout"} {
			in, _ := seed(t, false)
			_, err := repo.CreateOperation(ctx, domaindocker.OperationInput{HostID: in.HostID, ProjectID: in.ID, OperationKind: "fixture", Status: status})
			must(t, err)
			in.HostID = "config-host-b"
			if _, err := service.UpdateProject(ctx, principal, in.ID, in); !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("terminal status %s was treated as proof of no external resources: %v", status, err)
			}
		}
	})
	t.Run("writers_wait_for_project_lock", func(t *testing.T) { assertWritersWaitForProjectLock(t, ctx, db, repo, must, seed) })
	t.Run("removed_names_do_not_delete_inventory", func(t *testing.T) {
		dockerProjectConfigurationWithPostgresPhase18(t, &ctx, &repo, &service, &principal, &must, &services, &seed)
	})
}

func dockerProjectConfigurationWithPostgresPhase1(tb *testing.T, err error) {

	tb.Helper()
	if err != nil {
		tb.Fatal(err)
	}
}

func dockerProjectConfigurationWithPostgresPhase2(tb *testing.T, id string, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedMust *func(tb *testing.T, err error)) domaindocker.Project {

	tb.Helper()
	p, err := (*sharedRepo).GetProject((*sharedCtx), id)
	(*sharedMust)(tb, err)
	return p
}

func dockerProjectConfigurationWithPostgresPhase3(tb *testing.T, id string, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedMust *func(tb *testing.T, err error)) []domaindocker.Service {

	tb.Helper()
	items, err := (*sharedRepo).ListServices((*sharedCtx), domaindocker.ServiceFilter{ProjectID: id, Limit: 100})
	(*sharedMust)(tb, err)
	return items
}

func dockerProjectConfigurationWithPostgresPhase4(tb *testing.T, table, id string, sharedDb **gorm.DB, sharedMust *func(tb *testing.T, err error)) int64 {

	tb.Helper()
	var n int64
	(*sharedMust)(tb, (*sharedDb).Raw("SELECT count(*) FROM "+table+" WHERE id = ?", id).Scan(&n).Error)
	return n
}

func dockerProjectConfigurationWithPostgresPhase5(tb *testing.T, observed bool, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedMust *func(tb *testing.T, err error), sharedInput *func(id string) domaindocker.ProjectInput, sharedServices *func(tb *testing.T, id string) []domaindocker.Service) (domaindocker.ProjectInput, domaindocker.Service) {

	tb.Helper()
	in := (*sharedInput)(uuid.NewString())
	_, err := (*sharedRepo).CreateProjectConfiguration((*sharedCtx), in, []string{"api"})
	(*sharedMust)(tb, err)
	if !observed {
		return in, (*sharedServices)(tb, in.ID)[0]
	}
	now := time.Now().UTC()
	_, err = (*sharedRepo).UpdateProjectRuntime((*sharedCtx), in.ID, "running", "running", &now)
	(*sharedMust)(tb, err)
	item, err := (*sharedRepo).UpsertService((*sharedCtx), domaindocker.ServiceInput{ProjectID: in.ID, HostID: in.HostID, Name: "api", Image: "fixture:observed", Status: "running", ContainerID: "fixture-container", RestartCount: 3, CPUPercent: 2.5, MemoryBytes: 4096, NetworkRxBytes: 7, NetworkTxBytes: 11, Config: map[string]any{"observed": true}})
	(*sharedMust)(tb, err)
	return in, item
}

func dockerProjectConfigurationWithPostgresPhase6(tb *testing.T, sharedDb **gorm.DB, sharedMust *func(tb *testing.T, err error)) {

	tb.Helper()
	(*sharedMust)(tb, (*sharedDb).Exec(`CREATE FUNCTION fixture_reject_definition() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
			IF NEW.name = 'zz_reject' THEN RAISE EXCEPTION 'fixture definition rollback'; END IF; RETURN NEW; END $$;
			CREATE TRIGGER fixture_definition_failure BEFORE INSERT ON docker_services FOR EACH ROW EXECUTE FUNCTION fixture_reject_definition();`).Error)
	tb.Cleanup(func() {
		(*sharedMust)(tb, (*sharedDb).Exec(`DROP TRIGGER fixture_definition_failure ON docker_services; DROP FUNCTION fixture_reject_definition();`).Error)
	})
}

func dockerProjectConfigurationWithPostgresPhase7(t *testing.T, sharedCtx *context.Context, sharedService **appdocker.Service, sharedPrincipal *domainidentity.Principal, sharedMust *func(tb *testing.T, err error), sharedInput *func(id string) domaindocker.ProjectInput, sharedServices *func(tb *testing.T, id string) []domaindocker.Service) {

	in := (*sharedInput)(uuid.NewString())
	created, err := (*sharedService).CreateProject((*sharedCtx), (*sharedPrincipal), in)
	(*sharedMust)(t, err)
	items := (*sharedServices)(t, created.ID)
	if len(items) != 1 || items[0].LastSeenAt != nil || items[0].ContainerID != "" || items[0].Status != "defined" {
		t.Fatalf("configuration fabricated runtime evidence: %#v", items)
	}
}

func dockerProjectConfigurationWithPostgresPhase8(t *testing.T, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedService **appdocker.Service, sharedPrincipal *domainidentity.Principal, sharedMust *func(tb *testing.T, err error), sharedProject *func(tb *testing.T, id string) domaindocker.Project, sharedServices *func(tb *testing.T, id string) []domaindocker.Service, sharedSeed *func(tb *testing.T, observed bool) (domaindocker.ProjectInput, domaindocker.Service)) {

	in, observed := (*sharedSeed)(t, true)
	before := (*sharedProject)(t, in.ID)
	in.Name, in.ComposeContent = "renamed", "services:\n  api:\n    image: fixture:v2\n  worker:\n    image: fixture:worker\n"
	after, err := (*sharedService).UpdateProject((*sharedCtx), (*sharedPrincipal), in.ID, in)
	(*sharedMust)(t, err)
	got, err := (*sharedRepo).GetService((*sharedCtx), observed.ID)
	(*sharedMust)(t, err)
	if !reflect.DeepEqual(got, observed) || after.Status != before.Status || after.DesiredState != before.DesiredState || !reflect.DeepEqual(after.LastDeployedAt, before.LastDeployedAt) {
		t.Fatal("configuration replaced persisted runtime state")
	}
	if after.ResourceUID != before.ResourceUID || after.ConfigurationRevision == before.ConfigurationRevision || len((*sharedServices)(t, in.ID)) != 2 {
		t.Fatal("configuration identity or desired definitions did not commit")
	}
}

func dockerProjectConfigurationWithPostgresPhase9(t *testing.T, sharedCtx *context.Context, sharedService **appdocker.Service, sharedPrincipal *domainidentity.Principal, sharedMust *func(tb *testing.T, err error), sharedSeed *func(tb *testing.T, observed bool) (domaindocker.ProjectInput, domaindocker.Service)) {

	in, _ := (*sharedSeed)(t, true)
	in.Status, in.DesiredState = "stopped", "stopped"
	p, err := (*sharedService).UpdateProject((*sharedCtx), (*sharedPrincipal), in.ID, in)
	(*sharedMust)(t, err)
	if p.Status != "stopped" || p.DesiredState != "stopped" {
		t.Fatal("explicit legacy state no longer works")
	}
}

func dockerProjectConfigurationWithPostgresPhase10(t *testing.T, sharedCtx *context.Context, sharedService **appdocker.Service, sharedPrincipal *domainidentity.Principal, sharedInput *func(id string) domaindocker.ProjectInput, sharedServices *func(tb *testing.T, id string) []domaindocker.Service, sharedCount *func(tb *testing.T, table string, id string) int64, sharedRollbackTrigger *func(tb *testing.T)) {

	(*sharedRollbackTrigger)(t)
	in := (*sharedInput)(uuid.NewString())
	in.ComposeContent = "services:\n  aa_first: {}\n  zz_reject: {}\n"
	if _, err := (*sharedService).CreateProject((*sharedCtx), (*sharedPrincipal), in); err == nil {
		t.Fatal("injected definition error was accepted")
	}
	if (*sharedCount)(t, "docker_projects", in.ID) != 0 || len((*sharedServices)(t, in.ID)) != 0 {
		t.Fatal("failed creation left a project or first definition")
	}
}

func dockerProjectConfigurationWithPostgresPhase11(t *testing.T, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedService **appdocker.Service, sharedPrincipal *domainidentity.Principal, sharedMust *func(tb *testing.T, err error), sharedProject *func(tb *testing.T, id string) domaindocker.Project, sharedServices *func(tb *testing.T, id string) []domaindocker.Service, sharedSeed *func(tb *testing.T, observed bool) (domaindocker.ProjectInput, domaindocker.Service), sharedRollbackTrigger *func(tb *testing.T)) {

	in, observed := (*sharedSeed)(t, true)
	before := (*sharedProject)(t, in.ID)
	(*sharedRollbackTrigger)(t)
	in.Name, in.ComposeContent = "must-rollback", "services:\n  aa_first: {}\n  api: {}\n  zz_reject: {}\n"
	if _, err := (*sharedService).UpdateProject((*sharedCtx), (*sharedPrincipal), in.ID, in); err == nil {
		t.Fatal("injected definition error was accepted")
	}
	after := (*sharedProject)(t, in.ID)
	got, err := (*sharedRepo).GetService((*sharedCtx), observed.ID)
	(*sharedMust)(t, err)
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(observed, got) || len((*sharedServices)(t, in.ID)) != 1 {
		t.Fatal("configuration transaction partially committed")
	}
}

func dockerProjectConfigurationWithPostgresPhase12(t *testing.T, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedInput *func(id string) domaindocker.ProjectInput, sharedServices *func(tb *testing.T, id string) []domaindocker.Service, sharedCount *func(tb *testing.T, table string, id string) int64, sharedRollbackTrigger *func(tb *testing.T)) {

	(*sharedRollbackTrigger)(t)
	in := (*sharedInput)(uuid.NewString())
	if _, err := (*sharedRepo).CreateProjectIdempotent((*sharedCtx), in, "fixture-digest", []string{"aa_first", "zz_reject"}); err == nil {
		t.Fatal("injected creation error was accepted")
	}
	if (*sharedCount)(t, "docker_projects", in.ID) != 0 || (*sharedCount)(t, "docker_project_creations", in.ID) != 0 || len((*sharedServices)(t, in.ID)) != 0 {
		t.Fatal("failed idempotent creation left durable data")
	}
}

func dockerProjectConfigurationWithPostgresPhase13(t *testing.T, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedMust *func(tb *testing.T, err error), sharedSeed *func(tb *testing.T, observed bool) (domaindocker.ProjectInput, domaindocker.Service)) {

	in, old := (*sharedSeed)(t, true)
	update := domaindocker.ServiceInput{ID: old.ID, ProjectID: in.ID, HostID: in.HostID, Name: "api", Image: "fixture:changed", Status: "stopped", ContainerID: "fixture-new-container", RestartCount: 8, CPUPercent: 9.5, MemoryBytes: 8192, NetworkRxBytes: 13, NetworkTxBytes: 17, Config: map[string]any{"changed": true}}
	got, err := (*sharedRepo).UpsertService((*sharedCtx), update)
	(*sharedMust)(t, err)
	if got.ID != old.ID || got.ContainerID != update.ContainerID || got.RestartCount != 8 || got.CPUPercent != 9.5 || got.MemoryBytes != 8192 || got.NetworkRxBytes != 13 || got.NetworkTxBytes != 17 || got.Image != update.Image || got.Status != update.Status || got.LastSeenAt == nil || !got.LastSeenAt.After(*old.LastSeenAt) {
		t.Fatal("real runtime write was disabled or lost fields")
	}
	update.HostID = "config-host-b"
	if _, err := (*sharedRepo).UpsertService((*sharedCtx), update); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("runtime host mismatch accepted: %v", err)
	}
	after, err := (*sharedRepo).GetService((*sharedCtx), old.ID)
	(*sharedMust)(t, err)
	if !reflect.DeepEqual(got, after) {
		t.Fatal("host-denied runtime write changed inventory")
	}
}

func dockerProjectConfigurationWithPostgresPhase14(t *testing.T, sharedCtx *context.Context, sharedDb **gorm.DB, sharedRepo **dockerrepo.Repository, sharedMust *func(tb *testing.T, err error), sharedSeed *func(tb *testing.T, observed bool) (domaindocker.ProjectInput, domaindocker.Service)) {

	in, _ := (*sharedSeed)(t, false)
	before, err := (*sharedRepo).UpsertService((*sharedCtx), domaindocker.ServiceInput{ProjectID: in.ID, HostID: in.HostID, Name: "vanish_fixture", Status: "running", ContainerID: "fixture-before"})
	(*sharedMust)(t, err)
	(*sharedMust)(t, (*sharedDb).Exec(`CREATE FUNCTION fixture_vanish_service() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
			IF NEW.name = 'vanish_fixture' THEN DELETE FROM docker_services WHERE id=NEW.id; END IF; RETURN NULL; END $$;
			CREATE TRIGGER fixture_readback_failure AFTER UPDATE ON docker_services FOR EACH ROW EXECUTE FUNCTION fixture_vanish_service();`).Error)
	t.Cleanup(func() {
		(*sharedMust)(t, (*sharedDb).Exec(`DROP TRIGGER fixture_readback_failure ON docker_services; DROP FUNCTION fixture_vanish_service();`).Error)
	})
	if _, err := (*sharedRepo).UpsertService((*sharedCtx), domaindocker.ServiceInput{ProjectID: in.ID, HostID: in.HostID, Name: "vanish_fixture", Status: "stopped"}); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("missing readback was reported as success: %v", err)
	}
	after, err := (*sharedRepo).GetService((*sharedCtx), before.ID)
	(*sharedMust)(t, err)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("runtime readback failure committed a partial write")
	}
}

func dockerProjectConfigurationWithPostgresPhase15(t *testing.T, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedService **appdocker.Service, sharedPrincipal *domainidentity.Principal, sharedMust *func(tb *testing.T, err error), sharedProject *func(tb *testing.T, id string) domaindocker.Project, sharedServices *func(tb *testing.T, id string) []domaindocker.Service, sharedSeed *func(tb *testing.T, observed bool) (domaindocker.ProjectInput, domaindocker.Service)) {

	in, definition := (*sharedSeed)(t, false)
	_, err := (*sharedRepo).UpdateProjectConfiguration((*sharedCtx), in.ID, in, []string{"api", "retained"})
	(*sharedMust)(t, err)
	in.HostID = "config-host-b"
	_, err = (*sharedService).UpdateProject((*sharedCtx), (*sharedPrincipal), in.ID, in)
	(*sharedMust)(t, err)
	got, err := (*sharedRepo).GetService((*sharedCtx), definition.ID)
	(*sharedMust)(t, err)
	if got.HostID != in.HostID || got.ID != definition.ID || got.LastSeenAt != nil {
		t.Fatal("cold definition host move did not preserve identity")
	}
	for _, retained := range (*sharedServices)(t, in.ID) {
		if retained.HostID != in.HostID || retained.LastSeenAt != nil {
			t.Fatal("retained definition stayed on the old Host")
		}
	}
	live, observed := (*sharedSeed)(t, true)
	before := (*sharedProject)(t, live.ID)
	live.HostID = "config-host-b"
	if _, err := (*sharedService).UpdateProject((*sharedCtx), (*sharedPrincipal), live.ID, live); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("live host move accepted: %v", err)
	}
	if !reflect.DeepEqual(before, (*sharedProject)(t, live.ID)) {
		t.Fatal("denied host move changed configuration")
	}
	got, err = (*sharedRepo).GetService((*sharedCtx), observed.ID)
	(*sharedMust)(t, err)
	if !reflect.DeepEqual(got, observed) {
		t.Fatal("denied host move changed observed inventory")
	}
}

func dockerProjectConfigurationWithPostgresPhase16(t *testing.T, sharedCtx *context.Context, sharedDb **gorm.DB, sharedService **appdocker.Service, sharedPrincipal *domainidentity.Principal, sharedMust *func(tb *testing.T, err error), sharedSeed *func(tb *testing.T, observed bool) (domaindocker.ProjectInput, domaindocker.Service)) {

	in, definition := (*sharedSeed)(t, false)
	(*sharedMust)(t, (*sharedDb).Exec("UPDATE docker_services SET last_seen_at=now() WHERE id=?", definition.ID).Error)
	in.HostID = "config-host-b"
	if _, err := (*sharedService).UpdateProject((*sharedCtx), (*sharedPrincipal), in.ID, in); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("legacy observation evidence silently cleared: %v", err)
	}
}

func dockerProjectConfigurationWithPostgresPhase17(t *testing.T, sharedCtx *context.Context, sharedDb **gorm.DB, sharedRepo **dockerrepo.Repository, sharedMust *func(tb *testing.T, err error), sharedSeed *func(tb *testing.T, observed bool) (domaindocker.ProjectInput, domaindocker.Service)) {

	in, svc := (*sharedSeed)(t, false)
	op, err := (*sharedRepo).CreateOperation((*sharedCtx), domaindocker.OperationInput{HostID: in.HostID, ServiceID: svc.ID, OperationKind: "fixture", Status: "failed"})
	(*sharedMust)(t, err)
	oldHost := in.HostID
	in.HostID = "config-host-b"
	// Reproduce a prior-version/direct writer moving metadata after a task.
	// The new configuration path deliberately rejects this uncertain history.
	(*sharedMust)(t, (*sharedDb).Exec("UPDATE docker_projects SET host_id=? WHERE id=?", in.HostID, in.ID).Error)
	(*sharedMust)(t, (*sharedDb).Exec("UPDATE docker_services SET host_id=? WHERE id=?", in.HostID, svc.ID).Error)
	if _, err := (*sharedRepo).CreateOperation((*sharedCtx), domaindocker.OperationInput{HostID: oldHost, ProjectID: in.ID, OperationKind: "fixture"}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale operation target accepted: %v", err)
	}
	port := domaindocker.PortMappingInput{HostID: oldHost, ProjectID: in.ID, Name: "fixture", HostPort: 31235, ContainerPort: 80}
	if _, err := (*sharedRepo).CreatePortMapping((*sharedCtx), port); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale port target accepted: %v", err)
	}
	op.Status = "queued"
	if _, err := (*sharedRepo).UpdateOperation((*sharedCtx), op); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("retry resurrected an old Host target: %v", err)
	}
}

func dockerProjectConfigurationWithPostgresPhase18(t *testing.T, sharedCtx *context.Context, sharedRepo **dockerrepo.Repository, sharedService **appdocker.Service, sharedPrincipal *domainidentity.Principal, sharedMust *func(tb *testing.T, err error), sharedServices *func(tb *testing.T, id string) []domaindocker.Service, sharedSeed *func(tb *testing.T, observed bool) (domaindocker.ProjectInput, domaindocker.Service)) {

	in, observed := (*sharedSeed)(t, true)
	in.ComposeContent = "services:\n  worker: {}\n"
	_, err := (*sharedService).UpdateProject((*sharedCtx), (*sharedPrincipal), in.ID, in)
	(*sharedMust)(t, err)
	got, err := (*sharedRepo).GetService((*sharedCtx), observed.ID)
	(*sharedMust)(t, err)
	if !reflect.DeepEqual(got, observed) || len((*sharedServices)(t, in.ID)) != 2 {
		t.Fatal("configuration removal deleted runtime inventory")
	}
}

func assertBoundReferencesPreventHostMove(t *testing.T, ctx context.Context, repo *dockerrepo.Repository, service *appdocker.Service, principal domainidentity.Principal, must func(*testing.T, error), seed func(*testing.T, bool) (domaindocker.ProjectInput, domaindocker.Service)) {
	t.Helper()

	for _, kind := range []string{"port", "operation"} {
		in, svc := seed(t, false)
		if kind == "port" {
			_, err := repo.CreatePortMapping(ctx, domaindocker.PortMappingInput{HostID: in.HostID, ServiceID: svc.ID, Name: "fixture", HostPort: 31234, ContainerPort: 80})
			must(t, err)
		} else {
			_, err := repo.CreateOperation(ctx, domaindocker.OperationInput{HostID: in.HostID, ServiceID: svc.ID, OperationKind: "fixture", Status: "queued"})
			must(t, err)
		}
		in.HostID = "config-host-b"
		if _, err := service.UpdateProject(ctx, principal, in.ID, in); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("%s reference allowed a host move: %v", kind, err)
		}
	}
}

func assertWritersWaitForProjectLock(t *testing.T, ctx context.Context, db *gorm.DB, repo *dockerrepo.Repository, must func(*testing.T, error), seed func(*testing.T, bool) (domaindocker.ProjectInput, domaindocker.Service)) {
	t.Helper()

	for _, kind := range []string{"runtime", "port", "operation"} {
		t.Run(kind, func(t *testing.T) {
			in, _ := seed(t, false)
			tx := db.Begin()
			must(t, tx.Error)
			defer tx.Rollback()
			must(t, tx.Exec("SELECT id FROM docker_projects WHERE id=? FOR UPDATE", in.ID).Error)
			done := make(chan error, 1)
			go func() {
				var err error
				switch kind {
				case "runtime":
					_, err = repo.UpsertService(ctx, domaindocker.ServiceInput{ProjectID: in.ID, HostID: in.HostID, Name: "api", Status: "running", ContainerID: "fixture-serialized"})
				case "port":
					_, err = repo.CreatePortMapping(ctx, domaindocker.PortMappingInput{HostID: in.HostID, ProjectID: in.ID, Name: "fixture", HostPort: 31236, ContainerPort: 80})
				case "operation":
					_, err = repo.CreateOperation(ctx, domaindocker.OperationInput{HostID: in.HostID, ProjectID: in.ID, OperationKind: "fixture"})
				}
				done <- err
			}()
			deadline, waiting := time.Now().Add(5*time.Second), false
			for time.Now().Before(deadline) {
				var n int64
				must(t, db.Raw(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid()
						AND wait_event_type='Lock' AND query LIKE '%FROM docker_projects%FOR UPDATE%'`).Scan(&n).Error)
				if n > 0 {
					waiting = true
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !waiting {
				t.Fatal("writer did not wait on the parent Project before writing child state")
			}
			must(t, tx.Commit().Error)
			select {
			case err := <-done:
				must(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("writer did not resume after parent lock release")
			}
		})
	}
}
