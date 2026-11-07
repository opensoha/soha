package directorysync_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appdirectory "github.com/opensoha/soha/internal/application/directorysync"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domain "github.com/opensoha/soha/internal/domain/directorysync"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	directoryrepo "github.com/opensoha/soha/internal/repository/directorysync"
)

type directoryConnectionRoles struct{}

func (directoryConnectionRoles) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"manager": {appaccess.PermAccessDirectoryManage}}, nil
}

type directoryConnectionAudit []domainaudit.Entry

func (a *directoryConnectionAudit) Record(_ context.Context, entry domainaudit.Entry) error {
	*a = append(*a, entry)
	return nil
}

type connectionTestConnector struct {
	appdirectory.Connector
	validate func() error
}

type connectionTestFactory struct {
	connector connectionTestConnector
}

func (f connectionTestFactory) Connector(string) (appdirectory.Connector, error) {
	return f.connector, nil
}

func (c connectionTestConnector) Validate(context.Context, domain.Connection) (domain.Capabilities, error) {
	return domain.Capabilities{Organizations: true}, c.validate()
}

type directoryConnectionScenario struct {
	ctx      context.Context
	store    *dbstore.Store
	repo     *directoryrepo.Repository
	service  *appdirectory.ConnectionService
	actor    domainidentity.Principal
	audit    directoryConnectionAudit
	input    appdirectory.ConnectionInput
	created  domain.Connection
	validate func() error
}

func TestDirectoryConnectionWritesWithPostgres(t *testing.T) {
	s := newDirectoryConnectionScenario(t)
	t.Run("commit_and_audit", s.checkCommitAndAudit)
	t.Run("denied_before_write", s.checkDeniedWrites)
	t.Run("policy_failure_rolls_back_create", s.checkPolicyRollback)
	t.Run("credential_failure_rolls_back_create_and_update", s.checkCredentialRollbackAndRetry)
	t.Run("validation_rejects_stale_configuration", s.checkStaleValidation)
}

func newDirectoryConnectionScenario(t *testing.T) *directoryConnectionScenario {
	t.Helper()
	store := originalDirectoryStore(t)
	key, err := keyring.NewKey("connection-test", "01234567890123456789012345678901", time.Now(), nil)
	requireDirectoryConnection(t, err)
	keys, err := keyring.New(key, nil)
	requireDirectoryConnection(t, err)
	s := &directoryConnectionScenario{
		ctx: t.Context(), store: store, repo: directoryrepo.New(store.DB(), keys),
		actor:    domainidentity.Principal{UserID: "directory-owner", Roles: []string{"manager"}},
		validate: func() error { return nil },
	}
	factory := connectionTestFactory{connector: connectionTestConnector{validate: func() error { return s.validate() }}}
	s.service, err = appdirectory.NewConnectionService(s.repo, factory, appaccess.NewPermissionResolver(directoryConnectionRoles{}), &s.audit, nil)
	requireDirectoryConnection(t, err)
	// #nosec G101 -- Synthetic credentials exercise binding, collision rollback and audit redaction in a disposable database.
	s.input = appdirectory.ConnectionInput{
		Name: "original", ProviderType: domain.ProviderSCIM, Policy: appdirectory.ConnectionPolicyInput{Mode: domain.PolicyManual},
		WebhookVerificationToken: "fixture-verification", WebhookEncryptKey: "fixture-webhook-key",
		SCIMBearerToken: "fixture-bearer", LDAPBindDN: "cn=fixture", LDAPBindPassword: "fixture-bind-password",
	}
	s.created, _, err = s.service.CreateConnection(s.ctx, s.actor, s.input)
	requireDirectoryConnection(t, err)
	return s
}

