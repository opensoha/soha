package dbtx

import (
	"context"
	"testing"

	"gorm.io/gorm"
)

func TestBoundRequiresMatchingTransaction(t *testing.T) {
	base, tx, other := &gorm.DB{}, &gorm.DB{}, &gorm.DB{}
	bound := context.WithValue(context.Background(), contextKey{}, binding{base: base, tx: tx})
	for _, tc := range []struct {
		name string
		ctx  context.Context
		db   *gorm.DB
		want *gorm.DB
	}{
		{"same base", bound, base, tx},
		{"other database", bound, other, nil},
		{"unbound", context.Background(), base, nil},
		{"nested handle", bound, tx, nil},
		{"nil base", bound, nil, nil},
		{"nil transaction", context.WithValue(context.Background(), contextKey{}, binding{base: base}), base, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Bound(tc.ctx, tc.db)
			if got != tc.want || ok != (tc.want != nil) {
				t.Fatalf("transaction=%p bound=%v, want %p", got, ok, tc.want)
			}
		})
	}
}
