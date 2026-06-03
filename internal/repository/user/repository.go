package user

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	domainaccess "github.com/soha/soha/internal/domain/access"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("user not found")

type User struct {
	ID          string
	Username    string
	Email       string
	DisplayName string
	Status      string
	Tags        []string
	Preferences map[string]any
}

type Session struct {
	ID             string
	UserID         string
	RefreshTokenID string
	ProviderType   string
	Status         string
	ExpiresAt      time.Time
	LastSeenAt     time.Time
	Metadata       map[string]any
}

type EphemeralToken struct {
	Token     string
	Kind      string
	Payload   map[string]any
	ExpiresAt time.Time
	CreatedAt time.Time
}

type OIDCIdentity struct {
	ID             string
	UserID         string
	ProviderType   string
	ProviderID     string
	ProviderUserID string
	Profile        map[string]any
	LastLoginAt    time.Time
}

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) FindByLogin(ctx context.Context, login string) (User, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, username, email, display_name, status, tags, preferences
		FROM users
		WHERE username = ? OR email = ?
		LIMIT 1
	`, login, strings.ToLower(login)).Row()
	return scanUser(row)
}

func (r *Repository) FindByEmail(ctx context.Context, email string) (User, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, username, email, display_name, status, tags, preferences
		FROM users
		WHERE email = ?
		LIMIT 1
	`, strings.ToLower(email)).Row()
	return scanUser(row)
}

func (r *Repository) GetByID(ctx context.Context, userID string) (User, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, username, email, display_name, status, tags, preferences
		FROM users
		WHERE id = ?
		LIMIT 1
	`, userID).Row()
	return scanUser(row)
}

func (r *Repository) UpsertUser(ctx context.Context, user User) error {
	tags, err := json.Marshal(user.Tags)
	if err != nil {
		return fmt.Errorf("marshal user tags: %w", err)
	}
	preferences, err := json.Marshal(user.Preferences)
	if err != nil {
		return fmt.Errorf("marshal user preferences: %w", err)
	}
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO users (id, username, email, display_name, status, tags, preferences, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			username = EXCLUDED.username,
			email = EXCLUDED.email,
			display_name = EXCLUDED.display_name,
			status = EXCLUDED.status,
			tags = EXCLUDED.tags,
			preferences = EXCLUDED.preferences,
			updated_at = EXCLUDED.updated_at
	`, user.ID, user.Username, strings.ToLower(user.Email), user.DisplayName, user.Status, string(tags), string(preferences), now, now).Error
}

func (r *Repository) SetPasswordHash(ctx context.Context, userID, passwordHash string) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO user_password_credentials (user_id, password_hash, password_updated_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET
			password_hash = EXCLUDED.password_hash,
			password_updated_at = EXCLUDED.password_updated_at,
			updated_at = EXCLUDED.updated_at
	`, userID, passwordHash, now, now, now).Error
}

func (r *Repository) GetPasswordHash(ctx context.Context, userID string) (string, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT password_hash
		FROM user_password_credentials
		WHERE user_id = ?
		LIMIT 1
	`, userID).Row()
	var passwordHash string
	if err := row.Scan(&passwordHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return passwordHash, nil
}

