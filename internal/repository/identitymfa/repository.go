package identitymfa

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	domainmfa "github.com/opensoha/soha/internal/domain/identitymfa"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

const challengeRateLimit = 5

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) ListCredentials(ctx context.Context, userID string) ([]domainmfa.Credential, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, user_id, credential_type, display_name, COALESCE(external_id, ''),
		       secret_ciphertext, sign_count, created_at, last_used_at, revoked_at
		FROM identity_mfa_credentials
		WHERE user_id = ? AND revoked_at IS NULL
		ORDER BY created_at DESC
	`, strings.TrimSpace(userID)).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]domainmfa.Credential, 0)
	for rows.Next() {
		item, scanErr := scanCredential(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ActiveCredential(ctx context.Context, userID, credentialType string) (domainmfa.Credential, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, user_id, credential_type, display_name, COALESCE(external_id, ''),
		       secret_ciphertext, sign_count, created_at, last_used_at, revoked_at
		FROM identity_mfa_credentials
		WHERE user_id = ? AND credential_type = ? AND revoked_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1
	`, strings.TrimSpace(userID), strings.TrimSpace(credentialType)).Row()
	item, err := scanCredential(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domainmfa.Credential{}, fmt.Errorf("%w: MFA credential not found", apperrors.ErrNotFound)
	}
	return item, err
}

func (r *Repository) CreateCredential(ctx context.Context, item domainmfa.Credential) error {
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO identity_mfa_credentials (
			id, user_id, credential_type, display_name, external_id, secret_ciphertext,
			sign_count, created_at
		) VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?)
	`, item.ID, item.UserID, item.Type, item.DisplayName, item.ExternalID,
		item.SecretCiphertext, item.SignCount, item.CreatedAt).Error
}

func (r *Repository) RevokeCredential(ctx context.Context, userID, credentialID string, now time.Time) error {
	result := r.db.WithContext(ctx).Exec(`
		UPDATE identity_mfa_credentials
		SET revoked_at = ?
		WHERE id = ? AND user_id = ? AND revoked_at IS NULL
	`, now, strings.TrimSpace(credentialID), strings.TrimSpace(userID))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: MFA credential not found", apperrors.ErrNotFound)
	}
	return nil
}

func (r *Repository) ResetMFA(ctx context.Context, userID string, revokeSessions bool, now time.Time) (domainmfa.AdminResetCounts, error) {
	counts := domainmfa.AdminResetCounts{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		credentials := tx.Exec(`UPDATE identity_mfa_credentials SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, now, strings.TrimSpace(userID))
		if credentials.Error != nil {
			return credentials.Error
		}
		counts.RevokedCredentials = int(credentials.RowsAffected)
		recovery := tx.Exec(`UPDATE identity_recovery_codes SET used_at = ? WHERE user_id = ? AND used_at IS NULL`, now, strings.TrimSpace(userID))
		if recovery.Error != nil {
			return recovery.Error
		}
		counts.RevokedRecoveryCodes = int(recovery.RowsAffected)
		challenges := tx.Exec(`UPDATE identity_mfa_challenges SET consumed_at = ? WHERE user_id = ? AND consumed_at IS NULL`, now, strings.TrimSpace(userID))
		if challenges.Error != nil {
			return challenges.Error
		}
		counts.RevokedChallenges = int(challenges.RowsAffected)
		if revokeSessions {
			sessions := tx.Exec(`UPDATE sessions SET status = 'revoked', updated_at = ? WHERE user_id = ? AND status = 'active'`, now, strings.TrimSpace(userID))
			if sessions.Error != nil {
				return sessions.Error
			}
			counts.RevokedSessions = int(sessions.RowsAffected)
		}
		return nil
	})
	return counts, err
}

func (r *Repository) CreateChallenge(ctx context.Context, item domainmfa.Challenge) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, item.UserID).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Raw(`
			SELECT COUNT(*)
			FROM identity_mfa_challenges
			WHERE user_id = ? AND created_at >= ?
		`, item.UserID, item.CreatedAt.Add(-5*time.Minute)).Scan(&count).Error; err != nil {
			return err
		}
		if count >= challengeRateLimit {
			return fmt.Errorf("%w: MFA challenge rate limit exceeded", apperrors.ErrConflict)
		}
		result := tx.Exec(`
			INSERT INTO identity_mfa_challenges (
				id, user_id, session_id, challenge_type, secret_ciphertext,
				attempts, max_attempts, expires_at, created_at
			)
			SELECT ?, ?, ?, ?, ?, 0, ?, ?, ?
			FROM sessions
			WHERE id = ? AND user_id = ? AND status = 'active' AND expires_at > ?
		`, item.ID, item.UserID, item.SessionID, item.Type, item.SecretCiphertext,
			item.MaxAttempts, item.ExpiresAt, item.CreatedAt,
			item.SessionID, item.UserID, item.CreatedAt)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("%w: active session required for MFA", apperrors.ErrUnauthorized)
		}
		return nil
	})
}

