package identity

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	userrepo "github.com/opensoha/soha/internal/repository/user"
)

func TestReconcileExternalUserSyncsLoginRolesAndOrganizations(t *testing.T) {
	ctx := context.Background()
	repo := newLoginMappingUserRepo()
	repo.roleRefs["readonly"] = "readonly"
	repo.roleRefs["ops"] = "ops"
	repo.teamRefs["dept-open-1"] = "platform-org"
	service := &Service{users: repo}

	principal, err := service.reconcileExternalUser(ctx, domainsettings.LoginProviderSettings{
		ID:                "feishu-main",
		Type:              "feishu",
		DefaultRoles:      []string{"readonly"},
		SyncRolesOnLogin:  true,
		RoleField:         "role_ids",
		SyncOrgsOnLogin:   true,
		OrganizationField: "department_ids",
		RoleSyncMode:      "append",
		OrgSyncMode:       "append",
	}, genericProfile{
		ID:    "ou_1",
		Email: "user@example.com",
		Name:  "User",
		Raw: map[string]any{
			"role_ids":       []any{"ops"},
			"department_ids": []any{"dept-open-1"},
		},
	})
	if err != nil {
		t.Fatalf("reconcile external user: %v", err)
	}

	if !hasString(principal.Roles, "readonly") || !hasString(principal.Roles, "ops") {
		t.Fatalf("expected synced roles, got %#v", principal.Roles)
	}
	if !hasString(principal.Teams, "platform-org") {
		t.Fatalf("expected synced organization, got %#v", principal.Teams)
	}
	if binding := repo.roleBindings[principal.UserID]["ops"]; binding.source != "feishu" || binding.providerID != "feishu-main" {
		t.Fatalf("expected provider-managed role binding, got %#v", binding)
	}
	if binding := repo.teamBindings[principal.UserID]["platform-org"]; binding.source != "feishu" || binding.providerID != "feishu-main" {
		t.Fatalf("expected provider-managed team binding, got %#v", binding)
	}
}

func TestSyncLoginProviderBindingsReplaceExternalOnly(t *testing.T) {
	ctx := context.Background()
	repo := newLoginMappingUserRepo()
	repo.roleRefs["ops"] = "ops"
	repo.teamRefs["dept-open-1"] = "platform-org"
	repo.roleBindings["u1"] = map[string]loginMappingBinding{
		"local-role": {source: "local"},
		"stale-role": {source: "feishu", providerID: "feishu-main"},
	}
	repo.teamBindings["u1"] = map[string]loginMappingBinding{
		"local-team": {source: "local"},
		"stale-team": {source: "feishu", providerID: "feishu-main"},
	}
	service := &Service{users: repo}

	err := service.syncLoginProviderBindings(ctx, "u1", domainsettings.LoginProviderSettings{
		ID:                "feishu-main",
		Type:              "feishu",
		SyncRolesOnLogin:  true,
		RoleField:         "role_ids",
		RoleSyncMode:      "replace_external",
		SyncOrgsOnLogin:   true,
		OrganizationField: "department_ids",
		OrgSyncMode:       "replace_external",
	}, map[string]any{
		"role_ids":       []any{"ops"},
		"department_ids": []any{"dept-open-1"},
	})
	if err != nil {
		t.Fatalf("sync login provider bindings: %v", err)
	}

	if !repo.hasRoleBinding("u1", "local-role") || !repo.hasTeamBinding("u1", "local-team") {
		t.Fatalf("expected local bindings to remain, roles=%#v teams=%#v", repo.roleBindings["u1"], repo.teamBindings["u1"])
	}
	if repo.hasRoleBinding("u1", "stale-role") || repo.hasTeamBinding("u1", "stale-team") {
		t.Fatalf("expected stale external bindings removed, roles=%#v teams=%#v", repo.roleBindings["u1"], repo.teamBindings["u1"])
	}
	if !repo.hasRoleBinding("u1", "ops") || !repo.hasTeamBinding("u1", "platform-org") {
		t.Fatalf("expected current external bindings added, roles=%#v teams=%#v", repo.roleBindings["u1"], repo.teamBindings["u1"])
	}
}

