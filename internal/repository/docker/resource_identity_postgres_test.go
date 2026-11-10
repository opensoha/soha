package docker_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	dockerrepo "github.com/opensoha/soha/internal/repository/docker"
	testpostgres "github.com/opensoha/soha/internal/testsupport/postgres"
	"go.uber.org/zap"
	gorm "gorm.io/gorm"
)

func TestDockerResourceIdentityWithPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_CATALOG_TEST_POSTGRES_PORT to this run's disposable PostgreSQL")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	cfg := testpostgres.Configure(t, config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "postgres", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 12, MaxIdleConns: 4})
	admin, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	cfg.Name = "docker_identity_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(t.Context(), "CREATE DATABASE "+cfg.Name); err != nil {
		t.Fatal(err)
	}
	store, err := dbstore.New(cfg, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { dockerResourceIdentityWithPostgresPhase1(&t, &cfg, &admin, &store) })
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	migrations := filepath.Join("..", "..", "..", "migrations", "postgres")
	legacy := t.TempDir()
	copyLegacyDockerMigrations(t, migrations, legacy)
	if err := store.MigrateFromFile(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	db := store.DB().WithContext(ctx)
	must := func(tb *testing.T, err error) { dockerResourceIdentityWithPostgresPhase2(tb, err) }
	for _, id := range []string{"legacy-host-1", "legacy-host-2"} {
		must(t, db.Exec(`INSERT INTO docker_hosts(id,name) VALUES (?,?)`, id, id).Error)
	}
	for _, id := range []string{"legacy-project-1", "legacy-project-2"} {
		must(t, db.Exec(`INSERT INTO docker_projects(id,host_id,name,slug,compose_content) VALUES (?,?,?,?,?)`, id, "legacy-host-1", id, id, "services: {}\n").Error)
	}
	// Keep the migration root stable: the existing ledger records full paths.
	// Add the upgrade to that same copied installation, then exercise its normal
	// single-file configuration entry, which must discover the new sibling.
	// #nosec G304 -- fixed repository migrations directory; filenames are fixed literals or basenames from its os.ReadDir result.
	upgrade, err := os.ReadFile(filepath.Join(migrations, "0130_docker_resource_identity.sql"))
	must(t, err)
	// #nosec G703 -- private t.TempDir destination and fixed migration filename/basename, without external path input.
	must(t, os.WriteFile(filepath.Join(legacy, "0130_docker_resource_identity.sql"), upgrade, 0o600))
	// The current repository also reads runtime identity; retain the original
	// 0130 assertions while applying its additive successor through the ledger.
	// #nosec G304 -- fixed repository migrations directory; filenames are fixed literals or basenames from its os.ReadDir result.
	runtimeUpgrade, err := os.ReadFile(filepath.Join(migrations, "0133_docker_runtime_identity.sql"))
	must(t, err)
	// #nosec G703 -- private t.TempDir destination and fixed migration filename/basename, without external path input.
	must(t, os.WriteFile(filepath.Join(legacy, "0133_docker_runtime_identity.sql"), runtimeUpgrade, 0o600))
	must(t, store.MigrateFromFile(ctx, filepath.Join(legacy, "0001_init.sql")))
	repo := dockerrepo.New(store.DB())
	host := func(tb *testing.T, id string) domaindocker.Host {
		return dockerResourceIdentityWithPostgresPhase3(tb, id, &ctx, &must, &repo)
	}
	project := func(tb *testing.T, id string) domaindocker.Project {
		return dockerResourceIdentityWithPostgresPhase4(tb, id, &ctx, &must, &repo)
	}
	valid := func(tb *testing.T, uidText, revisionText string) {
		dockerResourceIdentityWithPostgresPhase5(tb, uidText, revisionText)
	}
	t.Run("legacy_backfill", func(t *testing.T) { dockerResourceIdentityWithPostgresPhase6(t, &host, &project, &valid) })
	t.Run("insert_ignores_caller_identity", func(t *testing.T) { dockerResourceIdentityWithPostgresPhase7(t, &db, &must, &host, &project, &valid) })
	t.Run("configuration_fields_rotate", func(t *testing.T) {
		for _, entry := range []struct{ table, id, set string }{
			{"hosts", "legacy-host-1", "name='renamed'"}, {"hosts", "legacy-host-1", "endpoint='http://fixture.invalid'"},
			{"hosts", "legacy-host-1", "labels='{\"fixture\":true}'::jsonb"}, {"hosts", "legacy-host-1", "config='{\"fixture\":1}'::jsonb"},
			{"hosts", "legacy-host-1", "available_port_start=23456"}, {"hosts", "legacy-host-1", "vm_id='fixture-vm'"},
			{"projects", "legacy-project-1", "host_id='legacy-host-2'"}, {"projects", "legacy-project-1", "slug='renamed-project'"},
			{"projects", "legacy-project-1", "compose_content='services: {web: {image: alpine}}'"}, {"projects", "legacy-project-1", "env_content='SYNTHETIC=fixture'"},
			{"projects", "legacy-project-1", "config='{\"fixture\":1}'::jsonb"}, {"projects", "legacy-project-1", "labels='{\"fixture\":true}'::jsonb"},
			{"projects", "legacy-project-1", "ttl_seconds=30"}, {"projects", "legacy-project-1", "expires_at=now()+interval '30 seconds'"},
		} {
			t.Run(entry.table+"/"+strings.Split(entry.set, "=")[0], func(t *testing.T) {
				var beforeUID, beforeRevision, afterUID, afterRevision string
				must(t, db.Raw("SELECT resource_uid::text,configuration_revision::text FROM docker_"+entry.table+" WHERE id=?", entry.id).Row().Scan(&beforeUID, &beforeRevision))
				must(t, db.Exec("UPDATE docker_"+entry.table+" SET "+entry.set+" WHERE id=?", entry.id).Error)
				must(t, db.Raw("SELECT resource_uid::text,configuration_revision::text FROM docker_"+entry.table+" WHERE id=?", entry.id).Row().Scan(&afterUID, &afterRevision))
				if beforeUID != afterUID || beforeRevision == afterRevision {
					t.Fatal("legacy configuration write did not rotate revision independently of UID")
				}
			})
		}
	})
	t.Run("runtime_updates_stable", func(t *testing.T) {
		dockerResourceIdentityWithPostgresPhase8(t, &ctx, &db, &must, &repo, &host, &project)
	})
	t.Run("identity_updates_rejected", func(t *testing.T) { assertDockerIdentityColumnsRejectUpdates(t, db) })
	t.Run("repository_return_matches_storage", func(t *testing.T) {
		dockerResourceIdentityWithPostgresPhase9(t, &ctx, &must, &repo, &host, &project, &valid)
	})
	t.Run("delete_recreate_aba", func(t *testing.T) { dockerResourceIdentityWithPostgresPhase10(t, &ctx, &db, &must, &repo) })
	t.Run("concurrent_conditional_update", func(t *testing.T) { dockerResourceIdentityWithPostgresPhase11(t, &db, &must, &host) })
	t.Run("transaction_rollback", func(t *testing.T) { dockerResourceIdentityWithPostgresPhase12(t, &db, &must, &host) })
	t.Run("legacy_json_stays_private", func(t *testing.T) { dockerResourceIdentityWithPostgresPhase13(t, &must, &host, &project) })
	t.Run("migration_sql_replay_stable", func(t *testing.T) {
		dockerResourceIdentityWithPostgresPhase14(t, &store, &ctx, &migrations, &must, &host)
	})
	t.Run("concurrent_migration_replay", func(t *testing.T) {
		dockerResourceIdentityWithPostgresPhase15(t, &store, &ctx, &migrations, &must, &host)
	})
	t.Run("migration_rerun_stable", func(t *testing.T) { dockerResourceIdentityWithPostgresPhase16(t, &store, &ctx, &legacy, &must, &host) })
}

