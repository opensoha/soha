//go:build unix

package pluginrelease

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/infrastructure/githubplugin"
)

type releaseFixture struct {
	source      domainplugin.PluginRepositoryPreviewSource
	manifest    domainplugin.PluginManifest
	fingerprint string
	digest      string
	release     *packageRelease
	assets      map[string][]byte
}

func signedFixture(t *testing.T, web []byte, backend bool) releaseFixture {
	t.Helper()
	binary := []byte("fixture backend")
	components := sohaapi.PluginPackageComponents{
		Web: &sohaapi.PluginReleaseArtifact{Name: "site.tar.gz", Sha256: digestBytes(web), ByteLength: int64(len(web))},
	}
	if backend {
		components.Backend = &sohaapi.PluginStorageDriverPackage{
			DriverID: "example.blob", Protocol: "object-storage/v1",
			Platforms: map[string]sohaapi.PluginReleaseArtifact{
				runtime.GOOS + "/" + runtime.GOARCH: {Name: "driver", Sha256: digestBytes(binary), ByteLength: int64(len(binary))},
			},
		}
	}
	manifest := domainplugin.PluginManifest{
		ID: "example.blob", Name: "Blob", Version: "1.0.0", Publisher: "example",
		Type: "resource-extension", Runtime: &sohaapi.PluginRuntimeSpec{Mode: "executable-package-v1"},
		Components: &components,
	}
	rawManifest := append(marshalFixture(t, manifest), '\n')
	source := domainplugin.PluginRepositoryPreviewSource{
		Kind: "repository", CanonicalSource: "https://github.com/example/plugin",
		PinnedRef: "refs/tags/v1.0.0", CommitSha: strings.Repeat("a", 40),
		RawManifestChecksum: "sha256:" + digestBytes(rawManifest),
	}
	attestation := &sohaapi.PluginReleaseAttestation{
		SchemaVersion: "1", Repository: source.CanonicalSource, CommitSha: source.CommitSha,
		PluginID: manifest.ID, Version: manifest.Version, Components: components, RawManifestSha256: digestBytes(rawManifest),
	}
	raw := marshalFixture(t, attestation)
	rawAttestation := append(append([]byte(nil), raw...), '\n')
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	assets, err := packageArtifacts(components, runtime.GOOS+"/"+runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	return releaseFixture{
		source: source, manifest: manifest, fingerprint: digestBytes(public), digest: digestBytes(raw),
		release: &packageRelease{
			attestation: attestation, fingerprint: digestBytes(public), artifacts: assets,
			documents: map[string][]byte{
				manifestName: rawManifest, attestationName: rawAttestation, signatureName: ed25519.Sign(private, rawAttestation),
				PublisherFilename: pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}),
			},
		},
		assets: map[string][]byte{"driver": binary, "site.tar.gz": web},
	}
}

