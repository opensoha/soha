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
	if current, ok := ctx.Value(contextKey{}).(binding); ok && current.base == db {
		return current.tx.WithContext(ctx)
	}
	return db.WithContext(ctx)
}

func Within(ctx context.Context, db *gorm.DB, apply func(context.Context) error) error {
	return DB(ctx, db).Transaction(func(tx *gorm.DB) error {
		return apply(context.WithValue(ctx, contextKey{}, binding{base: db, tx: tx}))
	})
}