func (r *Repository) ListRoles(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT role_id
		FROM user_role_bindings
		WHERE user_id = ?
		ORDER BY role_id ASC
	`, userID).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := make([]string, 0)
	for rows.Next() {
		var roleID string
		if err := rows.Scan(&roleID); err != nil {
			return nil, err
		}
		roles = append(roles, roleID)
	}
	return roles, rows.Err()
}

func (r *Repository) ReplaceRoleBindings(ctx context.Context, userID string, roleIDs []string) error {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	if err := tx.Exec(`DELETE FROM user_role_bindings WHERE user_id = ?`, userID).Error; err != nil {
		tx.Rollback()
		return err
	}
	if len(roleIDs) > 0 {
		now := time.Now().UTC()
		if err := insertUserRoleBindings(tx, userID, roleIDs, now); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit().Error
}

func (r *Repository) ReplaceTeamBindings(ctx context.Context, userID string, teamIDs []string) error {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	if err := tx.Exec(`DELETE FROM user_team_bindings WHERE user_id = ?`, userID).Error; err != nil {
		tx.Rollback()
		return err
	}
	if len(teamIDs) > 0 {
		now := time.Now().UTC()
		if err := insertUserTeamBindings(tx, userID, teamIDs, now); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit().Error
}

func (r *Repository) ListTeams(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT team_id
		FROM user_team_bindings
		WHERE user_id = ?
		ORDER BY team_id ASC
	`, userID).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	teams := make([]string, 0)
	for rows.Next() {
		var teamID string
		if err := rows.Scan(&teamID); err != nil {
			return nil, err
		}
		teams = append(teams, teamID)
	}
	return teams, rows.Err()
}

func (r *Repository) ListProjects(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT project_id
		FROM user_project_bindings
		WHERE user_id = ?
		ORDER BY project_id ASC
	`, userID).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := make([]string, 0)
	for rows.Next() {
		var projectID string
		if err := rows.Scan(&projectID); err != nil {
			return nil, err
		}
		projects = append(projects, projectID)
	}
	return projects, rows.Err()
}

func (r *Repository) FindIdentity(ctx context.Context, providerType, providerID, providerUserID string) (OIDCIdentity, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, user_id, provider_type, provider_id, provider_user_id, profile, last_login_at
		FROM user_identities
		WHERE provider_type = ? AND provider_id = ? AND provider_user_id = ?
		LIMIT 1
	`, providerType, providerID, providerUserID).Row()
	return scanIdentity(row)
}

func (r *Repository) ListIdentitiesByUserID(ctx context.Context, userID string) ([]OIDCIdentity, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, user_id, provider_type, provider_id, provider_user_id, profile, last_login_at
		FROM user_identities
		WHERE user_id = ?
		ORDER BY provider_type ASC, provider_id ASC, provider_user_id ASC
	`, userID).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]OIDCIdentity, 0)
	for rows.Next() {
		item, err := scanIdentityRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) UpsertOIDCIdentity(ctx context.Context, identity OIDCIdentity) error {
	profile, err := json.Marshal(identity.Profile)
	if err != nil {
		return fmt.Errorf("marshal oidc profile: %w", err)
	}
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO user_identities (id, user_id, provider_type, provider_id, provider_user_id, profile, last_login_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (provider_type, provider_id, provider_user_id) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			profile = EXCLUDED.profile,
			last_login_at = EXCLUDED.last_login_at,
			updated_at = EXCLUDED.updated_at
	`, identity.ID, identity.UserID, identity.ProviderType, identity.ProviderID, identity.ProviderUserID, string(profile), identity.LastLoginAt, now, now).Error
}

func (r *Repository) CreateSession(ctx context.Context, session Session) error {
	metadata, err := json.Marshal(session.Metadata)
	if err != nil {
		return fmt.Errorf("marshal session metadata: %w", err)
	}
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO sessions (id, user_id, refresh_token_id, provider_type, status, expires_at, last_seen_at, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, session.ID, session.UserID, session.RefreshTokenID, session.ProviderType, session.Status, session.ExpiresAt, session.LastSeenAt, string(metadata), now, now).Error
}

func (r *Repository) GetSessionByRefreshID(ctx context.Context, refreshID string) (Session, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, user_id, refresh_token_id, provider_type, status, expires_at, last_seen_at, metadata
		FROM sessions
		WHERE refresh_token_id = ?
		LIMIT 1
	`, refreshID).Row()
	return scanSession(row)
}

func (r *Repository) GetAuthSessionByID(ctx context.Context, sessionID string) (Session, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, user_id, refresh_token_id, provider_type, status, expires_at, last_seen_at, metadata
		FROM sessions
		WHERE id = ?
		LIMIT 1
	`, sessionID).Row()
	return scanSession(row)
}

