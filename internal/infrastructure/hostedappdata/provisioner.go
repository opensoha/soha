// Package hostedappdata provisions application databases in the dedicated data pool.
package hostedappdata

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	resourcePrefix = "soha_"
	poolLockKey    = 0x736f686161707064
	poolTimeout    = 30 * time.Second
)

var (
	// ErrCapacity indicates the pool cannot admit a new database.
	ErrCapacity = errors.New("hosted app data pool is at capacity")
	// ErrIsolation indicates the pool permits unwanted access or an existing resource has unexpected ownership.
	ErrIsolation = errors.New("hosted app data pool isolation check failed")

	appIDPattern    = regexp.MustCompile(`^app_[0-9a-f]{32}$`)
	passwordPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{32,128}$`)
)

// Provisioner connects to the dedicated app-data pool using its administrator's
// maintenance database. All Core replicas must use the same maintenance database.
type Provisioner struct {
	admin     *pgx.ConnConfig
	endpoint  url.URL
	poolBytes int64
}

// New configures a dedicated PostgreSQL pool and its logical database-size
// admission threshold. Use a single TCP URL with an explicit maintenance
// database, administrator username, and optional sslmode (default require).
func New(adminDSN string, maxPoolBytes int64) (*Provisioner, error) {
	if maxPoolBytes <= 0 {
		return nil, errors.New("hosted app data pool capacity must be positive")
	}
	endpoint, err := url.Parse(strings.TrimSpace(adminDSN))
	if err != nil || !validEndpoint(endpoint) {
		return nil, errors.New("invalid hosted app data administrator URL")
	}
	parameters, err := url.ParseQuery(endpoint.RawQuery)
	if err != nil || len(parameters) > 0 && (len(parameters) != 1 || len(parameters["sslmode"]) != 1 || parameters.Get("sslmode") == "") {
		return nil, errors.New("invalid hosted app data administrator URL parameters")
	}
	if !parameters.Has("sslmode") {
		parameters.Set("sslmode", "require")
	}
	if endpoint.Port() == "" {
		endpoint.Host = net.JoinHostPort(endpoint.Hostname(), "5432")
	}
	endpoint.RawQuery = parameters.Encode()
	admin, err := pgx.ParseConfig(endpoint.String())
	if err != nil {
		return nil, errors.New("invalid hosted app data administrator connection settings")
	}
	admin.RuntimeParams["search_path"] = "pg_catalog"
	endpoint.User = nil
	return &Provisioner{admin: admin, endpoint: *endpoint, poolBytes: maxPoolBytes}, nil
}

func validEndpoint(endpoint *url.URL) bool {
	if endpoint.Scheme != "postgres" && endpoint.Scheme != "postgresql" {
		return false
	}
	database := strings.TrimPrefix(endpoint.Path, "/")
	return endpoint.Hostname() != "" && !strings.ContainsAny(endpoint.Hostname(), ",/\\") &&
		endpoint.User != nil && endpoint.User.Username() != "" && !strings.ContainsRune(endpoint.User.Username(), 0) && endpoint.Fragment == "" &&
		database != "" && !strings.ContainsAny(database, "/\x00") && !strings.HasPrefix(database, resourcePrefix+"app_")
}

// Provision reconciles a dedicated database and returns its runtime connection
// URL. Reconciliation uses the same stable password supplied for the application.
func (p *Provisioner) Provision(ctx context.Context, appID, password string) (string, error) {
	if !appIDPattern.MatchString(appID) || !passwordPattern.MatchString(password) {
		return "", errors.New("invalid hosted app data application ID or password")
	}
	ctx, cancel := context.WithTimeout(ctx, poolTimeout)
	defer cancel()
	conn, err := p.lockAdmin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close(ctx) }()

	database := resourcePrefix + appID
	role := database + "_runtime"
	if err := p.prepareDatabase(ctx, conn, database, role); err != nil {
		return "", err
	}
	if err := p.configureSchema(ctx, database, role); err != nil {
		return "", err
	}
	if err := activateRole(ctx, conn, role, password); err != nil {
		return "", err
	}

	return p.runtimeURL(database, role, password), nil
}

func (p *Provisioner) lockAdmin(ctx context.Context) (*pgx.Conn, error) {
	conn, err := pgx.ConnectConfig(ctx, p.admin.Copy())
	if err != nil {
		return nil, operationError("connect to administrator database", err)
	}
	// Serialize both admission and reconciliation across Core replicas.
	if _, err := conn.Exec(ctx, "SELECT pg_catalog.pg_advisory_lock($1)", int64(poolLockKey)); err != nil {
		_ = conn.Close(ctx)
		return nil, operationError("lock pool provisioner", err)
	}
	return conn, nil
}

func (p *Provisioner) runtimeURL(database, role, password string) string {
	endpoint := p.endpoint
	endpoint.User = url.UserPassword(role, password)
	endpoint.Path = "/" + database
	endpoint.RawPath = ""
	return endpoint.String()
}

// Usage returns PostgreSQL's measured database size in bytes for a managed app.
func (p *Provisioner) Usage(ctx context.Context, appID string) (int64, error) {
	if !appIDPattern.MatchString(appID) {
		return 0, errors.New("invalid hosted app data application ID")
	}
	ctx, cancel := context.WithTimeout(ctx, poolTimeout)
	defer cancel()
	conn, err := pgx.ConnectConfig(ctx, p.admin.Copy())
	if err != nil {
		return 0, operationError("connect to administrator database", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var size int64
	err = conn.QueryRow(ctx, `
		SELECT pg_catalog.pg_database_size(d.oid)
		FROM pg_catalog.pg_database d
		WHERE d.datname = $1
			AND d.datdba = (SELECT oid FROM pg_catalog.pg_roles WHERE rolname = CURRENT_USER)
			AND pg_catalog.shobj_description(d.oid, 'pg_database') = $2
	`, resourcePrefix+appID, managedDatabase).Scan(&size)
	if err != nil {
		return 0, operationError("measure application database size", err)
	}
	return size, nil
}

func operationError(operation string, err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("hosted app data: %s: %w", operation, context.Canceled)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("hosted app data: %s: %w", operation, context.DeadlineExceeded)
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("hosted app data: %s: %w", operation, pgx.ErrNoRows)
	}
	// Driver errors can contain SQL text, password verifiers, or DSN values.
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		return fmt.Errorf("hosted app data: %s (SQLSTATE %s)", operation, postgresError.Code)
	}
	return fmt.Errorf("hosted app data: %s", operation)
}
