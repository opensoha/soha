package directorysync

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appdirectorysync "github.com/opensoha/soha/internal/application/directorysync"
	domain "github.com/opensoha/soha/internal/domain/directorysync"
	"gorm.io/gorm"
)

// DatabaseProjector applies normalized directory state to the existing access
// tables while keeping all local roles, passwords, and manually-owned bindings.
type DatabaseProjector struct {
	db  *gorm.DB
	now func() time.Time
}

func NewDatabaseProjector(db *gorm.DB) *DatabaseProjector {
	return &DatabaseProjector{db: db, now: time.Now}
}

func (p *DatabaseProjector) Apply(ctx context.Context, connection domain.Connection, policy domain.Policy, plan domain.Plan) error {
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := p.applyOrganizations(tx, connection, plan.Organizations); err != nil {
			return err
		}
		if policy.SyncPeople {
			if err := p.applyPeople(tx, connection, policy, plan.People, plan.Memberships); err != nil {
				return err
			}
		}
		return applyProjections(tx, projectionBatch{
			connectionID:  connection.ID,
			organizations: plan.Organizations,
			people:        plan.People,
			memberships:   plan.Memberships,
			includePeople: policy.SyncPeople,
			now:           p.now().UTC(),
		})
	})
}

func (p *DatabaseProjector) ApplyDelta(ctx context.Context, connection domain.Connection, policy domain.Policy, delta domain.Delta) error {
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if delta.Organization != nil {
			if err := p.applyOrganizationDelta(tx, connection, *delta.Organization); err != nil {
				return err
			}
		}
		if delta.Person != nil {
			if !policy.SyncPeople {
				return domain.ErrPeopleSyncDisabled
			}
			if err := p.applyPeople(tx, connection, policy, []domain.Person{*delta.Person}, delta.Memberships); err != nil {
				return err
			}
		}
		return applyDeltaProjection(tx, connection.ID, delta, p.now().UTC())
	})
}

func (p *DatabaseProjector) applyOrganizationDelta(tx *gorm.DB, connection domain.Connection, organization domain.Organization) error {
	now := p.now().UTC()
	teamID := stableID("team", connection.ID, organization.ExternalID)
	if organization.Status == domain.ProjectionArchived {
		return tx.Exec(`UPDATE teams SET metadata=(COALESCE(metadata,'{}'::json)::jsonb || '{"directoryStatus":"archived"}'::jsonb)::json,updated_at=? WHERE id=? AND source=?`, now, teamID, directorySource(connection)).Error
	}
	parentID := ""
	if organization.ExternalParentID != "" {
		parentID = stableID("team", connection.ID, organization.ExternalParentID)
	}
	slug := "directory-" + shortHash(connection.ID+"\x00"+organization.ExternalID)
	var oldPath string
	if err := tx.Raw(`SELECT COALESCE(org_path, '') FROM teams WHERE id=?`, teamID).Row().Scan(&oldPath); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	path := "/" + slug
	if parentID != "" {
		var parentPath string
		if err := tx.Raw(`SELECT COALESCE(org_path, '') FROM teams WHERE id=?`, parentID).Row().Scan(&parentPath); err != nil {
			return err
		}
		path = strings.TrimRight(parentPath, "/") + "/" + slug
	}
	metadata, _ := json.Marshal(map[string]any{"directoryConnectionId": connection.ID, "directoryConnectionName": connection.Name, "directoryProviderType": connection.ProviderType, "directoryStatus": organization.Status, "lastSyncedAt": now})
	if err := tx.Exec(`INSERT INTO teams (id,parent_id,name,slug,org_path,source,external_id,metadata,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?::json,?,?) ON CONFLICT (id) DO UPDATE SET parent_id=EXCLUDED.parent_id,name=EXCLUDED.name,org_path=EXCLUDED.org_path,source=EXCLUDED.source,external_id=EXCLUDED.external_id,metadata=(COALESCE(teams.metadata,'{}'::json)::jsonb || EXCLUDED.metadata::jsonb)::json,updated_at=EXCLUDED.updated_at`, teamID, projectorNullString(parentID), organization.Name, slug, path, directorySource(connection), organization.ExternalID, string(metadata), now, now).Error; err != nil {
		return err
	}
	if oldPath != "" && oldPath != path {
		return tx.Exec(`UPDATE teams SET org_path=? || substring(org_path FROM char_length(?)+1),updated_at=? WHERE source=? AND org_path LIKE ? AND id<>?`, path, oldPath, now, directorySource(connection), oldPath+"/%", teamID).Error
	}
	return nil
}