func (r *Repository) TouchSession(ctx context.Context, refreshID string, lastSeenAt time.Time) error {
	return r.db.WithContext(ctx).Exec(`
		UPDATE sessions
		SET last_seen_at = ?, updated_at = ?
		WHERE refresh_token_id = ?
	`, lastSeenAt, time.Now().UTC(), refreshID).Error
}

func (r *Repository) RevokeSession(ctx context.Context, refreshID string) error {
	return r.db.WithContext(ctx).Exec(`
		UPDATE sessions
		SET status = 'revoked', updated_at = ?
		WHERE refresh_token_id = ?
	`, time.Now().UTC(), refreshID).Error
}

func (r *Repository) GetSessionByID(ctx context.Context, sessionID string) (domainidentity.SessionRecord, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT
			s.id,
			s.user_id,
			u.display_name,
			u.email,
			s.provider_type,
			s.status,
			s.expires_at,
			s.last_seen_at,
			s.created_at,
			s.refresh_token_id,
			s.metadata
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.id = ?
		LIMIT 1
	`, sessionID).Row()
	return scanSessionRecord(row)
}

func (r *Repository) ListSessionRecords(ctx context.Context, limit int) ([]domainidentity.SessionRecord, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT
			s.id,
			s.user_id,
			u.display_name,
			u.email,
			s.provider_type,
			s.status,
			s.expires_at,
			s.last_seen_at,
			s.created_at,
			s.refresh_token_id,
			s.metadata
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		ORDER BY CASE WHEN s.last_seen_at IS NULL THEN 1 ELSE 0 END ASC, s.last_seen_at DESC, s.created_at DESC
		LIMIT ?
	`, limit).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domainidentity.SessionRecord, 0, limit)
	for rows.Next() {
		item, err := scanSessionRecordRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListSessionRecordsByUserID(ctx context.Context, userID string, limit int) ([]domainidentity.SessionRecord, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT
			s.id,
			s.user_id,
			u.display_name,
			u.email,
			s.provider_type,
			s.status,
			s.expires_at,
			s.last_seen_at,
			s.created_at,
			s.refresh_token_id,
			s.metadata
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.user_id = ?
		ORDER BY CASE WHEN s.last_seen_at IS NULL THEN 1 ELSE 0 END ASC, s.last_seen_at DESC, s.created_at DESC
		LIMIT ?
	`, userID, limit).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domainidentity.SessionRecord, 0, limit)
	for rows.Next() {
		item, err := scanSessionRecordRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) RevokeSessionByID(ctx context.Context, sessionID string) error {
	result := r.db.WithContext(ctx).Exec(`
		UPDATE sessions
		SET status = 'revoked', updated_at = ?
		WHERE id = ?
	`, time.Now().UTC(), sessionID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) RevokeSessionsByUserID(ctx context.Context, userID string) error {
	return r.db.WithContext(ctx).Exec(`
		UPDATE sessions
		SET status = 'revoked', updated_at = ?
		WHERE user_id = ? AND status = 'active'
	`, time.Now().UTC(), userID).Error
}

func (r *Repository) CreateEphemeralToken(ctx context.Context, token EphemeralToken) error {
	payload, err := json.Marshal(token.Payload)
	if err != nil {
		return fmt.Errorf("marshal ephemeral token payload: %w", err)
	}
	createdAt := token.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO auth_ephemeral_tokens (token, kind, payload, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, token.Token, token.Kind, string(payload), token.ExpiresAt, createdAt).Error
}

func (r *Repository) ConsumeEphemeralToken(ctx context.Context, tokenID, kind string) (EphemeralToken, error) {
	row := r.db.WithContext(ctx).Raw(`
		DELETE FROM auth_ephemeral_tokens
		WHERE token = ? AND kind = ? AND expires_at > ?
		RETURNING token, kind, payload, expires_at, created_at
	`, tokenID, kind, time.Now().UTC()).Row()
	return scanEphemeralToken(row)
}

func (r *Repository) DeleteUser(ctx context.Context, userID string) error {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	for _, query := range []string{
		`DELETE FROM sessions WHERE user_id = ?`,
		`DELETE FROM user_identities WHERE user_id = ?`,
		`DELETE FROM user_password_credentials WHERE user_id = ?`,
		`DELETE FROM user_project_bindings WHERE user_id = ?`,
		`DELETE FROM user_team_bindings WHERE user_id = ?`,
		`DELETE FROM user_role_bindings WHERE user_id = ?`,
		`DELETE FROM user_preferences WHERE user_id = ?`,
	} {
		if err := tx.Exec(query, userID).Error; err != nil {
			tx.Rollback()
			return err
		}
	}
	result := tx.Exec(`DELETE FROM users WHERE id = ?`, userID)
	if result.Error != nil {
		tx.Rollback()
		return result.Error
	}
	if result.RowsAffected == 0 {
		tx.Rollback()
		return ErrNotFound
	}
	return tx.Commit().Error
}

func (r *Repository) ListUsers(ctx context.Context) ([]domainaccess.UserRecord, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT
			u.id,
			u.username,
			u.email,
			u.display_name,
			u.status,
			u.tags
		FROM users u
		ORDER BY u.username ASC, u.id ASC
	`).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domainaccess.UserRecord, 0)
	userIDs := make([]string, 0)
	for rows.Next() {
		var item domainaccess.UserRecord
		var tags []byte
		if err := rows.Scan(&item.ID, &item.Username, &item.Email, &item.DisplayName, &item.Status, &tags); err != nil {
			return nil, err
		}
		if len(tags) > 0 {
			_ = json.Unmarshal(tags, &item.Tags)
		}
		items = append(items, item)
		userIDs = append(userIDs, item.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	roleMap, err := r.loadStringBindings(ctx, "user_role_bindings", "user_id", "role_id", userIDs)
	if err != nil {
		return nil, err
	}
	teamMap, err := r.loadStringBindings(ctx, "user_team_bindings", "user_id", "team_id", userIDs)
	if err != nil {
		return nil, err
	}
	projectMap, err := r.loadStringBindings(ctx, "user_project_bindings", "user_id", "project_id", userIDs)
	if err != nil {
		return nil, err
	}
	identityLogins, err := r.loadLatestTimes(ctx, `
		SELECT user_id, MAX(last_login_at) AS latest_at
		FROM user_identities
		WHERE user_id IN ?
		GROUP BY user_id
	`, userIDs)
	if err != nil {
		return nil, err
	}
	sessionLogins, err := r.loadLatestTimes(ctx, `
		SELECT user_id, MAX(created_at) AS latest_at
		FROM sessions
		WHERE user_id IN ?
		GROUP BY user_id
	`, userIDs)
	if err != nil {
		return nil, err
	}

	for index := range items {
		item := &items[index]
		item.Roles = roleMap[item.ID]
		item.Teams = teamMap[item.ID]
		item.Projects = projectMap[item.ID]
		item.LastLoginAt = maxTimePointer(identityLogins[item.ID], sessionLogins[item.ID])
	}
	return items, nil
}

func (r *Repository) CreateUser(ctx context.Context, input domainaccess.UserInput) (domainaccess.UserRecord, error) {
	tags, err := json.Marshal(input.Tags)
	if err != nil {
		return domainaccess.UserRecord{}, fmt.Errorf("marshal user tags: %w", err)
	}
	preferences, err := json.Marshal(input.Preferences)
	if err != nil {
		return domainaccess.UserRecord{}, fmt.Errorf("marshal user preferences: %w", err)
	}
	now := time.Now().UTC()
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return domainaccess.UserRecord{}, tx.Error
	}
	if err := tx.Exec(`
		INSERT INTO users (id, username, email, display_name, status, tags, preferences, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, input.ID, input.Username, strings.ToLower(input.Email), input.DisplayName, input.Status, string(tags), string(preferences), now, now).Error; err != nil {
		tx.Rollback()
		return domainaccess.UserRecord{}, err
	}
	if strings.TrimSpace(input.Password) != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if err != nil {
			tx.Rollback()
			return domainaccess.UserRecord{}, fmt.Errorf("hash password: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO user_password_credentials (user_id, password_hash, password_updated_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (user_id) DO UPDATE SET
				password_hash = EXCLUDED.password_hash,
				password_updated_at = EXCLUDED.password_updated_at,
				updated_at = EXCLUDED.updated_at
		`, input.ID, string(hash), now, now, now).Error; err != nil {
			tx.Rollback()
			return domainaccess.UserRecord{}, err
		}
	}
	if input.RoleIDs != nil && len(input.RoleIDs) > 0 {
		if err := insertUserRoleBindings(tx, input.ID, input.RoleIDs, now); err != nil {
			tx.Rollback()
			return domainaccess.UserRecord{}, err
		}
	}
	if input.TeamIDs != nil && len(input.TeamIDs) > 0 {
		if err := insertUserTeamBindings(tx, input.ID, input.TeamIDs, now); err != nil {
			tx.Rollback()
			return domainaccess.UserRecord{}, err
		}
	}
	if err := tx.Commit().Error; err != nil {
		return domainaccess.UserRecord{}, err
	}
	return domainaccess.UserRecord{
		ID:          input.ID,
		Username:    input.Username,
		Email:       strings.ToLower(input.Email),
		DisplayName: input.DisplayName,
		Status:      input.Status,
		Tags:        append([]string(nil), input.Tags...),
		Roles:       append([]string(nil), input.RoleIDs...),
		Teams:       append([]string(nil), input.TeamIDs...),
		Projects:    []string{},
	}, nil
}

