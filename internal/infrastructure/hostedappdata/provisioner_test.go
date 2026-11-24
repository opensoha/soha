package hostedappdata

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPostgresInputAndErrorRedaction(t *testing.T) {
	const secret = "SyntheticPasswordForTestOnly_135"
	for _, value := range []string{
		"postgres://user:" + secret + "@bad%host/maintenance",
		"postgres://user:" + secret + "@db1,db2/maintenance",
		"postgres://user:" + secret + "@db/maintenance?user=other",
		"postgres://user:" + secret + "@db/maintenance?sslmode=disable&sslmode=require",
		"postgres://user:" + secret + "@db/soha_app_" + strings.Repeat("0", 32),
	} {
		if _, err := New(value, 1); err == nil || strings.Contains(err.Error(), secret) {
			t.Fatal("expected a redacted URL validation error")
		}
	}
	p := mustProvisioner(t, "postgres://user:"+secret+"@db/maintenance", 1)
	if p.endpoint.User != nil || p.endpoint.Query().Get("sslmode") != "require" {
		t.Fatal("runtime endpoint did not discard the administrator identity or default to TLS")
	}
	if _, err := p.Provision(t.Context(), "app_bad", secret); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatal("expected a redacted input validation error")
	}
	err := operationError("connect", &pgconn.PgError{Code: "28000", Message: secret, Detail: secret})
	if strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "28000") {
		t.Fatal("database error redaction lost the SQLSTATE or exposed a password")
	}
}

func TestProvisionWithPostgres(t *testing.T) {
	dsn := os.Getenv("SOHA_HOSTED_APPDATA_TEST_ADMIN_DSN")
	if dsn == "" {
		t.Skip("set SOHA_HOSTED_APPDATA_TEST_ADMIN_DSN to a disposable PostgreSQL cluster")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	p := mustProvisioner(t, dsn, 1<<30)
	admin := connect(t, ctx, dsn)
	var cluster string
	if err := admin.QueryRow(ctx, "SHOW cluster_name").Scan(&cluster); err != nil || cluster != "task135-appdata-test" {
		t.Fatal("integration test requires its own PostgreSQL instance with cluster_name=task135-appdata-test")
	}

	const (
		appA     = "app_0123456789abcdef0123456789abcdef"
		appB     = "app_123456789abcdef0123456789abcdef0"
		appC     = "app_23456789abcdef0123456789abcdef01"
		password = "TestOnlySyntheticHostedApp135_gH5jW3C"
	)
	t.Run("reject_open_default_databases", func(t *testing.T) {
		if _, err := p.Provision(ctx, appA, password); !errors.Is(err, ErrIsolation) {
			t.Fatalf("expected isolation failure, got %v", err)
		}
		assertUnallocated(t, ctx, admin, appA)
	})
	if _, err := admin.Exec(ctx, "REVOKE CONNECT, TEMPORARY ON DATABASE postgres, template1 FROM PUBLIC"); err != nil {
		t.Fatal("could not restrict task-owned system database access")
	}
	var runtimeURL string
	t.Run("reconcile_partial_create_across_replicas", func(t *testing.T) {
		runtimeURL = testReconcilePartialCreate(t, ctx, p, admin, dsn, appA, password)
	})
	if runtimeURL == "" {
		t.Fatal("remaining cases require provisioned app data")
	}

	t.Run("scoped_data_and_schema", func(t *testing.T) {
		testRuntimeAccess(t, ctx, runtimeURL)
		version, err := p.SchemaVersion(ctx, appA)
		if err != nil || version != initialSchemaVersion {
			t.Fatalf("expected schema version %d, got %d, %v", initialSchemaVersion, version, err)
		}
		used, err := p.Usage(ctx, appA)
		if err != nil || used <= 0 {
			t.Fatalf("expected a positive measured size, got %d, %v", used, err)
		}
	})
	t.Run("other_databases_and_default_catalog", func(t *testing.T) {
		if _, err := p.Provision(ctx, appB, password); err != nil {
			t.Fatal(err)
		}
		other, err := url.Parse(runtimeURL)
		if err != nil {
			t.Fatal("invalid runtime URL")
		}
		for _, name := range []string{resourcePrefix + appB, "postgres", "template1"} {
			other.Path = "/" + name
			assertConnectionDenied(t, ctx, other.String(), "42501")
		}
	})
	t.Run("role_connection_limit", func(t *testing.T) {
		for range 5 {
			connect(t, ctx, runtimeURL)
		}
		assertConnectionDenied(t, ctx, runtimeURL, "53300")
	})
	t.Run("capacity_rejects_new_database_and_preserves_existing_data", func(t *testing.T) {
		testProvisionAtCapacity(t, ctx, admin, dsn, appA, appC, password, runtimeURL)
	})
	t.Run("reject_unbound_runtime_role", func(t *testing.T) {
		if _, err := admin.Exec(ctx, "CREATE ROLE "+quoteIdentifier(resourcePrefix+appC+"_runtime")); err != nil {
			t.Fatal("could not stage an unbound role collision")
		}
		if _, err := p.Provision(ctx, appC, password); !errors.Is(err, ErrIsolation) {
			t.Fatalf("expected ownership collision, got %v", err)
		}
	})
	t.Run("revision_read_write_and_cleanup", func(t *testing.T) {
		testRevisionAccess(t, ctx, p, appA, appB, password, runtimeURL)
	})
	t.Run("compatible_schema_upgrade_preserves_versions_and_data", func(t *testing.T) {
		testSchemaUpgrade(t, ctx, p, appA, appB, password)
	})
}

func testReconcilePartialCreate(t *testing.T, ctx context.Context, p *Provisioner, admin *pgx.Conn, dsn, appID, password string) string {
	t.Helper()
	db := resourcePrefix + appID
	if err := reserveRole(ctx, admin, db+"_runtime", managedRole, appConnections); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoteIdentifier(db)+" TEMPLATE template0 ALLOW_CONNECTIONS false"); err != nil {
		t.Fatal("could not stage the interrupted app database creation")
	}
	replica := mustProvisioner(t, dsn, 1<<30)
	type result struct {
		dsn string
		err error
	}
	results := make(chan result, 2)
	for _, provisioner := range []*Provisioner{p, replica} {
		go func() {
			value, err := provisioner.Provision(ctx, appID, password)
			results <- result{dsn: value, err: err}
		}()
	}
	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("reconciliation failed: %v / %v", first.err, second.err)
	}
	if first.dsn == "" || first.dsn != second.dsn {
		t.Fatal("reconciling replicas did not return the same runtime connection URL")
	}
	return first.dsn
}