func requireDirectoryConnection(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (s *directoryConnectionScenario) assertCredentials(t *testing.T) {
	t.Helper()
	id, err := s.repo.ResolveSCIMConnection(s.ctx, appdirectory.HashSCIMToken(s.input.SCIMBearerToken))
	requireDirectoryConnection(t, err)
	webhook, err := s.repo.GetWebhookCredential(s.ctx, s.created.ID)
	requireDirectoryConnection(t, err)
	ldap, err := s.repo.GetConnectionCredential(s.ctx, s.created.ID)
	requireDirectoryConnection(t, err)
	if id != s.created.ID || webhook.VerificationToken != s.input.WebhookVerificationToken || webhook.EncryptKey != s.input.WebhookEncryptKey ||
		ldap.Username != s.input.LDAPBindDN || ldap.Password != s.input.LDAPBindPassword {
		t.Fatal("connection credential binding changed")
	}
}

func (s *directoryConnectionScenario) checkCommitAndAudit(t *testing.T) {
	t.Helper()
	item, policy, err := s.repo.GetConnection(s.ctx, s.created.ID)
	requireDirectoryConnection(t, err)
	if item.Name != s.input.Name || policy.Mode != domain.PolicyManual || item.CreatedBy != s.actor.UserID || policy.UpdatedBy != s.actor.UserID {
		t.Fatal("connection and policy were not committed")
	}
	s.assertCredentials(t)
	if len(s.audit) != 1 || s.audit[0].ActorID != s.actor.UserID || s.audit[0].ResourceName != s.created.ID || s.audit[0].Result != "success" {
		t.Fatal("missing or misattributed success audit")
	}
	encoded, err := json.Marshal(s.audit[0])
	requireDirectoryConnection(t, err)
	for _, value := range []string{s.input.WebhookVerificationToken, s.input.WebhookEncryptKey, s.input.SCIMBearerToken, s.input.LDAPBindPassword} {
		if strings.Contains(string(encoded), value) {
			t.Fatal("audit contained fixture credential material")
		}
	}
}

func (s *directoryConnectionScenario) checkDeniedWrites(t *testing.T) {
	t.Helper()
	denied := s.actor
	denied.PermissionKeys = []string{appaccess.PermAccessDirectoryView}
	if _, _, err := s.service.CreateConnection(s.ctx, denied, s.input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("denied create returned %v", err)
	}
	if _, _, err := s.service.UpdateConnection(s.ctx, denied, s.created.ID, s.input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("denied update returned %v", err)
	}
	if len(s.audit) != 1 {
		t.Fatal("denied write recorded success")
	}
}

func (s *directoryConnectionScenario) checkPolicyRollback(t *testing.T) {
	t.Helper()
	requireDirectoryConnection(t, s.store.Exec(s.ctx, `ALTER TABLE directory_sync_policies ADD CONSTRAINT fixture_policy_write CHECK (schedule <> 'blocked-policy')`))
	blocked := s.input
	blocked.Name, blocked.Policy.Schedule = "policy failure", "blocked-policy"
	_, _, err := s.service.CreateConnection(s.ctx, s.actor, blocked)
	assertDirectoryPGError(t, err, "23514")
	var count int
	requireDirectoryConnection(t, s.store.DB().WithContext(s.ctx).Raw(`SELECT COUNT(*) FROM directory_connections WHERE name = ?`, blocked.Name).Row().Scan(&count))
	if count != 0 || len(s.audit) != 1 {
		t.Fatal("policy failure left a connection or recorded success")
	}
}

func (s *directoryConnectionScenario) checkCredentialRollbackAndRetry(t *testing.T) {
	t.Helper()
	changed := s.input
	changed.Name, changed.WebhookVerificationToken = "other connection", "fixture-other-verification"
	changed.Policy.SyncPeople = true
	_, _, err := s.service.CreateConnection(s.ctx, s.actor, changed)
	assertDirectoryPGError(t, err, "23505")
	var count int
	requireDirectoryConnection(t, s.store.DB().WithContext(s.ctx).Raw(`SELECT COUNT(*) FROM directory_connections WHERE name = ?`, changed.Name).Row().Scan(&count))
	if count != 0 || len(s.audit) != 1 {
		t.Fatal("credential failure left a connection or recorded success")
	}

	changed.SCIMBearerToken = "fixture-other-bearer"
	other, _, err := s.service.CreateConnection(s.ctx, s.actor, changed)
	requireDirectoryConnection(t, err)
	before, _, err := s.repo.GetConnection(s.ctx, s.created.ID)
	requireDirectoryConnection(t, err)
	_, _, err = s.service.UpdateConnection(s.ctx, s.actor, s.created.ID, changed)
	assertDirectoryPGError(t, err, "23505")
	item, policy, err := s.repo.GetConnection(s.ctx, s.created.ID)
	requireDirectoryConnection(t, err)
	if item.Name != s.input.Name || !item.UpdatedAt.Equal(before.UpdatedAt) || policy.SyncPeople || len(s.audit) != 2 {
		t.Fatal("failed update changed connection or policy or recorded success")
	}
	s.assertCredentials(t)

	changed.SCIMBearerToken, changed.WebhookVerificationToken, changed.LDAPBindPassword = "", "", ""
	updated, _, err := s.service.UpdateConnection(s.ctx, s.actor, s.created.ID, changed)
	requireDirectoryConnection(t, err)
	item, policy, err = s.repo.GetConnection(s.ctx, s.created.ID)
	requireDirectoryConnection(t, err)
	if updated.ID != s.created.ID || other.ID == s.created.ID || item.Name != changed.Name || !policy.SyncPeople || len(s.audit) != 3 {
		t.Fatal("retry did not commit exactly one connection update")
	}
	s.assertCredentials(t)
}

func (s *directoryConnectionScenario) checkStaleValidation(t *testing.T) {
	t.Helper()
	updated := s.input
	updated.Name = "changed while validating"
	s.validate = func() error {
		_, _, err := s.service.UpdateConnection(s.ctx, s.actor, s.created.ID, updated)
		return err
	}
	if _, err := s.service.ValidateConnection(s.ctx, s.actor, s.created.ID); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale validation returned %v", err)
	}
	item, _, err := s.repo.GetConnection(s.ctx, s.created.ID)
	requireDirectoryConnection(t, err)
	if item.Name != updated.Name || item.LastValidatedAt != nil || len(s.audit) != 4 {
		t.Fatal("stale validation changed current connection or recorded success")
	}
	s.validate = func() error { return nil }
	_, err = s.service.ValidateConnection(s.ctx, s.actor, s.created.ID)
	requireDirectoryConnection(t, err)
	item, _, err = s.repo.GetConnection(s.ctx, s.created.ID)
	requireDirectoryConnection(t, err)
	if item.Status != domain.ConnectionHealthy || item.LastValidatedAt == nil || len(s.audit) != 5 {
		t.Fatal("current validation did not commit or record success")
	}
}

func assertDirectoryPGError(t *testing.T, err error, code string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Fatalf("expected SQLSTATE %s, got %v", code, err)
	}
}