func (r *Repository) UpdateUser(ctx context.Context, userID string, input domainaccess.UserInput) (domainaccess.UserRecord, error) {
	tags, err := json.Marshal(input.Tags)
	if err != nil {
		return domainaccess.UserRecord{}, fmt.Errorf("marshal user tags: %w", err)
	}
	preferences, err := json.Marshal(input.Preferences)
	if err != nil {
		return domainaccess.UserRecord{}, fmt.Errorf("marshal user preferences: %w", err)
	}
	now := time.Now().UTC()
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return domainaccess.UserRecord{}, tx.Error
	}
	result := tx.Exec(`
		UPDATE users
		SET username = ?, email = ?, display_name = ?, status = ?, tags = ?, preferences = ?, updated_at = ?
		WHERE id = ?
	`, input.Username, strings.ToLower(input.Email), input.DisplayName, input.Status, string(tags), string(preferences), now, userID)
	if result.Error != nil {
		tx.Rollback()
		return domainaccess.UserRecord{}, result.Error
	}
	if result.RowsAffected == 0 {
		tx.Rollback()
		return domainaccess.UserRecord{}, ErrNotFound
	}
	if strings.TrimSpace(input.Password) != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if err != nil {
			tx.Rollback()
			return domainaccess.UserRecord{}, fmt.Errorf("hash password: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO user_password_credentials (user_id, password_hash, password_updated_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (user_id) DO UPDATE SET
				password_hash = EXCLUDED.password_hash,
				password_updated_at = EXCLUDED.password_updated_at,
				updated_at = EXCLUDED.updated_at
		`, userID, string(hash), now, now, now).Error; err != nil {
			tx.Rollback()
			return domainaccess.UserRecord{}, err
		}
	}
	if input.RoleIDs != nil {
		if err := replaceUserRoleBindingsTx(tx, userID, input.RoleIDs, now); err != nil {
			tx.Rollback()
			return domainaccess.UserRecord{}, err
		}
	}
	if input.TeamIDs != nil {
		if err := replaceUserTeamBindingsTx(tx, userID, input.TeamIDs, now); err != nil {
			tx.Rollback()
			return domainaccess.UserRecord{}, err
		}
	}
	if err := tx.Commit().Error; err != nil {
		return domainaccess.UserRecord{}, err
	}
	user, err := r.GetByID(ctx, userID)
	if err != nil {
		return domainaccess.UserRecord{}, err
	}
	roles, _ := r.ListRoles(ctx, userID)
	teams, _ := r.ListTeams(ctx, userID)
	projects, _ := r.ListProjects(ctx, userID)
	return domainaccess.UserRecord{
		ID:          user.ID,
		Username:    user.Username,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Status:      user.Status,
		Tags:        user.Tags,
		Roles:       roles,
		Teams:       teams,
		Projects:    projects,
	}, nil
}

func (r *Repository) ListTeamsDetailed(ctx context.Context) ([]domainaccess.TeamRecord, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT
			t.id,
			t.name,
			t.slug,
			t.metadata,
			COUNT(DISTINCT utb.user_id) AS user_count
		FROM teams t
		LEFT JOIN user_team_bindings utb ON utb.team_id = t.id
		GROUP BY t.id, t.name, t.slug, t.metadata
		ORDER BY t.name ASC, t.id ASC
	`).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domainaccess.TeamRecord, 0)
	for rows.Next() {
		var item domainaccess.TeamRecord
		var metadata []byte
		if err := rows.Scan(&item.ID, &item.Name, &item.Slug, &metadata, &item.UserCount); err != nil {
			return nil, err
		}
		if len(metadata) > 0 {
			_ = json.Unmarshal(metadata, &item.Metadata)
		}
		if item.Metadata == nil {
			item.Metadata = map[string]any{}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateTeam(ctx context.Context, input domainaccess.TeamInput) (domainaccess.TeamRecord, error) {
	metadata, err := json.Marshal(input.Metadata)
	if err != nil {
		return domainaccess.TeamRecord{}, fmt.Errorf("marshal team metadata: %w", err)
	}
	now := time.Now().UTC()
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO teams (id, name, slug, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, strings.TrimSpace(input.ID), strings.TrimSpace(input.Name), strings.TrimSpace(input.Slug), string(metadata), now, now).Error; err != nil {
		return domainaccess.TeamRecord{}, err
	}
	return domainaccess.TeamRecord{
		ID:        strings.TrimSpace(input.ID),
		Name:      strings.TrimSpace(input.Name),
		Slug:      strings.TrimSpace(input.Slug),
		Metadata:  input.Metadata,
		UserCount: 0,
	}, nil
}

func (r *Repository) UpdateTeam(ctx context.Context, teamID string, input domainaccess.TeamInput) (domainaccess.TeamRecord, error) {
	metadata, err := json.Marshal(input.Metadata)
	if err != nil {
		return domainaccess.TeamRecord{}, fmt.Errorf("marshal team metadata: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE teams
		SET name = ?, slug = ?, metadata = ?, updated_at = ?
		WHERE id = ?
	`, strings.TrimSpace(input.Name), strings.TrimSpace(input.Slug), string(metadata), time.Now().UTC(), strings.TrimSpace(teamID))
	if result.Error != nil {
		return domainaccess.TeamRecord{}, result.Error
	}
	if result.RowsAffected == 0 {
		return domainaccess.TeamRecord{}, gorm.ErrRecordNotFound
	}
	items, err := r.ListTeamsDetailed(ctx)
	if err != nil {
		return domainaccess.TeamRecord{}, err
	}
	for _, item := range items {
		if item.ID == strings.TrimSpace(teamID) {
			return item, nil
		}
	}
	return domainaccess.TeamRecord{}, gorm.ErrRecordNotFound
}

func (r *Repository) DeleteTeam(ctx context.Context, teamID string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM teams WHERE id = ?`, strings.TrimSpace(teamID))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func scanUser(row *sql.Row) (User, error) {
	var user User
	var tags []byte
	var preferences []byte
	if err := row.Scan(&user.ID, &user.Username, &user.Email, &user.DisplayName, &user.Status, &tags, &preferences); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	if len(tags) > 0 {
		_ = json.Unmarshal(tags, &user.Tags)
	}
	if len(preferences) > 0 {
		_ = json.Unmarshal(preferences, &user.Preferences)
	}
	return user, nil
}

func scanIdentity(row *sql.Row) (OIDCIdentity, error) {
	var identity OIDCIdentity
	var profile []byte
	var lastLoginAt sql.NullTime
	if err := row.Scan(&identity.ID, &identity.UserID, &identity.ProviderType, &identity.ProviderID, &identity.ProviderUserID, &profile, &lastLoginAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return OIDCIdentity{}, ErrNotFound
		}
		return OIDCIdentity{}, err
	}
	if len(profile) > 0 {
		_ = json.Unmarshal(profile, &identity.Profile)
	}
	if lastLoginAt.Valid {
		identity.LastLoginAt = lastLoginAt.Time
	}
	return identity, nil
}

func scanIdentityRows(rows *sql.Rows) (OIDCIdentity, error) {
	var identity OIDCIdentity
	var profile []byte
	var lastLoginAt sql.NullTime
	if err := rows.Scan(&identity.ID, &identity.UserID, &identity.ProviderType, &identity.ProviderID, &identity.ProviderUserID, &profile, &lastLoginAt); err != nil {
		return OIDCIdentity{}, err
	}
	if len(profile) > 0 {
		_ = json.Unmarshal(profile, &identity.Profile)
	}
	if lastLoginAt.Valid {
		identity.LastLoginAt = lastLoginAt.Time
	}
	return identity, nil
}

func scanSession(row *sql.Row) (Session, error) {
	var session Session
	var metadata []byte
	var lastSeenAt sql.NullTime
	if err := row.Scan(&session.ID, &session.UserID, &session.RefreshTokenID, &session.ProviderType, &session.Status, &session.ExpiresAt, &lastSeenAt, &metadata); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, ErrNotFound
		}
		return Session{}, err
	}
	if lastSeenAt.Valid {
		session.LastSeenAt = lastSeenAt.Time
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &session.Metadata)
	}
	return session, nil
}