func testProvisionAtCapacity(t *testing.T, ctx context.Context, admin *pgx.Conn, dsn, existingAppID, newAppID, password, runtimeURL string) {
	t.Helper()
	full := mustProvisioner(t, dsn, 1)
	if _, err := full.Provision(ctx, newAppID, password); !errors.Is(err, ErrCapacity) {
		t.Fatalf("expected capacity failure, got %v", err)
	}
	assertUnallocated(t, ctx, admin, newAppID)
	reconciled, err := full.Provision(ctx, existingAppID, password)
	if err != nil || reconciled != runtimeURL {
		t.Fatalf("reconciliation at capacity failed: %v", err)
	}
	conn := connect(t, ctx, runtimeURL)
	var value string
	err = conn.QueryRow(ctx, "SELECT data->>'status' FROM entries WHERE id = 'retained'").Scan(&value)
	if err != nil || value != "updated" {
		t.Fatal("existing application data did not survive reconciliation")
	}
}

func testRuntimeAccess(t *testing.T, ctx context.Context, runtimeURL string) {
	t.Helper()
	conn := connect(t, ctx, runtimeURL)
	for _, statement := range []string{
		"INSERT INTO entries (id, data) VALUES ('retained', '{\"status\":\"created\"}'), ('remove', '{}')",
		"UPDATE entries SET data = '{\"status\":\"updated\"}' WHERE id = 'retained'",
		"DELETE FROM entries WHERE id = 'remove'",
	} {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatal("runtime role could not write the baseline app table")
		}
	}
	for _, statement := range []string{
		"CREATE TABLE app.forbidden (id int)",
		"CREATE TABLE public.forbidden (id int)",
		"CREATE TEMP TABLE forbidden (id int)",
		"CREATE SCHEMA forbidden",
		"ALTER TABLE app.entries ADD COLUMN forbidden text",
		"TRUNCATE TABLE app.entries",
		"SELECT rolpassword FROM pg_catalog.pg_authid",
		"SELECT pg_catalog.lo_create(0::oid)",
	} {
		_, err := conn.Exec(ctx, statement)
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.Code != "42501" {
			t.Fatalf("runtime role was not denied for %s", statement)
		}
	}
}

