package copilot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"time"
)

func (r *Repository) LockPersonalGuestRenewal(ctx context.Context, item domain.PersonalGuestRecord) error {
	tx := dbtx.DB(ctx, r.db)
	if err := checkPersonalGuestBinding(tx, item); err != nil {
		return err
	}
	var id string
	err := tx.Raw(`SELECT binding_id FROM ai_personal_guests WHERE binding_id=? AND binding_version=? AND certificate_fingerprint=? AND status='registered' FOR UPDATE`, item.Binding.ID, item.Binding.Version, item.CertificateFingerprint).Row().Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return apperrors.ErrConflict
	}
	return err
}

func (r *Repository) GetPersonalGuestRenewal(ctx context.Context, requestID string) (domain.PersonalGuestRenewal, error) {
	var item domain.PersonalGuestRenewal
	var raw []byte
	err := dbtx.DB(ctx, r.db).Raw(`SELECT result,old_fingerprint,new_fingerprint,csr_hash,commit_before,committed FROM ai_personal_guest_renewals WHERE request_id=? AND NOT superseded`, requestID).Row().Scan(&raw, &item.OldFingerprint, &item.NewFingerprint, &item.CSRHash, &item.CommitBefore, &item.Committed)
	if errors.Is(err, sql.ErrNoRows) {
		return item, apperrors.ErrNotFound
	}
	if err != nil {
		return item, err
	}
	return item, json.Unmarshal(raw, &item.Result)
}

func (r *Repository) SavePersonalGuestRenewal(ctx context.Context, item domain.PersonalGuestRenewal) error {
	raw, err := json.Marshal(item.Result)
	if err != nil {
		return err
	}
	tx := dbtx.DB(ctx, r.db)
	if err := tx.Exec(`UPDATE ai_personal_guest_renewals SET superseded=TRUE WHERE binding_id=? AND NOT committed AND NOT superseded AND commit_before<=?`, item.Result.Binding.ID, item.Result.Binding.IssuedAt).Error; err != nil {
		return err
	}
	return guestMutationResult(tx.Exec(`INSERT INTO ai_personal_guest_renewals(request_id,binding_id,old_fingerprint,new_fingerprint,csr_hash,result,commit_before) SELECT ?,?,?,?,?,CAST(? AS jsonb),? WHERE NOT EXISTS(SELECT 1 FROM ai_personal_guest_renewals WHERE binding_id=? AND NOT committed AND NOT superseded) ON CONFLICT DO NOTHING`, item.Result.RequestID, item.Result.Binding.ID, item.OldFingerprint, item.NewFingerprint, item.CSRHash, string(raw), item.CommitBefore, item.Result.Binding.ID), apperrors.ErrConflict)
}

func (r *Repository) CommitPersonalGuestRenewal(ctx context.Context, old domain.PersonalGuestRecord, item domain.PersonalGuestRenewal, now time.Time) error {
	tx := dbtx.DB(ctx, r.db)
	raw, err := json.Marshal(item.Result.Binding)
	if err != nil {
		return err
	}
	if err := guestMutationResult(tx.Exec(`UPDATE ai_personal_guest_renewals SET committed=TRUE WHERE request_id=? AND binding_id=? AND new_fingerprint=? AND NOT committed AND NOT superseded AND commit_before>?`, item.Result.RequestID, old.Binding.ID, item.NewFingerprint, now), apperrors.ErrConflict); err != nil {
		return err
	}
	return guestMutationResult(tx.Exec(`UPDATE ai_personal_guests SET binding=CAST(? AS jsonb),binding_version=?,certificate_fingerprint=? WHERE binding_id=? AND binding_version=? AND certificate_fingerprint=? AND status='registered'`, string(raw), item.Result.Binding.Version, item.NewFingerprint, old.Binding.ID, old.Binding.Version, old.CertificateFingerprint), apperrors.ErrConflict)
}

func (r *Repository) ReadLockedPersonalGuest(ctx context.Context, id string) (domain.PersonalGuestRecord, error) {
	item, err := r.GetPersonalGuest(ctx, id)
	if err != nil {
		return item, err
	}
	tx := dbtx.DB(ctx, r.db)
	if err := lockPersonalGuestBindingAuthority(tx, item); err != nil {
		return item, err
	}
	current, err := scanPersonalGuest(tx.Raw(`SELECT `+personalGuestColumns+` FROM ai_personal_guests WHERE binding_id=? FOR UPDATE`, id).Row())
	if err != nil {
		return current, err
	}
	return current, checkPersonalGuestBinding(tx, current)
}
