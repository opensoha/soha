package copilot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	guest "github.com/opensoha/soha-contracts/agent/guest"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

const personalGuestColumns = `binding,certificate_fingerprint,access_version,enrollment_id,challenge_hash,enrollment_issued_at,enrollment_expires_at,status,last_sequence,last_heartbeat_at`

func scanPersonalGuest(row *sql.Row) (domain.PersonalGuestRecord, error) {
	var item domain.PersonalGuestRecord
	var binding []byte
	err := row.Scan(&binding, &item.CertificateFingerprint, &item.AccessVersion, &item.EnrollmentID, &item.ChallengeHash,
		&item.EnrollmentIssuedAt, &item.EnrollmentExpiresAt, &item.Status, &item.LastSequence, &item.LastHeartbeatAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, apperrors.ErrNotFound
	}
	if err != nil {
		return item, fmt.Errorf("read personal guest: %w", err)
	}
	if err := json.Unmarshal(binding, &item.Binding); err != nil {
		return item, fmt.Errorf("decode personal guest binding: %w", err)
	}
	if err := item.Validate(); err != nil {
		return item, fmt.Errorf("validate personal guest record: %w", err)
	}
	return item, nil
}

func (r *Repository) GetPersonalGuest(ctx context.Context, id string) (domain.PersonalGuestRecord, error) {
	return scanPersonalGuest(dbtx.DB(ctx, r.db).Raw(`SELECT `+personalGuestColumns+` FROM ai_personal_guests WHERE binding_id=?`, id).Row())
}

func (r *Repository) WithinPersonalGuestTransaction(ctx context.Context, apply func(context.Context) error) error {
	return dbtx.Within(ctx, r.db, apply)
}

// Locks serialize guest mutations with access revocation and compute changes.
func lockPersonalGuestBindingAuthority(tx *gorm.DB, item domain.PersonalGuestRecord) error {
	if item.Validate() != nil {
		return apperrors.ErrInvalidArgument
	}
	var assistantID string
	err := tx.Raw(`SELECT e.assistant_id FROM ai_personal_environments e
 JOIN ai_personal_assistants a ON a.id=e.assistant_id AND a.created_by=e.created_by
 JOIN ai_personal_agent_access x ON x.user_id=e.created_by
 WHERE e.assistant_id=? AND e.created_by=? AND e.backend='vm' AND e.provider=?
 AND e.compute_ref=? AND e.compute_generation=? AND e.data_ref=? AND e.policy_revision=?
 AND e.vm_identity->>'providerIdentityHash'=? AND e.vm_identity->>'dataDiskSerial'=? AND (e.vm_identity->>'dataDiskBytes')::BIGINT=?
 AND e.state NOT IN ('missing','deleted','deleting','deletion_unknown','sleeping','expired')
 AND x.status='approved' AND x.version=? FOR UPDATE OF e,a,x`,
		item.Binding.AssistantID, item.Binding.OwnerID, item.Binding.Provider, item.Binding.ComputeRef, item.Binding.ComputeGeneration,
		item.Binding.DataRef, item.Binding.PolicyRevision, item.Binding.ProviderIdentityHash, item.Binding.DataDiskSerial, item.Binding.DataDiskBytes, item.AccessVersion).Row().Scan(&assistantID)
	if errors.Is(err, sql.ErrNoRows) {
		return apperrors.ErrAccessDenied
	}
	if err != nil {
		return fmt.Errorf("check current personal guest authority: %w", err)
	}
	return nil
}

func checkPersonalGuestBinding(tx *gorm.DB, item domain.PersonalGuestRecord) error {
	if err := lockPersonalGuestBindingAuthority(tx, item); err != nil {
		return err
	}
	// A fresh statement after the owner lock sees a concurrently prepared revision.
	var version int64
	if err := tx.Raw(`SELECT COALESCE(MAX(binding_version),0) FROM ai_personal_guests WHERE assistant_id=?`, item.Binding.AssistantID).Row().Scan(&version); err != nil {
		return fmt.Errorf("check current personal guest revision: %w", err)
	}
	if version > item.Binding.Version {
		return apperrors.ErrAccessDenied
	}
	return nil
}

func (r *Repository) CheckPersonalGuestBinding(ctx context.Context, item domain.PersonalGuestRecord) error {
	return dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error { return checkPersonalGuestBinding(tx, item) })
}

