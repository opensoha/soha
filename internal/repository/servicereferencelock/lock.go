// Package servicereferencelock orders writes protected by active service references.
package servicereferencelock

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

// Applications takes the same transaction-scoped keys as the reference triggers.
// Call before locking or writing any protected rows.
func Applications(tx *gorm.DB, applicationIDs ...string) error {
	for _, id := range applicationSet(applicationIDs) {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "service-references:"+id).Error; err != nil {
			return fmt.Errorf("lock service reference application: %w", err)
		}
	}
	return nil
}

// Resolve handles rows whose application can change. Discover the entire scope
// without row locks, take the ordered keys, then reject a changed scope before
// any write. Acquiring a newly discovered key now could invert the lock order.
func Resolve(tx *gorm.DB, read func() ([]string, error), additional ...string) error {
	before, err := read()
	if err != nil {
		return err
	}
	if err := Applications(tx, append(slices.Clone(before), additional...)...); err != nil {
		return err
	}
	after, err := read()
	if err != nil {
		return err
	}
	if !slices.Equal(applicationSet(before), applicationSet(after)) {
		return fmt.Errorf("%w: service reference application scope changed; reload and retry", apperrors.ErrConflict)
	}
	return nil
}

// Binding locks the binding's current application and any destination.
func Binding(tx *gorm.DB, bindingID string, additional ...string) error {
	return Resolve(tx, func() ([]string, error) {
		var id string
		err := tx.Raw(`SELECT application_id FROM application_environments WHERE id = ?`, bindingID).Row().Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, apperrors.ErrNotFound
		}
		return []string{id}, err
	}, additional...)
}

func applicationSet(ids []string) []string {
	ids = slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return id == "" })
	slices.Sort(ids)
	return slices.Compact(ids)
}