func (p *DatabaseProjector) ApplyOrganizations(ctx context.Context, connection domain.Connection, organizations []domain.Organization, dryRun bool) error {
	if dryRun {
		return nil
	}
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return p.applyOrganizations(tx, connection, organizations)
	})
}

func (p *DatabaseProjector) applyOrganizations(tx *gorm.DB, connection domain.Connection, organizations []domain.Organization) error {
	source := directorySource(connection)
	now := p.now().UTC()
	seen := make([]string, 0, len(organizations))
	teamByExternalID := make(map[string]string, len(organizations))
	for i := range organizations {
		teamID := stableID("team", connection.ID, organizations[i].ExternalID)
		organizations[i].LocalTeamID = teamID
		teamByExternalID[organizations[i].ExternalID] = teamID
	}
	for i := range organizations {
		organization := &organizations[i]
		teamID := organization.LocalTeamID
		seen = append(seen, organization.ExternalID)
		parentID := teamByExternalID[organization.ExternalParentID]
		slug := "directory-" + shortHash(connection.ID+"\x00"+organization.ExternalID)
		path := organization.Path
		if path == "" {
			path = "/" + slug
			if parentID != "" {
				var parentPath string
				if err := tx.Raw(`SELECT COALESCE(org_path, '') FROM teams WHERE id = ?`, parentID).Row().Scan(&parentPath); err != nil && !errors.Is(err, sql.ErrNoRows) {
					return err
				}
				if parentPath != "" {
					path = strings.TrimRight(parentPath, "/") + "/" + slug
				}
			}
		}
		metadata, _ := json.Marshal(map[string]any{
			"directoryConnectionId":   connection.ID,
			"directoryConnectionName": connection.Name,
			"directoryProviderType":   connection.ProviderType,
			"directoryStatus":         organization.Status,
			"lastSyncedAt":            now,
		})
		if err := tx.Exec(`
				INSERT INTO teams (id,parent_id,name,slug,org_path,source,external_id,metadata,created_at,updated_at)
				VALUES (?,?,?,?,?,?,?,?::json,?,?)
				ON CONFLICT (id) DO UPDATE SET parent_id=EXCLUDED.parent_id,name=EXCLUDED.name,org_path=EXCLUDED.org_path,
					source=EXCLUDED.source,external_id=EXCLUDED.external_id,
					metadata=(COALESCE(teams.metadata,'{}'::json)::jsonb || EXCLUDED.metadata::jsonb)::json,updated_at=EXCLUDED.updated_at
			`, teamID, projectorNullString(parentID), organization.Name, slug, path, source, organization.ExternalID, string(metadata), now, now).Error; err != nil {
			return err
		}
	}
	if len(seen) == 0 {
		return tx.Exec(`UPDATE teams SET metadata=(COALESCE(metadata,'{}'::json)::jsonb || '{"directoryStatus":"archived"}'::jsonb)::json,updated_at=? WHERE source=?`, now, source).Error
	}
	return tx.Exec(`UPDATE teams SET metadata=(COALESCE(metadata,'{}'::json)::jsonb || '{"directoryStatus":"archived"}'::jsonb)::json,updated_at=? WHERE source=? AND external_id NOT IN ?`, now, source, seen).Error

}

