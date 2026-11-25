package hostedappdata

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
)

const (
	managedDatabase = "OpenSoha hosted app-data database"
	managedRole     = "OpenSoha hosted app-data runtime"
	appConnections  = 5
)

func (p *Provisioner) prepareDatabase(ctx context.Context, conn *pgx.Conn, database, role string) error {
	roleOID, roleLogin, err := existingRole(ctx, conn, role, managedRole, appConnections)
	if err != nil {
		return err
	}
	exists, err := existingDatabase(ctx, conn, database, roleOID, roleLogin)
	if err != nil {
		return err
	}
	if err := checkPoolIsolation(ctx, conn, database, roleOID); err != nil {
		return err
	}
	if exists {
		return secureDatabase(ctx, conn, database, role)
	}
	if roleLogin {
		return fmt.Errorf("%w: application database missing for an active role", ErrIsolation)
	}
	if err := p.checkCapacity(ctx, conn); err != nil {
		return err
	}
	if roleOID == 0 {
		if err := reserveRole(ctx, conn, role, managedRole, appConnections); err != nil {
			return err
		}
	}
	// CREATE DATABASE cannot run in a transaction. Keep it closed until its ACL commits.
	_, err = conn.Exec(ctx, "CREATE DATABASE "+quoteIdentifier(database)+" TEMPLATE template0 ALLOW_CONNECTIONS false")
	if err != nil {
		return operationError("create application database", err)
	}
	return secureDatabase(ctx, conn, database, role)
}

func existingRole(ctx context.Context, conn *pgx.Conn, role, marker string, connectionLimit int) (uint32, bool, error) {
	var oid uint32
	var login, bound bool
	err := conn.QueryRow(ctx, `
		SELECT r.oid, r.rolcanlogin,
			COALESCE(pg_catalog.shobj_description(r.oid, 'pg_authid') = $2, false)
			AND NOT (r.rolsuper OR r.rolinherit OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication OR r.rolbypassrls)
			AND r.rolconnlimit = $3
			AND NOT EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m WHERE m.member = r.oid)
		FROM pg_catalog.pg_roles r
		WHERE r.rolname = $1
	`, role, marker, connectionLimit).Scan(&oid, &login, &bound)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, operationError("inspect application role", err)
	}
	if !bound {
		return 0, false, fmt.Errorf("%w: unexpected application role", ErrIsolation)
	}
	return oid, login, nil
}

func existingDatabase(ctx context.Context, conn *pgx.Conn, database string, roleOID uint32, roleLogin bool) (bool, error) {
	var owned, closed bool
	var marker string
	err := conn.QueryRow(ctx, `
		SELECT d.datdba = (SELECT oid FROM pg_catalog.pg_roles WHERE rolname = CURRENT_USER) AND NOT d.datistemplate,
			NOT d.datallowconn, COALESCE(pg_catalog.shobj_description(d.oid, 'pg_database'), '')
		FROM pg_catalog.pg_database d
		WHERE d.datname = $1
	`, database).Scan(&owned, &closed, &marker)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, operationError("inspect application database", err)
	}
	resumingCreate := closed && marker == "" && !roleLogin
	if !owned || roleOID == 0 || marker != managedDatabase && !resumingCreate {
		return false, fmt.Errorf("%w: unexpected application database", ErrIsolation)
	}
	return true, nil
}

func checkPoolIsolation(ctx context.Context, conn *pgx.Conn, database string, roleOID uint32) error {
	var exposed bool
	err := conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_catalog.pg_database d
			WHERE d.datallowconn AND d.datname <> $1 AND (
				EXISTS (
					SELECT 1 FROM pg_catalog.aclexplode(COALESCE(d.datacl, pg_catalog.acldefault('d', d.datdba))) acl
					WHERE acl.grantee = 0 AND acl.privilege_type IN ('CONNECT', 'TEMPORARY')
				)
				OR CASE WHEN $2::oid = 0 THEN false ELSE pg_catalog.has_database_privilege($2, d.oid, 'CONNECT') END
			)
		)
	`, database, roleOID).Scan(&exposed)
	if err != nil {
		return operationError("check database pool isolation", err)
	}
	if exposed {
		return fmt.Errorf("%w: remove PUBLIC CONNECT and TEMPORARY and foreign runtime access from other connectable databases", ErrIsolation)
	}
	return nil
}

func (p *Provisioner) checkCapacity(ctx context.Context, conn *pgx.Conn) error {
	var used, templateBytes int64
	err := conn.QueryRow(ctx, `
		SELECT SUM(pg_catalog.pg_database_size(d.oid))::bigint,
			MAX(pg_catalog.pg_database_size(d.oid)) FILTER (WHERE d.datname = 'template0')
		FROM pg_catalog.pg_database d
	`).Scan(&used, &templateBytes)
	if err != nil {
		return operationError("measure database pool capacity", err)
	}
	if used >= p.poolBytes || templateBytes > p.poolBytes-used {
		return ErrCapacity
	}
	return nil
}

func reserveRole(ctx context.Context, conn *pgx.Conn, role, marker string, connectionLimit int) error {
	identifier := quoteIdentifier(role)
	return executeTransaction(ctx, conn, "reserve application role",
		"CREATE ROLE "+identifier+" NOLOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT "+strconv.Itoa(connectionLimit),
		"COMMENT ON ROLE "+identifier+" IS '"+marker+"'",
	)
}

func secureDatabase(ctx context.Context, conn *pgx.Conn, database, role string) error {
	dbIdentifier, roleIdentifier := quoteIdentifier(database), quoteIdentifier(role)
	return executeTransaction(ctx, conn, "secure application database",
		"REVOKE ALL ON DATABASE "+dbIdentifier+" FROM PUBLIC",
		"REVOKE CREATE, TEMPORARY ON DATABASE "+dbIdentifier+" FROM "+roleIdentifier,
		"COMMENT ON DATABASE "+dbIdentifier+" IS '"+managedDatabase+"'",
		"ALTER DATABASE "+dbIdentifier+" ALLOW_CONNECTIONS true",
		"ALTER DATABASE "+dbIdentifier+" CONNECTION LIMIT "+strconv.Itoa(appConnections),
	)
}

func quoteIdentifier(name string) string {
	return pgx.Identifier{name}.Sanitize()
}

func executeTransaction(ctx context.Context, conn *pgx.Conn, operation string, statements ...string) error {
	err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		return executeStatements(ctx, tx, statements...)
	})
	if err != nil {
		return operationError(operation, err)
	}
	return nil
}

func executeStatements(ctx context.Context, tx pgx.Tx, statements ...string) error {
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