func testRevisionAccess(t *testing.T, ctx context.Context, p *Provisioner, appID, otherAppID, password, runtimeURL string) {
	t.Helper()
	const revID = "rev_3456789abcdef0123456789abcdef012"
	dsn, err := p.ProvisionRevision(ctx, appID, revID, password)
	if err != nil {
		t.Fatal(err)
	}
	candidate := connect(t, ctx, dsn)
	var value string
	err = candidate.QueryRow(ctx, "SELECT data->>'status' FROM entries WHERE id = 'retained'").Scan(&value)
	if err != nil || value != "updated" {
		t.Fatal("candidate could not read the current data")
	}
	assertRevisionWriteDenied(t, ctx, candidate)
	for _, query := range []string{
		"UPDATE entries SET data = '{}' WHERE id = 'retained'",
		"DELETE FROM entries WHERE id = 'retained'",
		"SELECT pg_catalog.lo_from_bytea(0, 'test'::bytea)",
		"SELECT pg_catalog.pg_logical_emit_message(true, 'test', 'test'::text)",
	} {
		if _, err := candidate.Exec(ctx, query); !testSQLState(err, "42501") {
			t.Fatalf("candidate SQL was not rejected: %s", query)
		}
	}
	other, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid revision URL")
	}
	other.Path = "/" + resourcePrefix + otherAppID
	assertConnectionDenied(t, ctx, other.String(), "42501")

	if err := p.SetRevisionWritable(ctx, otherAppID, revID, true); !errors.Is(err, ErrIsolation) {
		t.Fatalf("expected revision binding failure, got %v", err)
	}
	if err := p.SetRevisionWritable(ctx, appID, revID, true); err != nil {
		t.Fatal(err)
	}
	if reconciled, err := p.ProvisionRevision(ctx, appID, revID, password); err != nil || reconciled != dsn {
		t.Fatalf("could not reconcile writable revision: %v", err)
	}
	if _, err := candidate.Exec(ctx, "INSERT INTO entries VALUES ('revision_write', '{}')"); err != nil {
		t.Fatal("reconciliation did not preserve the revision write grant")
	}
	if err := p.CheckRevisionCredential(ctx, appID, revID, password); err != nil {
		t.Fatalf("ready revision credential could not be verified: %v", err)
	}
	if err := p.SetRevisionWritable(ctx, appID, revID, false); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckRevisionCredential(ctx, appID, revID, password); err == nil {
		t.Fatal("rollback accepted a credential with revoked writes")
	}
	assertRevisionWriteDenied(t, ctx, candidate)
	connect(t, ctx, dsn)
	assertConnectionDenied(t, ctx, dsn, "53300")
	previous := connect(t, ctx, runtimeURL)
	connect(t, ctx, runtimeURL)
	connect(t, ctx, runtimeURL)
	assertConnectionDenied(t, ctx, runtimeURL, "53300")
	if err := p.DeleteRevisionRole(ctx, appID, revID); err != nil {
		t.Fatal(err)
	}
	if _, err := candidate.Exec(ctx, "SELECT 1"); err == nil {
		t.Fatal("runtime session survived role cleanup")
	}
	if _, err := previous.Exec(ctx, "SELECT 1"); err != nil {
		t.Fatal("revision role cleanup closed another role's session")
	}
	assertConnectionDenied(t, ctx, dsn, "28P01")
	if err := p.CheckRevisionCredential(ctx, appID, revID, password); err == nil {
		t.Fatal("rollback accepted a deleted database login")
	}
	if err := p.DeleteRevisionRole(ctx, appID, revID); err != nil {
		t.Fatal("revision role cleanup was not idempotent")
	}
}

func assertRevisionWriteDenied(t *testing.T, ctx context.Context, candidate *pgx.Conn) {
	t.Helper()
	if _, err := candidate.Exec(ctx, "INSERT INTO entries VALUES ('forbidden', '{}')"); !testSQLState(err, "42501") {
		t.Fatal("read-only revision received write access")
	}
}

func testSchemaUpgrade(t *testing.T, ctx context.Context, p *Provisioner, appID, otherAppID, password string) {
	t.Helper()
	const (
		oldRevision = "rev_456789abcdef0123456789abcdef0123"
		newRevision = "rev_56789abcdef0123456789abcdef01234"
	)
	oldRuntime := connectWritableRevision(t, ctx, p, appID, oldRevision, password)
	if _, err := oldRuntime.Exec(ctx, "INSERT INTO entries (id, data) VALUES ('migration', '{\"value\":\"v1\"}')"); err != nil {
		t.Fatal("v1 could not insert data")
	}
	if err := p.UpgradeSchema(ctx, appID, compatibleSchemaVersion+1); !errors.Is(err, ErrSchemaVersion) {
		t.Fatalf("expected unsupported schema target, got %v", err)
	}
	if err := p.UpgradeSchema(ctx, appID, initialSchemaVersion); err != nil {
		t.Fatal(err)
	}
	assertBusyMigrationKeepsV1(t, ctx, p, appID, oldRuntime)
	if err := p.UpgradeSchema(ctx, appID, compatibleSchemaVersion); err != nil {
		t.Fatal(err)
	}
	if err := p.UpgradeSchema(ctx, appID, compatibleSchemaVersion); err != nil {
		t.Fatalf("upgrading to the current version failed: %v", err)
	}
	if _, err := p.Provision(ctx, appID, password); err != nil {
		t.Fatalf("provision did not recognize the upgraded database: %v", err)
	}
	if version, err := p.SchemaVersion(ctx, appID); err != nil || version != compatibleSchemaVersion {
		t.Fatalf("expected installed schema v2, got v%d, %v", version, err)
	}
	if version, err := p.SchemaVersion(ctx, otherAppID); err != nil || version != initialSchemaVersion {
		t.Fatalf("another application's schema changed to v%d, %v", version, err)
	}

	newRuntime := connectWritableRevision(t, ctx, p, appID, newRevision, password)
	assertCompatibleRevisionWrites(t, ctx, p, appID, oldRuntime, newRuntime)
}