func TestAccessTokenRequiresFreshAuthzVersion(t *testing.T) {
	ctx := context.Background()
	repo := newLoginMappingUserRepo()
	repo.usersByID["u1"] = userrepo.User{
		ID:           "u1",
		Username:     "user1",
		Email:        "user1@example.com",
		DisplayName:  "User One",
		Status:       "active",
		AuthzVersion: 1,
	}
	repo.roleBindings["u1"] = map[string]loginMappingBinding{"readonly": {source: "local"}}

	service := &Service{
		cfg: cfgpkg.AuthConfig{JWT: cfgpkg.JWTConfig{
			Secret:     "test-secret",
			Issuer:     "soha-test",
			AccessTTL:  time.Minute,
			RefreshTTL: time.Hour,
		}},
		users: repo,
	}
	result, err := service.issueAuthResult(ctx, domainidentity.Principal{
		UserID:   "u1",
		UserName: "User One",
		Email:    "user1@example.com",
		Roles:    []string{"readonly"},
	}, "password")
	if err != nil {
		t.Fatalf("issue auth result: %v", err)
	}

	user := repo.usersByID["u1"]
	user.AuthzVersion = 2
	repo.usersByID["u1"] = user

	if _, _, err := service.ParseAccessToken(ctx, result.Tokens.AccessToken); err == nil {
		t.Fatalf("ParseAccessToken error = nil, want stale authz rejection")
	}

	refreshed, err := service.RefreshSession(ctx, result.Tokens.RefreshToken)
	if err != nil {
		t.Fatalf("RefreshSession after authz bump returned error: %v", err)
	}
	principal, _, err := service.ParseAccessToken(ctx, refreshed.Tokens.AccessToken)
	if err != nil {
		t.Fatalf("ParseAccessToken for refreshed token returned error: %v", err)
	}
	if principal.UserID != "u1" || !hasString(principal.Roles, "readonly") {
		t.Fatalf("unexpected refreshed principal: %#v", principal)
	}
}

func TestStreamTicketIsSingleUseAndPathBound(t *testing.T) {
	ctx := context.Background()
	repo := newLoginMappingUserRepo()
	repo.usersByID["u1"] = userrepo.User{
		ID:           "u1",
		Username:     "user1",
		Email:        "user1@example.com",
		DisplayName:  "User One",
		Status:       "active",
		AuthzVersion: 1,
	}
	repo.roleBindings["u1"] = map[string]loginMappingBinding{"ops": {source: "local"}}
	repo.sessionsByID["s1"] = userrepo.Session{
		ID:           "s1",
		UserID:       "u1",
		Status:       "active",
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
		AuthzVersion: 1,
	}
	service := &Service{users: repo}

	path := "/api/v1/clusters/cluster-a/workloads/pods/pod-a/logs/stream"
	ticket, err := service.IssueStreamTicket(ctx, domainidentity.Principal{UserID: "u1"}, domainidentity.AccessContext{TokenKind: "session_access", SessionID: "s1"}, domainidentity.StreamTicketRequest{Path: path})
	if err != nil {
		t.Fatalf("IssueStreamTicket returned error: %v", err)
	}
	if ticket.Ticket == "" || ticket.ExpiresAt.Before(time.Now().UTC()) {
		t.Fatalf("unexpected stream ticket: %#v", ticket)
	}

	principal, accessCtx, err := service.ParseStreamTicket(ctx, ticket.Ticket, path)
	if err != nil {
		t.Fatalf("ParseStreamTicket returned error: %v", err)
	}
	if principal.UserID != "u1" || !hasString(principal.Roles, "ops") {
		t.Fatalf("unexpected stream principal: %#v", principal)
	}
	if accessCtx.TokenKind != "stream_ticket" || accessCtx.SessionID != "s1" {
		t.Fatalf("unexpected stream access context: %#v", accessCtx)
	}
	if _, _, err := service.ParseStreamTicket(ctx, ticket.Ticket, path); err == nil {
		t.Fatal("second ParseStreamTicket error = nil, want single-use rejection")
	}
}