func (p *DatabaseProjector) ApplyPeople(ctx context.Context, connection domain.Connection, policy domain.Policy, people []domain.Person, memberships []domain.Membership, dryRun bool) error {
	if !policy.SyncPeople {
		return domain.ErrPeopleSyncDisabled
	}
	if dryRun {
		return nil
	}
	return p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return p.applyPeople(tx, connection, policy, people, memberships)
	})
}

func (p *DatabaseProjector) applyPeople(tx *gorm.DB, connection domain.Connection, policy domain.Policy, people []domain.Person, memberships []domain.Membership) error {
	providerID := connection.LoginProviderID
	if providerID == "" {
		providerID = connection.ID
	}
	source := directorySource(connection)
	personUsers := make(map[string]string, len(people))
	for i := range people {
		person := &people[i]
		subject := strings.TrimSpace(person.ProviderSubject)
		if subject == "" {
			subject = strings.TrimSpace(person.ExternalID)
		}
		if person.Status != domain.ProjectionActive {
			if err := p.deactivatePerson(tx, connection, policy, *person, subject, source, providerID); err != nil {
				return err
			}
			continue
		}
		userID, suppressed, err := p.resolvePerson(tx, connection, policy, *person, subject)
		if err != nil {
			return err
		}
		if suppressed || userID == "" {
			reason := "review_required"
			if suppressed {
				reason = "identity_link_suppressed"
			}
			if err := tx.Exec(`INSERT INTO directory_conflicts (id,connection_id,object_type,external_id,reason,status,created_at) VALUES (?,?,?,?,?,'open',?) ON CONFLICT DO NOTHING`, uuid.NewString(), connection.ID, "person", person.ExternalID, reason, p.now().UTC()).Error; err != nil {
				return err
			}
			continue
		}
		person.LocalUserID = userID
		personUsers[person.ExternalID] = userID
		profile, _ := json.Marshal(map[string]any{"displayName": person.DisplayName, "email": person.Email, "phone": person.Phone, "avatarUrl": person.AvatarURL, "directoryConnectionId": connection.ID})
		if err := tx.Exec(`INSERT INTO user_identities (id,user_id,provider_type,provider_id,provider_user_id,profile,created_at,updated_at)
				VALUES (?,?,?,?,?,?,?,?) ON CONFLICT(provider_type,provider_id,provider_user_id) DO UPDATE SET user_id=EXCLUDED.user_id,profile=EXCLUDED.profile,updated_at=EXCLUDED.updated_at`,
			uuid.NewString(), userID, connection.ProviderType, providerID, subject, string(profile), p.now().UTC(), p.now().UTC()).Error; err != nil {
			return err
		}
	}
	teamsByPerson := make(map[string][]string)
	for i := range memberships {
		membership := &memberships[i]
		userID := personUsers[membership.ExternalPersonID]
		if userID == "" {
			continue
		}
		teamID := stableID("team", connection.ID, membership.ExternalOrganizationID)
		membership.LocalUserID, membership.LocalTeamID = userID, teamID
		teamsByPerson[userID] = append(teamsByPerson[userID], teamID)
	}
	for _, userID := range personUsers {
		if err := tx.Exec(`DELETE FROM user_team_bindings WHERE user_id=? AND source=? AND COALESCE(provider_id,'')=?`, userID, source, providerID).Error; err != nil {
			return err
		}
		for _, teamID := range uniqueStrings(teamsByPerson[userID]) {
			if err := tx.Exec(`INSERT INTO user_team_bindings (id,user_id,team_id,source,provider_id,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`, uuid.NewString(), userID, teamID, source, providerID, p.now().UTC(), p.now().UTC()).Error; err != nil {
				return err
			}
		}
	}
	return nil

}