func dockerResourceIdentityWithPostgresPhase1(sharedT **testing.T, sharedCfg *config.DatabaseConfig, sharedAdmin **dbstore.Store, sharedStore **dbstore.Store) {

	if err := (*sharedStore).Close(); err != nil {
		(*sharedT).Error(err)
	}
	if err := (*sharedAdmin).Exec(context.Background(), "DROP DATABASE "+(*sharedCfg).Name); err != nil {
		(*sharedT).Error(err)
	}
}

func dockerResourceIdentityWithPostgresPhase2(tb *testing.T, err error) {

	tb.Helper()
	if err != nil {
		tb.Fatal(err)
	}
}

func dockerResourceIdentityWithPostgresPhase3(tb *testing.T, id string, sharedCtx *context.Context, sharedMust *func(tb *testing.T, err error), sharedRepo **dockerrepo.Repository) domaindocker.Host {

	tb.Helper()
	item, err := (*sharedRepo).GetHost((*sharedCtx), id)
	(*sharedMust)(tb, err)
	return item
}

func dockerResourceIdentityWithPostgresPhase4(tb *testing.T, id string, sharedCtx *context.Context, sharedMust *func(tb *testing.T, err error), sharedRepo **dockerrepo.Repository) domaindocker.Project {

	tb.Helper()
	item, err := (*sharedRepo).GetProject((*sharedCtx), id)
	(*sharedMust)(tb, err)
	return item
}

