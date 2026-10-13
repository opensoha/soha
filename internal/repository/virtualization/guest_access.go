package virtualization

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

func (r *Repository) GetVMAccessBinding(ctx context.Context, id string) (domain.VMAccessBinding, error) {
	var data []byte
	err := dbtx.DB(ctx, r.db).Raw(`SELECT binding FROM virtualization_vm_access_bindings WHERE vm_id=?`, id).Row().Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.VMAccessBinding{}, ErrNotFound
	}
	if err != nil {
		return domain.VMAccessBinding{}, err
	}
	var binding domain.VMAccessBinding
	err = json.Unmarshal(data, &binding)
	return binding, err
}

func (r *Repository) SaveVMAccessBinding(ctx context.Context, binding domain.VMAccessBinding, expected int) (domain.VMAccessBinding, error) {
	binding.Revision = expected + 1
	data, err := json.Marshal(binding)
	if err != nil {
		return domain.VMAccessBinding{}, err
	}
	result := dbtx.DB(ctx, r.db).Exec(`INSERT INTO virtualization_vm_access_bindings(vm_id,revision,binding) SELECT ?,?,?::jsonb WHERE ?=0
 ON CONFLICT(vm_id) DO UPDATE SET revision=EXCLUDED.revision,binding=EXCLUDED.binding,updated_at=now() WHERE virtualization_vm_access_bindings.revision=?`, binding.VMID, binding.Revision, string(data), expected, expected)
	// Existing bindings use a separate CAS update, including expected revisions above zero.
	if expected > 0 {
		result = dbtx.DB(ctx, r.db).Exec(`UPDATE virtualization_vm_access_bindings SET revision=?,binding=?::jsonb,updated_at=now() WHERE vm_id=? AND revision=?`, binding.Revision, string(data), binding.VMID, expected)
	}
	if result.Error != nil {
		return domain.VMAccessBinding{}, result.Error
	}
	if result.RowsAffected != 1 {
		return domain.VMAccessBinding{}, apperrors.ErrConflict
	}
	return binding, nil
}

func (r *Repository) CreateVMSSHSession(ctx context.Context, session domain.VMSSHSession) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "vm-ssh/"+session.Principal.UserID).Error; err != nil {
			return err
		}
		var count int
		if err := tx.Raw(`SELECT count(*) FROM virtualization_vm_ssh_sessions WHERE actor_id=? AND status IN ('pending','active') AND expires_at>?`, session.Principal.UserID, time.Now().UTC()).Scan(&count).Error; err != nil {
			return err
		}
		if count >= 8 {
			return apperrors.ErrConflict
		}
		return tx.Exec(`INSERT INTO virtualization_vm_ssh_sessions(id,vm_id,actor_id,status,expires_at,session,encrypted_lease,created_at) VALUES(?,?,?,?,?,?::jsonb,?,?)`, session.ID, session.VMID, session.Principal.UserID, session.Status, session.ExpiresAt, string(data), session.LeaseCredential, session.CreatedAt).Error
	})
}

func (r *Repository) GetVMSSHSession(ctx context.Context, id string) (domain.VMSSHSession, error) {
	var data []byte
	var status, reason, encrypted string
	err := dbtx.DB(ctx, r.db).Raw(`SELECT session,status,reason,encrypted_lease FROM virtualization_vm_ssh_sessions WHERE id=?`, id).Row().Scan(&data, &status, &reason, &encrypted)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.VMSSHSession{}, ErrNotFound
	}
	if err != nil {
		return domain.VMSSHSession{}, err
	}
	var session domain.VMSSHSession
	if err = json.Unmarshal(data, &session); err != nil {
		return domain.VMSSHSession{}, err
	}
	session.Status = status
	session.Reason = reason
	session.LeaseCredential = encrypted
	return session, nil
}

func (r *Repository) TransitionVMSSHSession(ctx context.Context, id string, previous []string, status, reason string) error {
	result := dbtx.DB(ctx, r.db).Exec(`UPDATE virtualization_vm_ssh_sessions SET status=?,reason=?,updated_at=now() WHERE id=? AND status IN ?`, status, reason, id, previous)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return apperrors.ErrConflict
	}
	return nil
}

func (r *Repository) ActivateVMSSHSession(ctx context.Context, session domain.VMSSHSession) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	result := dbtx.DB(ctx, r.db).Exec(`UPDATE virtualization_vm_ssh_sessions SET session=?::jsonb,encrypted_lease=?,status='active',expires_at=?,updated_at=now() WHERE id=? AND status='pending'`, string(data), session.LeaseCredential, session.ExpiresAt, session.ID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return apperrors.ErrConflict
	}
	return nil
}

func (r *Repository) ListVMSecretUsages(ctx context.Context, secretID string) ([]domain.VMSecretUsage, error) {
	prefix := "soha://secrets/" + secretID + "/versions/"
	var rows []domain.VMSecretUsage
	err := dbtx.DB(ctx, r.db).Raw(`
 SELECT vm_id AS target_id,vm_id,'login_binding' AS usage,
 substring(binding->>'credentialRef' from '[0-9]+$')::int AS version,revision AS binding_revision,'active' AS status
 FROM virtualization_vm_access_bindings WHERE starts_with(binding->>'credentialRef',?)
 UNION ALL
 SELECT id,vm_id,'ssh_session',substring(session->'binding'->>'credentialRef' from '[0-9]+$')::int,
 (session->>'bindingRevision')::int,status FROM virtualization_vm_ssh_sessions
 WHERE starts_with(session->'binding'->>'credentialRef',?) AND status IN ('pending','active','unknown')
 UNION ALL
 SELECT t.id,COALESCE(t.vm_id,''),'cloud_init_operation',(ref->>'Version')::int,0,t.status
 FROM virtualization_tasks t CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(t.payload->'guestPublicKeyRefs')='array' THEN t.payload->'guestPublicKeyRefs' ELSE '[]'::jsonb END) ref
 WHERE ref->>'SecretID'=? ORDER BY usage,target_id,version`, prefix, prefix, secretID).Scan(&rows).Error
	return rows, err
}
