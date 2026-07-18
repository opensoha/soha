package directorysync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/directorysync"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	"gorm.io/gorm"
)

type Repository struct {
	db   *gorm.DB
	keys keyring.Ring
}

func New(db *gorm.DB, keys ...keyring.Ring) *Repository {
	r := &Repository{db: db}
	if len(keys) > 0 {
		r.keys = keys[0]
	}
	return r
}

func (r *Repository) CreateConnection(ctx context.Context, item domain.Connection, policy domain.Policy) (domain.Connection, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		capabilities, _ := json.Marshal(item.Capabilities)
		metadata, _ := json.Marshal(item.Metadata)
		if err := tx.Exec(`INSERT INTO directory_connections (id,name,provider_type,login_provider_id,credential_ref,enabled,capabilities,status,last_validated_at,metadata,created_by,updated_by,created_at,updated_at) VALUES (?,?,?,?,?,?,?::jsonb,?,?,?::jsonb,?,?,?,?)`, item.ID, item.Name, item.ProviderType, nullString(item.LoginProviderID), nullString(item.CredentialRef), item.Enabled, string(capabilities), item.Status, item.LastValidatedAt, string(metadata), item.CreatedBy, item.UpdatedBy, item.CreatedAt, item.UpdatedAt).Error; err != nil {
			return err
		}
		return upsertPolicy(tx, policy)
	})
	return item, err
}

func (r *Repository) UpdateConnection(ctx context.Context, item domain.Connection, policy domain.Policy) (domain.Connection, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		capabilities, _ := json.Marshal(item.Capabilities)
		metadata, _ := json.Marshal(item.Metadata)
		res := tx.Exec(`UPDATE directory_connections SET name=?,provider_type=?,login_provider_id=?,credential_ref=?,enabled=?,capabilities=?::jsonb,status=?,last_validated_at=?,metadata=?::jsonb,updated_by=?,updated_at=? WHERE id=?`, item.Name, item.ProviderType, nullString(item.LoginProviderID), nullString(item.CredentialRef), item.Enabled, string(capabilities), item.Status, item.LastValidatedAt, string(metadata), item.UpdatedBy, item.UpdatedAt, item.ID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: directory connection", apperrors.ErrNotFound)
		}
		return upsertPolicy(tx, policy)
	})
	return item, err
}

func upsertPolicy(tx *gorm.DB, p domain.Policy) error {
	domains, _ := json.Marshal(p.TrustedEmailDomains)
	mappings, _ := json.Marshal(p.FieldMappings)
	return tx.Exec(`INSERT INTO directory_sync_policies (connection_id,sync_organizations,sync_people,mode,schedule,full_reconcile_schedule,provision_mode,trusted_email_domains,verified_email_auto_link,user_disable_policy,missing_object_policy,field_mappings,updated_by,updated_at) VALUES (?,?,?,?,?,?,?,?::jsonb,?,?,?,?::jsonb,?,?) ON CONFLICT (connection_id) DO UPDATE SET sync_organizations=EXCLUDED.sync_organizations,sync_people=EXCLUDED.sync_people,mode=EXCLUDED.mode,schedule=EXCLUDED.schedule,full_reconcile_schedule=EXCLUDED.full_reconcile_schedule,provision_mode=EXCLUDED.provision_mode,trusted_email_domains=EXCLUDED.trusted_email_domains,verified_email_auto_link=EXCLUDED.verified_email_auto_link,user_disable_policy=EXCLUDED.user_disable_policy,missing_object_policy=EXCLUDED.missing_object_policy,field_mappings=EXCLUDED.field_mappings,updated_by=EXCLUDED.updated_by,updated_at=EXCLUDED.updated_at`, p.ConnectionID, p.SyncOrganizations, p.SyncPeople, p.Mode, p.Schedule, p.FullReconcileSchedule, p.ProvisionMode, string(domains), p.VerifiedEmailAutoLink, p.UserDisablePolicy, p.MissingObjectPolicy, string(mappings), p.UpdatedBy, p.UpdatedAt).Error
}