func dockerResourceIdentityWithPostgresPhase5(tb *testing.T, uidText, revisionText string) {

	tb.Helper()
	for _, value := range []string{uidText, revisionText} {
		parsed, err := uuid.Parse(value)
		if err != nil || parsed == uuid.Nil {
			tb.Fatalf("missing database-owned identity: %q", value)
		}
	}
}

func dockerResourceIdentityWithPostgresPhase6(t *testing.T, sharedHost *func(tb *testing.T, id string) domaindocker.Host, sharedProject *func(tb *testing.T, id string) domaindocker.Project, sharedValid *func(tb *testing.T, uidText string, revisionText string)) {

	seen := map[string]bool{}
	for _, item := range []domaindocker.Host{(*sharedHost)(t, "legacy-host-1"), (*sharedHost)(t, "legacy-host-2")} {
		(*sharedValid)(t, item.ResourceUID, item.ConfigurationRevision)
		if seen[item.ResourceUID] {
			t.Fatal("backfill reused Host UID")
		}
		seen[item.ResourceUID] = true
	}
	for _, item := range []domaindocker.Project{(*sharedProject)(t, "legacy-project-1"), (*sharedProject)(t, "legacy-project-2")} {
		(*sharedValid)(t, item.ResourceUID, item.ConfigurationRevision)
		if seen[item.ResourceUID] {
			t.Fatal("backfill reused resource UID")
		}
		seen[item.ResourceUID] = true
	}
}

func dockerResourceIdentityWithPostgresPhase7(t *testing.T, sharedDb **gorm.DB, sharedMust *func(tb *testing.T, err error), sharedHost *func(tb *testing.T, id string) domaindocker.Host, sharedProject *func(tb *testing.T, id string) domaindocker.Project, sharedValid *func(tb *testing.T, uidText string, revisionText string)) {

	fake := uuid.NewString()
	(*sharedMust)(t, (*sharedDb).Exec(`INSERT INTO docker_hosts(id,name,resource_uid,configuration_revision) VALUES ('caller-host','caller',?,?)`, fake, fake).Error)
	(*sharedMust)(t, (*sharedDb).Exec(`INSERT INTO docker_projects(id,host_id,name,slug,resource_uid,configuration_revision) VALUES ('caller-project','caller-host','caller','caller',?,?)`, fake, fake).Error)
	h, p := (*sharedHost)(t, "caller-host"), (*sharedProject)(t, "caller-project")
	(*sharedValid)(t, h.ResourceUID, h.ConfigurationRevision)
	(*sharedValid)(t, p.ResourceUID, p.ConfigurationRevision)
	if h.ResourceUID == fake || h.ConfigurationRevision == fake || p.ResourceUID == fake || p.ConfigurationRevision == fake {
		t.Fatal("caller controlled identity")
	}
}

