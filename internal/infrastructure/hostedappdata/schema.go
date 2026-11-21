package hostedappdata

import (
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const (
	initialSchemaVersion    = 1
	compatibleSchemaVersion = 2
	schemaV1Marker          = "OpenSoha hosted app-data schema v1"
	schemaV2Marker          = "OpenSoha hosted app-data schema v2"
	readSchemaVersion       = `
		SELECT COALESCE(pg_catalog.obj_description(n.oid, 'pg_namespace'), '')
		FROM pg_catalog.pg_namespace n
		WHERE n.nspname = 'app'
			AND n.nspowner = (SELECT oid FROM pg_catalog.pg_roles WHERE rolname = CURRENT_USER)
	`
)

// ErrSchemaVersion indicates an unsupported schema target or an attempted downgrade.
var ErrSchemaVersion = errors.New("unsupported hosted application schema version")

// UpgradeSchema applies a known additive schema upgrade to an owned application.
func (p *Provisioner) UpgradeSchema(ctx context.Context, appID string, target int) error {
	if !appIDPattern.MatchString(appID) {
		return errors.New("invalid hosted app data application ID")
	}
	if target != initialSchemaVersion && target != compatibleSchemaVersion {
		return fmt.Errorf("%w: target %d", ErrSchemaVersion, target)
	}
	ctx, cancel := context.WithTimeout(ctx, poolTimeout)
	defer cancel()
	admin, err := p.lockAdmin(ctx)
	if err != nil {
		return err
	}
	defer admin.Close(ctx)
	database := resourcePrefix + appID
	if err := requireManagedDatabase(ctx, admin, database); err != nil {
		return err
	}
	conn, err := p.connectDatabase(ctx, database)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		return applySchemaUpgrade(ctx, tx, target)
	})
	if err != nil && !errors.Is(err, ErrIsolation) && !errors.Is(err, ErrSchemaVersion) {
		return operationError("upgrade application schema", err)
	}
	return err
}

func applySchemaUpgrade(ctx context.Context, tx pgx.Tx, target int) error {
	var marker string
	if err := tx.QueryRow(ctx, readSchemaVersion).Scan(&marker); err != nil {
		return err
	}
	current, err := parseSchemaVersion(marker)
	if err != nil || target == current {
		return err
	}
	if target < current {
		return fmt.Errorf("%w: cannot downgrade from %d to %d", ErrSchemaVersion, current, target)
	}
	// Adding a column takes an exclusive lock, even with no data rewrite.
	return executeStatements(ctx, tx,
		"SET LOCAL lock_timeout = '1s'",
		"ALTER TABLE app.entries ADD COLUMN updated_at timestamp with time zone",
		"COMMENT ON SCHEMA app IS '"+schemaV2Marker+"'",
	)
}

// SchemaVersion returns the version installed by the platform in the application database.
func (p *Provisioner) SchemaVersion(ctx context.Context, appID string) (int, error) {
	if !appIDPattern.MatchString(appID) {
		return 0, errors.New("invalid hosted app data application ID")
	}
	ctx, cancel := context.WithTimeout(ctx, poolTimeout)
	defer cancel()
	conn, err := p.connectDatabase(ctx, resourcePrefix+appID)
	if err != nil {
		return 0, err
	}
	defer conn.Close(ctx)
	var marker string
	err = conn.QueryRow(ctx, readSchemaVersion+`
		AND EXISTS (
			SELECT 1 FROM pg_catalog.pg_database d
			WHERE d.datname = CURRENT_DATABASE()
				AND d.datdba = n.nspowner
				AND pg_catalog.shobj_description(d.oid, 'pg_database') = $1
		)
	`, managedDatabase).Scan(&marker)
	if err != nil {
		return 0, operationError("read application schema version", err)
	}
	return parseSchemaVersion(marker)
}

func parseSchemaVersion(marker string) (int, error) {
	switch marker {
	case schemaV1Marker:
		return initialSchemaVersion, nil
	case schemaV2Marker:
		return compatibleSchemaVersion, nil
	default:
		return 0, fmt.Errorf("%w: unexpected application schema version", ErrIsolation)
	}
}

func (p *Provisioner) connectDatabase(ctx context.Context, database string) (*pgx.Conn, error) {
	config := p.admin.Copy()
	config.Database = database
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, operationError("connect to application database", err)
	}
	return conn, nil
}

