package secret

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strings"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/secret"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"golang.org/x/crypto/ssh"
)

func normalizedKind(kind domain.Kind) domain.Kind {
	if kind == "" {
		return domain.KindOpaque
	}
	return kind
}

func publicSSHKey(key *domain.SSHPublicKey) *sohaapi.SecretSSHPublicKey {
	if key == nil {
		return nil
	}
	return &sohaapi.SecretSSHPublicKey{AuthorizedKey: key.AuthorizedKey, Algorithm: key.Algorithm, Fingerprint: key.Fingerprint}
}

func normalizeResolvedSSH(version domain.Version, value string) (string, error) {
	if normalizedKind(version.Kind) == domain.KindOpaque {
		return value, nil
	}
	material, public, err := sshMaterial(version.Kind, value, "")
	if err != nil {
		return "", unavailableReference()
	}
	if version.PublicKey != nil && (public == nil || public.Fingerprint != version.PublicKey.Fingerprint) {
		return "", unavailableReference()
	}
	return material, nil
}

func validateCredentialBindings(kind domain.Kind, bindings []domain.Binding) error {
	kind = normalizedKind(kind)
	if kind != domain.KindOpaque && kind != domain.KindSSHKeypair && kind != domain.KindSSHPublicKey && kind != domain.KindSSHPassword {
		return fmt.Errorf("%w: invalid secret kind", apperrors.ErrInvalidArgument)
	}
	for _, binding := range bindings {
		if kind == domain.KindOpaque && binding.Purpose != "" || kind != domain.KindOpaque && binding.Purpose == "" || kind == domain.KindSSHPublicKey && binding.Purpose != "vm_cloud_init" || kind == domain.KindSSHPassword && binding.Purpose == "vm_cloud_init" {
			return fmt.Errorf("%w: secret kind does not support this binding purpose", apperrors.ErrInvalidArgument)
		}
	}
	if kind != domain.KindOpaque && len(bindings) == 0 {
		return fmt.Errorf("%w: SSH credentials require an explicit VM purpose and target", apperrors.ErrInvalidArgument)
	}
	return nil
}

func sshMaterial(kind domain.Kind, value, passphrase string) (string, *domain.SSHPublicKey, error) {
	invalid := func() (string, *domain.SSHPublicKey, error) {
		return "", nil, fmt.Errorf("%w: invalid SSH credential material", apperrors.ErrInvalidArgument)
	}
	if len(passphrase) > 4096 {
		return invalid()
	}
	var public ssh.PublicKey
	switch kind {
	case domain.KindSSHKeypair:
		canonical, key, err := parseSSHKeypair(value, passphrase)
		if err != nil {
			return invalid()
		}
		value, public = canonical, key
	case domain.KindSSHPublicKey:
		key, _, options, rest, err := ssh.ParseAuthorizedKey([]byte(value))
		if err != nil || len(options) != 0 || strings.TrimSpace(string(rest)) != "" || passphrase != "" {
			return invalid()
		}
		public = key
		value = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	case domain.KindSSHPassword:
		if value == "" || len(value) > 4096 || strings.ContainsRune(value, '\x00') || passphrase != "" {
			return invalid()
		}
		return value, nil, nil
	case domain.KindOpaque:
		if passphrase != "" {
			return invalid()
		}
		return value, nil, nil
	default:
		return invalid()
	}
	if !validSSHPublicKey(public) {
		return invalid()
	}
	return value, &domain.SSHPublicKey{AuthorizedKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(public))), Algorithm: public.Type(), Fingerprint: ssh.FingerprintSHA256(public)}, nil
}

func (s *Service) buildCredentialVersion(ctx context.Context, kind domain.Kind, value *string, vault *domain.VaultKV2Reference, passphrase string) (domain.Version, error) {
	kind = normalizedKind(kind)
	if kind == domain.KindOpaque {
		if passphrase != "" {
			return domain.Version{}, apperrors.ErrInvalidArgument
		}
		version, err := s.buildVersion(value, vault)
		version.Kind = kind
		return version, err
	}
	if (value == nil) == (vault == nil) {
		return domain.Version{}, apperrors.ErrInvalidArgument
	}
	if value != nil {
		material, public, err := sshMaterial(kind, *value, passphrase)
		if err != nil {
			return domain.Version{}, err
		}
		version, err := s.buildVersion(&material, nil)
		version.Kind, version.PublicKey = kind, public
		return version, err
	}
	if passphrase != "" {
		return domain.Version{}, apperrors.ErrInvalidArgument
	}
	version, err := s.buildVersion(nil, vault)
	if err != nil {
		return version, err
	}
	raw, err := s.vault.Read(ctx, *version.VaultKV2)
	if err != nil || len(raw) > maxSecretValueBytes {
		return domain.Version{}, unavailableReference()
	}
	_, public, err := sshMaterial(kind, raw, "")
	if err != nil {
		return domain.Version{}, err
	}
	version.Kind, version.PublicKey = kind, public
	return version, nil
}