func (p *DatabaseProjector) deactivatePerson(tx *gorm.DB, connection domain.Connection, policy domain.Policy, person domain.Person, subject, source, providerID string) error {
	userID := strings.TrimSpace(person.LocalUserID)
	if userID == "" {
		err := tx.Raw(`SELECT user_id FROM user_identities WHERE provider_type=? AND provider_id=? AND provider_user_id=? LIMIT 1`, connection.ProviderType, providerID, subject).Row().Scan(&userID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
	}
	if err := tx.Exec(`DELETE FROM user_team_bindings WHERE user_id=? AND source=? AND COALESCE(provider_id,'')=?`, userID, source, providerID).Error; err != nil {
		return err
	}
	if policy.UserDisablePolicy != domain.DisableManagedOnly {
		return nil
	}
	var managed bool
	if err := tx.Raw(`SELECT COALESCE(preferences->>'directoryManagedBy','')=? FROM users WHERE id=?`, connection.ID, userID).Row().Scan(&managed); err != nil || !managed {
		return err
	}
	now := p.now().UTC()
	if person.Status == domain.ProjectionSuspended {
		lastEventAt := person.LastSeenAt
		if lastEventAt.IsZero() {
			lastEventAt = now
		}
		preferences, _ := json.Marshal(map[string]any{"directoryStatus": person.Status, "employmentStatus": "suspended", "departedAt": nil, "lastDirectoryEventAt": lastEventAt})
		return tx.Exec(`UPDATE users SET status='disabled',preferences=COALESCE(preferences,'{}'::jsonb) || ?::jsonb,updated_at=? WHERE id=?`, string(preferences), now, userID).Error
	}
	departedAt := now
	if person.DepartedAt != nil {
		departedAt = person.DepartedAt.UTC()
	}
	preferences, _ := json.Marshal(map[string]any{"directoryStatus": person.Status, "employmentStatus": "departed", "departedAt": departedAt, "lastDirectoryEventAt": departedAt})
	if err := tx.Exec(`UPDATE users SET status='disabled',preferences=COALESCE(preferences,'{}'::jsonb) || ?::jsonb,updated_at=? WHERE id=?`, string(preferences), now, userID).Error; err != nil {
		return err
	}
	if err := tx.Exec(`UPDATE sessions SET status='revoked',updated_at=? WHERE user_id=? AND status='active'`, now, userID).Error; err != nil {
		return err
	}
	if err := tx.Exec(`UPDATE identity_oidc_refresh_tokens SET revoked_at=COALESCE(revoked_at,?) WHERE session_id IN (SELECT id FROM identity_oidc_sessions WHERE user_id=?)`, now, userID).Error; err != nil {
		return err
	}
	if err := tx.Exec(`UPDATE identity_oidc_sessions SET revoked_at=COALESCE(revoked_at,?) WHERE user_id=?`, now, userID).Error; err != nil {
		return err
	}
	return tx.Exec(`UPDATE personal_access_tokens SET revoked_at=COALESCE(revoked_at,?),updated_at=? WHERE user_id=?`, now, now, userID).Error
}

func applyDeltaProjection(tx *gorm.DB, connectionID string, delta domain.Delta, now time.Time) error {
	if delta.Organization != nil {
		item := prepareOrganizationProjection(*delta.Organization, connectionID, now)
		if delta.Action == domain.ChangeArchive {
			if err := tx.Exec(`UPDATE directory_organizations SET status=?,archived_at=?,last_seen_at=? WHERE connection_id=? AND external_id=?`, domain.ProjectionArchived, item.ArchivedAt, item.LastSeenAt, connectionID, item.ExternalID).Error; err != nil {
				return err
			}
		} else if err := tx.Exec(`INSERT INTO directory_organizations (id,connection_id,external_id,external_parent_id,local_team_id,name,path,status,source_version,raw_hash,first_seen_at,last_seen_at,archived_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(connection_id,external_id) DO UPDATE SET external_parent_id=EXCLUDED.external_parent_id,name=EXCLUDED.name,status=EXCLUDED.status,source_version=EXCLUDED.source_version,last_seen_at=EXCLUDED.last_seen_at,archived_at=EXCLUDED.archived_at`, item.ID, item.ConnectionID, item.ExternalID, nullString(item.ExternalParentID), nullString(item.LocalTeamID), item.Name, item.Path, item.Status, item.SourceVersion, item.RawHash, item.FirstSeenAt, item.LastSeenAt, item.ArchivedAt).Error; err != nil {
			return err
		}
	}
	if delta.Person == nil {
		return nil
	}
	item := preparePersonProjection(*delta.Person, connectionID, now)
	if delta.Action == domain.ChangeArchive {
		if err := tx.Exec(`UPDATE directory_people SET status=?,archived_at=?,departed_at=?,last_seen_at=? WHERE connection_id=? AND external_id=?`, domain.ProjectionArchived, item.ArchivedAt, item.DepartedAt, item.LastSeenAt, connectionID, item.ExternalID).Error; err != nil {
			return err
		}
	} else if err := tx.Exec(`INSERT INTO directory_people (id,connection_id,external_id,provider_subject,local_user_id,username,display_name,email,email_verified,phone,avatar_url,status,source_version,raw_hash,first_seen_at,last_seen_at,archived_at,departed_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(connection_id,external_id) DO UPDATE SET provider_subject=EXCLUDED.provider_subject,local_user_id=COALESCE(EXCLUDED.local_user_id,directory_people.local_user_id),username=EXCLUDED.username,display_name=EXCLUDED.display_name,email=EXCLUDED.email,email_verified=EXCLUDED.email_verified,phone=EXCLUDED.phone,avatar_url=EXCLUDED.avatar_url,status=EXCLUDED.status,source_version=EXCLUDED.source_version,last_seen_at=EXCLUDED.last_seen_at,archived_at=EXCLUDED.archived_at,departed_at=EXCLUDED.departed_at`, item.ID, item.ConnectionID, item.ExternalID, item.ProviderSubject, nullString(item.LocalUserID), item.Username, item.DisplayName, item.Email, item.EmailVerified, item.Phone, item.AvatarURL, item.Status, item.SourceVersion, item.RawHash, item.FirstSeenAt, item.LastSeenAt, item.ArchivedAt, item.DepartedAt).Error; err != nil {
		return err
	}
	if err := tx.Exec(`DELETE FROM directory_memberships WHERE connection_id=? AND external_person_id=?`, connectionID, item.ExternalID).Error; err != nil {
		return err
	}
	for _, membership := range delta.Memberships {
		if membership.LastSeenAt.IsZero() {
			membership.LastSeenAt = now
		}
		if err := tx.Exec(`INSERT INTO directory_memberships (connection_id,external_person_id,external_organization_id,local_user_id,local_team_id,status,last_seen_at) VALUES (?,?,?,?,?,?,?)`, connectionID, membership.ExternalPersonID, membership.ExternalOrganizationID, nullString(membership.LocalUserID), nullString(membership.LocalTeamID), membership.Status, membership.LastSeenAt).Error; err != nil {
			return err
		}
	}
	return nil
}

func (p *DatabaseProjector) resolvePerson(tx *gorm.DB, connection domain.Connection, policy domain.Policy, person domain.Person, subject string) (string, bool, error) {
	providerID := connection.LoginProviderID
	if providerID == "" {
		providerID = connection.ID
	}
	var suppressedUserID string
	err := tx.Raw(`SELECT user_id FROM identity_link_suppressions WHERE provider_type=? AND provider_id=? AND provider_user_id=? AND cleared_at IS NULL LIMIT 1`, connection.ProviderType, providerID, subject).Row().Scan(&suppressedUserID)
	if err == nil {
		return "", true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}
	var userID string
	err = tx.Raw(`SELECT user_id FROM user_identities WHERE provider_type=? AND provider_id=? AND provider_user_id=? LIMIT 1`, connection.ProviderType, providerID, subject).Row().Scan(&userID)
	if err == nil {
		return userID, false, p.updateManagedProfile(tx, connection.ID, userID, person)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false, err
	}
	if person.EmailVerified && policy.VerifiedEmailAutoLink && trustedEmail(person.Email, policy.TrustedEmailDomains) {
		rows, queryErr := tx.Raw(`SELECT id FROM users WHERE lower(email)=lower(?) ORDER BY id LIMIT 2`, person.Email).Rows()
		if queryErr != nil {
			return "", false, queryErr
		}
		defer rows.Close()
		matches := []string{}
		for rows.Next() {
			if scanErr := rows.Scan(&userID); scanErr != nil {
				return "", false, scanErr
			}
			matches = append(matches, userID)
		}
		if err := rows.Err(); err != nil {
			return "", false, err
		}
		if len(matches) == 1 {
			return matches[0], false, p.updateManagedProfile(tx, connection.ID, matches[0], person)
		}
		if len(matches) > 1 {
			return "", false, nil
		}
	}
	if policy.ProvisionMode != domain.ProvisionCreateAndLink {
		return "", false, nil
	}
	userID = uuid.NewString()
	username := strings.TrimSpace(person.Username)
	if username == "" {
		username = subject
	}
	lastEventAt := person.LastSeenAt
	if lastEventAt.IsZero() {
		lastEventAt = p.now().UTC()
	}
	preferences, _ := json.Marshal(map[string]any{"avatarUrl": person.AvatarURL, "phone": person.Phone, "directoryManagedBy": connection.ID, "directoryStatus": domain.ProjectionActive, "employmentStatus": "active", "lastDirectoryEventAt": lastEventAt})
	if err := tx.Exec(`INSERT INTO users (id,username,email,display_name,status,tags,preferences,created_at,updated_at) VALUES (?,?,?,?,?,'[]',?::json,?,?)`, userID, username, strings.ToLower(strings.TrimSpace(person.Email)), person.DisplayName, "active", string(preferences), p.now().UTC(), p.now().UTC()).Error; err != nil {
		return "", false, fmt.Errorf("create directory user: %w", err)
	}
	return userID, false, nil
}

func (p *DatabaseProjector) updateManagedProfile(tx *gorm.DB, connectionID, userID string, person domain.Person) error {
	lastEventAt := person.LastSeenAt
	if lastEventAt.IsZero() {
		lastEventAt = p.now().UTC()
	}
	preferences, _ := json.Marshal(map[string]any{"avatarUrl": person.AvatarURL, "phone": person.Phone, "directoryStatus": domain.ProjectionActive, "employmentStatus": "active", "departedAt": nil, "lastDirectoryEventAt": lastEventAt})
	return tx.Exec(`UPDATE users SET display_name=CASE WHEN ?<>'' THEN ? ELSE display_name END,email=CASE WHEN ?<>'' THEN lower(?) ELSE email END,status=CASE WHEN COALESCE(preferences->>'directoryManagedBy','')=? THEN 'active' ELSE status END,preferences=COALESCE(preferences,'{}'::jsonb) || ?::jsonb,updated_at=? WHERE id=?`, person.DisplayName, person.DisplayName, person.Email, person.Email, connectionID, string(preferences), p.now().UTC(), userID).Error
}

func directorySource(connection domain.Connection) string { return "directory:" + connection.ID }

func stableID(kind, connectionID, externalID string) string {
	h := sha256.Sum256([]byte(kind + "\x00" + connectionID + "\x00" + externalID))
	return kind + "-" + hex.EncodeToString(h[:16])
}

func shortHash(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:6])
}

func trustedEmail(email string, domains []string) bool {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(email)), "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	for _, domain := range domains {
		if strings.EqualFold(strings.TrimSpace(domain), parts[1]) {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok || value == "" {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func projectorNullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

var _ appdirectorysync.LocalProjector = (*DatabaseProjector)(nil)