func (r *Repository) GetChallenge(ctx context.Context, id, userID, sessionID string, now time.Time) (domainmfa.Challenge, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, user_id, session_id, challenge_type, secret_ciphertext, attempts,
		       max_attempts, expires_at, consumed_at, created_at
		FROM identity_mfa_challenges
		WHERE id = ? AND user_id = ? AND session_id = ?
		  AND consumed_at IS NULL AND expires_at > ? AND attempts < max_attempts
		LIMIT 1
	`, strings.TrimSpace(id), strings.TrimSpace(userID), strings.TrimSpace(sessionID), now).Row()
	item, err := scanChallenge(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domainmfa.Challenge{}, fmt.Errorf("%w: MFA challenge is invalid, expired, or consumed", apperrors.ErrConflict)
	}
	return item, err
}

func (r *Repository) IncrementChallengeAttempt(ctx context.Context, id, userID, sessionID string) error {
	return r.db.WithContext(ctx).Exec(`
		UPDATE identity_mfa_challenges
		SET attempts = attempts + 1
		WHERE id = ? AND user_id = ? AND session_id = ? AND consumed_at IS NULL
	`, id, userID, sessionID).Error
}

func (r *Repository) ConsumeChallenge(ctx context.Context, id, userID, sessionID string, now time.Time) error {
	result := r.db.WithContext(ctx).Exec(`
		UPDATE identity_mfa_challenges
		SET consumed_at = ?
		WHERE id = ? AND user_id = ? AND session_id = ?
		  AND consumed_at IS NULL AND expires_at > ? AND attempts < max_attempts
	`, now, id, userID, sessionID, now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: MFA challenge was already consumed", apperrors.ErrConflict)
	}
	return nil
}

func (r *Repository) CompleteChallenge(ctx context.Context, challenge domainmfa.Challenge, credential *domainmfa.Credential, method string, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`
			UPDATE identity_mfa_challenges
			SET consumed_at = ?
			WHERE id = ? AND user_id = ? AND session_id = ?
			  AND consumed_at IS NULL AND expires_at > ? AND attempts < max_attempts
		`, now, challenge.ID, challenge.UserID, challenge.SessionID, now)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("%w: MFA challenge was already consumed", apperrors.ErrConflict)
		}
		if credential != nil {
			if err := tx.Exec(`
				INSERT INTO identity_mfa_credentials (
					id, user_id, credential_type, display_name, external_id,
					secret_ciphertext, sign_count, created_at
				) VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?)
			`, credential.ID, credential.UserID, credential.Type, credential.DisplayName,
				credential.ExternalID, credential.SecretCiphertext, credential.SignCount,
				credential.CreatedAt).Error; err != nil {
				return err
			}
		}
		return markSessionStepUp(tx, challenge.SessionID, challenge.UserID, method, now)
	})
}

func (r *Repository) CompleteWebAuthnChallenge(ctx context.Context, challenge domainmfa.Challenge, credential domainmfa.Credential, ciphertext []byte, signCount uint32, method string, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`UPDATE identity_mfa_challenges SET consumed_at = ? WHERE id = ? AND user_id = ? AND session_id = ? AND consumed_at IS NULL AND expires_at > ? AND attempts < max_attempts`, now, challenge.ID, challenge.UserID, challenge.SessionID, now)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("%w: MFA challenge was already consumed", apperrors.ErrConflict)
		}
		updated := tx.Exec(`UPDATE identity_mfa_credentials SET secret_ciphertext = ?, sign_count = ?, last_used_at = ? WHERE id = ? AND user_id = ? AND revoked_at IS NULL AND sign_count <= ?`, string(ciphertext), signCount, now, credential.ID, credential.UserID, credential.SignCount)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected == 0 {
			return fmt.Errorf("%w: WebAuthn credential counter changed", apperrors.ErrConflict)
		}
		return markSessionStepUp(tx, challenge.SessionID, challenge.UserID, method, now)
	})
}

func (r *Repository) ReplaceRecoveryCodes(ctx context.Context, userID string, codes []domainmfa.RecoveryCode) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`DELETE FROM identity_recovery_codes WHERE user_id = ?`, userID).Error; err != nil {
			return err
		}
		for _, code := range codes {
			if err := tx.Exec(`
				INSERT INTO identity_recovery_codes (id, user_id, code_hash, created_at)
				VALUES (?, ?, ?, ?)
			`, code.ID, code.UserID, code.CodeHash, code.CreatedAt).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repository) ListRecoveryCodes(ctx context.Context, userID string) ([]domainmfa.RecoveryCode, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, user_id, code_hash, created_at, used_at
		FROM identity_recovery_codes
		WHERE user_id = ? AND used_at IS NULL
		ORDER BY created_at ASC
	`, strings.TrimSpace(userID)).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]domainmfa.RecoveryCode, 0)
	for rows.Next() {
		var item domainmfa.RecoveryCode
		if err := rows.Scan(&item.ID, &item.UserID, &item.CodeHash, &item.CreatedAt, &item.UsedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ConsumeRecoveryCode(ctx context.Context, id, userID string, now time.Time) error {
	result := r.db.WithContext(ctx).Exec(`
		UPDATE identity_recovery_codes
		SET used_at = ?
		WHERE id = ? AND user_id = ? AND used_at IS NULL
	`, now, id, userID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: recovery code was already consumed", apperrors.ErrConflict)
	}
	return nil
}

func (r *Repository) ConsumeRecoveryCodeAndStepUp(ctx context.Context, challengeID, codeID, userID, sessionID string, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		challengeResult := tx.Exec(`
			UPDATE identity_mfa_challenges
			SET consumed_at = ?
			WHERE id = ? AND user_id = ? AND session_id = ?
			  AND consumed_at IS NULL AND expires_at > ? AND attempts < max_attempts
		`, now, challengeID, userID, sessionID, now)
		if challengeResult.Error != nil {
			return challengeResult.Error
		}
		if challengeResult.RowsAffected == 0 {
			return fmt.Errorf("%w: MFA challenge was already consumed", apperrors.ErrConflict)
		}
		result := tx.Exec(`
			UPDATE identity_recovery_codes
			SET used_at = ?
			WHERE id = ? AND user_id = ? AND used_at IS NULL
		`, now, codeID, userID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("%w: recovery code was already consumed", apperrors.ErrConflict)
		}
		return markSessionStepUp(tx, sessionID, userID, "recovery_code", now)
	})
}

