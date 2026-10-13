package secret

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/secret"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	"golang.org/x/crypto/ssh"
)

func TestSSHMaterialRejectsWeakKeysAndAuthorizationOptions(t *testing.T) {
	// #nosec G403 -- Test proves weak RSA is rejected.
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	public, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	value := string(ssh.MarshalAuthorizedKey(public))
	if _, _, err = sshMaterial(domain.KindSSHPublicKey, value, ""); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatal("weak RSA accepted", err)
	}
	if _, _, err = sshMaterial(domain.KindSSHPublicKey, "command=\"bad\" "+value, ""); err == nil {
		t.Fatal("authorized_key options accepted")
	}
}

func TestSSHCredentialProjectionPurposeAndLease(t *testing.T) {
	repo := newMemoryRepository()
	audit := &captureAudit{}
	s, err := New(repo, appaccess.NewPermissionResolver(testRoleReader{"admin": {appaccess.PermSecretView, appaccess.PermSecretCreate, appaccess.PermSecretUpdate, appaccess.PermSecretRotate, appaccess.PermSecretRevoke, appaccess.PermSecretUse}}), audit, discardOperations{}, testSecretKeyring(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	principal := domainidentity.Principal{UserID: "admin", Roles: []string{"admin"}}
	input := sohaapi.SecretGenerateSSHKeyRequest{Name: "vm-key", ScopeType: sohaapi.SecretScopeWorkspace, ScopeID: "default", Bindings: []sohaapi.SecretBinding{{TargetType: sohaapi.SecretBindingVM, TargetRef: "vm-one", Purpose: sohaapi.SecretPurposeVMAuth}, {TargetType: sohaapi.SecretBindingVMCreate, TargetRef: "cluster-one", Purpose: sohaapi.SecretPurposeVMCloudInit}}}
	created, err := s.GenerateSSHKey(context.Background(), principal, input)
	if err != nil || created.PublicKey == nil || created.PublicKey.Algorithm != "ssh-ed25519" {
		t.Fatalf("generation: %+v %v", created, err)
	}
	stored := repo.versions[created.ID][1]
	if !secretcrypto.Encrypted(stored.Ciphertext) {
		t.Fatal("SSH key stored without encryption")
	}
	encoded, _ := json.Marshal(created)
	if strings.Contains(string(encoded), "PRIVATE KEY") || strings.Contains(string(encoded), "passphrase") {
		t.Fatal("private material in metadata")
	}
	refs := map[string]string{"SSH_AUTH": "soha://secrets/" + created.ID}
	target := domain.Target{Type: "vm", Ref: "vm-one", Purpose: "vm_auth"}
	pinned, err := s.PinReferences(context.Background(), principal, refs, target)
	if err != nil || pinned[0].Version != 1 {
		t.Fatalf("pin: %+v %v", pinned, err)
	}
	for _, denied := range []domain.Target{{Type: "vm", Ref: "vm-two", Purpose: "vm_auth"}, {Type: "vm", Ref: "vm-one", Purpose: "vm_exec"}, {Type: "connection", Ref: "vm-one"}} {
		if _, err := s.PinReferences(context.Background(), principal, refs, denied); err == nil {
			t.Fatalf("wrong VM or purpose accepted: %+v", denied)
		}
	}
	_, keys, err := s.PinSSHPublicKeys(context.Background(), principal, refs, domain.Target{Type: "vm_create", Ref: "cluster-one", Purpose: "vm_cloud_init"})
	if err != nil || len(keys) != 1 || keys[0].Fingerprint != created.PublicKey.Fingerprint {
		t.Fatalf("public projection: %+v %v", keys, err)
	}
	testSSHCredentialLease(t, s, principal, pinned, target, created.ID, refs, audit)
}

func testSSHCredentialLease(t *testing.T, s *Service, principal domainidentity.Principal, pinned []domain.Reference, target domain.Target, secretID string, refs map[string]string, audit *captureAudit) {
	t.Helper()
	lease, err := s.IssueLease(context.Background(), principal, pinned, target, "vm_ssh_session", "session-one", "agent-one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemLease(context.Background(), lease.ID, lease.Token, "agent-one"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("generic lease endpoint accepted SSH session", err)
	}
	if _, err := s.RedeemVMSSHLease(context.Background(), lease.ID, lease.Token, "agent-one", "session-other"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("cross-session redemption accepted", err)
	}
	if _, err := s.RedeemVMSSHLease(context.Background(), lease.ID, lease.Token, "agent-two", "session-one"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("other Agent redeemed lease", err)
	}
	values, err := s.RedeemVMSSHLease(context.Background(), lease.ID, lease.Token, "agent-one", "session-one")
	if err != nil || !strings.Contains(values.Values["SSH_AUTH"], "PRIVATE KEY") {
		t.Fatal("execution material unavailable", err)
	}
	if _, err := s.RedeemVMSSHLease(context.Background(), lease.ID, lease.Token, "agent-one", "session-one"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("lease replay accepted", err)
	}
	if _, err := s.Disable(context.Background(), principal, secretID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PinReferences(context.Background(), principal, refs, target); err == nil {
		t.Fatal("disabled credential remained usable")
	}
	auditJSON, _ := json.Marshal(audit.entries)
	if strings.Contains(string(auditJSON), "PRIVATE KEY") || strings.Contains(string(auditJSON), lease.Token) {
		t.Fatal("audit leaked private material")
	}
}

func TestSSHCredentialKindCannotBorrowGenericBinding(t *testing.T) {
	for _, bindings := range [][]domain.Binding{{{TargetType: "connection", TargetRef: "cluster"}}, {{TargetType: "vm_create", TargetRef: "cluster", Purpose: "vm_cloud_init"}}} {
		kind := domain.KindSSHKeypair
		if bindings[0].Purpose != "" {
			kind = domain.KindSSHPassword
		}
		if err := validateCredentialBindings(kind, bindings); err == nil {
			t.Fatalf("unsafe kind/purpose: %s %+v", kind, bindings)
		}
	}
	if _, _, err := sshMaterial(domain.KindSSHPublicKey, "command=evil ssh-ed25519 AAAA", ""); err == nil {
		t.Fatal("authorized-key options accepted")
	}
	if err := validateTarget(domain.Target{Type: "vm_create", Ref: "cluster", Purpose: "vm_auth"}); err == nil {
		t.Fatal("target/purpose Cartesian product accepted")
	}
}

func TestSSHCommandLeaseUsesExactExecPurpose(t *testing.T) {
	repo := newMemoryRepository()
	audit := &captureAudit{}
	s, err := New(repo, appaccess.NewPermissionResolver(testRoleReader{"admin": {appaccess.PermSecretView, appaccess.PermSecretCreate, appaccess.PermSecretUse, appaccess.PermSecretUpdate}}), audit, discardOperations{}, testSecretKeyring(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	principal := domainidentity.Principal{UserID: "admin", Roles: []string{"admin"}}
	created, err := s.GenerateSSHKey(context.Background(), principal, sohaapi.SecretGenerateSSHKeyRequest{Name: "command-key", ScopeType: sohaapi.SecretScopeWorkspace, ScopeID: "default", Bindings: []sohaapi.SecretBinding{{TargetType: sohaapi.SecretBindingVM, TargetRef: "vm-one", Purpose: sohaapi.SecretPurposeVMExec}}})
	if err != nil {
		t.Fatal(err)
	}
	refs := map[string]string{"SSH_AUTH": "soha://secrets/" + created.ID}
	target := domain.Target{Type: "vm", Ref: "vm-one", Purpose: "vm_exec"}
	pinned, err := s.PinReferences(context.Background(), principal, refs, target)
	if err != nil {
		t.Fatal(err)
	}
	for _, denied := range []domain.Target{{Type: "vm", Ref: "vm-one", Purpose: "vm_auth"}, {Type: "vm", Ref: "vm-two", Purpose: "vm_exec"}, {Type: "vm_create", Ref: "vm-one", Purpose: "vm_cloud_init"}} {
		if _, err = s.IssueLease(context.Background(), principal, pinned, denied, "vm_ssh_session", "session-one", "agent-one"); err == nil {
			t.Fatalf("wrong scope/purpose accepted: %+v", denied)
		}
	}
	testSSHCredentialLease(t, s, principal, pinned, target, created.ID, refs, audit)
}