func (s *Service) GenerateSSHKey(ctx context.Context, principal domainidentity.Principal, input sohaapi.SecretGenerateSSHKeyRequest) (sohaapi.SecretMetadata, error) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return sohaapi.SecretMetadata{}, err
	}
	block, err := ssh.MarshalPrivateKey(key, "")
	if err != nil {
		return sohaapi.SecretMetadata{}, err
	}
	value := string(pem.EncodeToMemory(block))
	bindings := make([]domain.Binding, 0, len(input.Bindings))
	for _, b := range input.Bindings {
		bindings = append(bindings, domain.Binding{TargetType: string(b.TargetType), TargetRef: b.TargetRef, Purpose: string(b.Purpose)})
	}
	return s.Create(ctx, principal, domain.CreateInput{Kind: domain.KindSSHKeypair, Name: input.Name, Description: input.Description, ScopeType: domain.ScopeType(input.ScopeType), ScopeID: input.ScopeID, Bindings: bindings, Value: &value})
}

func (s *Service) PinSSHPublicKeys(ctx context.Context, principal domainidentity.Principal, refs map[string]string, target domain.Target) ([]domain.Reference, []domain.SSHPublicKey, error) {
	if target.Purpose != "vm_cloud_init" {
		return nil, nil, apperrors.ErrInvalidArgument
	}
	pinned, err := s.PinReferences(ctx, principal, refs, target)
	if err != nil {
		return nil, nil, err
	}
	keys := make([]domain.SSHPublicKey, 0, len(pinned))
	for _, ref := range pinned {
		_, version, err := s.availableReference(ctx, principal, ref, target)
		if err != nil {
			return nil, nil, err
		}
		if version.PublicKey == nil {
			return nil, nil, unavailableReference()
		}
		keys = append(keys, *version.PublicKey)
	}
	return pinned, keys, nil
}

func validSSHPublicKey(public ssh.PublicKey) bool {
	if public.Type() == ssh.KeyAlgoRSA {
		cryptoKey, ok := public.(ssh.CryptoPublicKey)
		if !ok {
			return false
		}
		key, ok := cryptoKey.CryptoPublicKey().(*rsa.PublicKey)
		if !ok || key.N.BitLen() < 2048 {
			return false
		}
	} else if public.Type() != ssh.KeyAlgoED25519 && !strings.HasPrefix(public.Type(), "ecdsa-sha2-nistp") {
		return false
	}
	return true
}
func parseSSHKeypair(value, passphrase string) (string, ssh.PublicKey, error) {
	material := domain.SSHKeypairMaterial{PrivateKey: value, Passphrase: passphrase}
	if strings.HasPrefix(strings.TrimSpace(value), "{") {
		if passphrase != "" || json.Unmarshal([]byte(value), &material) != nil {
			return "", nil, apperrors.ErrInvalidArgument
		}
	}
	if len(material.Passphrase) > 4096 || len(material.PrivateKey) > maxSecretValueBytes {
		return "", nil, apperrors.ErrInvalidArgument
	}
	var signer ssh.Signer
	var err error
	if material.Passphrase == "" {
		signer, err = ssh.ParsePrivateKey([]byte(material.PrivateKey))
	} else {
		signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(material.PrivateKey), []byte(material.Passphrase))
	}
	if err != nil {
		return "", nil, apperrors.ErrInvalidArgument
	}
	// #nosec G117 -- Canonical private material is returned only to encrypted Secret storage, never metadata.
	encoded, err := json.Marshal(material)
	if err != nil {
		return "", nil, apperrors.ErrInvalidArgument
	}
	return string(encoded), signer.PublicKey(), nil
}
