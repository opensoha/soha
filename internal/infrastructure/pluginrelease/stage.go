package pluginrelease

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var (
	errReleaseChanged = apperrors.NewBusiness(apperrors.ErrConflict, "plugin_repository_release_changed",
		"The signed release changed after preview. Preview it again.", "签名发行版在预览后发生变化，请重新预览。")
	errVersionConflict = apperrors.NewBusiness(apperrors.ErrConflict, "plugin_repository_package_conflict",
		"The local plugin version already contains different release bytes.", "该本地插件版本已包含不同的发行版内容。")
)

// Stage re-verifies the previewed release and publishes its complete verified
// package with one directory rename. A matching existing package is reusable.
func (store *Store) Stage(
	ctx context.Context, source domainplugin.PluginRepositoryPreviewSource, manifest domainplugin.PluginManifest,
	rawManifest []byte, trustedFingerprint string, trustedPublicKey []byte, expectedAttestationDigest string,
) (string, error) {
	if trustedFingerprint == "" {
		return "", errPublisher
	}
	release, err := store.inspect(ctx, source, manifest, rawManifest, trustedFingerprint, trustedPublicKey)
	if err != nil {
		return "", err
	}
	if release == nil {
		return "", errReleaseRequired
	}
	if !matchesAttestationDigest(release.attestation, expectedAttestationDigest) {
		return "", errReleaseChanged
	}
	root, err := openPackageRoot(store.root, true)
	if err != nil {
		return "", errPackageRoot
	}
	defer func() { _ = root.Close() }()
	pluginRoot, err := privateChild(root, manifest.ID, true)
	if err != nil {
		return "", errPackageRoot
	}
	defer func() { _ = pluginRoot.Close() }()
	checkExisting := func() error {
		return verifyLocalVersion(ctx, pluginRoot, source, manifest, trustedFingerprint, expectedAttestationDigest)
	}
	if err := store.publish(ctx, pluginRoot, manifest.Version, release, checkExisting); err != nil {
		return "", err
	}
	return filepath.Join(store.root, manifest.ID, manifest.Version), nil
}

func (store *Store) publish(
	ctx context.Context, pluginRoot *os.Root, version string, release *packageRelease, checkExisting func() error,
) (err error) {
	if _, statErr := pluginRoot.Lstat(version); !errors.Is(statErr, fs.ErrNotExist) {
		return reusePackage(pluginRoot, checkExisting)
	}
	temp := ".stage-" + strings.ToLower(rand.Text())
	if err := pluginRoot.Mkdir(temp, 0o700); err != nil {
		return errPackageRoot
	}
	removeTemp := true
	defer func() {
		if removeTemp && pluginRoot.RemoveAll(temp) != nil {
			err = errors.Join(err, errPackageRoot)
		}
	}()
	staged, err := privateChild(pluginRoot, temp, false)
	if err != nil {
		return errPackageRoot
	}
	defer func() { _ = staged.Close() }()
	if err := store.writePackage(ctx, staged, release); err != nil {
		return err
	}
	if err := syncDirectory(staged, "."); err != nil {
		return errPackageRoot
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// A complete version is nonempty and cannot be overwritten by a rename.
	if err := pluginRoot.Rename(temp, version); err != nil {
		if _, statErr := pluginRoot.Lstat(version); statErr == nil {
			return reusePackage(pluginRoot, checkExisting)
		}
		return errPackageRoot
	}
	removeTemp = false
	if err := syncDirectory(pluginRoot, "."); err != nil {
		return errPackageRoot
	}
	return nil
}

func reusePackage(pluginRoot *os.Root, checkExisting func() error) error {
	if err := checkExisting(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return errVersionConflict
	}
	if err := syncDirectory(pluginRoot, "."); err != nil {
		return errPackageRoot
	}
	return nil
}

func (store *Store) writePackage(ctx context.Context, staged *os.Root, release *packageRelease) error {
	for name, content := range release.documents {
		err := writePrivateFile(staged, name, func(output io.Writer) error {
			_, err := output.Write(content)
			return err
		})
		if err != nil {
			return errPackageRoot
		}
	}
	for _, artifact := range release.artifacts {
		err := writePrivateFile(staged, artifact.Name, func(output io.Writer) error {
			digest := sha256.New()
			err := store.client.CopyReleaseAsset(ctx, release.release, artifact.Name, artifact.ByteLength, io.MultiWriter(output, digest))
			if err != nil {
				if invalidAssetLength(err) {
					return errArtifact
				}
				return err
			}
			if hex.EncodeToString(digest.Sum(nil)) != artifact.Sha256 {
				return errArtifact
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	if web := release.attestation.Components.Web; web != nil {
		return extractWeb(ctx, staged, web.Name)
	}
	return nil
}

func matchesAttestationDigest(attestation *sohaapi.PluginReleaseAttestation, expected string) bool {
	raw, err := json.Marshal(attestation)
	return err == nil && digestBytes(raw) == expected
}