func dockerResourceIdentityWithPostgresPhase8(t *testing.T, sharedCtx *context.Context, sharedDb **gorm.DB, sharedMust *func(tb *testing.T, err error), sharedRepo **dockerrepo.Repository, sharedHost *func(tb *testing.T, id string) domaindocker.Host, sharedProject *func(tb *testing.T, id string) domaindocker.Project) {

	h, p := (*sharedHost)(t, "legacy-host-1"), (*sharedProject)(t, "legacy-project-1")
	(*sharedMust)(t, (*sharedRepo).HeartbeatHost((*sharedCtx), h.ID, "fixture-agent"))
	(*sharedMust)(t, (*sharedDb).Exec(`UPDATE docker_hosts SET status='ready',agent_version='fixture',cpu_core_count=2,last_heartbeat_at=now(),updated_at=now() WHERE id=?`, h.ID).Error)
	_, err := (*sharedRepo).UpdateProjectRuntime((*sharedCtx), p.ID, "running", "running", nil)
	(*sharedMust)(t, err)
	if (*sharedHost)(t, h.ID).ResourceUID != h.ResourceUID || (*sharedHost)(t, h.ID).ConfigurationRevision != h.ConfigurationRevision || (*sharedProject)(t, p.ID).ConfigurationRevision != p.ConfigurationRevision {
		t.Fatal("runtime update changed configuration identity")
	}
}

func dockerResourceIdentityWithPostgresPhase9(t *testing.T, sharedCtx *context.Context, sharedMust *func(tb *testing.T, err error), sharedRepo **dockerrepo.Repository, sharedHost *func(tb *testing.T, id string) domaindocker.Host, sharedProject *func(tb *testing.T, id string) domaindocker.Project, sharedValid *func(tb *testing.T, uidText string, revisionText string)) {

	h, err := (*sharedRepo).CreateHost((*sharedCtx), domaindocker.HostInput{ID: "repository-host", Name: "repository"})
	(*sharedMust)(t, err)
	(*sharedValid)(t, h.ResourceUID, h.ConfigurationRevision)
	old := h.ConfigurationRevision
	h, err = (*sharedRepo).UpdateHost((*sharedCtx), h.ID, domaindocker.HostInput{Name: "renamed repository"})
	(*sharedMust)(t, err)
	if h.ConfigurationRevision == old || h.ConfigurationRevision != (*sharedHost)(t, h.ID).ConfigurationRevision {
		t.Fatal("Host update returned stale identity")
	}
	old = h.ConfigurationRevision
	h, err = (*sharedRepo).TouchHostRuntime((*sharedCtx), h.ID, domaindocker.HostInput{Config: map[string]any{"fixture": "changed-runtime-config"}})
	(*sharedMust)(t, err)
	if h.ConfigurationRevision == old || h.ConfigurationRevision != (*sharedHost)(t, h.ID).ConfigurationRevision {
		t.Fatal("runtime config writer returned stale configuration identity")
	}
	p, err := (*sharedRepo).CreateProject((*sharedCtx), domaindocker.ProjectInput{ID: "repository-project", HostID: h.ID, Name: "repository", ComposeContent: "services: {}\n"})
	(*sharedMust)(t, err)
	(*sharedValid)(t, p.ResourceUID, p.ConfigurationRevision)
	old = p.ConfigurationRevision
	p, err = (*sharedRepo).UpdateProject((*sharedCtx), p.ID, domaindocker.ProjectInput{HostID: h.ID, Name: "renamed", ComposeContent: "services: {}\n"})
	(*sharedMust)(t, err)
	if p.ConfigurationRevision == old || p.ConfigurationRevision != (*sharedProject)(t, p.ID).ConfigurationRevision {
		t.Fatal("Project update returned stale identity")
	}
}

