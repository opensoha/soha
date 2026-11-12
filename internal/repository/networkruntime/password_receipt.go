package networkruntime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

var passwordReceiptHash = regexp.MustCompile(`^[a-f0-9]{64}$`)

type passwordReceiptRow struct {
	TokenHash              string `gorm:"primaryKey"`
	RuntimeID              string
	NASID                  string
	SiteID                 string
	StationID              string
	SubjectID              string
	PasswordUpdatedAt      time.Time
	CreatedAt              time.Time
	ExpiresAt              time.Time
	AuthorizationRequestID *string
	ConsumedAt             *time.Time
}

func (passwordReceiptRow) TableName() string { return "network_password_receipts" }

func passwordReceiptDenied() error {
	return fmt.Errorf("%w: network password proof is invalid", apperrors.ErrUnauthorized)
}

func (r *Repository) SavePasswordReceipt(ctx context.Context, receipt domain.PasswordReceipt) error {
	if !passwordReceiptHash.MatchString(receipt.TokenHash) || !receipt.ExpiresAt.After(receipt.CreatedAt) || receipt.ExpiresAt.After(receipt.CreatedAt.Add(30*time.Second)) {
		return passwordReceiptDenied()
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := currentPasswordAccount(tx, receipt.SubjectID, receipt.PasswordUpdatedAt); err != nil {
			return err
		}
		// Retain consumed rows while their authorization may be retried. Cleanup
		// is bounded and does not delete another live proof or extend its lifetime.
		if err := tx.Exec(`DELETE FROM network_password_receipts WHERE token_hash IN (SELECT token_hash FROM network_password_receipts WHERE expires_at<? ORDER BY expires_at LIMIT 100)`, receipt.CreatedAt.Add(-24*time.Hour)).Error; err != nil {
			return err
		}
		row := passwordReceiptRow{TokenHash: receipt.TokenHash, RuntimeID: receipt.RuntimeID, NASID: receipt.NASID, SiteID: receipt.SiteID, StationID: receipt.StationID, SubjectID: receipt.SubjectID, PasswordUpdatedAt: receipt.PasswordUpdatedAt, CreatedAt: receipt.CreatedAt, ExpiresAt: receipt.ExpiresAt}
		return tx.Create(&row).Error
	})
}

func (r *Repository) PasswordReceipt(ctx context.Context, hash string) (domain.PasswordReceipt, error) {
	var row passwordReceiptRow
	if !passwordReceiptHash.MatchString(hash) {
		return domain.PasswordReceipt{}, passwordReceiptDenied()
	}
	if err := r.db.WithContext(ctx).Where("token_hash=?", hash).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.PasswordReceipt{}, passwordReceiptDenied()
		}
		return domain.PasswordReceipt{}, err
	}
	result := domain.PasswordReceipt{TokenHash: row.TokenHash, RuntimeID: row.RuntimeID, NASID: row.NASID, SiteID: row.SiteID, StationID: row.StationID, SubjectID: row.SubjectID, PasswordUpdatedAt: row.PasswordUpdatedAt, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt}
	if row.AuthorizationRequestID != nil {
		result.AuthorizationRequestID = *row.AuthorizationRequestID
	}
	return result, nil
}

// currentPasswordAccount runs inside the proof/session transaction. Lock the
// parent user as well as its password version, so status/password updates and
// new MFA credential inserts cannot overtake the checked authorization.
func currentPasswordAccount(tx *gorm.DB, subjectID string, version time.Time) error {
	var status string
	if err := tx.Raw(`SELECT status FROM users WHERE id=? FOR UPDATE`, subjectID).Row().Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return passwordReceiptDenied()
		}
		return err
	}
	if status != "active" {
		return passwordReceiptDenied()
	}
	var current time.Time
	if err := tx.Raw(`SELECT password_updated_at FROM user_password_credentials WHERE user_id=? FOR SHARE`, subjectID).Row().Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return passwordReceiptDenied()
		}
		return err
	}
	if !current.Equal(version) {
		return passwordReceiptDenied()
	}
	var mfa bool
	if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM identity_mfa_credentials WHERE user_id=? AND revoked_at IS NULL)`, subjectID).Scan(&mfa).Error; err != nil {
		return err
	}
	if mfa {
		return passwordReceiptDenied()
	}
	return nil
}

// consumePasswordReceipt is called only within the transaction that creates the
// station record and persists the NAS authorization/session. A failed transaction
// leaves neither a consumed proof nor an authorized station behind.
func consumePasswordReceipt(tx *gorm.DB, receipt domain.PasswordReceipt, requestID string, now time.Time) error {
	if err := currentPasswordAccount(tx, receipt.SubjectID, receipt.PasswordUpdatedAt); err != nil {
		return err
	}
	var row passwordReceiptRow
	if err := tx.Raw(`SELECT * FROM network_password_receipts WHERE token_hash=? FOR UPDATE`, receipt.TokenHash).Scan(&row).Error; err != nil {
		return err
	}
	if row.TokenHash == "" || row.RuntimeID != receipt.RuntimeID || row.NASID != receipt.NASID || row.SiteID != receipt.SiteID || row.StationID != receipt.StationID || row.SubjectID != receipt.SubjectID || !row.PasswordUpdatedAt.Equal(receipt.PasswordUpdatedAt) {
		return passwordReceiptDenied()
	}
	if row.AuthorizationRequestID != nil {
		if *row.AuthorizationRequestID == requestID {
			return nil
		}
		return passwordReceiptDenied()
	}
	if !row.ExpiresAt.After(now) {
		return passwordReceiptDenied()
	}
	return tx.Model(&passwordReceiptRow{}).Where("token_hash=? AND authorization_request_id IS NULL", row.TokenHash).Updates(map[string]any{"authorization_request_id": requestID, "consumed_at": now}).Error
}
