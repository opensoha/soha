package postgres

import (
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	dbinfra "github.com/opensoha/soha/internal/infrastructure/db"
)

// Store is kept as a compatibility alias for legacy imports.
type Store = dbinfra.Store

// New keeps the legacy package path usable while delegating to dbinfra.
//
// Deprecated: use internal/infrastructure/db.New directly.
func New(cfg cfgpkg.DatabaseConfig) (*Store, error) {
	return dbinfra.New(cfg)
}