func dockerResourceIdentityWithPostgresPhase10(t *testing.T, sharedCtx *context.Context, sharedDb **gorm.DB, sharedMust *func(tb *testing.T, err error), sharedRepo **dockerrepo.Repository) {

	h, err := (*sharedRepo).CreateHost((*sharedCtx), domaindocker.HostInput{ID: "aba-host", Name: "aba"})
	(*sharedMust)(t, err)
	p, err := (*sharedRepo).CreateProject((*sharedCtx), domaindocker.ProjectInput{ID: "aba-project", HostID: h.ID, Name: "aba"})
	(*sharedMust)(t, err)
	(*sharedMust)(t, (*sharedRepo).DeleteProject((*sharedCtx), p.ID))
	nextP, err := (*sharedRepo).CreateProject((*sharedCtx), domaindocker.ProjectInput{ID: p.ID, HostID: h.ID, Name: "aba"})
	(*sharedMust)(t, err)
	if p.ResourceUID == nextP.ResourceUID || p.ConfigurationRevision == nextP.ConfigurationRevision {
		t.Fatal("Project public ID revived old identity")
	}
	stale := (*sharedDb).Exec(`UPDATE docker_projects SET name='stale' WHERE id=? AND resource_uid=? AND configuration_revision=?`, p.ID, p.ResourceUID, p.ConfigurationRevision)
	(*sharedMust)(t, stale.Error)
	if stale.RowsAffected != 0 {
		t.Fatal("stale Project guard matched recreated row")
	}
	(*sharedMust)(t, (*sharedRepo).DeleteProject((*sharedCtx), nextP.ID))
	(*sharedMust)(t, (*sharedRepo).DeleteHost((*sharedCtx), h.ID))
	nextH, err := (*sharedRepo).CreateHost((*sharedCtx), domaindocker.HostInput{ID: h.ID, Name: "aba"})
	(*sharedMust)(t, err)
	if h.ResourceUID == nextH.ResourceUID || h.ConfigurationRevision == nextH.ConfigurationRevision {
		t.Fatal("Host public ID revived old identity")
	}
	stale = (*sharedDb).Exec(`UPDATE docker_hosts SET name='stale' WHERE id=? AND resource_uid=? AND configuration_revision=?`, h.ID, h.ResourceUID, h.ConfigurationRevision)
	(*sharedMust)(t, stale.Error)
	if stale.RowsAffected != 0 {
		t.Fatal("stale Host guard matched recreated row")
	}
}

func dockerResourceIdentityWithPostgresPhase11(t *testing.T, sharedDb **gorm.DB, sharedMust *func(tb *testing.T, err error), sharedHost *func(tb *testing.T, id string) domaindocker.Host) {

	h := (*sharedHost)(t, "repository-host")
	start := make(chan struct{})
	type result struct {
		rows int64
		err  error
	}
	results := make(chan result, 2)
	for _, name := range []string{"first-writer", "second-writer"} {
		go func(name string) {
			<-start
			r := (*sharedDb).Exec(`UPDATE docker_hosts SET name=? WHERE id=? AND resource_uid=? AND configuration_revision=?`, name, h.ID, h.ResourceUID, h.ConfigurationRevision)
			results <- result{r.RowsAffected, r.Error}
		}(name)
	}
	close(start)
	a, b := <-results, <-results
	(*sharedMust)(t, a.err)
	(*sharedMust)(t, b.err)
	if a.rows+b.rows != 1 {
		t.Fatalf("conditional writers affected %d/%d rows", a.rows, b.rows)
	}
}

func dockerResourceIdentityWithPostgresPhase12(t *testing.T, sharedDb **gorm.DB, sharedMust *func(tb *testing.T, err error), sharedHost *func(tb *testing.T, id string) domaindocker.Host) {

	before := (*sharedHost)(t, "legacy-host-1")
	tx := (*sharedDb).Begin()
	(*sharedMust)(t, tx.Error)
	if err := tx.Exec(`UPDATE docker_hosts SET name='rollback' WHERE id=?`, before.ID).Error; err != nil {
		_ = tx.Rollback().Error
		t.Fatal(err)
	}
	var revision string
	(*sharedMust)(t, tx.Raw(`SELECT configuration_revision::text FROM docker_hosts WHERE id=?`, before.ID).Row().Scan(&revision))
	if revision == before.ConfigurationRevision {
		_ = tx.Rollback().Error
		t.Fatal("transaction did not generate a new revision")
	}
	(*sharedMust)(t, tx.Rollback().Error)
	if after := (*sharedHost)(t, before.ID); after.ResourceUID != before.ResourceUID || after.ConfigurationRevision != before.ConfigurationRevision || after.Name != before.Name {
		t.Fatal("rolled back identity escaped transaction")
	}
}

func dockerResourceIdentityWithPostgresPhase13(t *testing.T, sharedMust *func(tb *testing.T, err error), sharedHost *func(tb *testing.T, id string) domaindocker.Host, sharedProject *func(tb *testing.T, id string) domaindocker.Project) {

	h, p := (*sharedHost)(t, "legacy-host-1"), (*sharedProject)(t, "legacy-project-1")
	for _, value := range []any{h, p} {
		raw, err := json.Marshal(value)
		(*sharedMust)(t, err)
		for _, hidden := range []string{h.ResourceUID, h.ConfigurationRevision, p.ResourceUID, p.ConfigurationRevision, "resource_uid", "configurationRevision"} {
			if strings.Contains(string(raw), hidden) {
				t.Fatal("legacy JSON exposed private identity")
			}
		}
	}
}