func scanSessionRecord(row *sql.Row) (domainidentity.SessionRecord, error) {
	var item domainidentity.SessionRecord
	var metadata []byte
	var lastSeenAt sql.NullTime
	if err := row.Scan(&item.ID, &item.UserID, &item.UserName, &item.Email, &item.ProviderType, &item.Status, &item.ExpiresAt, &lastSeenAt, &item.CreatedAt, &item.RefreshTokenID, &metadata); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainidentity.SessionRecord{}, ErrNotFound
		}
		return domainidentity.SessionRecord{}, err
	}
	if lastSeenAt.Valid {
		item.LastSeenAt = lastSeenAt.Time
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &item.Metadata)
	}
	return item, nil
}

func scanSessionRecordRows(rows *sql.Rows) (domainidentity.SessionRecord, error) {
	var item domainidentity.SessionRecord
	var metadata []byte
	var lastSeenAt sql.NullTime
	if err := rows.Scan(&item.ID, &item.UserID, &item.UserName, &item.Email, &item.ProviderType, &item.Status, &item.ExpiresAt, &lastSeenAt, &item.CreatedAt, &item.RefreshTokenID, &metadata); err != nil {
		return domainidentity.SessionRecord{}, err
	}
	if lastSeenAt.Valid {
		item.LastSeenAt = lastSeenAt.Time
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &item.Metadata)
	}
	return item, nil
}

