package sohacli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/opensoha/soha/contracts/apphosting/sourceignore"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

const (
	hostLocalMaxFileCount         = 10000
	hostLocalMaxUncompressedBytes = 512 << 20
	hostMaxSamplePaths            = 20
	hostMaxRelativePathBytes      = 1024
)

type hostStaticBundle struct {
	Dir               string
	AbsDir            string
	Kind              sohaapi.HostedAppKind
	Archive           []byte
	BundleSHA256      string
	CompressedBytes   int64
	UncompressedBytes int64
	FileCount         int32
	HasIndexHTML      bool
	SamplePaths       []string
}

type hostBundleEntry struct {
	relPath  string
	fullPath string
	size     int64
}

func buildHostStaticBundle(dir string) (hostStaticBundle, error) {
	return buildHostBundle(dir, sohaapi.HostedAppKindStatic)
}

func buildHostDynamicBundle(dir string) (hostStaticBundle, error) {
	return buildHostBundle(dir, sohaapi.HostedAppKindDynamic)
}

func buildHostBundle(dir string, kind sohaapi.HostedAppKind) (hostStaticBundle, error) {
	targetDir := strings.TrimSpace(dir)
	if targetDir == "" {
		targetDir = "."
	}
	rootInfo, err := os.Lstat(targetDir)
	if err != nil {
		return hostStaticBundle{}, fmt.Errorf("inspect bundle directory %q: %w", targetDir, err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return hostStaticBundle{}, fmt.Errorf("bundle directory %q must not be a symlink", targetDir)
	}
	if !rootInfo.IsDir() {
		return hostStaticBundle{}, fmt.Errorf("bundle path %q is not a directory", targetDir)
	}
	absDir, err := filepath.Abs(targetDir)
	if err != nil {
		return hostStaticBundle{}, fmt.Errorf("resolve bundle directory %q: %w", targetDir, err)
	}

	var ignores *sourceignore.Rules
	if kind == sohaapi.HostedAppKindDynamic {
		ignores = new(sourceignore.Rules)
	}
	entries, err := scanHostBundleDirectory(absDir, "", kind, ignores)
	if err != nil {
		return hostStaticBundle{}, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].relPath < entries[j].relPath
	})

	hasIndex := false
	var totalUncompressed int64
	seen := make(map[string]struct{}, len(entries))
	samplePaths := make([]string, 0, minInt(len(entries), hostMaxSamplePaths))
	for _, entry := range entries {
		if _, duplicate := seen[entry.relPath]; duplicate {
			return hostStaticBundle{}, fmt.Errorf("bundle contains duplicate normalized path %q", entry.relPath)
		}
		seen[entry.relPath] = struct{}{}
		if entry.relPath == "index.html" {
			hasIndex = true
		}
		totalUncompressed += entry.size
		if totalUncompressed > hostLocalMaxUncompressedBytes {
			return hostStaticBundle{}, fmt.Errorf("bundle exceeds local safety limit of %d MiB uncompressed; verify you are packaging a built static output directory", hostLocalMaxUncompressedBytes>>20)
		}
		if len(samplePaths) < hostMaxSamplePaths {
			samplePaths = append(samplePaths, entry.relPath)
		}
	}
	if kind == sohaapi.HostedAppKindStatic && !hasIndex {
		return hostStaticBundle{}, fmt.Errorf("bundle directory %q is missing root index.html; build your static frontend first and pass the output directory (for example: soha host deploy ./dist)", targetDir)
	}
	if kind == sohaapi.HostedAppKindDynamic {
		if err := validateHostDynamicBundle(entries); err != nil {
			return hostStaticBundle{}, err
		}
	}

	archiveBytes, actualUncompressed, err := writeDeterministicTarGz(entries)
	if err != nil {
		return hostStaticBundle{}, err
	}
	if len(entries) > hostLocalMaxFileCount {
		return hostStaticBundle{}, fmt.Errorf("bundle exceeds local file count limit (%d > %d)", len(entries), hostLocalMaxFileCount)
	}
	fileCount := int32(len(entries)) // #nosec G115 -- bounded above by hostLocalMaxFileCount (10000)
	sum := sha256.Sum256(archiveBytes)
	return hostStaticBundle{
		Dir:               targetDir,
		AbsDir:            absDir,
		Kind:              kind,
		Archive:           archiveBytes,
		BundleSHA256:      hex.EncodeToString(sum[:]),
		CompressedBytes:   int64(len(archiveBytes)),
		UncompressedBytes: actualUncompressed,
		FileCount:         fileCount,
		HasIndexHTML:      hasIndex,
		SamplePaths:       samplePaths,
	}, nil
}

