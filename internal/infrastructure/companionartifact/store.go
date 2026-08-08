package companionartifact

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	domaincompanion "github.com/opensoha/soha/internal/domain/companion"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	maxExpandedBytes = int64(512 << 20)
	maxArchiveFiles  = 512
	maxPathDepth     = 8
)

type Store struct {
	root            string
	maxPackageBytes int64
}

func NewStore(root string, maxPackageBytes int64) (*Store, error) {
	root = strings.TrimSpace(root)
	if root == "" || maxPackageBytes <= 0 {
		return nil, fmt.Errorf("%w: companion artifact store configuration is required", apperrors.ErrInvalidArgument)
	}
	return &Store{root: root, maxPackageBytes: maxPackageBytes}, nil
}

func (s *Store) Install(ctx context.Context, descriptor domaincompanion.PackageDescriptor, pack domaincompanion.PackManifest, source io.Reader) (string, error) {
	if err := os.MkdirAll(filepath.Join(s.root, "sha256"), 0o750); err != nil {
		return "", fmt.Errorf("create companion artifact store: %w", err)
	}
	stage, err := os.MkdirTemp(s.root, ".stage-")
	if err != nil {
		return "", fmt.Errorf("create companion artifact stage: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()

	archivePath := filepath.Join(stage, "package")
	archive, err := os.OpenFile(archivePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create companion package stage: %w", err)
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(archive, hash), io.LimitReader(source, s.maxPackageBytes+1))
	closeErr := archive.Close()
	if copyErr != nil {
		return "", fmt.Errorf("stage companion package: %w", copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close staged companion package: %w", closeErr)
	}
	if written > s.maxPackageBytes || written != descriptor.SizeBytes {
		return "", fmt.Errorf("%w: companion package size mismatch", apperrors.ErrInvalidArgument)
	}
	digest := "sha256:" + hex.EncodeToString(hash.Sum(nil))
	if digest != strings.TrimSpace(descriptor.Sha256) {
		return "", fmt.Errorf("%w: companion package checksum mismatch", apperrors.ErrInvalidArgument)
	}

	payload := filepath.Join(stage, "payload")
	if err := os.Mkdir(payload, 0o750); err != nil {
		return "", fmt.Errorf("create companion payload stage: %w", err)
	}
	assets, err := declaredAssets(pack)
	if err != nil {
		return "", err
	}
	switch string(descriptor.ContentType) {
	case "application/zip":
		err = extractZip(ctx, archivePath, payload, assets)
	case "application/gzip":
		err = extractTar(ctx, archivePath, payload, assets, true)
	case "application/x-tar":
		err = extractTar(ctx, archivePath, payload, assets, false)
	default:
		err = fmt.Errorf("%w: unsupported companion package content type", apperrors.ErrInvalidArgument)
	}
	if err != nil {
		return "", err
	}
	if _, ok := assets[pack.EntryAsset]; !ok {
		return "", fmt.Errorf("%w: companion entry asset is not declared", apperrors.ErrInvalidArgument)
	}

	hexDigest := strings.TrimPrefix(digest, "sha256:")
	finalPath := filepath.Join(s.root, "sha256", hexDigest)
	if _, err := os.Stat(finalPath); err == nil {
		return digest, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("stat companion artifact: %w", err)
	}
	if err := os.Rename(payload, finalPath); err != nil {
		if _, statErr := os.Stat(finalPath); statErr == nil {
			return digest, nil
		}
		return "", fmt.Errorf("publish companion artifact: %w", err)
	}
	return digest, nil
}

func (s *Store) Open(storageDigest, assetPath string) (io.ReadCloser, error) {
	if !validDigest(storageDigest) {
		return nil, fmt.Errorf("%w: invalid companion artifact digest", apperrors.ErrInvalidArgument)
	}
	normalized, err := safeAssetPath(assetPath)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(filepath.Join(s.root, "sha256", strings.TrimPrefix(storageDigest, "sha256:")))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: companion artifact not found", apperrors.ErrNotFound)
		}
		return nil, err
	}
	defer func() { _ = root.Close() }()
	file, err := root.Open(normalized)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: companion asset not found", apperrors.ErrNotFound)
	}
	return file, err
}

func declaredAssets(pack domaincompanion.PackManifest) (map[string]domaincompanion.Asset, error) {
	assets := make(map[string]domaincompanion.Asset, len(pack.Assets))
	for _, asset := range pack.Assets {
		normalized, err := safeAssetPath(asset.Path)
		if err != nil {
			return nil, err
		}
		if normalized != asset.Path {
			return nil, fmt.Errorf("%w: companion asset path must be normalized", apperrors.ErrInvalidArgument)
		}
		if _, exists := assets[normalized]; exists {
			return nil, fmt.Errorf("%w: duplicate companion asset path", apperrors.ErrInvalidArgument)
		}
		assets[normalized] = asset
	}
	return assets, nil
}

