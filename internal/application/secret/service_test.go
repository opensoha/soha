package secret

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

type testRoleReader map[string][]string

func (r testRoleReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return r, nil
}

type memoryRepository struct {
	items    map[string]domainsecret.Secret
	versions map[string]map[int]domainsecret.Version
	leases   map[string]domainsecret.Lease
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{items: map[string]domainsecret.Secret{}, versions: map[string]map[int]domainsecret.Version{}, leases: map[string]domainsecret.Lease{}}
}

func (r *memoryRepository) List(_ context.Context, _ domainsecret.Filter) ([]domainsecret.Secret, error) {
	items := make([]domainsecret.Secret, 0, len(r.items))
	for _, item := range r.items {
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryRepository) Get(_ context.Context, id string) (domainsecret.Secret, error) {
	item, ok := r.items[id]
	if !ok {
		return domainsecret.Secret{}, apperrors.ErrNotFound
	}
	return item, nil
}

func (r *memoryRepository) Create(_ context.Context, item domainsecret.Secret, version domainsecret.Version) (domainsecret.Secret, error) {
	r.items[item.ID] = item
	r.versions[item.ID] = map[int]domainsecret.Version{version.Version: version}
	return item, nil
}

func (r *memoryRepository) Update(_ context.Context, item domainsecret.Secret) (domainsecret.Secret, error) {
	r.items[item.ID] = item
	return item, nil
}

func (r *memoryRepository) ListVersions(_ context.Context, id string) ([]domainsecret.Version, error) {
	items := make([]domainsecret.Version, 0, len(r.versions[id]))
	for _, item := range r.versions[id] {
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryRepository) GetVersion(_ context.Context, id string, version int) (domainsecret.Version, error) {
	item, ok := r.versions[id][version]
	if !ok {
		return domainsecret.Version{}, apperrors.ErrNotFound
	}
	return item, nil
}

func (r *memoryRepository) Rotate(_ context.Context, id string, version domainsecret.Version) (domainsecret.Version, error) {
	item := r.items[id]
	version.Version = item.CurrentVersion + 1
	r.versions[id][version.Version] = version
	item.CurrentVersion = version.Version
	r.items[id] = item
	return version, nil
}

func (r *memoryRepository) RevokeVersion(_ context.Context, id string, version int, at time.Time) (domainsecret.Version, error) {
	item := r.versions[id][version]
	item.Status = domainsecret.VersionRevoked
	item.RevokedAt = &at
	r.versions[id][version] = item
	return item, nil
}

func (r *memoryRepository) CreateLease(_ context.Context, lease domainsecret.Lease) error {
	r.leases[lease.ID] = lease
	return nil
}

func (r *memoryRepository) RedeemLease(_ context.Context, id, tokenHash, agentID string, at time.Time) (domainsecret.Lease, error) {
	lease, ok := r.leases[id]
	if !ok || lease.TokenHash != tokenHash || lease.AgentID != agentID || lease.RedeemedAt != nil || lease.RevokedAt != nil || !lease.ExpiresAt.After(at) {
		return domainsecret.Lease{}, apperrors.ErrNotFound
	}
	lease.RedeemedAt = &at
	r.leases[id] = lease
	return lease, nil
}

func (r *memoryRepository) RevokeSubjectLeases(_ context.Context, subjectType, subjectID string, at time.Time) error {
	for id, lease := range r.leases {
		if lease.SubjectType == subjectType && lease.SubjectID == subjectID && lease.RedeemedAt == nil && lease.RevokedAt == nil {
			lease.RevokedAt = &at
			r.leases[id] = lease
		}
	}
	return nil
}

type captureAudit struct{ entries []domainaudit.Entry }

func (a *captureAudit) Record(_ context.Context, entry domainaudit.Entry) error {
	a.entries = append(a.entries, entry)
	return nil
}

type discardOperations struct{}

func (discardOperations) Record(context.Context, domainoperation.Entry) error { return nil }

type vaultReaderStub struct {
	value      string
	err        error
	references []domainsecret.VaultKV2Reference
}

func (r *vaultReaderStub) Read(_ context.Context, reference domainsecret.VaultKV2Reference) (string, error) {
	r.references = append(r.references, reference)
	return r.value, r.err
}

func TestSecretLifecycleEncryptsPinsAndFailsClosed(t *testing.T) {
	repo := newMemoryRepository()
	audit := &captureAudit{}
	service, err := New(repo, appaccess.NewPermissionResolver(testRoleReader{
		"admin":     {appaccess.PermSecretView, appaccess.PermSecretCreate, appaccess.PermSecretUpdate, appaccess.PermSecretRotate, appaccess.PermSecretRevoke, appaccess.PermSecretUse},
		"developer": {appaccess.PermSecretView, appaccess.PermSecretUse},
	}), audit, discardOperations{}, testSecretKeyring(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC) }
	admin := domainidentity.Principal{UserID: "admin", UserName: "Admin", Roles: []string{"admin"}}
	value := "plaintext-must-not-leak"
	created, err := service.Create(context.Background(), admin, domainsecret.CreateInput{
		Name: "registry-token", Value: &value, ScopeType: domainsecret.ScopeProject, ScopeID: "demo",
		Bindings: []domainsecret.Binding{{TargetType: "capability", TargetRef: "docker.project.deploy"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	stored := repo.versions[created.ID][1]
	if !secretcrypto.Encrypted(stored.Ciphertext) || stored.Ciphertext == "plaintext-must-not-leak" {
		t.Fatalf("stored ciphertext is not encrypted: %q", stored.Ciphertext)
	}
	encoded, _ := json.Marshal(created)
	if string(encoded) == "" || strings.Contains(string(encoded), "plaintext-must-not-leak") {
		t.Fatalf("metadata leaked plaintext: %s", encoded)
	}

	developer := domainidentity.Principal{UserID: "dev", Roles: []string{"developer"}, Projects: []string{"demo"}}
	pinned, err := service.PinReferences(context.Background(), developer, map[string]string{
		"REGISTRY_TOKEN": "soha://secrets/" + created.ID,
	}, domainsecret.Target{Type: "capability", Ref: "docker.project.deploy"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pinned) != 1 || pinned[0].Version != 1 {
		t.Fatalf("pinned refs = %#v", pinned)
	}
	values, err := service.ResolvePinnedReferences(context.Background(), developer, pinned, domainsecret.Target{Type: "capability", Ref: "docker.project.deploy"})
	if err != nil {
		t.Fatal(err)
	}
	if values["REGISTRY_TOKEN"] != "plaintext-must-not-leak" {
		t.Fatalf("resolved value = %q", values["REGISTRY_TOKEN"])
	}
	grant, err := service.IssueLease(context.Background(), developer, pinned, domainsecret.Target{Type: "capability", Ref: "docker.project.deploy"}, "execution_task", "task-1", "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	storedLease := repo.leases[grant.ID]
	if storedLease.TokenHash == "" || storedLease.TokenHash == grant.Token {
		t.Fatal("lease token was not stored as a one-way hash")
	}
	if _, err := service.RedeemLease(context.Background(), grant.ID, grant.Token, "agent-2"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("wrong agent error = %v, want fail-closed not found", err)
	}
	redemption, err := service.RedeemLease(context.Background(), grant.ID, grant.Token, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	if redemption.Values["REGISTRY_TOKEN"] != "plaintext-must-not-leak" {
		t.Fatalf("redeemed value = %q", redemption.Values["REGISTRY_TOKEN"])
	}
	if _, err := service.RedeemLease(context.Background(), grant.ID, grant.Token, "agent-1"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("second redemption error = %v, want one-time fail-closed not found", err)
	}
	_, err = service.ResolvePinnedReferences(context.Background(), developer, pinned, domainsecret.Target{Type: "capability", Ref: "virtualization.vm.create"})
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("wrong target error = %v, want fail-closed not found", err)
	}
	for _, entry := range audit.entries {
		payload, _ := json.Marshal(entry)
		if strings.Contains(string(payload), "plaintext-must-not-leak") {
			t.Fatalf("audit leaked plaintext: %s", payload)
		}
	}
}

func TestVaultKV2SecretResolvesThroughExistingAuthorizationBoundary(t *testing.T) {
	repo := newMemoryRepository()
	audit := &captureAudit{}
	reader := &vaultReaderStub{value: "external-value"}
	service, err := New(repo, appaccess.NewPermissionResolver(testRoleReader{
		"admin":     {appaccess.PermSecretView, appaccess.PermSecretCreate, appaccess.PermSecretUpdate, appaccess.PermSecretRotate, appaccess.PermSecretRevoke, appaccess.PermSecretUse},
		"developer": {appaccess.PermSecretUse},
	}), audit, discardOperations{}, testSecretKeyring(t), reader)
	if err != nil {
		t.Fatal(err)
	}
	admin := domainidentity.Principal{UserID: "admin", Roles: []string{"admin"}}
	reference := &domainsecret.VaultKV2Reference{Mount: "team/kv", Path: "demo/app", Key: " token ", Version: 7}
	created, err := service.Create(context.Background(), admin, domainsecret.CreateInput{
		Name: "vault-token", VaultKV2: reference, ScopeType: domainsecret.ScopeProject, ScopeID: "demo",
		Bindings: []domainsecret.Binding{{TargetType: "capability", TargetRef: "docker.project.deploy"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	stored := repo.versions[created.ID][1]
	if stored.SourceType != domainsecret.SourceVaultKV2 || stored.Ciphertext != "" || stored.VaultKV2 == nil || *stored.VaultKV2 != *reference {
		t.Fatalf("stored Vault version = %#v", stored)
	}
	developer := domainidentity.Principal{UserID: "dev", Roles: []string{"developer"}, Projects: []string{"demo"}}
	target := domainsecret.Target{Type: "capability", Ref: "docker.project.deploy"}
	pinned, err := service.PinReferences(context.Background(), developer, map[string]string{"TOKEN": "soha://secrets/" + created.ID}, target)
	if err != nil {
		t.Fatal(err)
	}
	values, err := service.ResolvePinnedReferences(context.Background(), developer, pinned, target)
	if err != nil || values["TOKEN"] != "external-value" || len(reader.references) != 1 || reader.references[0] != *reference {
		t.Fatalf("resolved values=%#v refs=%#v err=%v", values, reader.references, err)
	}
	reader.err = errors.New("provider body contains external-value")
	if _, err := service.ResolvePinnedReferences(context.Background(), developer, pinned, target); !errors.Is(err, apperrors.ErrNotFound) || strings.Contains(err.Error(), "external-value") {
		t.Fatalf("provider failure = %v, want redacted unavailable error", err)
	}
}

func testSecretKeyring(t *testing.T) keyring.Ring {
	t.Helper()
	key, err := keyring.NewKey("test-v1", "stable-test-secret-key-32-bytes", time.Now().UTC(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ring
}