func validateHostDynamicBundle(entries []hostBundleEntry) error {
	byPath := make(map[string]hostBundleEntry, len(entries))
	for _, entry := range entries {
		byPath[entry.relPath] = entry
	}

	configEntry, ok := byPath["soha-host.json"]
	if !ok {
		return fmt.Errorf("dynamic source is missing root soha-host.json; add {\"template\":\"node\"|\"python\",\"database\":true|false}")
	}
	raw, err := readRegularBundleFile(configEntry.fullPath, configEntry.relPath)
	if err != nil {
		return err
	}
	var config struct {
		Template      string `json:"template"`
		Database      *bool  `json:"database"`
		SchemaVersion int    `json:"schemaVersion"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return fmt.Errorf("invalid soha-host.json: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("invalid soha-host.json: trailing content is not allowed")
	}
	if config.Database == nil {
		return fmt.Errorf("invalid soha-host.json: database must be true or false")
	}
	if (!*config.Database && config.SchemaVersion != 0) || config.SchemaVersion < 0 || config.SchemaVersion > 2 {
		return fmt.Errorf("invalid soha-host.json: schemaVersion requires a database and must be 1 or 2 if specified")
	}

	var required []string
	switch config.Template {
	case "node":
		required = []string{"server.js", "package.json", "package-lock.json"}
	case "python":
		required = []string{"app.py", "requirements.txt"}
	default:
		return fmt.Errorf("invalid soha-host.json: template must be node or python")
	}
	missing := make([]string, 0, len(required))
	for _, name := range required {
		if _, exists := byPath[name]; !exists {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("dynamic %s source is missing required root file(s): %s", config.Template, strings.Join(missing, ", "))
	}
	return nil
}

func scanHostBundleDirectory(currentAbs, currentRel string, kind sohaapi.HostedAppKind, ignores *sourceignore.Rules) ([]hostBundleEntry, error) {
	if ignores != nil {
		if err := loadHostSourceIgnore(currentAbs, currentRel, ignores); err != nil {
			return nil, err
		}
	}
	dirEntries, err := os.ReadDir(currentAbs)
	if err != nil {
		return nil, fmt.Errorf("read directory %q: %w", firstNonEmptyString(currentRel, "."), err)
	}
	var collected []hostBundleEntry
	for _, item := range dirEntries {
		name := item.Name()
		if isIgnoredHostBundleEntry(name, kind) || (ignores != nil && name == ".gitignore") {
			continue
		}
		relPath := name
		if currentRel != "" {
			relPath = currentRel + "/" + name
		}
		if err := validateHostBundleRelPath(relPath); err != nil {
			return nil, err
		}
		fullPath := filepath.Join(currentAbs, name)
		info, err := os.Lstat(fullPath)
		if err != nil {
			return nil, fmt.Errorf("inspect %q: %w", relPath, err)
		}
		if ignores != nil && ignores.Ignored(relPath, info.IsDir()) {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("bundle contains symlink %q; remove symlinks or copy regular files into the output directory before deploying", relPath)
		}
		if reason, sensitive := classifySensitiveHostBundleFile(relPath); sensitive {
			return nil, fmt.Errorf("bundle contains sensitive file %q (%s); remove secret or credential files before deploying", relPath, reason)
		}
		if info.IsDir() {
			nested, err := scanHostBundleDirectory(fullPath, relPath, kind, ignores)
			if err != nil {
				return nil, err
			}
			collected = append(collected, nested...)
			if len(collected) > hostLocalMaxFileCount {
				return nil, fmt.Errorf("bundle exceeds local safety limit of %d files; verify node_modules or build caches are excluded", hostLocalMaxFileCount)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("bundle contains non-regular file %q (mode %s); only regular files are supported", relPath, info.Mode().String())
		}
		collected = append(collected, hostBundleEntry{
			relPath:  relPath,
			fullPath: fullPath,
			size:     info.Size(),
		})
		if len(collected) > hostLocalMaxFileCount {
			return nil, fmt.Errorf("bundle exceeds local safety limit of %d files; verify node_modules or build caches are excluded", hostLocalMaxFileCount)
		}
	}
	return collected, nil
}

func loadHostSourceIgnore(directory, relativeDirectory string, rules *sourceignore.Rules) error {
	relative := path.Join(relativeDirectory, ".gitignore")
	raw, err := readRegularBundleFileWithinLimit(filepath.Join(directory, ".gitignore"), relative, sourceignore.MaxFileBytes)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := rules.Add(relativeDirectory, raw); err != nil {
		return fmt.Errorf("read ignore rules %q: %w", relative, err)
	}
	return nil
}

func isIgnoredHostBundleEntry(name string, kind sohaapi.HostedAppKind) bool {
	switch name {
	case ".git", "node_modules", ".DS_Store":
		return true
	case ".venv", "venv", "__pycache__", "deps":
		return kind == sohaapi.HostedAppKindDynamic
	default:
		return false
	}
}

func validateHostBundleRelPath(relPath string) error {
	if relPath == "" || len(relPath) > hostMaxRelativePathBytes {
		return fmt.Errorf("invalid bundle file path %q: path must be 1..%d bytes", relPath, hostMaxRelativePathBytes)
	}
	if strings.HasPrefix(relPath, "/") || strings.Contains(relPath, `\`) {
		return fmt.Errorf("unsafe bundle file path %q: path must be relative with forward slashes", relPath)
	}
	for _, r := range relPath {
		if r == 0 || unicode.IsControl(r) {
			return fmt.Errorf("unsafe bundle file path %q: control characters are not allowed", relPath)
		}
	}
	for _, segment := range strings.Split(relPath, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("unsafe bundle file path %q: path traversal segments are not allowed", relPath)
		}
	}
	if path.Clean(relPath) != relPath {
		return fmt.Errorf("unsafe bundle file path %q: path is not canonical", relPath)
	}
	return nil
}

func classifySensitiveHostBundleFile(relPath string) (string, bool) {
	base := strings.ToLower(path.Base(relPath))
	switch {
	case base == ".env" || strings.HasPrefix(base, ".env.") || base == ".envrc":
		return "dotenv environment file", true
	case strings.HasSuffix(base, ".pem"):
		return "PEM certificate or key file", true
	case strings.HasSuffix(base, ".key"):
		return "private key file", true
	case strings.HasSuffix(base, ".p12") || strings.HasSuffix(base, ".pfx"):
		return "PKCS#12 key archive", true
	case strings.HasPrefix(base, "id_rsa") || strings.HasPrefix(base, "id_ed25519") || strings.HasPrefix(base, "id_ecdsa") || strings.HasPrefix(base, "id_dsa"):
		return "SSH key material", true
	case base == "credentials.json" || base == ".npmrc" || base == ".netrc":
		return "credential configuration file", true
	default:
		return "", false
	}
}

func writeDeterministicTarGz(entries []hostBundleEntry) ([]byte, int64, error) {
	var buf bytes.Buffer
	gzWriter, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, 0, err
	}
	gzWriter.Name = ""
	gzWriter.Comment = ""
	gzWriter.ModTime = time.Unix(0, 0).UTC()
	gzWriter.OS = 255

	tarWriter := tar.NewWriter(gzWriter)
	var totalBytes int64
	epoch := time.Unix(0, 0).UTC()

	for _, entry := range entries {
		// #nosec G304 -- entry.fullPath is verified via Lstat and canonical relative traversal under absDir.
		raw, err := readRegularBundleFile(entry.fullPath, entry.relPath)
		if err != nil {
			_ = tarWriter.Close()
			_ = gzWriter.Close()
			return nil, 0, err
		}
		totalBytes += int64(len(raw))
		if totalBytes > hostLocalMaxUncompressedBytes {
			_ = tarWriter.Close()
			_ = gzWriter.Close()
			return nil, 0, fmt.Errorf("bundle exceeds local safety limit of %d MiB uncompressed", hostLocalMaxUncompressedBytes>>20)
		}
		if err := inspectHostBundleContent(entry.relPath, raw); err != nil {
			_ = tarWriter.Close()
			_ = gzWriter.Close()
			return nil, 0, err
		}
		header := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     entry.relPath,
			Size:     int64(len(raw)),
			Mode:     0o644,
			Uid:      0,
			Gid:      0,
			Uname:    "",
			Gname:    "",
			ModTime:  epoch,
			Format:   tar.FormatPAX,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			_ = tarWriter.Close()
			_ = gzWriter.Close()
			return nil, 0, fmt.Errorf("archive header %q: %w", entry.relPath, err)
		}
		if _, err := tarWriter.Write(raw); err != nil {
			_ = tarWriter.Close()
			_ = gzWriter.Close()
			return nil, 0, fmt.Errorf("archive write %q: %w", entry.relPath, err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		_ = gzWriter.Close()
		return nil, 0, fmt.Errorf("finalize tar archive: %w", err)
	}
	if err := gzWriter.Close(); err != nil {
		return nil, 0, fmt.Errorf("finalize gzip archive: %w", err)
	}
	return buf.Bytes(), totalBytes, nil
}

func readRegularBundleFile(fullPath, relPath string) ([]byte, error) {
	return readRegularBundleFileWithinLimit(fullPath, relPath, hostLocalMaxUncompressedBytes)
}

func readRegularBundleFileWithinLimit(fullPath, relPath string, maxBytes int64) ([]byte, error) {
	info, err := os.Lstat(fullPath)
	if err != nil {
		return nil, fmt.Errorf("inspect %q: %w", relPath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("bundle file %q changed to a symlink or non-regular file during packaging", relPath)
	}
	// #nosec G304 -- fullPath is validated under the user-selected bundle directory.
	file, err := os.Open(fullPath)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", relPath, err)
	}
	defer func() { _ = file.Close() }()
	openInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", relPath, err)
	}
	if !os.SameFile(info, openInfo) || !openInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("bundle file %q changed while reading", relPath)
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", relPath, err)
	}
	if int64(len(raw)) > maxBytes {
		return nil, fmt.Errorf("file %q exceeds local safety limit", relPath)
	}
	return raw, nil
}

func inferDefaultHostedAppName(absDir string) string {
	cleaned := filepath.Clean(absDir)
	base := strings.TrimSpace(filepath.Base(cleaned))
	switch strings.ToLower(base) {
	case "dist", "build", "out", "public", "_site":
		parent := strings.TrimSpace(filepath.Base(filepath.Dir(cleaned)))
		if parent != "" && parent != "." && parent != string(filepath.Separator) {
			base = parent
		}
	}
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "hosted-app"
	}
	if len(base) > 128 {
		base = base[:128]
	}
	return base
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