func extractZip(ctx context.Context, archivePath, target string, assets map[string]domaincompanion.Asset) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("%w: open companion zip", apperrors.ErrInvalidArgument)
	}
	defer func() { _ = reader.Close() }()
	if len(reader.File) > maxArchiveFiles {
		return fmt.Errorf("%w: companion archive contains too many entries", apperrors.ErrInvalidArgument)
	}
	seen := map[string]bool{}
	var expanded int64
	for _, entry := range reader.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		name, err := safeAssetPath(entry.Name)
		if err != nil {
			return err
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		if entry.Mode()&os.ModeSymlink != 0 || !entry.Mode().IsRegular() {
			return fmt.Errorf("%w: companion archive links and special files are not allowed", apperrors.ErrInvalidArgument)
		}
		asset, ok := assets[name]
		if !ok || seen[name] {
			return fmt.Errorf("%w: companion archive contains undeclared or duplicate asset", apperrors.ErrInvalidArgument)
		}
		expanded += int64(entry.UncompressedSize64)
		if expanded > maxExpandedBytes || int64(entry.UncompressedSize64) != asset.SizeBytes {
			return fmt.Errorf("%w: companion asset size mismatch", apperrors.ErrInvalidArgument)
		}
		source, err := entry.Open()
		if err != nil {
			return err
		}
		err = writeAsset(target, name, asset, source)
		_ = source.Close()
		if err != nil {
			return err
		}
		seen[name] = true
	}
	return requireAllAssets(assets, seen)
}

func extractTar(ctx context.Context, archivePath, target string, assets map[string]domaincompanion.Asset, compressed bool) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	var reader io.Reader = file
	if compressed {
		gzipReader, err := gzip.NewReader(file)
		if err != nil {
			return fmt.Errorf("%w: open companion gzip", apperrors.ErrInvalidArgument)
		}
		defer func() { _ = gzipReader.Close() }()
		reader = gzipReader
	}
	tarReader := tar.NewReader(reader)
	seen := map[string]bool{}
	var expanded int64
	entries := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("%w: read companion tar", apperrors.ErrInvalidArgument)
		}
		entries++
		if entries > maxArchiveFiles {
			return fmt.Errorf("%w: companion archive contains too many entries", apperrors.ErrInvalidArgument)
		}
		name, err := safeAssetPath(header.Name)
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			return fmt.Errorf("%w: companion archive links and special files are not allowed", apperrors.ErrInvalidArgument)
		}
		asset, ok := assets[name]
		if !ok || seen[name] || header.Size != asset.SizeBytes {
			return fmt.Errorf("%w: companion archive contains undeclared, duplicate, or invalid asset", apperrors.ErrInvalidArgument)
		}
		expanded += header.Size
		if expanded > maxExpandedBytes {
			return fmt.Errorf("%w: companion archive exceeds expanded size limit", apperrors.ErrInvalidArgument)
		}
		if err := writeAsset(target, name, asset, io.LimitReader(tarReader, header.Size)); err != nil {
			return err
		}
		seen[name] = true
	}
	return requireAllAssets(assets, seen)
}

func writeAsset(target, name string, asset domaincompanion.Asset, source io.Reader) error {
	destination := filepath.Join(target, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(source, asset.SizeBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != asset.SizeBytes || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != asset.Sha256 {
		return fmt.Errorf("%w: companion asset checksum mismatch", apperrors.ErrInvalidArgument)
	}
	if string(asset.ContentType) == "image/svg+xml" {
		if err := validateSVG(destination); err != nil {
			return err
		}
	}
	return nil
}

func validateSVG(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	decoder := xml.NewDecoder(io.LimitReader(file, 8<<20))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: invalid companion SVG", apperrors.ErrInvalidArgument)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		name := strings.ToLower(start.Name.Local)
		if name == "script" || name == "foreignobject" {
			return fmt.Errorf("%w: active SVG content is not allowed", apperrors.ErrInvalidArgument)
		}
		for _, attribute := range start.Attr {
			attributeName := strings.ToLower(attribute.Name.Local)
			value := strings.TrimSpace(strings.ToLower(attribute.Value))
			if strings.HasPrefix(attributeName, "on") || (attributeName == "href" && (strings.Contains(value, ":") || strings.HasPrefix(value, "//"))) {
				return fmt.Errorf("%w: active or external SVG references are not allowed", apperrors.ErrInvalidArgument)
			}
		}
	}
}

func requireAllAssets(assets map[string]domaincompanion.Asset, seen map[string]bool) error {
	if len(assets) != len(seen) {
		return fmt.Errorf("%w: companion archive is missing declared assets", apperrors.ErrInvalidArgument)
	}
	return nil
}

func safeAssetPath(value string) (string, error) {
	if value == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("%w: invalid companion asset path", apperrors.ErrInvalidArgument)
	}
	normalized := path.Clean(value)
	if normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") || len(normalized) > 512 || strings.Count(normalized, "/") >= maxPathDepth {
		return "", fmt.Errorf("%w: invalid companion asset path", apperrors.ErrInvalidArgument)
	}
	return normalized, nil
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}