func (r *Repository) CreatePersonalGuest(ctx context.Context, item domain.PersonalGuestRecord) error {
	if item.Validate() != nil || item.Status != "pending" || item.LastSequence != 0 || item.LastHeartbeatAt != nil {
		return apperrors.ErrInvalidArgument
	}
	payload, err := json.Marshal(item.Binding)
	if err != nil {
		return err
	}
	return dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := checkPersonalGuestBinding(tx, item); err != nil {
			return err
		}
		result := tx.Exec(`INSERT INTO ai_personal_guests
 (binding_id,assistant_id,binding_version,binding,certificate_fingerprint,access_version,enrollment_id,challenge_hash,enrollment_issued_at,enrollment_expires_at,status)
 VALUES (?,?,?,?,?,?,?,?,?,?,'pending') ON CONFLICT DO NOTHING`, item.Binding.ID, item.Binding.AssistantID, item.Binding.Version,
			string(payload), item.CertificateFingerprint, item.AccessVersion, item.EnrollmentID, item.ChallengeHash, item.EnrollmentIssuedAt, item.EnrollmentExpiresAt)
		return guestMutationResult(result, apperrors.ErrConflict)
	})
}

func (r *Repository) ConsumePersonalGuestEnrollment(ctx context.Context, item domain.PersonalGuestRecord, tokenHash string, now time.Time) error {
	payload, err := json.Marshal(item.Binding)
	if err != nil || now.IsZero() {
		return apperrors.ErrInvalidArgument
	}
	return dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		var pending bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM ai_personal_guests
 WHERE binding_id=? AND binding_version=? AND certificate_fingerprint=? AND access_version=?
 AND enrollment_id=? AND challenge_hash=? AND status='pending' AND binding=CAST(? AS jsonb))`, item.Binding.ID, item.Binding.Version,
			item.CertificateFingerprint, item.AccessVersion, item.EnrollmentID, tokenHash, string(payload)).Row().Scan(&pending); err != nil {
			return fmt.Errorf("check frozen personal guest enrollment: %w", err)
		}
		if !pending {
			return apperrors.ErrUnauthorized
		}
		if err := checkPersonalGuestBinding(tx, item); err != nil {
			return err
		}
		result := tx.Exec(`UPDATE ai_personal_guests SET status='registered'
 WHERE binding_id=? AND binding_version=? AND certificate_fingerprint=? AND access_version=?
 AND enrollment_id=? AND challenge_hash=? AND status='pending' AND enrollment_issued_at<=? AND enrollment_expires_at>?
 AND binding=CAST(? AS jsonb) AND (binding->>'expiresAt')::TIMESTAMPTZ>?`, item.Binding.ID, item.Binding.Version, item.CertificateFingerprint, item.AccessVersion,
			item.EnrollmentID, tokenHash, now, now, string(payload), now)
		return guestMutationResult(result, apperrors.ErrUnauthorized)
	})
}

func (r *Repository) RecordPersonalGuestHeartbeat(ctx context.Context, item domain.PersonalGuestRecord, heartbeat guest.Heartbeat, now time.Time) error {
	if err := heartbeat.ValidateFor(item.Binding, now, item.LastSequence); err != nil {
		return apperrors.ErrConflict
	}
	payload, err := json.Marshal(item.Binding)
	if err != nil {
		return err
	}
	return dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := checkPersonalGuestBinding(tx, item); err != nil {
			return err
		}
		result := tx.Exec(`UPDATE ai_personal_guests SET last_sequence=?,last_heartbeat_at=?
 WHERE binding_id=? AND binding_version=? AND certificate_fingerprint=? AND access_version=?
 AND status='registered' AND last_sequence<? AND binding=CAST(? AS jsonb) AND (binding->>'expiresAt')::TIMESTAMPTZ>?`, heartbeat.Sequence, now,
			item.Binding.ID, item.Binding.Version, item.CertificateFingerprint, item.AccessVersion, heartbeat.Sequence, string(payload), now)
		return guestMutationResult(result, apperrors.ErrConflict)
	})
}

func guestMutationResult(result *gorm.DB, conflict error) error {
	if result.Error != nil {
		return fmt.Errorf("persist personal guest state: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return conflict
	}
	return nil
}