func assertCompatibleRevisionWrites(t *testing.T, ctx context.Context, p *Provisioner, appID string, oldRuntime, newRuntime *pgx.Conn) {
	t.Helper()
	var value string
	var timestampMissing bool
	err := newRuntime.QueryRow(ctx, "SELECT data->>'value', updated_at IS NULL FROM entries WHERE id = 'migration'").Scan(&value, &timestampMissing)
	if err != nil || value != "v1" || !timestampMissing {
		t.Fatal("upgrade did not preserve the original row and nullable column")
	}
	if _, err := newRuntime.Exec(ctx, "UPDATE entries SET data = '{\"value\":\"v2\"}', updated_at = now() WHERE id = 'migration'"); err != nil {
		t.Fatal("v2 could not write its added field")
	}
	if err := p.UpgradeSchema(ctx, appID, initialSchemaVersion); !errors.Is(err, ErrSchemaVersion) {
		t.Fatalf("expected downgrade rejection, got %v", err)
	}
	err = oldRuntime.QueryRow(ctx, "SELECT data->>'value' FROM entries WHERE id = 'migration'").Scan(&value)
	if err != nil || value != "v2" {
		t.Fatal("v1 could not read the data written by v2")
	}
	if _, err := oldRuntime.Exec(ctx, "UPDATE entries SET data = '{\"value\":\"v1_again\"}' WHERE id = 'migration'"); err != nil {
		t.Fatal("v1 could not keep writing after upgrade")
	}
	err = newRuntime.QueryRow(ctx, "SELECT data->>'value', updated_at IS NULL FROM entries WHERE id = 'migration'").Scan(&value, &timestampMissing)
	if err != nil || value != "v1_again" || timestampMissing {
		t.Fatal("v1 write affected the v2 field")
	}
}

func assertBusyMigrationKeepsV1(t *testing.T, ctx context.Context, p *Provisioner, appID string, oldRuntime *pgx.Conn) {
	t.Helper()
	tx, err := oldRuntime.Begin(ctx)
	if err != nil {
		t.Fatal("v1 could not open a read transaction")
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Error(operationError("roll back integration transaction", err))
		}
	}()
	if _, err := tx.Exec(ctx, "SELECT id, data FROM entries"); err != nil {
		t.Fatal("v1 could not read its data")
	}
	err = p.UpgradeSchema(ctx, appID, compatibleSchemaVersion)
	if err == nil || !strings.Contains(err.Error(), "55P03") {
		t.Fatalf("expected lock timeout for busy app data, got %v", err)
	}
	if version, err := p.SchemaVersion(ctx, appID); err != nil || version != initialSchemaVersion {
		t.Fatalf("failed upgrade changed the installed version to v%d, %v", version, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal("failed upgrade affected the open v1 transaction")
	}
}

func connectWritableRevision(t *testing.T, ctx context.Context, p *Provisioner, appID, revisionID, password string) *pgx.Conn {
	t.Helper()
	dsn, err := p.ProvisionRevision(ctx, appID, revisionID, password)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetRevisionWritable(ctx, appID, revisionID, true); err != nil {
		t.Fatal(err)
	}
	return connect(t, ctx, dsn)
}

func testSQLState(err error, code string) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == code
}

func mustProvisioner(t *testing.T, dsn string, maxPoolBytes int64) *Provisioner {
	t.Helper()
	p, err := New(dsn, maxPoolBytes)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func connect(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(operationError("connect integration role", err))
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	return conn
}

func assertConnectionDenied(t *testing.T, ctx context.Context, dsn, code string) {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err == nil {
		_ = conn.Close(ctx)
		t.Fatal("runtime role opened a forbidden connection")
	}
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != code {
		t.Fatalf("expected SQLSTATE %s, got %v", code, operationError("connect integration role", err))
	}
}

func assertUnallocated(t *testing.T, ctx context.Context, conn *pgx.Conn, appID string) {
	t.Helper()
	var exists bool
	err := conn.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_database WHERE datname = $1)
		OR EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = $2)
	`, resourcePrefix+appID, resourcePrefix+appID+"_runtime").Scan(&exists)
	if err != nil || exists {
		t.Fatal("failed provisioning left an unexpected app database or role")
	}
}
