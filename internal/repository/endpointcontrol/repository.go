package endpointcontrol

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	ec "github.com/opensoha/soha-contracts/endpointcontrol"
	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }
func policy(tx *gorm.DB, id string) (ec.Policy, error) {
	p := ec.Policy{DeviceID: id}
	var draft, published []byte
	err := tx.Raw(`SELECT revision,draft,published,published_version,updated_at FROM endpoint_channel_policies WHERE device_id=?`, id).Row().Scan(&p.Revision, &draft, &published, &p.PublishedVersion, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(draft, &p.Draft); err != nil {
		return p, err
	}
	if len(published) > 0 && string(published) != "null" {
		p.Published = &ec.Controls{}
		err = json.Unmarshal(published, p.Published)
	}
	return p, err
}
func (r *Repository) View(ctx context.Context, id string) (ec.View, error) {
	tx := r.db.WithContext(ctx)
	p, err := policy(tx, id)
	if err != nil {
		return ec.View{}, err
	}
	v := ec.View{Policy: p}
	var raw []byte
	var received time.Time
	// Receipt is the latest individual report. Fresh is an aggregate across all
	// runtimes that have ever received a published policy for this device.
	err = tx.Raw(`SELECT r.observation,r.received_at FROM endpoint_channel_reports r WHERE r.device_id=? ORDER BY r.received_at DESC LIMIT 1`, id).Row().Scan(&raw, &received)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	} else if err == nil {
		v.Receipt = &ec.Observation{}
		if err = json.Unmarshal(raw, v.Receipt); err != nil {
			return v, err
		}
		v.ReceivedAt = &received
	}
	if err != nil {
		return v, err
	}
	if err = tx.Raw(`SELECT count(*),count(*) FILTER (WHERE NOT EXISTS (
 SELECT 1 FROM network_runtime_credentials c JOIN endpoint_channel_reports r ON r.credential_id=c.id
 JOIN network_access_devices d ON d.id=c.device_id AND d.tenant_id=c.tenant_id AND d.workspace_id=c.workspace_id
 WHERE c.runtime_id=o.runtime_id AND c.device_id=o.device_id AND c.subject_id=d.owner_user_id::text
 AND c.status='active' AND c.revoked_at IS NULL AND c.not_before<=clock_timestamp() AND c.expires_at>clock_timestamp()
 AND c.runtime_kind='endpoint' AND c.capabilities @> '["windows-channel-controls-v1"]'::jsonb
 AND c.tenant_id='default' AND c.workspace_id='default' AND d.status='active' AND d.platform='windows'
 AND c.generation=(SELECT MAX(n.generation) FROM network_runtime_credentials n WHERE n.runtime_id=c.runtime_id AND n.tenant_id=c.tenant_id AND n.workspace_id=c.workspace_id)
 AND r.device_id=o.device_id AND r.policy_version=? AND r.received_at BETWEEN clock_timestamp()-interval '1 minute' AND clock_timestamp()+interval '30 seconds'
 )) FROM endpoint_channel_runtime_owners o WHERE o.device_id=?`, p.PublishedVersion, id).Row().Scan(&v.RuntimeCount, &v.PendingRuntimeCount); err != nil {
		return v, err
	}
	v.Fresh = v.RuntimeCount > 0 && v.PendingRuntimeCount == 0
	return v, nil
}
func lockDevice(tx *gorm.DB, id string) (time.Time, error) {
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "endpoint-channel-controls:"+id).Error; err != nil {
		return time.Time{}, err
	}
	var updated time.Time
	err := tx.Raw(`SELECT updated_at FROM network_access_devices WHERE id=? AND tenant_id='default' AND workspace_id='default' FOR SHARE`, id).Row().Scan(&updated)
	if errors.Is(err, sql.ErrNoRows) {
		return updated, apperrors.ErrNotFound
	}
	return updated, err
}
func audit(tx *gorm.DB, id, actor, action string, revision int64, metadata any) error {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO endpoint_channel_events(device_id,actor_id,action,revision,metadata) VALUES(?,?,?,?,?::jsonb)`, id, actor, action, revision, string(raw)).Error
}
func (r *Repository) Save(ctx context.Context, id, actor string, input ec.SaveInput, updated time.Time) (ec.Policy, error) {
	var out ec.Policy
	raw, _ := json.Marshal(input.Draft)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now, err := lockDevice(tx, id)
		if err != nil {
			return err
		}
		if !now.Equal(updated) {
			return apperrors.ErrConflict
		}
		p, err := policy(tx, id)
		if err != nil {
			return err
		}
		if p.Revision != input.ExpectedRevision {
			return apperrors.ErrConflict
		}
		if err := tx.Exec(`INSERT INTO endpoint_channel_policies(device_id,revision,draft,updated_at) VALUES(?,1,?::jsonb,clock_timestamp()) ON CONFLICT(device_id) DO UPDATE SET revision=endpoint_channel_policies.revision+1,draft=EXCLUDED.draft,updated_at=EXCLUDED.updated_at`, id, string(raw)).Error; err != nil {
			return err
		}
		out, err = policy(tx, id)
		if err != nil {
			return err
		}
		return audit(tx, id, actor, "saved", out.Revision, input.Draft)
	})
	return out, err
}
func (r *Repository) Publish(ctx context.Context, id, actor string, revision int64, updated time.Time) (ec.Policy, error) {
	var out ec.Policy
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now, err := lockDevice(tx, id)
		if err != nil {
			return err
		}
		if !now.Equal(updated) {
			return apperrors.ErrConflict
		}
		q := tx.Exec(`UPDATE endpoint_channel_policies SET published=draft,published_version=revision+1,revision=revision+1,updated_at=clock_timestamp() WHERE device_id=? AND revision=?`, id, revision)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		out, err = policy(tx, id)
		if err != nil {
			return err
		}
		return audit(tx, id, actor, "published", out.Revision, out.Published)
	})
	return out, err
}
func tokenHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func authorizeCredential(tx *gorm.DB, c runtime.Credential) error {
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('network-wireguard-mutations'))`).Error; err != nil {
		return err
	}
	var id string
	err := tx.Raw(`SELECT c.id FROM network_runtime_credentials c JOIN network_access_devices d ON d.id=c.device_id AND d.tenant_id=c.tenant_id AND d.workspace_id=c.workspace_id WHERE c.id=? AND c.runtime_id=? AND c.device_id=? AND c.subject_id=d.owner_user_id::text AND c.runtime_kind='endpoint' AND c.status='active' AND c.revoked_at IS NULL AND c.not_before<=clock_timestamp() AND c.expires_at>clock_timestamp() AND d.status='active' AND d.platform='windows' AND c.tenant_id='default' AND c.workspace_id='default' AND c.capabilities @> '["windows-channel-controls-v1"]'::jsonb AND c.generation=(SELECT MAX(n.generation) FROM network_runtime_credentials n WHERE n.runtime_id=c.runtime_id AND n.tenant_id=c.tenant_id AND n.workspace_id=c.workspace_id) FOR SHARE OF c,d`, c.ID, c.RuntimeID, c.DeviceID).Row().Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return apperrors.ErrAccessDenied
	}
	return err
}
func (r *Repository) Desired(ctx context.Context, c runtime.Credential, challenge string) (ec.Desired, error) {
	var out ec.Desired
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeCredential(tx, c); err != nil {
			return err
		}
		if _, err := lockDevice(tx, c.DeviceID); err != nil {
			return err
		}
		p, err := policy(tx, c.DeviceID)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		out = ec.Desired{Configured: p.Published != nil, SchemaVersion: ec.Version, DeviceID: c.DeviceID, RuntimeID: c.RuntimeID, CredentialID: c.ID, PolicyVersion: p.PublishedVersion, Challenge: challenge, IssuedAt: now, ValidUntil: now.Add(time.Minute)}
		if c.ExpiresAt.Before(out.ValidUntil) {
			out.ValidUntil = c.ExpiresAt
		}
		if p.Published != nil {
			out.Controls = *p.Published
		}
		if p.Published != nil {
			if err := tx.Exec(`INSERT INTO endpoint_channel_runtime_owners(device_id,runtime_id) VALUES(?,?) ON CONFLICT DO NOTHING`, c.DeviceID, c.RuntimeID).Error; err != nil {
				return err
			}
		}
		return tx.Exec(`INSERT INTO endpoint_channel_challenges(credential_id,device_id,policy_version,token_hash,expires_at) VALUES(?,?,?,?,?) ON CONFLICT(credential_id) DO UPDATE SET device_id=EXCLUDED.device_id,policy_version=EXCLUDED.policy_version,token_hash=EXCLUDED.token_hash,expires_at=EXCLUDED.expires_at,consumed=false`, c.ID, c.DeviceID, p.PublishedVersion, tokenHash(challenge), out.ValidUntil).Error
	})
	return out, err
}
func (r *Repository) Report(ctx context.Context, c runtime.Credential, in ec.Receipt) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeCredential(tx, c); err != nil {
			return err
		}
		if _, err := lockDevice(tx, c.DeviceID); err != nil {
			return err
		}
		p, err := policy(tx, c.DeviceID)
		if err != nil {
			return err
		}
		if p.PublishedVersion != in.PolicyVersion || !reportMatchesPolicy(p, in) {
			return apperrors.ErrConflict
		}
		q := tx.Exec(`UPDATE endpoint_channel_challenges SET consumed=true WHERE credential_id=? AND device_id=? AND policy_version=? AND token_hash=? AND NOT consumed AND expires_at>clock_timestamp() AND ?::timestamptz BETWEEN clock_timestamp()-interval '1 minute' AND clock_timestamp()+interval '30 seconds'`, c.ID, c.DeviceID, in.PolicyVersion, tokenHash(in.Challenge), in.ObservedAt)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		observed := ec.Observation{RuntimeID: c.RuntimeID, CredentialID: c.ID, PolicyVersion: in.PolicyVersion, ObservedAt: in.ObservedAt, Channels: in.Channels}
		raw, _ := json.Marshal(observed)
		if err := tx.Exec(`INSERT INTO endpoint_channel_reports(device_id,credential_id,policy_version,observation,received_at) VALUES(?,?,?,?::jsonb,clock_timestamp()) ON CONFLICT(credential_id) DO UPDATE SET device_id=EXCLUDED.device_id,policy_version=EXCLUDED.policy_version,observation=EXCLUDED.observation,received_at=EXCLUDED.received_at`, c.DeviceID, c.ID, in.PolicyVersion, string(raw)).Error; err != nil {
			return err
		}
		return audit(tx, c.DeviceID, c.SubjectID, "reported", in.PolicyVersion, observed)
	})
}

func reportMatchesPolicy(p ec.Policy, in ec.Receipt) bool {
	controls := ec.Controls{}
	if p.Published != nil {
		controls = *p.Published
	}
	enabled := map[string]bool{"usb": controls.USB, "print": controls.Print, "clipboard": controls.Clipboard}
	for _, c := range in.Channels {
		if p.Published == nil && c.State != "unconfigured" {
			return false
		}
		if p.Published != nil && enabled[c.Name] && (c.State == "off" || c.State == "unconfigured") {
			return false
		}
		if p.Published != nil && !enabled[c.Name] && c.State != "off" && c.State != "conflict" && c.State != "failed" {
			return false
		}
	}
	return true
}