func marshalFixture(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type releaseBytes map[string][]byte

func (releaseBytes) MatchingReleases(context.Context, domainplugin.PluginRepositoryPreviewSource, string) ([]githubplugin.Release, error) {
	return nil, nil
}

func (content releaseBytes) CopyReleaseAsset(_ context.Context, _ githubplugin.Release, name string, _ int64, destination io.Writer) error {
	_, err := destination.Write(content[name])
	return err
}

func TestStageRequiresApprovedFingerprintAndMatchedRelease(t *testing.T) {
	t.Parallel()
	fixture := signedFixture(t, siteArchive(t, []webTarEntry{{name: "index.html", data: "hello"}}), false)
	store := &Store{client: releaseBytes(fixture.assets), root: testPackageRoot(t)}
	manifest := fixture.release.documents[manifestName]
	_, err := store.Stage(t.Context(), fixture.source, fixture.manifest, manifest, "", nil, fixture.digest)
	if !errors.Is(err, errPublisher) {
		t.Fatalf("Stage without trust = %v", err)
	}
	_, err = store.Stage(t.Context(), fixture.source, fixture.manifest, manifest, fixture.fingerprint, nil, fixture.digest)
	if !errors.Is(err, errReleaseRequired) {
		t.Fatalf("Stage without signed release = %v", err)
	}
	if _, err := os.Lstat(store.root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected confirmations created a package directory: %v", err)
	}
}

func TestVerifyAttestationRejectsMismatchesAndDuplicateKeys(t *testing.T) {
	t.Parallel()
	fixture := signedFixture(t, siteArchive(t, []webTarEntry{{name: "index.html", data: "hello"}}), true)
	raw := fixture.release.documents[attestationName]
	manifest := fixture.release.documents[manifestName]
	if got, err := verifyAttestation(fixture.source, fixture.manifest, manifest, raw); err != nil ||
		!matchesAttestationDigest(got, fixture.digest) {
		t.Fatalf("valid attestation: %v, %v", got, err)
	}
	tests := map[string][]byte{
		"duplicate":      bytes.Replace(raw, []byte(`"pluginId":`), []byte(`"pluginId":"example.blob","pluginId":`), 1),
		"unknown":        bytes.Replace(raw, []byte(`"pluginId":`), []byte(`"unknown":true,"pluginId":`), 1),
		"wrong commit":   bytes.Replace(raw, []byte(fixture.source.CommitSha), []byte(strings.Repeat("b", 40)), 1),
		"wrong source":   bytes.Replace(raw, []byte(fixture.source.CanonicalSource), []byte("https://github.com/other/plugin"), 1),
		"wrong version":  bytes.Replace(raw, []byte(`"version":"1.0.0"`), []byte(`"version":"1.0.1"`), 1),
		"wrong artifact": bytes.Replace(raw, []byte("site.tar.gz"), []byte("bad.tar.gz"), 1),
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := verifyAttestation(fixture.source, fixture.manifest, manifest, raw)
			if !errors.Is(err, errInventory) {
				t.Fatalf("verifyAttestation() = %v", err)
			}
		})
	}
}

func TestPublishedPackageReusesOnlyVerifiedVersion(t *testing.T) {
	t.Parallel()
	web := siteArchive(t, []webTarEntry{{name: "./index.html", data: "hello"}, {name: "assets/main.js", data: "script"}})
	fixture := signedFixture(t, web, true)
	store := &Store{client: releaseBytes(fixture.assets), root: testPackageRoot(t)}
	pluginRoot := testPluginDirectory(t, store, fixture)
	checkExisting := func() error {
		return store.VerifyLocal(t.Context(), fixture.source, fixture.manifest, fixture.fingerprint, fixture.digest)
	}
	if err := store.publish(t.Context(), pluginRoot, fixture.manifest.Version, fixture.release, checkExisting); err != nil {
		t.Fatal(err)
	}
	if err := checkExisting(); err != nil {
		t.Fatalf("offline verification after publish: %v", err)
	}
	t.Run("wrong fingerprint", func(t *testing.T) {
		err := store.VerifyLocal(t.Context(), fixture.source, fixture.manifest, strings.Repeat("0", 64), fixture.digest)
		if !errors.Is(err, errPublisher) {
			t.Fatalf("wrong fingerprint: %v", err)
		}
	})
	t.Run("wrong reviewed evidence", func(t *testing.T) {
		err := store.VerifyLocal(t.Context(), fixture.source, fixture.manifest, fixture.fingerprint, strings.Repeat("0", 64))
		if !errors.Is(err, errReleaseChanged) {
			t.Fatalf("wrong reviewed attestation: %v", err)
		}
	})
	// A retry uses the protected existing package and does not download assets.
	store.client = nil
	if err := store.publish(t.Context(), pluginRoot, fixture.manifest.Version, fixture.release, checkExisting); err != nil {
		t.Fatalf("idempotent stage: %v", err)
	}
	webIndex := filepath.Join(store.root, fixture.manifest.ID, fixture.manifest.Version, webDirectory, "index.html")
	replaceFixtureFile(t, webIndex, []byte("wrong"))
	if err := checkExisting(); !errors.Is(err, errArtifact) {
		t.Fatalf("unpacked bytes failed to trigger local verification: %v", err)
	}
	if err := store.publish(t.Context(), pluginRoot, fixture.manifest.Version, fixture.release, checkExisting); !errors.Is(err, errVersionConflict) {
		t.Fatalf("preexisting package was silently replaced: %v", err)
	}
}