func TestStreamTicketAllowsDockerRuntimeStreams(t *testing.T) {
	ctx := context.Background()
	repo := newLoginMappingUserRepo()
	repo.usersByID["u1"] = userrepo.User{
		ID:           "u1",
		Username:     "user1",
		Email:        "user1@example.com",
		DisplayName:  "User One",
		Status:       "active",
		AuthzVersion: 1,
	}
	repo.sessionsByID["s1"] = userrepo.Session{
		ID:           "s1",
		UserID:       "u1",
		Status:       "active",
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
		AuthzVersion: 1,
	}
	service := &Service{users: repo}

	for _, path := range []string{
		"/api/v1/docker/projects/project-1/runtime/logs/stream",
		"/api/v1/docker/projects/project-1/runtime/terminal",
	} {
		ticket, err := service.IssueStreamTicket(ctx, domainidentity.Principal{UserID: "u1"}, domainidentity.AccessContext{TokenKind: "session_access", SessionID: "s1"}, domainidentity.StreamTicketRequest{Path: path})
		if err != nil {
			t.Fatalf("IssueStreamTicket(%q) returned error: %v", path, err)
		}
		if _, accessCtx, err := service.ParseStreamTicket(ctx, ticket.Ticket, path); err != nil {
			t.Fatalf("ParseStreamTicket(%q) returned error: %v", path, err)
		} else if accessCtx.TokenKind != "stream_ticket" {
			t.Fatalf("unexpected stream access context: %#v", accessCtx)
		}
	}
}

func TestStreamTicketRejectsMismatchedPath(t *testing.T) {
	ctx := context.Background()
	repo := newLoginMappingUserRepo()
	repo.usersByID["u1"] = userrepo.User{
		ID:           "u1",
		Username:     "user1",
		Email:        "user1@example.com",
		DisplayName:  "User One",
		Status:       "active",
		AuthzVersion: 1,
	}
	repo.sessionsByID["s1"] = userrepo.Session{
		ID:           "s1",
		UserID:       "u1",
		Status:       "active",
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
		AuthzVersion: 1,
	}
	service := &Service{users: repo}

	ticket, err := service.IssueStreamTicket(ctx, domainidentity.Principal{UserID: "u1"}, domainidentity.AccessContext{TokenKind: "session_access", SessionID: "s1"}, domainidentity.StreamTicketRequest{Path: "/api/v1/virtualization/operations/task-1/stream"})
	if err != nil {
		t.Fatalf("IssueStreamTicket returned error: %v", err)
	}
	if _, _, err := service.ParseStreamTicket(ctx, ticket.Ticket, "/api/v1/virtualization/operations/task-2/stream"); err == nil {
		t.Fatal("ParseStreamTicket error = nil, want path mismatch rejection")
	}
}

type loginMappingBinding struct {
	source     string
	providerID string
}

type loginMappingUserRepo struct {
	usersByID    map[string]userrepo.User
	emailToID    map[string]string
	identities   map[string]userrepo.OIDCIdentity
	roleRefs     map[string]string
	teamRefs     map[string]string
	roleBindings map[string]map[string]loginMappingBinding
	teamBindings map[string]map[string]loginMappingBinding
	sessionsByID map[string]userrepo.Session
	refreshToID  map[string]string
	ephemeral    map[string]userrepo.EphemeralToken
}

func newLoginMappingUserRepo() *loginMappingUserRepo {
	return &loginMappingUserRepo{
		usersByID:    map[string]userrepo.User{},
		emailToID:    map[string]string{},
		identities:   map[string]userrepo.OIDCIdentity{},
		roleRefs:     map[string]string{},
		teamRefs:     map[string]string{},
		roleBindings: map[string]map[string]loginMappingBinding{},
		teamBindings: map[string]map[string]loginMappingBinding{},
		sessionsByID: map[string]userrepo.Session{},
		refreshToID:  map[string]string{},
		ephemeral:    map[string]userrepo.EphemeralToken{},
	}
}