func scanEphemeralToken(row *sql.Row) (EphemeralToken, error) {
	var item EphemeralToken
	var payload []byte
	if err := row.Scan(&item.Token, &item.Kind, &payload, &item.ExpiresAt, &item.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return EphemeralToken{}, ErrNotFound
		}
		return EphemeralToken{}, err
	}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &item.Payload)
	}
	return item, nil
}

func (r *Repository) loadStringBindings(ctx context.Context, tableName, ownerColumn, valueColumn string, ownerIDs []string) (map[string][]string, error) {
	items := make(map[string][]string, len(ownerIDs))
	for _, ownerID := range ownerIDs {
		items[ownerID] = []string{}
	}
	if len(ownerIDs) == 0 {
		return items, nil
	}
	query := fmt.Sprintf(`
		SELECT %s, %s
		FROM %s
		WHERE %s IN ?
		ORDER BY %s ASC, %s ASC
	`, ownerColumn, valueColumn, tableName, ownerColumn, ownerColumn, valueColumn)
	rows, err := r.db.WithContext(ctx).Raw(query, ownerIDs).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ownerID string
		var value string
		if err := rows.Scan(&ownerID, &value); err != nil {
			return nil, err
		}
		items[ownerID] = append(items[ownerID], value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for ownerID := range items {
		items[ownerID] = compactSortedStrings(items[ownerID])
	}
	return items, nil
}

func (r *Repository) loadLatestTimes(ctx context.Context, query string, ownerIDs []string) (map[string]time.Time, error) {
	items := make(map[string]time.Time, len(ownerIDs))
	if len(ownerIDs) == 0 {
		return items, nil
	}
	rows, err := r.db.WithContext(ctx).Raw(query, ownerIDs).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ownerID string
		var latestAt sql.NullTime
		if err := rows.Scan(&ownerID, &latestAt); err != nil {
			return nil, err
		}
		if latestAt.Valid {
			items[ownerID] = latestAt.Time
		}
	}
	return items, rows.Err()
}

