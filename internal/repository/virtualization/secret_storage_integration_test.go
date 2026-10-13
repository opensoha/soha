package virtualization

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appsecret "github.com/opensoha/soha/internal/application/secret"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	secretrepo "github.com/opensoha/soha/internal/repository/secret"
)

type secretTestPermissions struct{}

func (secretTestPermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"admin": {"secret.view", "secret.create", "secret.update", "secret.rotate", "secret.revoke", "secret.use"}}, nil
}

type secretTestAudit struct{}

func (secretTestAudit) Record(context.Context, domainaudit.Entry) error { return nil }

type secretTestOperations struct{}

func (secretTestOperations) Record(context.Context, domainoperation.Entry) error { return nil }

func testSSHSecretStorageWithPostgres(t *testing.T, repo *Repository, ctx context.Context) {
	testVMAccessRepository(t, repo, ctx)
	t.Helper()
	key, err := keyring.NewKey("task51-synthetic", "task51-test-only-encryption-key-32", time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	store := secretrepo.New(repo.db)
	service, err := appsecret.New(store, appaccess.NewPermissionResolver(secretTestPermissions{}), secretTestAudit{}, secretTestOperations{}, ring, nil)
	if err != nil {
		t.Fatal(err)
	}
	principal := domainidentity.Principal{UserID: "task51-test", Roles: []string{"admin"}}
	vm := uuid.NewString()
	created, err := service.GenerateSSHKey(ctx, principal, sohaapi.SecretGenerateSSHKeyRequest{Name: "task51-" + vm, ScopeType: sohaapi.SecretScopeWorkspace, ScopeID: "default", Bindings: []sohaapi.SecretBinding{{TargetType: sohaapi.SecretBindingVM, TargetRef: vm, Purpose: sohaapi.SecretPurposeVMAuth}}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.Get(ctx, principal, created.ID)
	if err != nil || got.Kind != sohaapi.SecretKindSSHKeypair || got.PublicKey == nil || got.PublicKey.Fingerprint != created.PublicKey.Fingerprint || got.Bindings[0].Purpose != sohaapi.SecretPurposeVMAuth {
		t.Fatalf("typed storage lost projection or purpose: %+v %v", got, err)
	}
	versions, err := service.ListVersions(ctx, principal, created.ID)
	if err != nil || len(versions) != 1 || versions[0].PublicKey == nil || versions[0].Kind != sohaapi.SecretKindSSHKeypair {
		t.Fatalf("version projection: %+v %v", versions, err)
	}
	testPersistedSSHLease(t, ctx, service, principal, vm, created.ID)
}

func testPersistedSSHLease(t *testing.T, ctx context.Context, service *appsecret.Service, principal domainidentity.Principal, vm, secretID string) {
	t.Helper()
	target := domainsecret.Target{Type: "vm", Ref: vm, Purpose: "vm_auth"}
	refs, err := service.PinReferences(ctx, principal, map[string]string{"SSH_AUTH": "soha://secrets/" + secretID}, target)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := uuid.NewString()
	lease, err := service.IssueLease(ctx, principal, refs, target, "vm_ssh_session", sessionID, "agent-one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RedeemVMSSHLease(ctx, lease.ID, lease.Token, "agent-two", sessionID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("cross-Agent lease accepted", err)
	}
	if _, err := service.RedeemVMSSHLease(ctx, lease.ID, lease.Token, "agent-one", sessionID); err != nil {
		t.Fatal("persisted lease failed purpose binding", err)
	}
	if _, err := service.RedeemVMSSHLease(ctx, lease.ID, lease.Token, "agent-one", sessionID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("persisted lease replay accepted", err)
	}
	if _, err := service.Disable(ctx, principal, secretID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PinReferences(ctx, principal, map[string]string{"SSH_AUTH": refs[0].URI}, target); err == nil {
		t.Fatal("disabled persisted key remained available")
	}
}
