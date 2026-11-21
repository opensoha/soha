package hostedappdata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	revisionConnections  = 2
	revisionCheckTimeout = 5 * time.Second
)

var revisionIDPattern = regexp.MustCompile(`^rev_[0-9a-f]{32}$`)

type revisionBinding struct {
	database string
	role     string
	marker   string
}

func bindRevision(appID, revisionID string) (revisionBinding, error) {
	if !appIDPattern.MatchString(appID) || !revisionIDPattern.MatchString(revisionID) {
		return revisionBinding{}, errors.New("invalid hosted app data application or revision ID")
	}
	digest := sha256.Sum256([]byte(appID + ":" + revisionID))
	return revisionBinding{
		database: resourcePrefix + appID,
		role:     "soha_rev_" + hex.EncodeToString(digest[:24]),
		marker:   "OpenSoha hosted app-data revision " + appID + " " + revisionID,
	}, nil
}

// RevisionRuntimeURL returns the platform-generated revision connection URL
// without creating, reconciling, or rotating any database credential.
func (p *Provisioner) RevisionRuntimeURL(appID, revisionID, password string) (string, error) {
	target, err := bindRevision(appID, revisionID)
	if err != nil {
		return "", err
	}
	if !passwordPattern.MatchString(password) {
		return "", errors.New("invalid hosted app data revision password")
	}
	return p.runtimeURL(target.database, target.role, password), nil
}