func (r *Repository) MarkSessionStepUp(ctx context.Context, sessionID, userID, method string, now time.Time) error {
	return markSessionStepUp(r.db.WithContext(ctx), sessionID, userID, method, now)
}

func markSessionStepUp(db *gorm.DB, sessionID, userID, method string, now time.Time) error {
	metadata := fmt.Sprintf(`{"mfa":true,"amr":["mfa",%q],"mfaAuthenticatedAt":%q}`, method, now.Format(time.RFC3339Nano))
	result := db.Exec(`
		UPDATE sessions
		SET metadata = COALESCE(metadata, '{}'::jsonb) || ?::jsonb, updated_at = ?
		WHERE id = ? AND user_id = ? AND status = 'active' AND expires_at > ?
	`, metadata, now, strings.TrimSpace(sessionID), strings.TrimSpace(userID), now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: active session required for MFA", apperrors.ErrUnauthorized)
	}
	return nil
}

func (r *Repository) HasRecentStepUp(ctx context.Context, sessionID, userID string, since time.Time) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Raw(`
		SELECT COUNT(*)
		FROM sessions
		WHERE id = ? AND user_id = ? AND status = 'active' AND expires_at > NOW()
		  AND COALESCE((metadata ->> 'mfa')::boolean, false)
		  AND NULLIF(metadata ->> 'mfaAuthenticatedAt', '')::timestamptz >= ?
	`, strings.TrimSpace(sessionID), strings.TrimSpace(userID), since).Scan(&count).Error
	return count == 1, err
}

type scanner interface {
	Scan(...any) error
}

func scanCredential(row scanner) (domainmfa.Credential, error) {
	var item domainmfa.Credential
	var signCount int64
	if err := row.Scan(&item.ID, &item.UserID, &item.Type, &item.DisplayName, &item.ExternalID,
		&item.SecretCiphertext, &signCount, &item.CreatedAt, &item.LastUsedAt, &item.RevokedAt); err != nil {
		return domainmfa.Credential{}, err
	}
	if signCount > 0 {
		item.SignCount = uint32(signCount)
	}
	return item, nil
}

func scanChallenge(row scanner) (domainmfa.Challenge, error) {
	var item domainmfa.Challenge
	err := row.Scan(&item.ID, &item.UserID, &item.SessionID, &item.Type, &item.SecretCiphertext,
		&item.Attempts, &item.MaxAttempts, &item.ExpiresAt, &item.ConsumedAt, &item.CreatedAt)
	return item, err
}
