package networkruntime

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"gorm.io/gorm"
)

func passwordBudgetHash(kind, value string) string {
	digest := sha256.Sum256([]byte(kind + "\x00" + value))
	return fmt.Sprintf("%x", digest)
}

type passwordBudgetRow struct {
	BucketHash      string
	WindowStart     time.Time
	Attempts        int
	Failures        int
	LockedUntil     *time.Time
	ActiveAttemptID *string
	ActiveUntil     *time.Time
	ExpiresAt       time.Time
}

func (passwordBudgetRow) TableName() string { return "network_password_budgets" }

// BeginPasswordAttempt persists rate admission and takes a short subject lease.
// The lease prevents parallel bcrypt attempts against one account across replicas.
func (r *Repository) BeginPasswordAttempt(ctx context.Context, attempt domain.PasswordAttempt) (bool, error) {
	if attempt.ID == "" || attempt.RuntimeID == "" || attempt.NASID == "" || attempt.Source == "" || attempt.SubjectID == "" || attempt.Now.IsZero() {
		return false, passwordReceiptDenied()
	}
	allowed := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('network-password-budgets'))`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`DELETE FROM network_password_budgets WHERE bucket_hash IN (SELECT bucket_hash FROM network_password_budgets WHERE expires_at<=? ORDER BY expires_at LIMIT 100)`, attempt.Now).Error; err != nil {
			return err
		}
		for _, budget := range []struct {
			kind, value string
			limit       int
		}{{"nas", attempt.RuntimeID + ":" + attempt.NASID, 60}, {"source", attempt.Source, 120}} {
			ok, err := admitPasswordRate(tx, passwordBudgetHash(budget.kind, budget.value), budget.limit, attempt.Now)
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}
		}
		var subjectErr error
		allowed, subjectErr = admitPasswordSubject(tx, attempt)
		return subjectErr
	})
	return allowed, err
}

func ensurePasswordBudget(tx *gorm.DB, key string, now time.Time) error {
	// Bound identity-cardinality attacks. Existing buckets keep working at capacity.
	return tx.Exec(`INSERT INTO network_password_budgets(bucket_hash,window_start,expires_at) SELECT ?,?,? WHERE EXISTS(SELECT 1 FROM network_password_budgets WHERE bucket_hash=?) OR (SELECT COUNT(*) FROM network_password_budgets)<10000 ON CONFLICT DO NOTHING`, key, now, now.Add(24*time.Hour), key).Error
}

func admitPasswordRate(tx *gorm.DB, key string, limit int, now time.Time) (bool, error) {
	if err := ensurePasswordBudget(tx, key, now); err != nil {
		return false, err
	}
	var row passwordBudgetRow
	if err := tx.Where("bucket_hash=?", key).First(&row).Error; err != nil {
		return false, err
	}
	if !row.WindowStart.Add(time.Minute).After(now) {
		row.Attempts = 0
		row.WindowStart = now
	}
	if row.Attempts >= limit {
		return false, nil
	}
	err := tx.Model(&passwordBudgetRow{}).Where("bucket_hash=?", key).Updates(map[string]any{"window_start": row.WindowStart, "attempts": row.Attempts + 1, "expires_at": now.Add(24 * time.Hour)}).Error
	return err == nil, err
}

// FinishPasswordAttempt cannot clear another replica's newer lease. A failed
// or abandoned authentication never creates a receipt; leases expire boundedly.
func (r *Repository) FinishPasswordAttempt(ctx context.Context, attempt domain.PasswordAttempt, success bool, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('network-password-budgets'))`).Error; err != nil {
			return err
		}
		key := passwordBudgetHash("subject", attempt.SubjectID)
		var row passwordBudgetRow
		if err := tx.Where("bucket_hash=? AND active_attempt_id=?", key, attempt.ID).First(&row).Error; err != nil {
			return err
		}
		failures := row.Failures + 1
		var lockedUntil *time.Time
		if success {
			failures = 0
		} else if failures >= 5 {
			until := now.Add(5 * time.Minute)
			lockedUntil = &until
		}
		return tx.Model(&passwordBudgetRow{}).Where("bucket_hash=? AND active_attempt_id=?", key, attempt.ID).Updates(map[string]any{"failures": failures, "locked_until": lockedUntil, "active_attempt_id": nil, "active_until": nil, "expires_at": now.Add(24 * time.Hour)}).Error
	})
}

func admitPasswordSubject(tx *gorm.DB, attempt domain.PasswordAttempt) (bool, error) {
	key := passwordBudgetHash("subject", attempt.SubjectID)
	if err := ensurePasswordBudget(tx, key, attempt.Now); err != nil {
		return false, err
	}
	var row passwordBudgetRow
	if err := tx.Where("bucket_hash=?", key).First(&row).Error; err != nil {
		return false, err
	}
	if row.LockedUntil != nil && row.LockedUntil.After(attempt.Now) || row.ActiveUntil != nil && row.ActiveUntil.After(attempt.Now) {
		return false, nil
	}
	if row.LockedUntil != nil {
		row.Failures = 0
	}
	if row.ActiveAttemptID != nil {
		// An abandoned/cancelled worker does not reset failures. Charge its
		// expired reservation before admitting the next replica's attempt.
		row.Failures++
		if row.Failures >= 5 {
			return false, tx.Model(&passwordBudgetRow{}).Where("bucket_hash=?", key).Updates(map[string]any{"failures": row.Failures, "locked_until": attempt.Now.Add(5 * time.Minute), "active_attempt_id": nil, "active_until": nil}).Error
		}
	}
	result := tx.Model(&passwordBudgetRow{}).Where("bucket_hash=?", key).Updates(map[string]any{"failures": row.Failures, "locked_until": nil, "active_attempt_id": attempt.ID, "active_until": attempt.Now.Add(15 * time.Second), "expires_at": attempt.Now.Add(24 * time.Hour)})
	if result.Error != nil {
		return false, result.Error
	}
	return true, nil
}