func TestPublishDiscardsInvalidBytes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		web     []byte
		corrupt bool
	}{
		{"malformed Web archive", []byte("invalid archive"), false},
		{"tampered binary", siteArchive(t, []webTarEntry{{name: "index.html", data: "hello"}}), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fixture := signedFixture(t, tc.web, true)
			if tc.corrupt {
				fixture.assets["driver"] = []byte("tamper backend")
			}
			store := &Store{client: releaseBytes(fixture.assets), root: testPackageRoot(t)}
			pluginRoot := testPluginDirectory(t, store, fixture)
			err := store.publish(t.Context(), pluginRoot, fixture.manifest.Version, fixture.release, func() error {
				t.Fatal("unexpected existing package")
				return nil
			})
			if !errors.Is(err, errArtifact) {
				t.Fatalf("publish() = %v", err)
			}
			entries, err := os.ReadDir(filepath.Join(store.root, fixture.manifest.ID))
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed publication left entries: %v, %v", entries, err)
			}
		})
	}
}

func TestVerifyLocalRejectsSymlinkAndChangedSignature(t *testing.T) {
	t.Parallel()
	fixture := signedFixture(t, siteArchive(t, []webTarEntry{{name: "index.html", data: "hello"}}), false)
	store := &Store{client: releaseBytes(fixture.assets), root: testPackageRoot(t)}
	pluginRoot := testPluginDirectory(t, store, fixture)
	if err := store.publish(t.Context(), pluginRoot, fixture.manifest.Version, fixture.release, nil); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(store.root, fixture.manifest.ID, fixture.manifest.Version)
	verify := func() error {
		return store.VerifyLocal(t.Context(), fixture.source, fixture.manifest, fixture.fingerprint, fixture.digest)
	}
	if err := verify(); err != nil {
		t.Fatalf("verify Web-only package offline: %v", err)
	}
	attestation := filepath.Join(local, attestationName)
	replaceFixtureFile(t, attestation, bytes.TrimSpace(fixture.release.documents[attestationName]))
	if err := verify(); !errors.Is(err, errSignature) {
		t.Fatalf("reformatted but unsigned attestation: %v", err)
	}
	replaceFixtureFile(t, attestation, fixture.release.documents[attestationName])
	publicKey := filepath.Join(local, PublisherFilename)
	if err := os.Rename(publicKey, publicKey+".bak"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(PublisherFilename+".bak", publicKey); err != nil {
		t.Fatal(err)
	}
	if err := verify(); !errors.Is(err, errPackageRoot) {
		t.Fatalf("indirect publisher path: %v", err)
	}
}

func TestRemovePluginOnlyDeletesTheRetiredTree(t *testing.T) {
	t.Parallel()
	fixture := signedFixture(t, siteArchive(t, []webTarEntry{{name: "index.html", data: "hello"}}), true)
	store := &Store{client: releaseBytes(fixture.assets), root: testPackageRoot(t)}
	pluginRoot := testPluginDirectory(t, store, fixture)
	if err := store.publish(t.Context(), pluginRoot, fixture.manifest.Version, fixture.release, nil); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(store.root, "other.plugin")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.RemovePlugin(t.Context(), fixture.manifest.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.RemovePlugin(t.Context(), fixture.manifest.ID); err != nil {
		t.Fatalf("retirement retry: %v", err)
	}
	if err := os.Symlink("other.plugin", filepath.Join(store.root, fixture.manifest.ID)); err != nil {
		t.Fatal(err)
	}
	if err := store.RemovePlugin(t.Context(), fixture.manifest.ID); !errors.Is(err, errPackageRoot) {
		t.Fatalf("symlink removal attempt: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("adjacent plugin was affected: %v", err)
	}
}

func replaceFixtureFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
}

func testPackageRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "packages")
}

func testPluginDirectory(t *testing.T, store *Store, fixture releaseFixture) *os.Root {
	t.Helper()
	root, err := openPackageRoot(store.root, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	plugin, err := privateChild(root, fixture.manifest.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = plugin.Close() })
	return plugin
}

type webTarEntry struct {
	name string
	data string
	kind byte
}

func siteArchive(t *testing.T, entries []webTarEntry) []byte {
	t.Helper()
	var bytes bytes.Buffer
	zipped := gzip.NewWriter(&bytes)
	writer := tar.NewWriter(zipped)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: 0o644, Size: int64(len(entry.data)), Typeflag: entry.kind}
		if entry.kind == tar.TypeSymlink || entry.kind == tar.TypeLink {
			header.Linkname = "index.html"
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(writer, entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := errors.Join(writer.Close(), zipped.Close()); err != nil {
		t.Fatal(err)
	}
	return bytes.Bytes()
}