func (r *Repository) GetConnection(ctx context.Context, id string) (domain.Connection, domain.Policy, error) {
	var c domain.Connection
	var caps, meta, trusted, mappings []byte
	var login, credential sql.NullString
	var p domain.Policy
	err := r.db.WithContext(ctx).Raw(`SELECT c.id,c.name,c.provider_type,c.login_provider_id,c.credential_ref,c.enabled,c.capabilities,c.status,c.last_validated_at,c.metadata,c.created_by,c.updated_by,c.created_at,c.updated_at,p.connection_id,p.sync_organizations,p.sync_people,p.mode,p.schedule,p.full_reconcile_schedule,p.provision_mode,p.trusted_email_domains,p.verified_email_auto_link,p.user_disable_policy,p.missing_object_policy,p.field_mappings,p.updated_by,p.updated_at FROM directory_connections c JOIN directory_sync_policies p ON p.connection_id=c.id WHERE c.id=?`, id).Row().Scan(&c.ID, &c.Name, &c.ProviderType, &login, &credential, &c.Enabled, &caps, &c.Status, &c.LastValidatedAt, &meta, &c.CreatedBy, &c.UpdatedBy, &c.CreatedAt, &c.UpdatedAt, &p.ConnectionID, &p.SyncOrganizations, &p.SyncPeople, &p.Mode, &p.Schedule, &p.FullReconcileSchedule, &p.ProvisionMode, &trusted, &p.VerifiedEmailAutoLink, &p.UserDisablePolicy, &p.MissingObjectPolicy, &mappings, &p.UpdatedBy, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, p, fmt.Errorf("%w: directory connection", apperrors.ErrNotFound)
	}
	if err != nil {
		return c, p, err
	}
	c.LoginProviderID = login.String
	c.CredentialRef = credential.String
	_ = json.Unmarshal(caps, &c.Capabilities)
	_ = json.Unmarshal(meta, &c.Metadata)
	_ = json.Unmarshal(trusted, &p.TrustedEmailDomains)
	_ = json.Unmarshal(mappings, &p.FieldMappings)
	return c, p, nil
}

