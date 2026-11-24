package storagedriverhost

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
)

const (
	fixturePlugin   = "example.blob"
	fixtureDriver   = "example.native-blob"
	fixtureVersion  = "1.0.1"
	fixtureArtifact = "signed-storage-driver"
)

type signedFixture struct {
	config    DriverConfig
	public    []byte
	private   ed25519.PrivateKey
	manifest  map[string]any
	platform  string
	inventory map[string]any
}

func newSignedFixture(t *testing.T, platform string, binary []byte) signedFixture {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(binary)
	inventory := map[string]any{"backend": map[string]any{
		"driverId": fixtureDriver, "protocol": "object-storage/v1",
		"platforms": map[string]any{platform: releaseArtifact{
			Name: fixtureArtifact, ByteLength: int64(len(binary)), SHA256: hex.EncodeToString(digest[:]),
		}},
	}}
	return signedFixture{
		config: DriverConfig{
			PluginID: fixturePlugin, Version: fixtureVersion,
			Repository: "https://github.com/example/blob", Commit: strings.Repeat("a", 40),
		},
		public:  pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}),
		private: private, platform: platform, inventory: inventory,
		manifest: map[string]any{
			"id": fixturePlugin, "name": "Native Blob", "publisher": "example", "version": fixtureVersion, "type": "resource-extension",
			"runtime": map[string]string{"mode": "executable-package-v1"}, "components": inventory,
			"configSchema": map[string]string{"type": "object"},
		},
	}
}

func (f signedFixture) documents(t *testing.T) ([]byte, []byte, []byte) {
	t.Helper()
	manifest, err := json.Marshal(f.manifest)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(manifest)
	attestation, err := json.Marshal(map[string]any{
		"schemaVersion": "1", "repository": f.config.Repository, "commitSha": f.config.Commit,
		"pluginId": f.config.PluginID, "version": f.config.Version,
		"rawManifestSha256": hex.EncodeToString(digest[:]), "components": f.inventory,
	})
	if err != nil {
		t.Fatal(err)
	}
	attestation = append(attestation, '\n')
	return manifest, attestation, ed25519.Sign(f.private, attestation)
}

func TestVerifyRelease(t *testing.T) {
	t.Parallel()
	fixture := newSignedFixture(t, "linux/amd64", []byte("storage driver fixture bytes"))
	manifest, attest, signature := fixture.documents(t)
	release, err := verifyRelease(fixture.config, fixture.public, attest, signature, manifest, fixture.platform)
	if err != nil {
		t.Fatal(err)
	}
	if release.descriptor.ID != fixtureDriver || release.identity.PluginID != fixturePlugin || release.binary.Name != fixtureArtifact {
		t.Fatalf("incorrect release selection: %+v", release)
	}
	for _, test := range []struct {
		name   string
		change func(*DriverConfig, *[]byte, *[]byte, *[]byte, *string)
		why    string
	}{
		{
			name: "signature binds original trailing newline",
			change: func(_ *DriverConfig, _ *[]byte, attest, _ *[]byte, _ *string) {
				*attest = bytes.TrimSpace(*attest)
			},
			why: "signature",
		},
		{
			name:   "trusted source",
			change: func(cfg *DriverConfig, _, _, _ *[]byte, _ *string) { cfg.Repository += "-other" },
			why:    "pinned release",
		},
		{
			name:   "pinned commit",
			change: func(cfg *DriverConfig, _, _, _ *[]byte, _ *string) { cfg.Commit = strings.Repeat("b", 40) },
			why:    "pinned release",
		},
		{
			name:   "raw manifest bytes",
			change: func(_ *DriverConfig, manifest, _, _ *[]byte, _ *string) { *manifest = append(*manifest, '\n') },
			why:    "pinned release",
		},
		{
			name: "signed duplicate member",
			change: func(_ *DriverConfig, _ *[]byte, attest, signature *[]byte, _ *string) {
				*attest = bytes.Replace(*attest, []byte(`"schemaVersion":`), []byte(`"schemaVersion":"1","schemaVersion":`), 1)
				*signature = ed25519.Sign(fixture.private, *attest)
			},
			why: "attestation",
		},
		{
			name: "signed mismatched inventory",
			change: func(_ *DriverConfig, _ *[]byte, attest, signature *[]byte, _ *string) {
				*attest = bytes.Replace(*attest, []byte(fixtureArtifact), []byte("some-other-driver"), 1)
				*signature = ed25519.Sign(fixture.private, *attest)
			},
			why: "inventory mismatch",
		},
		{
			name:   "selected platform missing",
			change: func(_ *DriverConfig, _, _, _ *[]byte, platform *string) { *platform = "linux/arm64" },
			why:    "no artifact",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, rawManifest, rawAttest, sig, platform := fixture.config, bytes.Clone(manifest), bytes.Clone(attest), bytes.Clone(signature), fixture.platform
			test.change(&cfg, &rawManifest, &rawAttest, &sig, &platform)
			_, err := verifyRelease(cfg, fixture.public, rawAttest, sig, rawManifest, platform)
			if err == nil || !strings.Contains(err.Error(), test.why) {
				t.Fatalf("wanted %s error, got %v", test.why, err)
			}
		})
	}
}