func insertUserRoleBindings(tx *gorm.DB, userID string, roleIDs []string, now time.Time) error {
	var builder strings.Builder
	args := make([]any, 0, len(roleIDs)*6)
	builder.WriteString(`
		INSERT INTO user_role_bindings (id, user_id, role_id, scope, created_at, updated_at)
		VALUES
	`)
	for i, roleID := range roleIDs {
		if i > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(" (?, ?, ?, '{}', ?, ?)")
		args = append(args, fmt.Sprintf("%s:%s", userID, roleID), userID, roleID, now, now)
	}
	return tx.Exec(builder.String(), args...).Error
}

func insertUserTeamBindings(tx *gorm.DB, userID string, teamIDs []string, now time.Time) error {
	var builder strings.Builder
	args := make([]any, 0, len(teamIDs)*5)
	builder.WriteString(`
		INSERT INTO user_team_bindings (id, user_id, team_id, created_at, updated_at)
		VALUES
	`)
	for i, teamID := range teamIDs {
		if i > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(" (?, ?, ?, ?, ?)")
		args = append(args, fmt.Sprintf("%s:%s", userID, teamID), userID, teamID, now, now)
	}
	return tx.Exec(builder.String(), args...).Error
}

func replaceUserRoleBindingsTx(tx *gorm.DB, userID string, roleIDs []string, now time.Time) error {
	if err := tx.Exec(`DELETE FROM user_role_bindings WHERE user_id = ?`, userID).Error; err != nil {
		return err
	}
	if len(roleIDs) == 0 {
		return nil
	}
	return insertUserRoleBindings(tx, userID, roleIDs, now)
}

func replaceUserTeamBindingsTx(tx *gorm.DB, userID string, teamIDs []string, now time.Time) error {
	if err := tx.Exec(`DELETE FROM user_team_bindings WHERE user_id = ?`, userID).Error; err != nil {
		return err
	}
	if len(teamIDs) == 0 {
		return nil
	}
	return insertUserTeamBindings(tx, userID, teamIDs, now)
}

func compactSortedStrings(items []string) []string {
	if len(items) == 0 {
		return []string{}
	}
	sort.Strings(items)
	result := items[:0]
	var last string
	for index, item := range items {
		if index == 0 || item != last {
			result = append(result, item)
			last = item
		}
	}
	return append([]string(nil), result...)
}

func maxTimePointer(left, right time.Time) *time.Time {
	switch {
	case left.IsZero() && right.IsZero():
		return nil
	case right.After(left):
		value := right
		return &value
	default:
		value := left
		return &value
	}
}