func (r *Repository) ListConnections(ctx context.Context) ([]domain.Connection, error) {
	rows, err := r.db.WithContext(ctx).Raw(`SELECT id FROM directory_connections ORDER BY name,id`).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Connection{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		c, _, e := r.GetConnection(ctx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repository) CreateRun(ctx context.Context, run domain.Run) (domain.Run, error) {
	stats, _ := json.Marshal(run.Stats)
	err := r.db.WithContext(ctx).Exec(`INSERT INTO directory_sync_runs (id,connection_id,trigger,mode,include_people,status,cursor_before,cursor_after,idempotency_key,stats,error_code,error_summary,requested_by,started_at,heartbeat_at,finished_at,created_at) VALUES (?,?,?,?,?,?,?,?,?,?::jsonb,?,?,?,?,?,?,?)`, run.ID, run.ConnectionID, run.Trigger, run.Mode, run.IncludePeople, run.Status, run.CursorBefore, run.CursorAfter, nullString(run.IdempotencyKey), string(stats), run.ErrorCode, run.ErrorSummary, run.RequestedBy, run.StartedAt, run.HeartbeatAt, run.FinishedAt, run.CreatedAt).Error
	return run, err
}
func (r *Repository) GetRun(ctx context.Context, id string) (domain.Run, error) {
	var x domain.Run
	var stats []byte
	err := r.db.WithContext(ctx).Raw(`SELECT id,connection_id,trigger,mode,include_people,status,cursor_before,cursor_after,COALESCE(idempotency_key,''),stats,error_code,error_summary,requested_by,started_at,heartbeat_at,finished_at,created_at FROM directory_sync_runs WHERE id=?`, id).Row().Scan(&x.ID, &x.ConnectionID, &x.Trigger, &x.Mode, &x.IncludePeople, &x.Status, &x.CursorBefore, &x.CursorAfter, &x.IdempotencyKey, &stats, &x.ErrorCode, &x.ErrorSummary, &x.RequestedBy, &x.StartedAt, &x.HeartbeatAt, &x.FinishedAt, &x.CreatedAt)
	_ = json.Unmarshal(stats, &x.Stats)
	return x, err
}

func (r *Repository) ListRuns(ctx context.Context, connectionID string, limit int) ([]domain.Run, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.WithContext(ctx).Raw(`SELECT id FROM directory_sync_runs WHERE connection_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, connectionID, limit).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Run, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		run, err := r.GetRun(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, run)
	}
	return result, rows.Err()
}
func (r *Repository) GetActiveRun(ctx context.Context, connectionID string) (domain.Run, error) {
	var id string
	err := r.db.WithContext(ctx).Raw(`SELECT id FROM directory_sync_runs WHERE connection_id=? AND status IN ('queued','running') ORDER BY created_at DESC LIMIT 1`, connectionID).Row().Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Run{}, fmt.Errorf("%w: active directory run", apperrors.ErrNotFound)
	}
	if err != nil {
		return domain.Run{}, err
	}
	return r.GetRun(ctx, id)
}
func (r *Repository) TransitionRun(ctx context.Context, id, status string, stats domain.RunStats, code, summary string) error {
	now := time.Now().UTC()
	b, _ := json.Marshal(stats)
	allowedFrom := []string{}
	switch status {
	case domain.RunRunning:
		allowedFrom = []string{domain.RunQueued}
	case domain.RunSucceeded, domain.RunPartial, domain.RunFailed:
		allowedFrom = []string{domain.RunRunning}
	case domain.RunCanceled:
		allowedFrom = []string{domain.RunQueued, domain.RunRunning}
	case domain.RunQueued:
		allowedFrom = []string{domain.RunPartial, domain.RunFailed}
	default:
		return fmt.Errorf("%w: unknown target %q", domain.ErrInvalidRunState, status)
	}
	res := r.db.WithContext(ctx).Exec(`UPDATE directory_sync_runs SET status=?,stats=?::jsonb,error_code=?,error_summary=?,started_at=CASE WHEN ?='running' THEN COALESCE(started_at,?) ELSE started_at END,heartbeat_at=CASE WHEN ?='running' THEN ? ELSE heartbeat_at END,finished_at=CASE WHEN ? IN ('succeeded','partial','failed','canceled') THEN ? ELSE finished_at END WHERE id=? AND status IN ?`, status, string(b), code, summary, status, now, status, now, status, now, id, allowedFrom)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("%w: run %s cannot transition to %s", domain.ErrInvalidRunState, id, status)
	}
	return nil
}

func (r *Repository) ListOrganizations(ctx context.Context, c string) ([]domain.Organization, error) {
	rows, e := r.db.WithContext(ctx).Raw(`SELECT id,connection_id,external_id,COALESCE(external_parent_id,''),COALESCE(local_team_id,''),name,path,status,source_version,raw_hash,first_seen_at,last_seen_at,archived_at FROM directory_organizations WHERE connection_id=?`, c).Rows()
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []domain.Organization{}
	for rows.Next() {
		var x domain.Organization
		if e = rows.Scan(&x.ID, &x.ConnectionID, &x.ExternalID, &x.ExternalParentID, &x.LocalTeamID, &x.Name, &x.Path, &x.Status, &x.SourceVersion, &x.RawHash, &x.FirstSeenAt, &x.LastSeenAt, &x.ArchivedAt); e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (r *Repository) ListPeople(ctx context.Context, c string) ([]domain.Person, error) {
	rows, e := r.db.WithContext(ctx).Raw(`SELECT id,connection_id,external_id,provider_subject,COALESCE(local_user_id,''),username,display_name,email,email_verified,phone,avatar_url,status,source_version,raw_hash,first_seen_at,last_seen_at,archived_at FROM directory_people WHERE connection_id=?`, c).Rows()
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []domain.Person{}
	for rows.Next() {
		var x domain.Person
		if e = rows.Scan(&x.ID, &x.ConnectionID, &x.ExternalID, &x.ProviderSubject, &x.LocalUserID, &x.Username, &x.DisplayName, &x.Email, &x.EmailVerified, &x.Phone, &x.AvatarURL, &x.Status, &x.SourceVersion, &x.RawHash, &x.FirstSeenAt, &x.LastSeenAt, &x.ArchivedAt); e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (r *Repository) ListMemberships(ctx context.Context, c string) ([]domain.Membership, error) {
	rows, e := r.db.WithContext(ctx).Raw(`SELECT connection_id,external_person_id,external_organization_id,COALESCE(local_user_id,''),COALESCE(local_team_id,''),status,last_seen_at FROM directory_memberships WHERE connection_id=?`, c).Rows()
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []domain.Membership{}
	for rows.Next() {
		var x domain.Membership
		if e = rows.Scan(&x.ConnectionID, &x.ExternalPersonID, &x.ExternalOrganizationID, &x.LocalUserID, &x.LocalTeamID, &x.Status, &x.LastSeenAt); e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (r *Repository) ApplyProjections(ctx context.Context, connectionID string, orgs []domain.Organization, people []domain.Person, memberships []domain.Membership, includePeople bool) error {
	batch := projectionBatch{
		connectionID:  connectionID,
		organizations: orgs,
		people:        people,
		memberships:   memberships,
		includePeople: includePeople,
		now:           time.Now().UTC(),
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return applyProjections(tx, batch)
	})
}

type projectionBatch struct {
	connectionID  string
	organizations []domain.Organization
	people        []domain.Person
	memberships   []domain.Membership
	includePeople bool
	now           time.Time
}

func applyProjections(tx *gorm.DB, batch projectionBatch) error {
	if err := applyOrganizationProjections(tx, batch); err != nil {
		return err
	}
	if !batch.includePeople {
		if len(batch.people) > 0 || len(batch.memberships) > 0 {
			return domain.ErrPeopleSyncDisabled
		}
		return nil
	}
	if err := applyPeopleProjections(tx, batch); err != nil {
		return err
	}
	return replaceMembershipProjections(tx, batch)
}

func applyOrganizationProjections(tx *gorm.DB, batch projectionBatch) error {
	if err := tx.Exec(`UPDATE directory_organizations SET status=?, archived_at=?, last_seen_at=? WHERE connection_id=? AND status<>?`, domain.ProjectionArchived, batch.now, batch.now, batch.connectionID, domain.ProjectionArchived).Error; err != nil {
		return err
	}
	for _, organization := range batch.organizations {
		organization = prepareOrganizationProjection(organization, batch.connectionID, batch.now)
		if err := tx.Exec(`INSERT INTO directory_organizations (id,connection_id,external_id,external_parent_id,local_team_id,name,path,status,source_version,raw_hash,first_seen_at,last_seen_at,archived_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(connection_id,external_id) DO UPDATE SET external_parent_id=EXCLUDED.external_parent_id,local_team_id=COALESCE(EXCLUDED.local_team_id,directory_organizations.local_team_id),name=EXCLUDED.name,path=EXCLUDED.path,status=EXCLUDED.status,source_version=EXCLUDED.source_version,raw_hash=EXCLUDED.raw_hash,last_seen_at=EXCLUDED.last_seen_at,archived_at=EXCLUDED.archived_at`, organization.ID, organization.ConnectionID, organization.ExternalID, nullString(organization.ExternalParentID), nullString(organization.LocalTeamID), organization.Name, organization.Path, organization.Status, organization.SourceVersion, organization.RawHash, organization.FirstSeenAt, organization.LastSeenAt, organization.ArchivedAt).Error; err != nil {
			return err
		}
	}
	return nil
}

func prepareOrganizationProjection(item domain.Organization, connectionID string, now time.Time) domain.Organization {
	if item.ConnectionID == "" {
		item.ConnectionID = connectionID
	}
	if item.ID == "" {
		item.ID = item.ConnectionID + ":" + item.ExternalID
	}
	if item.Status == "" {
		item.Status = domain.ProjectionActive
	}
	if item.FirstSeenAt.IsZero() {
		item.FirstSeenAt = now
	}
	if item.LastSeenAt.IsZero() {
		item.LastSeenAt = now
	}
	return item
}

func applyPeopleProjections(tx *gorm.DB, batch projectionBatch) error {
	if err := tx.Exec(`UPDATE directory_people SET status=?, archived_at=?, last_seen_at=? WHERE connection_id=? AND status<>?`, domain.ProjectionArchived, batch.now, batch.now, batch.connectionID, domain.ProjectionArchived).Error; err != nil {
		return err
	}
	for _, person := range batch.people {
		person = preparePersonProjection(person, batch.connectionID, batch.now)
		if err := tx.Exec(`INSERT INTO directory_people (id,connection_id,external_id,provider_subject,local_user_id,username,display_name,email,email_verified,phone,avatar_url,status,source_version,raw_hash,first_seen_at,last_seen_at,archived_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(connection_id,external_id) DO UPDATE SET provider_subject=EXCLUDED.provider_subject,local_user_id=COALESCE(EXCLUDED.local_user_id,directory_people.local_user_id),username=EXCLUDED.username,display_name=EXCLUDED.display_name,email=EXCLUDED.email,email_verified=EXCLUDED.email_verified,phone=EXCLUDED.phone,avatar_url=EXCLUDED.avatar_url,status=EXCLUDED.status,source_version=EXCLUDED.source_version,raw_hash=EXCLUDED.raw_hash,last_seen_at=EXCLUDED.last_seen_at,archived_at=EXCLUDED.archived_at`, person.ID, person.ConnectionID, person.ExternalID, person.ProviderSubject, nullString(person.LocalUserID), person.Username, person.DisplayName, person.Email, person.EmailVerified, person.Phone, person.AvatarURL, person.Status, person.SourceVersion, person.RawHash, person.FirstSeenAt, person.LastSeenAt, person.ArchivedAt).Error; err != nil {
			return err
		}
	}
	return nil
}

func preparePersonProjection(item domain.Person, connectionID string, now time.Time) domain.Person {
	if item.ConnectionID == "" {
		item.ConnectionID = connectionID
	}
	if item.ID == "" {
		item.ID = item.ConnectionID + ":" + item.ExternalID
	}
	if item.Status == "" {
		item.Status = domain.ProjectionActive
	}
	if item.FirstSeenAt.IsZero() {
		item.FirstSeenAt = now
	}
	if item.LastSeenAt.IsZero() {
		item.LastSeenAt = now
	}
	return item
}

func replaceMembershipProjections(tx *gorm.DB, batch projectionBatch) error {
	if err := tx.Exec(`DELETE FROM directory_memberships WHERE connection_id=?`, batch.connectionID).Error; err != nil {
		return err
	}
	for _, membership := range batch.memberships {
		if membership.LastSeenAt.IsZero() {
			membership.LastSeenAt = batch.now
		}
		if err := tx.Exec(`INSERT INTO directory_memberships (connection_id,external_person_id,external_organization_id,local_user_id,local_team_id,status,last_seen_at) VALUES (?,?,?,?,?,?,?)`, batch.connectionID, membership.ExternalPersonID, membership.ExternalOrganizationID, nullString(membership.LocalUserID), nullString(membership.LocalTeamID), membership.Status, membership.LastSeenAt).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) CreateSuppression(ctx context.Context, s domain.IdentityLinkSuppression) error {
	return r.db.WithContext(ctx).Exec(`INSERT INTO identity_link_suppressions (id,user_id,provider_type,provider_id,provider_user_id,reason,created_by,created_at) VALUES (?,?,?,?,?,?,?,?)`, s.ID, s.UserID, s.ProviderType, s.ProviderID, s.ProviderUserID, s.Reason, s.CreatedBy, s.CreatedAt).Error
}
func (r *Repository) FindActiveSuppression(ctx context.Context, userID, providerType, providerID, providerUserID string) (*domain.IdentityLinkSuppression, error) {
	var s domain.IdentityLinkSuppression
	err := r.db.WithContext(ctx).Raw(`SELECT id,user_id,provider_type,provider_id,provider_user_id,reason,created_by,created_at,COALESCE(cleared_by,''),cleared_at FROM identity_link_suppressions WHERE user_id=? AND provider_type=? AND provider_id=? AND provider_user_id=? AND cleared_at IS NULL`, userID, providerType, providerID, providerUserID).Row().Scan(&s.ID, &s.UserID, &s.ProviderType, &s.ProviderID, &s.ProviderUserID, &s.Reason, &s.CreatedBy, &s.CreatedAt, &s.ClearedBy, &s.ClearedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &s, err
}
func (r *Repository) ClearSuppression(ctx context.Context, id, actor string, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return r.db.WithContext(ctx).Exec(`UPDATE identity_link_suppressions SET cleared_by=?,cleared_at=? WHERE id=? AND cleared_at IS NULL`, actor, at, id).Error
}

func (r *Repository) ListConflicts(ctx context.Context, connectionID string, limit int) ([]domain.Conflict, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	query := `SELECT id,connection_id,object_type,external_id,reason,status,resolution,created_at,resolved_by,resolved_at FROM directory_conflicts`
	args := []any{}
	if connectionID != "" {
		query += ` WHERE connection_id=?`
		args = append(args, connectionID)
	}
	query += ` ORDER BY created_at DESC,id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]domain.Conflict, 0)
	for rows.Next() {
		var item domain.Conflict
		if err := rows.Scan(&item.ID, &item.ConnectionID, &item.ObjectType, &item.ExternalID, &item.Reason, &item.Status, &item.Resolution, &item.CreatedAt, &item.ResolvedBy, &item.ResolvedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *Repository) ResolveConflict(ctx context.Context, id, resolution, actor string, at time.Time) error {
	if resolution != "ignore" && resolution != "retry" {
		return fmt.Errorf("%w: invalid conflict resolution", apperrors.ErrInvalidArgument)
	}
	status := "resolved"
	if resolution == "ignore" {
		status = "ignored"
	}
	result := r.db.WithContext(ctx).Exec(`UPDATE directory_conflicts SET status=?,resolution=?,resolved_by=?,resolved_at=? WHERE id=? AND status='open'`, status, resolution, actor, at, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: directory conflict", apperrors.ErrNotFound)
	}
	return nil
}

func (r *Repository) UnlinkIdentity(ctx context.Context, identityID, actor string, at time.Time) (domain.IdentityLinkSuppression, error) {
	var suppression domain.IdentityLinkSuppression
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var userID, providerType, providerID, providerUserID string
		err := tx.Raw(`SELECT user_id,provider_type,provider_id,provider_user_id FROM user_identities WHERE id=? FOR UPDATE`, identityID).Row().Scan(&userID, &providerType, &providerID, &providerUserID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: identity link", apperrors.ErrNotFound)
		}
		if err != nil {
			return err
		}
		var usable int
		if err := tx.Raw(`SELECT (SELECT COUNT(*) FROM user_password_credentials WHERE user_id=?) + (SELECT COUNT(*) FROM user_identities WHERE user_id=? AND id<>?)`, userID, userID, identityID).Row().Scan(&usable); err != nil {
			return err
		}
		if usable < 1 {
			return fmt.Errorf("%w: cannot unlink the last usable login method", apperrors.ErrConflict)
		}
		if err := tx.Exec(`DELETE FROM user_identities WHERE id=?`, identityID).Error; err != nil {
			return err
		}
		suppression = domain.IdentityLinkSuppression{ID: uuid.NewString(), UserID: userID, ProviderType: providerType, ProviderID: providerID, ProviderUserID: providerUserID, Reason: "explicit_unlink", CreatedBy: actor, CreatedAt: at}
		return tx.Exec(`INSERT INTO identity_link_suppressions (id,user_id,provider_type,provider_id,provider_user_id,reason,created_by,created_at) VALUES (?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, suppression.ID, suppression.UserID, suppression.ProviderType, suppression.ProviderID, suppression.ProviderUserID, suppression.Reason, suppression.CreatedBy, suppression.CreatedAt).Error
	})
	return suppression, err
}

func (r *Repository) SetWebhookCredential(ctx context.Context, credential domain.WebhookCredential) error {
	verificationToken, err := secretcrypto.EncryptStringWithKeyring(r.keys, credential.VerificationToken)
	if err != nil {
		return fmt.Errorf("encrypt directory webhook verification token: %w", err)
	}
	encryptKey, err := secretcrypto.EncryptStringWithKeyring(r.keys, credential.EncryptKey)
	if err != nil {
		return fmt.Errorf("encrypt directory webhook key: %w", err)
	}
	return r.db.WithContext(ctx).Exec(`INSERT INTO directory_webhook_credentials (connection_id,verification_token_encrypted,encrypt_key_encrypted,updated_at) VALUES (?,?,?,?) ON CONFLICT(connection_id) DO UPDATE SET verification_token_encrypted=EXCLUDED.verification_token_encrypted,encrypt_key_encrypted=EXCLUDED.encrypt_key_encrypted,updated_at=EXCLUDED.updated_at`, credential.ConnectionID, verificationToken, encryptKey, time.Now().UTC()).Error
}

func (r *Repository) GetWebhookCredential(ctx context.Context, connectionID string) (domain.WebhookCredential, error) {
	var verificationToken, encryptKey string
	err := r.db.WithContext(ctx).Raw(`SELECT verification_token_encrypted,encrypt_key_encrypted FROM directory_webhook_credentials WHERE connection_id=?`, connectionID).Row().Scan(&verificationToken, &encryptKey)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WebhookCredential{}, fmt.Errorf("%w: directory webhook credential", apperrors.ErrNotFound)
	}
	if err != nil {
		return domain.WebhookCredential{}, err
	}
	verificationToken, err = secretcrypto.DecryptStringWithKeyring(r.keys, verificationToken)
	if err != nil {
		return domain.WebhookCredential{}, err
	}
	encryptKey, err = secretcrypto.DecryptStringWithKeyring(r.keys, encryptKey)
	return domain.WebhookCredential{ConnectionID: connectionID, VerificationToken: verificationToken, EncryptKey: encryptKey}, err
}

func (r *Repository) EnqueueEvent(ctx context.Context, event domain.EventEnvelope) (bool, error) {
	result := r.db.WithContext(ctx).Exec(`INSERT INTO directory_event_inbox (id,connection_id,provider_event_id,event_type,occurred_at,received_at,status) VALUES (?,?,?,?,?,?,'queued') ON CONFLICT(connection_id,provider_event_id) DO NOTHING`, event.ID, event.ConnectionID, event.ProviderEventID, event.EventType, event.OccurredAt, event.ReceivedAt)
	return result.RowsAffected == 1, result.Error
}

func (r *Repository) ClaimEvents(ctx context.Context, limit int) ([]domain.EventEnvelope, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	result := []domain.EventEnvelope{}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		rows, err := tx.Raw(`SELECT id,connection_id,provider_event_id,event_type,occurred_at,received_at,status,error_summary,processed_at,attempts,claimed_at,next_attempt_at FROM directory_event_inbox WHERE status='queued' OR (status='failed' AND attempts<3 AND (next_attempt_at IS NULL OR next_attempt_at<=?)) ORDER BY received_at FOR UPDATE SKIP LOCKED LIMIT ?`, now, limit).Rows()
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item domain.EventEnvelope
			if err := rows.Scan(&item.ID, &item.ConnectionID, &item.ProviderEventID, &item.EventType, &item.OccurredAt, &item.ReceivedAt, &item.Status, &item.ErrorSummary, &item.ProcessedAt, &item.Attempts, &item.ClaimedAt, &item.NextAttemptAt); err != nil {
				return err
			}
			result = append(result, item)
		}
		for _, item := range result {
			if err := tx.Exec(`UPDATE directory_event_inbox SET status='processing',attempts=attempts+1,claimed_at=?,processed_at=NULL WHERE id=? AND status IN ('queued','failed')`, now, item.ID).Error; err != nil {
				return err
			}
		}
		return rows.Err()
	})
	return result, err
}

func (r *Repository) CompleteEvent(ctx context.Context, id, status, summary string, at time.Time) error {
	if status != "succeeded" && status != "failed" {
		return fmt.Errorf("%w: invalid event status", apperrors.ErrInvalidArgument)
	}
	result := r.db.WithContext(ctx).Exec(`UPDATE directory_event_inbox SET status=?,error_summary=?,processed_at=?,claimed_at=NULL,next_attempt_at=CASE WHEN ?='failed' THEN ? + LEAST(attempts,6) * interval '30 seconds' ELSE NULL END WHERE id=? AND status='processing'`, status, summary, at, status, at, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: directory event is not processing", apperrors.ErrConflict)
	}
	return nil
}

func (r *Repository) RecoverStaleEvents(ctx context.Context, staleBefore, at time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Exec(`UPDATE directory_event_inbox SET status='failed',error_summary='worker lease expired',claimed_at=NULL,next_attempt_at=? WHERE status='processing' AND claimed_at<?`, at, staleBefore)
	return result.RowsAffected, result.Error
}

func (r *Repository) RecoverStaleRuns(ctx context.Context, staleBefore, at time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Exec(`UPDATE directory_sync_runs SET status='failed',error_code='worker_stale',error_summary='worker lease expired',finished_at=? WHERE (status='running' AND COALESCE(heartbeat_at,started_at,created_at)<?) OR (status='queued' AND created_at<?)`, at, staleBefore, staleBefore)
	return result.RowsAffected, result.Error
}

func (r *Repository) SetSCIMToken(ctx context.Context, connectionID, tokenHash string, at time.Time) error {
	return r.SetSCIMTokenScoped(ctx, connectionID, tokenHash, []string{domain.SCIMScopeRead, domain.SCIMScopeWrite}, at)
}

func (r *Repository) ResolveSCIMConnection(ctx context.Context, tokenHash string) (string, error) {
	// The legacy API is used by handlers that do not distinguish HTTP read/write
	// operations, so only a full-access token is accepted through this path.
	return r.resolveSCIMConnectionForScopes(ctx, tokenHash, []string{domain.SCIMScopeRead, domain.SCIMScopeWrite})
}

func (r *Repository) SetSCIMTokenScoped(ctx context.Context, connectionID, tokenHash string, scopes []string, at time.Time) error {
	if len(scopes) == 0 {
		return fmt.Errorf("%w: SCIM token scope is required", apperrors.ErrInvalidArgument)
	}
	encoded, err := json.Marshal(scopes)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Exec(`INSERT INTO directory_scim_tokens (connection_id,token_hash,scopes,created_at) VALUES (?,?,?::jsonb,?) ON CONFLICT(connection_id) DO UPDATE SET token_hash=EXCLUDED.token_hash,scopes=EXCLUDED.scopes,rotated_at=EXCLUDED.created_at`, connectionID, tokenHash, string(encoded), at).Error
}

func (r *Repository) ResolveSCIMConnectionForScope(ctx context.Context, tokenHash, requiredScope string) (string, error) {
	return r.resolveSCIMConnectionForScopes(ctx, tokenHash, []string{requiredScope})
}

func (r *Repository) resolveSCIMConnectionForScopes(ctx context.Context, tokenHash string, requiredScopes []string) (string, error) {
	var connectionID string
	required, _ := json.Marshal(requiredScopes)
	err := r.db.WithContext(ctx).Raw(`SELECT t.connection_id FROM directory_scim_tokens t JOIN directory_connections c ON c.id=t.connection_id WHERE t.token_hash=? AND t.scopes @> ?::jsonb AND c.enabled=true AND c.provider_type=?`, tokenHash, string(required), domain.ProviderSCIM).Row().Scan(&connectionID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w: SCIM bearer token", apperrors.ErrUnauthorized)
	}
	return connectionID, err
}

func (r *Repository) UpsertSCIMOrganization(ctx context.Context, connectionID string, item domain.Organization) error {
	return r.db.WithContext(ctx).Exec(`INSERT INTO directory_scim_organizations (connection_id,external_id,name,external_parent_id,updated_at) VALUES (?,?,?,?,?) ON CONFLICT(connection_id,external_id) DO UPDATE SET name=EXCLUDED.name,external_parent_id=EXCLUDED.external_parent_id,updated_at=EXCLUDED.updated_at`, connectionID, item.ExternalID, item.Name, item.ExternalParentID, time.Now().UTC()).Error
}

func (r *Repository) DeleteSCIMOrganization(ctx context.Context, connectionID, externalID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`DELETE FROM directory_scim_memberships WHERE connection_id=? AND external_organization_id=?`, connectionID, externalID).Error; err != nil {
			return err
		}
		return tx.Exec(`DELETE FROM directory_scim_organizations WHERE connection_id=? AND external_id=?`, connectionID, externalID).Error
	})
}

func (r *Repository) UpsertSCIMPerson(ctx context.Context, connectionID string, item domain.Person) error {
	return r.db.WithContext(ctx).Exec(`INSERT INTO directory_scim_people (connection_id,external_id,username,display_name,email,phone,active,updated_at) VALUES (?,?,?,?,?,?,?,?) ON CONFLICT(connection_id,external_id) DO UPDATE SET username=EXCLUDED.username,display_name=EXCLUDED.display_name,email=EXCLUDED.email,phone=EXCLUDED.phone,active=EXCLUDED.active,updated_at=EXCLUDED.updated_at`, connectionID, item.ExternalID, item.Username, item.DisplayName, item.Email, item.Phone, item.Status == domain.ProjectionActive, time.Now().UTC()).Error
}

func (r *Repository) DeleteSCIMPerson(ctx context.Context, connectionID, externalID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`DELETE FROM directory_scim_memberships WHERE connection_id=? AND external_person_id=?`, connectionID, externalID).Error; err != nil {
			return err
		}
		return tx.Exec(`DELETE FROM directory_scim_people WHERE connection_id=? AND external_id=?`, connectionID, externalID).Error
	})
}

func (r *Repository) ReplaceSCIMMemberships(ctx context.Context, connectionID, organizationID string, personIDs []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`DELETE FROM directory_scim_memberships WHERE connection_id=? AND external_organization_id=?`, connectionID, organizationID).Error; err != nil {
			return err
		}
		for _, personID := range personIDs {
			if err := tx.Exec(`INSERT INTO directory_scim_memberships (connection_id,external_person_id,external_organization_id) VALUES (?,?,?) ON CONFLICT DO NOTHING`, connectionID, personID, organizationID).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repository) SCIMSnapshot(ctx context.Context, connectionID string) (domain.Snapshot, error) {
	snapshot := domain.Snapshot{}
	orgRows, err := r.db.WithContext(ctx).Raw(`SELECT external_id,name,external_parent_id,updated_at FROM directory_scim_organizations WHERE connection_id=? ORDER BY external_id`, connectionID).Rows()
	if err != nil {
		return snapshot, err
	}
	for orgRows.Next() {
		var item domain.Organization
		var updated time.Time
		if err := orgRows.Scan(&item.ExternalID, &item.Name, &item.ExternalParentID, &updated); err != nil {
			orgRows.Close()
			return snapshot, err
		}
		item.ConnectionID = connectionID
		item.Status = domain.ProjectionActive
		item.SourceVersion = updated.UTC().Format(time.RFC3339Nano)
		snapshot.Organizations = append(snapshot.Organizations, item)
	}
	orgRows.Close()
	peopleRows, err := r.db.WithContext(ctx).Raw(`SELECT external_id,username,display_name,email,phone,active,updated_at FROM directory_scim_people WHERE connection_id=? ORDER BY external_id`, connectionID).Rows()
	if err != nil {
		return snapshot, err
	}
	for peopleRows.Next() {
		var item domain.Person
		var active bool
		var updated time.Time
		if err := peopleRows.Scan(&item.ExternalID, &item.Username, &item.DisplayName, &item.Email, &item.Phone, &active, &updated); err != nil {
			peopleRows.Close()
			return snapshot, err
		}
		item.ConnectionID = connectionID
		item.ProviderSubject = item.ExternalID
		item.Status = domain.ProjectionSuspended
		if active {
			item.Status = domain.ProjectionActive
		}
		item.SourceVersion = updated.UTC().Format(time.RFC3339Nano)
		snapshot.People = append(snapshot.People, item)
	}
	peopleRows.Close()
	membershipRows, err := r.db.WithContext(ctx).Raw(`SELECT external_person_id,external_organization_id FROM directory_scim_memberships WHERE connection_id=? ORDER BY external_person_id,external_organization_id`, connectionID).Rows()
	if err != nil {
		return snapshot, err
	}
	defer membershipRows.Close()
	for membershipRows.Next() {
		var item domain.Membership
		if err := membershipRows.Scan(&item.ExternalPersonID, &item.ExternalOrganizationID); err != nil {
			return snapshot, err
		}
		item.ConnectionID = connectionID
		item.Status = domain.ProjectionActive
		snapshot.Memberships = append(snapshot.Memberships, item)
	}
	return snapshot, membershipRows.Err()
}

func (r *Repository) SetConnectionCredential(ctx context.Context, credential domain.ConnectionCredential) error {
	username, err := secretcrypto.EncryptStringWithKeyring(r.keys, credential.Username)
	if err != nil {
		return fmt.Errorf("encrypt directory credential username: %w", err)
	}
	password, err := secretcrypto.EncryptStringWithKeyring(r.keys, credential.Password)
	if err != nil {
		return fmt.Errorf("encrypt directory credential password: %w", err)
	}
	return r.db.WithContext(ctx).Exec(`INSERT INTO directory_connection_credentials (connection_id,username_encrypted,password_encrypted,updated_at) VALUES (?,?,?,?) ON CONFLICT(connection_id) DO UPDATE SET username_encrypted=EXCLUDED.username_encrypted,password_encrypted=EXCLUDED.password_encrypted,updated_at=EXCLUDED.updated_at`, credential.ConnectionID, username, password, time.Now().UTC()).Error
}

func (r *Repository) GetConnectionCredential(ctx context.Context, connectionID string) (domain.ConnectionCredential, error) {
	var username, password string
	err := r.db.WithContext(ctx).Raw(`SELECT username_encrypted,password_encrypted FROM directory_connection_credentials WHERE connection_id=?`, connectionID).Row().Scan(&username, &password)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ConnectionCredential{}, fmt.Errorf("%w: directory connection credential", apperrors.ErrNotFound)
	}
	if err != nil {
		return domain.ConnectionCredential{}, err
	}
	username, err = secretcrypto.DecryptStringWithKeyring(r.keys, username)
	if err != nil {
		return domain.ConnectionCredential{}, err
	}
	password, err = secretcrypto.DecryptStringWithKeyring(r.keys, password)
	return domain.ConnectionCredential{ConnectionID: connectionID, Username: username, Password: password}, err
}
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

var _ domain.Repository = (*Repository)(nil)
