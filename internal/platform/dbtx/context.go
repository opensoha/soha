// Package dbtx carries a bounded database transaction across existing domain
// services without replacing their validation, authorization or repositories.
package dbtx

import (
	"context"
	"gorm.io/gorm"
)

type contextKey struct{}
type binding struct{ base, tx *gorm.DB }

// DB joins only the explicitly bound database. A repository constructed with a
// nested transaction keeps its own handle; another database is never redirected.
func DB(ctx context.Context, db *gorm.DB) *gorm.DB {
	if tx, ok := Bound(ctx, db); ok {
		return tx.WithContext(ctx)
	}
	return db.WithContext(ctx)
}

// Bound returns the transaction only when it belongs to this exact base handle.
// Transaction-required projections use this instead of silently falling back to
// a separate connection. Callers apply their context with tx.WithContext(ctx).
func Bound(ctx context.Context, db *gorm.DB) (*gorm.DB, bool) {
	current, ok := ctx.Value(contextKey{}).(binding)
	if !ok || db == nil || current.base != db || current.tx == nil {
		return nil, false
	}
	return current.tx, true
}

func Within(ctx context.Context, db *gorm.DB, apply func(context.Context) error) error {
	return DB(ctx, db).Transaction(func(tx *gorm.DB) error {
		return apply(context.WithValue(ctx, contextKey{}, binding{base: db, tx: tx}))
	})
}