func dockerResourceIdentityWithPostgresPhase14(t *testing.T, sharedStore **dbstore.Store, sharedCtx *context.Context, sharedMigrations *string, sharedMust *func(tb *testing.T, err error), sharedHost *func(tb *testing.T, id string) domaindocker.Host) {

	before := (*sharedHost)(t, "legacy-host-1")
	// #nosec G304 -- fixed repository migrations directory; filenames are fixed literals or basenames from its os.ReadDir result.
	raw, err := os.ReadFile(filepath.Join((*sharedMigrations), "0130_docker_resource_identity.sql"))
	(*sharedMust)(t, err)
	(*sharedMust)(t, (*sharedStore).Exec((*sharedCtx), string(raw)))
	if after := (*sharedHost)(t, before.ID); after.ResourceUID != before.ResourceUID || after.ConfigurationRevision != before.ConfigurationRevision {
		t.Fatal("migration SQL replay rewrote existing identity")
	}
}

func dockerResourceIdentityWithPostgresPhase15(t *testing.T, sharedStore **dbstore.Store, sharedCtx *context.Context, sharedMigrations *string, sharedMust *func(tb *testing.T, err error), sharedHost *func(tb *testing.T, id string) domaindocker.Host) {

	before := (*sharedHost)(t, "legacy-host-1")
	// #nosec G304 -- fixed repository migrations directory; filenames are fixed literals or basenames from its os.ReadDir result.
	raw, err := os.ReadFile(filepath.Join((*sharedMigrations), "0130_docker_resource_identity.sql"))
	(*sharedMust)(t, err)
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() { <-start; results <- (*sharedStore).Exec((*sharedCtx), string(raw)) }()
	}
	close(start)
	(*sharedMust)(t, <-results)
	(*sharedMust)(t, <-results)
	if after := (*sharedHost)(t, before.ID); after.ResourceUID != before.ResourceUID || after.ConfigurationRevision != before.ConfigurationRevision {
		t.Fatal("concurrent migration changed identity")
	}
}

func dockerResourceIdentityWithPostgresPhase16(t *testing.T, sharedStore **dbstore.Store, sharedCtx *context.Context, sharedLegacy *string, sharedMust *func(tb *testing.T, err error), sharedHost *func(tb *testing.T, id string) domaindocker.Host) {

	before := (*sharedHost)(t, "legacy-host-1")
	(*sharedMust)(t, (*sharedStore).MigrateFromFile((*sharedCtx), filepath.Join((*sharedLegacy), "0001_init.sql")))
	if after := (*sharedHost)(t, before.ID); after.ResourceUID != before.ResourceUID || after.ConfigurationRevision != before.ConfigurationRevision {
		t.Fatal("migration replay rewrote identity")
	}
}

func copyLegacyDockerMigrations(t *testing.T, migrations, legacy string) {
	t.Helper()
	files, err := os.ReadDir(migrations)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".sql") && file.Name() < "0130" {
			// #nosec G304 -- fixed repository migrations directory; filenames are fixed literals or basenames from its os.ReadDir result.
			data, err := os.ReadFile(filepath.Join(migrations, file.Name()))
			if err != nil {
				t.Fatal(err)
			}
			// #nosec G703 -- private t.TempDir destination and fixed migration filename/basename, without external path input.
			if err := os.WriteFile(filepath.Join(legacy, file.Name()), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func assertDockerIdentityColumnsRejectUpdates(t *testing.T, db *gorm.DB) {
	t.Helper()

	for _, table := range []string{"hosts", "projects"} {
		for _, column := range []string{"resource_uid", "configuration_revision"} {
			t.Run(table+"/"+column, func(t *testing.T) {
				err := db.Exec("UPDATE docker_"+table+" SET "+column+"=gen_random_uuid() WHERE id=?", "legacy-"+strings.TrimSuffix(table, "s")+"-1").Error
				var state interface{ SQLState() string }
				if err == nil || !errors.As(err, &state) || state.SQLState() != "23514" {
					t.Fatalf("identity guard error=%v", err)
				}
			})
		}
	}
}
