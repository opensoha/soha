package pluginrelease

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"runtime"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
)

// VerifyLocal revalidates the pinned, signed version entirely from its local
// protected files, including the backend bytes and unpacked Web tree.
func (store *Store) VerifyLocal(
	ctx context.Context, source domainplugin.PluginRepositoryPreviewSource, manifest domainplugin.PluginManifest,
	trustedFingerprint, expectedAttestationDigest string,
) error {
	if trustedFingerprint == "" {
		return errPublisher
	}
	root, err := openPackageRoot(store.root, false)
	if err != nil {
		return errPackageRoot
	}
	defer func() { _ = root.Close() }()
	pluginRoot, err := privateChild(root, manifest.ID, false)
	if err != nil {
		return errPackageRoot
	}
	defer func() { _ = pluginRoot.Close() }()
	return verifyLocalVersion(ctx, pluginRoot, source, manifest, trustedFingerprint, expectedAttestationDigest)
}

func verifyLocalVersion(
	ctx context.Context, pluginRoot *os.Root, source domainplugin.PluginRepositoryPreviewSource,
	manifest domainplugin.PluginManifest, fingerprint, expectedAttestationDigest string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	version, err := privateChild(pluginRoot, manifest.Version, false)
	if err != nil {
		return errPackageRoot
	}
	defer func() { _ = version.Close() }()
	attestation, err := localAttestation(version, source, manifest, fingerprint)
	if err != nil {
		return err
	}
	if !matchesAttestationDigest(attestation, expectedAttestationDigest) {
		return errReleaseChanged
	}
	artifacts, err := packageArtifacts(attestation.Components, runtime.GOOS+"/"+runtime.GOARCH)
	if err != nil {
		return err
	}
	for _, asset := range artifacts {
		if err := verifyLocalAsset(ctx, version, asset); err != nil {
			return err
		}
	}
	if web := attestation.Components.Web; web != nil {
		return verifyLocalWeb(ctx, version, web.Name)
	}
	return nil
}

func localAttestation(
	version *os.Root, source domainplugin.PluginRepositoryPreviewSource, manifest domainplugin.PluginManifest, fingerprint string,
) (*sohaapi.PluginReleaseAttestation, error) {
	pem, err := readPrivateFile(version, PublisherFilename, maxPublisherPEM)
	if err != nil {
		return nil, errPackageRoot
	}
	key, err := parsePublisher(pem)
	if err != nil {
		return nil, err
	}
	if fingerprint != digestBytes(key) {
		return nil, errPublisher
	}
	attestation, err := readPrivateFile(version, attestationName, maxDocument)
	if err != nil {
		return nil, errPackageRoot
	}
	signature, err := readPrivateFile(version, signatureName, ed25519.SignatureSize)
	if err != nil || !ed25519.Verify(key, attestation, signature) {
		return nil, errSignature
	}
	rawManifest, err := readPrivateFile(version, manifestName, maxDocument)
	if err != nil {
		return nil, errPackageRoot
	}
	return verifyAttestation(source, manifest, rawManifest, attestation)
}

func verifyLocalAsset(ctx context.Context, version *os.Root, asset sohaapi.PluginReleaseArtifact) error {
	file, err := openPrivateFile(version, asset.Name)
	if err != nil {
		return errPackageRoot
	}
	defer func() { _ = file.Close() }()
	sum, err := fileDigest(ctx, file, asset.ByteLength)
	if err != nil {
		return err
	}
	if hex.EncodeToString(sum) != asset.Sha256 {
		return errArtifact
	}
	return nil
}

func fileDigest(ctx context.Context, file *os.File, size int64) ([]byte, error) {
	stat, err := file.Stat()
	if err != nil || stat.Size() != size {
		return nil, errArtifact
	}
	digest := sha256.New()
	read, err := io.Copy(digest, io.LimitReader(cancelReader{ctx, file}, size+1))
	if err != nil {
		return nil, errArchiveRead(err)
	}
	if read != size {
		return nil, errArtifact
	}
	return digest.Sum(nil), nil
}

func verifyLocalWeb(ctx context.Context, version *os.Root, archiveName string) error {
	archive, err := openPrivateFile(version, archiveName)
	if err != nil {
		return errPackageRoot
	}
	defer func() { _ = archive.Close() }()
	web, err := privateChild(version, webDirectory, false)
	if err != nil {
		return errPackageRoot
	}
	defer func() { _ = web.Close() }()
	expected, err := readWebArchive(ctx, archive, func(entry webPath, input io.Reader) error {
		return verifyWebPath(ctx, web, entry, input)
	})
	if err != nil {
		return err
	}
	// Detect extra entries, including symlinks, that were absent from the release.
	err = fs.WalkDir(web.FS(), ".", func(name string, _ fs.DirEntry, err error) error {
		if err != nil {
			return errPackageRoot
		}
		if name != "." {
			expected--
			if expected < 0 {
				return errArtifact
			}
		}
		return ctx.Err()
	})
	if err == nil && expected != 0 {
		return errArtifact
	}
	return err
}

func verifyWebPath(ctx context.Context, web *os.Root, entry webPath, input io.Reader) error {
	if entry.dir {
		info, err := web.Lstat(entry.name)
		if err != nil || !protectedDirectory(info, true) {
			return errArtifact
		}
		return nil
	}
	file, err := openPrivateFile(web, entry.name)
	if err != nil {
		return errArtifact
	}
	defer func() { _ = file.Close() }()
	digest, err := fileDigest(ctx, file, entry.size)
	if err != nil {
		return err
	}
	expected := sha256.New()
	if _, err := io.Copy(expected, input); err != nil {
		return errArchiveRead(err)
	}
	if !bytes.Equal(digest, expected.Sum(nil)) {
		return errArtifact
	}
	return nil
}
