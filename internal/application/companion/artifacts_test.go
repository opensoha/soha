package companion

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"

	domaincompanion "github.com/opensoha/soha/internal/domain/companion"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestVerifyPackageSignature(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	descriptor := domainplugin.PluginPackageDescriptor{
		Sha256: "sha256:1234", SignatureAlgorithm: "ed25519", SigningKeyID: "release-1",
	}
	descriptor.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(descriptor.Sha256)))

	status, err := verifyPackageSignature(descriptor, map[string]ed25519.PublicKey{"release-1": publicKey}, true)
	if err != nil {
		t.Fatalf("verifyPackageSignature() error = %v", err)
	}
	if status != "verified" {
		t.Fatalf("verifyPackageSignature() status = %q, want verified", status)
	}

	descriptor.Sha256 = "sha256:changed"
	if _, err := verifyPackageSignature(descriptor, map[string]ed25519.PublicKey{"release-1": publicKey}, true); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("tampered signature error = %v, want invalid argument", err)
	}
}

func TestVerifyProvenanceRequiredWithSignedPackages(t *testing.T) {
	service := &ArtifactService{options: ArtifactOptions{RequireSignature: true}}
	status, err := service.verifyProvenance(context.Background(), domainplugin.PluginPackageDescriptor{
		Sha256: "sha256:1234",
	})
	if status != "not_provided" || !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("verifyProvenance() = (%q, %v), want required error", status, err)
	}
}

func TestValidateManifestKeepsLive2DLicenseGateClosed(t *testing.T) {
	service := &ArtifactService{options: ArtifactOptions{AllowLive2D: false}}
	manifest := domainplugin.PluginManifest{
		ID: "example.pet", Version: "1.0.0", Type: "companion-pack",
		CompanionPack: &domaincompanion.PackManifest{
			Renderer: "live2d-cubism", EntryAsset: "model.json",
			License: domaincompanion.PackManifest{}.License,
			Assets:  []domaincompanion.Asset{{Path: "model.json", Kind: "model", ContentType: "application/json", SizeBytes: 2, Sha256: "sha256:00"}},
		},
	}
	manifest.CompanionPack.License.Name = "example"
	manifest.CompanionPack.License.URL = "https://example.com/license"
	manifest.CompanionPack.License.RedistributionAllowed = true

	err := service.validateManifest(manifest)
	if !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatalf("validateManifest() error = %v, want Live2D gate rejection", err)
	}
}
