package postgres

import (
	cfgpkg "github.com/kubecrux/kubecrux/internal/infrastructure/config"
	dbinfra "github.com/kubecrux/kubecrux/internal/infrastructure/db"
)

// Store is kept as a compatibility alias for legacy imports.
type Store = dbinfra.Store

// New keeps the legacy package path usable while delegating to dbinfra.
//
// Deprecated: use internal/infrastructure/db.New directly.
func New(cfg cfgpkg.DatabaseConfig) (*Store, error) {
	return dbinfra.New(cfg)
}