func (p *Provisioner) configureSchema(ctx context.Context, database, role string) error {
	conn, err := p.connectDatabase(ctx, database)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	dbIdentifier, roleIdentifier := quoteIdentifier(database), quoteIdentifier(role)
	err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if err := bootstrapSchema(ctx, tx); err != nil {
			return err
		}
		if err := restrictCatalogWrites(ctx, tx, role); err != nil {
			return err
		}
		return executeStatements(ctx, tx,
			"REVOKE ALL ON SCHEMA public, app FROM PUBLIC",
			"REVOKE CREATE ON SCHEMA app FROM "+roleIdentifier,
			"GRANT USAGE ON SCHEMA app TO "+roleIdentifier,
			"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA app TO "+roleIdentifier,
			"GRANT USAGE ON ALL SEQUENCES IN SCHEMA app TO "+roleIdentifier,
			"ALTER DEFAULT PRIVILEGES IN SCHEMA app GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO "+roleIdentifier,
			"ALTER DEFAULT PRIVILEGES IN SCHEMA app GRANT USAGE ON SEQUENCES TO "+roleIdentifier,
			"ALTER ROLE "+roleIdentifier+" IN DATABASE "+dbIdentifier+" SET search_path TO app",
			"GRANT CONNECT ON DATABASE "+dbIdentifier+" TO "+roleIdentifier,
		)
	})
	if errors.Is(err, ErrIsolation) {
		return err
	}
	if err != nil {
		return operationError("configure application schema", err)
	}
	return nil
}

func restrictCatalogWrites(ctx context.Context, tx pgx.Tx, role string) error {
	// Catalog routines can write persistent state without table or schema grants.
	const writeFunctions = `
		FROM pg_catalog.pg_proc p
		WHERE p.pronamespace = 'pg_catalog'::regnamespace
			AND p.proname IN ('lo_create', 'lo_creat', 'lo_from_bytea', 'pg_logical_emit_message')
	`
	var functions string
	err := tx.QueryRow(ctx, "SELECT string_agg(p.oid::regprocedure::text, ', ') "+writeFunctions).Scan(&functions)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "REVOKE EXECUTE ON FUNCTION "+functions+" FROM PUBLIC"); err != nil {
		return err
	}
	var accessible bool
	err = tx.QueryRow(ctx, "SELECT bool_or(pg_catalog.has_function_privilege($1::name, p.oid, 'EXECUTE')) "+writeFunctions, role).Scan(&accessible)
	if err != nil {
		return err
	}
	if accessible {
		return fmt.Errorf("%w: administrator must restrict catalog write functions", ErrIsolation)
	}
	return nil
}

func bootstrapSchema(ctx context.Context, tx pgx.Tx) error {
	var marker string
	err := tx.QueryRow(ctx, readSchemaVersion).Scan(&marker)
	if errors.Is(err, pgx.ErrNoRows) {
		return executeStatements(ctx, tx,
			"CREATE SCHEMA app",
			"CREATE TABLE app.entries (id text PRIMARY KEY, data jsonb NOT NULL)",
			"COMMENT ON SCHEMA app IS '"+schemaV1Marker+"'",
		)
	}
	if err != nil {
		return err
	}
	_, err = parseSchemaVersion(marker)
	return err
}

func activateRole(ctx context.Context, conn *pgx.Conn, role, password string) error {
	verifier, err := scramVerifier(password)
	if err != nil {
		return operationError("generate application password verifier", err)
	}
	// PostgreSQL utility statements cannot bind PASSWORD. format quotes the verifier and role.
	var statement string
	if err := conn.QueryRow(ctx, "SELECT pg_catalog.format('ALTER ROLE %I LOGIN PASSWORD %L', $1::text, $2::text)", role, verifier).Scan(&statement); err != nil {
		return operationError("prepare application login", err)
	}
	if _, err := conn.Exec(ctx, statement); err != nil {
		return operationError("enable application login", err)
	}
	return nil
}

// scramVerifier uses PostgreSQL's SCRAM password format so SQL never contains
// the plaintext password. Provision validates the input as SASLprep-safe ASCII.
func scramVerifier(password string) (string, error) {
	const iterations = 4096
	var salt [16]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return "", err
	}
	salted, err := pbkdf2.Key(sha256.New, password, salt[:], iterations, sha256.Size)
	if err != nil {
		return "", err
	}
	clientMAC := hmac.New(sha256.New, salted)
	clientMAC.Write([]byte("Client Key"))
	stored := sha256.Sum256(clientMAC.Sum(nil))
	serverMAC := hmac.New(sha256.New, salted)
	serverMAC.Write([]byte("Server Key"))
	encode := base64.StdEncoding.EncodeToString
	return fmt.Sprintf("SCRAM-SHA-256$%d:%s$%s:%s", iterations, encode(salt[:]), encode(stored[:]), encode(serverMAC.Sum(nil))), nil
}