func (r *loginMappingUserRepo) FindByLogin(context.Context, string) (userrepo.User, error) {
	return userrepo.User{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) FindByEmail(_ context.Context, email string) (userrepo.User, error) {
	if userID := r.emailToID[strings.ToLower(email)]; userID != "" {
		return r.usersByID[userID], nil
	}
	return userrepo.User{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) GetByID(_ context.Context, userID string) (userrepo.User, error) {
	if user, ok := r.usersByID[userID]; ok {
		if user.AuthzVersion < 1 {
			user.AuthzVersion = 1
		}
		return user, nil
	}
	return userrepo.User{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) GetAuthzState(_ context.Context, userID string) (userrepo.AuthzState, error) {
	if user, ok := r.usersByID[userID]; ok {
		authzVersion := user.AuthzVersion
		if authzVersion < 1 {
			authzVersion = 1
		}
		return userrepo.AuthzState{UserID: user.ID, Status: user.Status, AuthzVersion: authzVersion}, nil
	}
	return userrepo.AuthzState{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) UpsertUser(_ context.Context, user userrepo.User) error {
	if user.AuthzVersion < 1 {
		user.AuthzVersion = 1
	}
	r.usersByID[user.ID] = user
	r.emailToID[strings.ToLower(user.Email)] = user.ID
	return nil
}

func (r *loginMappingUserRepo) SetPasswordHash(context.Context, string, string) error {
	return nil
}

func (r *loginMappingUserRepo) GetPasswordHash(context.Context, string) (string, error) {
	return "", userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) ListRoles(_ context.Context, userID string) ([]string, error) {
	return sortedBindingIDs(r.roleBindings[userID]), nil
}

func (r *loginMappingUserRepo) ReplaceRoleBindings(_ context.Context, userID string, roleIDs []string) error {
	r.roleBindings[userID] = map[string]loginMappingBinding{}
	for _, roleID := range roleIDs {
		r.roleBindings[userID][roleID] = loginMappingBinding{source: "local"}
	}
	return nil
}

func (r *loginMappingUserRepo) ResolveRoleIDs(_ context.Context, refs []string) ([]string, error) {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if roleID := r.roleRefs[strings.TrimSpace(ref)]; roleID != "" {
			out = append(out, roleID)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (r *loginMappingUserRepo) SyncExternalRoleBindings(_ context.Context, userID, source, providerID string, roleIDs []string, replace bool) error {
	if r.roleBindings[userID] == nil {
		r.roleBindings[userID] = map[string]loginMappingBinding{}
	}
	if replace {
		for roleID, binding := range r.roleBindings[userID] {
			if binding.source == source && binding.providerID == providerID {
				delete(r.roleBindings[userID], roleID)
			}
		}
	}
	for _, roleID := range roleIDs {
		if _, exists := r.roleBindings[userID][roleID]; exists {
			continue
		}
		r.roleBindings[userID][roleID] = loginMappingBinding{source: source, providerID: providerID}
	}
	return nil
}

func (r *loginMappingUserRepo) ListTeams(_ context.Context, userID string) ([]string, error) {
	return sortedBindingIDs(r.teamBindings[userID]), nil
}

func (r *loginMappingUserRepo) ResolveTeamIDsForExternalRefs(_ context.Context, _, _ string, refs []string) ([]string, error) {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if teamID := r.teamRefs[strings.TrimSpace(ref)]; teamID != "" {
			out = append(out, teamID)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (r *loginMappingUserRepo) SyncExternalTeamBindings(_ context.Context, userID, source, providerID string, teamIDs []string, replace bool) error {
	if r.teamBindings[userID] == nil {
		r.teamBindings[userID] = map[string]loginMappingBinding{}
	}
	if replace {
		for teamID, binding := range r.teamBindings[userID] {
			if binding.source == source && binding.providerID == providerID {
				delete(r.teamBindings[userID], teamID)
			}
		}
	}
	for _, teamID := range teamIDs {
		if _, exists := r.teamBindings[userID][teamID]; exists {
			continue
		}
		r.teamBindings[userID][teamID] = loginMappingBinding{source: source, providerID: providerID}
	}
	return nil
}

func (r *loginMappingUserRepo) ListProjects(context.Context, string) ([]string, error) {
	return []string{}, nil
}

func (r *loginMappingUserRepo) FindIdentity(_ context.Context, providerType, providerID, providerUserID string) (userrepo.OIDCIdentity, error) {
	if identity, ok := r.identities[providerType+"|"+providerID+"|"+providerUserID]; ok {
		return identity, nil
	}
	return userrepo.OIDCIdentity{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) ListIdentitiesByUserID(context.Context, string) ([]userrepo.OIDCIdentity, error) {
	return []userrepo.OIDCIdentity{}, nil
}

func (r *loginMappingUserRepo) UpsertOIDCIdentity(_ context.Context, identity userrepo.OIDCIdentity) error {
	r.identities[identity.ProviderType+"|"+identity.ProviderID+"|"+identity.ProviderUserID] = identity
	return nil
}

func (r *loginMappingUserRepo) CreateSession(_ context.Context, session userrepo.Session) error {
	if session.AuthzVersion < 1 {
		session.AuthzVersion = 1
	}
	r.sessionsByID[session.ID] = session
	r.refreshToID[session.RefreshTokenID] = session.ID
	return nil
}

func (r *loginMappingUserRepo) GetSessionByRefreshID(_ context.Context, refreshID string) (userrepo.Session, error) {
	if sessionID := r.refreshToID[refreshID]; sessionID != "" {
		return r.sessionsByID[sessionID], nil
	}
	return userrepo.Session{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) GetAuthSessionByID(_ context.Context, sessionID string) (userrepo.Session, error) {
	if session, ok := r.sessionsByID[sessionID]; ok {
		return session, nil
	}
	return userrepo.Session{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) GetSessionByID(context.Context, string) (domainidentity.SessionRecord, error) {
	return domainidentity.SessionRecord{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) ListSessionRecords(context.Context, int) ([]domainidentity.SessionRecord, error) {
	return []domainidentity.SessionRecord{}, nil
}

func (r *loginMappingUserRepo) ListSessionRecordsByUserID(context.Context, string, int) ([]domainidentity.SessionRecord, error) {
	return []domainidentity.SessionRecord{}, nil
}

func (r *loginMappingUserRepo) RevokeSessionByID(context.Context, string) error {
	return nil
}

func (r *loginMappingUserRepo) TouchSession(_ context.Context, refreshID string, lastSeenAt time.Time, authzVersion int64) error {
	sessionID := r.refreshToID[refreshID]
	if sessionID == "" {
		return userrepo.ErrNotFound
	}
	session := r.sessionsByID[sessionID]
	session.LastSeenAt = lastSeenAt
	session.AuthzVersion = authzVersion
	r.sessionsByID[sessionID] = session
	return nil
}

func (r *loginMappingUserRepo) RevokeSession(_ context.Context, refreshID string) error {
	sessionID := r.refreshToID[refreshID]
	if sessionID != "" {
		session := r.sessionsByID[sessionID]
		session.Status = "revoked"
		r.sessionsByID[sessionID] = session
	}
	return nil
}

func (r *loginMappingUserRepo) CreateEphemeralToken(_ context.Context, token userrepo.EphemeralToken) error {
	if token.CreatedAt.IsZero() {
		token.CreatedAt = time.Now().UTC()
	}
	r.ephemeral[token.Kind+"|"+token.Token] = token
	return nil
}

func (r *loginMappingUserRepo) ConsumeEphemeralToken(_ context.Context, token, kind string) (userrepo.EphemeralToken, error) {
	key := kind + "|" + token
	item, ok := r.ephemeral[key]
	if !ok || item.ExpiresAt.Before(time.Now().UTC()) {
		return userrepo.EphemeralToken{}, userrepo.ErrNotFound
	}
	delete(r.ephemeral, key)
	return item, nil
}

func (r *loginMappingUserRepo) GetPersonalAccessTokenByHash(context.Context, string) (domainaigateway.PersonalAccessToken, error) {
	return domainaigateway.PersonalAccessToken{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) TouchPersonalAccessToken(context.Context, string, time.Time) error {
	return nil
}

func (r *loginMappingUserRepo) GetServiceAccountTokenByHash(context.Context, string) (domainaigateway.ServiceAccountToken, error) {
	return domainaigateway.ServiceAccountToken{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) TouchServiceAccountToken(context.Context, string, time.Time) error {
	return nil
}

func (r *loginMappingUserRepo) GetServiceAccount(context.Context, string) (domainaigateway.ServiceAccount, error) {
	return domainaigateway.ServiceAccount{}, userrepo.ErrNotFound
}

func (r *loginMappingUserRepo) hasRoleBinding(userID, roleID string) bool {
	_, ok := r.roleBindings[userID][roleID]
	return ok
}

func (r *loginMappingUserRepo) hasTeamBinding(userID, teamID string) bool {
	_, ok := r.teamBindings[userID][teamID]
	return ok
}

func sortedBindingIDs(items map[string]loginMappingBinding) []string {
	if len(items) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(items))
	for item := range items {
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func hasString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