// CheckRevisionCredential verifies the revision login and application grants without
// reconciling the pool. It returns an error only after bounded, read-only work.
func (p *Provisioner) CheckRevisionCredential(ctx context.Context, appID, revisionID, password string) error {
	dsn, err := p.RevisionRuntimeURL(appID, revisionID, password)
	if err != nil {
		return err
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return errors.New("invalid hosted app data revision connection settings")
	}
	ctx, cancel := context.WithTimeout(ctx, revisionCheckTimeout)
	defer cancel()
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return operationError("verify revision login", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var granted bool
	err = conn.QueryRow(ctx, `
		SELECT pg_catalog.has_schema_privilege(current_user, 'app', 'USAGE')
			AND pg_catalog.has_table_privilege(current_user, 'app.entries', 'SELECT')
			AND pg_catalog.has_table_privilege(current_user, 'app.entries', 'INSERT')
			AND pg_catalog.has_table_privilege(current_user, 'app.entries', 'UPDATE')
			AND pg_catalog.has_table_privilege(current_user, 'app.entries', 'DELETE')
	`).Scan(&granted)
	if err != nil {
		return operationError("verify revision grants", err)
	}
	if !granted {
		return errors.New("hosted app data: revision grants are unavailable")
	}
	return nil
}

// ProvisionRevision issues a revision-specific credential with read access to the
// baseline app data. Reconciling an active revision preserves its existing grants.
func (p *Provisioner) ProvisionRevision(ctx context.Context, appID, revisionID, password string) (string, error) {
	target, err := bindRevision(appID, revisionID)
	if err != nil {
		return "", err
	}
	if !passwordPattern.MatchString(password) {
		return "", errors.New("invalid hosted app data revision password")
	}
	ctx, cancel := context.WithTimeout(ctx, poolTimeout)
	defer cancel()
	conn, err := p.lockAdmin(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Close(ctx)
	if err := p.prepareRevision(ctx, conn, target); err != nil {
		return "", err
	}
	if err := activateRole(ctx, conn, target.role, password); err != nil {
		return "", err
	}
	return p.runtimeURL(target.database, target.role, password), nil
}

func (p *Provisioner) prepareRevision(ctx context.Context, conn *pgx.Conn, target revisionBinding) error {
	if err := requireManagedDatabase(ctx, conn, target.database); err != nil {
		return err
	}
	roleOID, _, err := existingRole(ctx, conn, target.role, target.marker, revisionConnections)
	if err != nil {
		return err
	}
	if err := checkPoolIsolation(ctx, conn, target.database, roleOID); err != nil {
		return err
	}
	if roleOID == 0 {
		if err := reserveRole(ctx, conn, target.role, target.marker, revisionConnections); err != nil {
			return err
		}
	}
	database, role := quoteIdentifier(target.database), quoteIdentifier(target.role)
	return p.revisionGrants(ctx, target, "grant candidate read access",
		"GRANT USAGE ON SCHEMA app TO "+role,
		"GRANT SELECT ON TABLE app.entries TO "+role,
		"ALTER ROLE "+role+" IN DATABASE "+database+" SET search_path TO app",
		"GRANT CONNECT ON DATABASE "+database+" TO "+role,
	)
}

// SetRevisionWritable changes the write grants of a single provisioned revision.
func (p *Provisioner) SetRevisionWritable(ctx context.Context, appID, revisionID string, writable bool) error {
	target, err := bindRevision(appID, revisionID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, poolTimeout)
	defer cancel()
	conn, err := p.lockAdmin(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	if err := requireManagedDatabase(ctx, conn, target.database); err != nil {
		return err
	}
	roleOID, login, err := existingRole(ctx, conn, target.role, target.marker, revisionConnections)
	if err != nil {
		return err
	}
	if !login {
		return fmt.Errorf("%w: revision login is not provisioned", ErrIsolation)
	}
	role := quoteIdentifier(target.role)
	if writable {
		if err := checkPoolIsolation(ctx, conn, target.database, roleOID); err != nil {
			return err
		}
		return p.revisionGrants(ctx, target, "grant revision writes", "GRANT INSERT, UPDATE, DELETE ON TABLE app.entries TO "+role)
	}
	return p.revisionGrants(ctx, target, "revoke revision writes", "REVOKE INSERT, UPDATE, DELETE ON TABLE app.entries FROM "+role)
}

// DeleteRevisionRole disables the bound login, closes its sessions, and removes
// the grants and role reserved by the platform for the revision.
func (p *Provisioner) DeleteRevisionRole(ctx context.Context, appID, revisionID string) error {
	target, err := bindRevision(appID, revisionID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, poolTimeout)
	defer cancel()
	conn, err := p.lockAdmin(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	roleOID, _, err := existingRole(ctx, conn, target.role, target.marker, revisionConnections)
	if err != nil || roleOID == 0 {
		return err
	}
	if err := requireManagedDatabase(ctx, conn, target.database); err != nil {
		return err
	}
	role := quoteIdentifier(target.role)
	if err := disconnectRevision(ctx, conn, role, roleOID); err != nil {
		return err
	}
	database := quoteIdentifier(target.database)
	if err := p.revisionGrants(ctx, target, "remove revision grants",
		"REVOKE ALL ON TABLE app.entries FROM "+role,
		"REVOKE ALL ON SCHEMA app FROM "+role,
		"REVOKE ALL ON DATABASE "+database+" FROM "+role,
	); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, "DROP ROLE "+role); err != nil {
		return operationError("drop revision role", err)
	}
	return nil
}

func disconnectRevision(ctx context.Context, conn *pgx.Conn, role string, roleOID uint32) error {
	if _, err := conn.Exec(ctx, "ALTER ROLE "+role+" NOLOGIN"); err != nil {
		return operationError("disable revision login", err)
	}
	var disconnected bool
	err := conn.QueryRow(ctx, `
		SELECT COALESCE(bool_and(pg_catalog.pg_terminate_backend(pid, 5000)), true)
		FROM pg_catalog.pg_stat_activity WHERE usesysid = $1
	`, roleOID).Scan(&disconnected)
	if err != nil {
		return operationError("disconnect revision sessions", err)
	}
	if !disconnected {
		return errors.New("hosted app data: a revision session did not disconnect")
	}
	return nil
}

func requireManagedDatabase(ctx context.Context, conn *pgx.Conn, database string) error {
	var exists bool
	err := conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_catalog.pg_database d
			WHERE d.datname = $1 AND d.datallowconn
				AND d.datdba = (SELECT oid FROM pg_catalog.pg_roles WHERE rolname = CURRENT_USER)
				AND pg_catalog.shobj_description(d.oid, 'pg_database') = $2
		)
	`, database, managedDatabase).Scan(&exists)
	if err != nil {
		return operationError("inspect managed application database", err)
	}
	if !exists {
		return fmt.Errorf("%w: application database is not provisioned", ErrIsolation)
	}
	return nil
}

func (p *Provisioner) revisionGrants(ctx context.Context, target revisionBinding, operation string, statements ...string) error {
	conn, err := p.connectDatabase(ctx, target.database)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	return executeTransaction(ctx, conn, operation, statements...)
}
