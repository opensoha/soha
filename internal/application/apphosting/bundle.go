package apphosting

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	appcontentdlp "github.com/opensoha/soha/internal/application/contentdlp"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	domaincontentdlp "github.com/opensoha/soha/internal/domain/contentdlp"
)

const (
	inMemorySpoolThreshold int64 = 4 << 20
	maxDLPInspectFileBytes       = 8 << 20
	maxTotalDLPTextBytes   int64 = 8 << 20
	dlpChunkWindowBytes          = 240 * 1024
	dlpChunkOverlapBytes         = 4 * 1024
)

var staticBundleDLPPolicy = domaincontentdlp.Content{
	Enabled:          true,
	Mode:             "block",
	MinimumDataLevel: "confidential",
	Detectors:        []string{"private_key", "aws_access_key", "credential_assignment"},
	Rules:            []domaincontentdlp.Rule{},
}

var textContentTypesByExt = map[string]string{
	".html":        "text/html; charset=utf-8",
	".htm":         "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".cjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".json":        "application/json",
	".map":         "application/json",
	".webmanifest": "application/manifest+json",
	".svg":         "image/svg+xml",
	".txt":         "text/plain; charset=utf-8",
	".xml":         "application/xml",
	".md":          "text/markdown; charset=utf-8",
	".csv":         "text/csv; charset=utf-8",
	".yaml":        "application/yaml",
	".yml":         "application/yaml",
	".toml":        "application/toml",
}

var binaryContentTypesByExt = map[string]string{
	".wasm":  "application/wasm",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".avif":  "image/avif",
	".ico":   "image/x-icon",
	".woff2": "font/woff2",
	".woff":  "font/woff",
	".ttf":   "font/ttf",
	".otf":   "font/otf",
}

var nestedArchiveExts = map[string]struct{}{
	".zip": {}, ".tar": {}, ".gz": {}, ".tgz": {}, ".bz2": {},
	".xz": {}, ".7z": {}, ".rar": {}, ".jar": {}, ".war": {},
}

type extractedBundleFile struct {
	Path        string
	ContentType string
	Data        []byte
}

type validatedBundle struct {
	Files      []extractedBundleFile
	TotalBytes int64
}

type spooledBundle struct {
	sha256Hex       string
	compressedBytes int64
	mem             []byte
	tempFile        *os.File
}

func (b *spooledBundle) OpenReader() (io.Reader, error) {
	if b.tempFile != nil {
		if _, err := b.tempFile.Seek(0, io.SeekStart); err != nil {
			return nil, fmt.Errorf("rewind spooled bundle: %w", err)
		}
		return b.tempFile, nil
	}
	return bytes.NewReader(b.mem), nil
}

func (b *spooledBundle) Close() {
	if b != nil && b.tempFile != nil {
		name := b.tempFile.Name()
		_ = b.tempFile.Close()
		_ = os.Remove(name)
		b.tempFile = nil
	}
}

func spoolCompressedBundle(src io.Reader, maxCompressedBytes int64) (*spooledBundle, error) {
	if src == nil {
		return nil, newBundleRejectedError("empty_bundle", "revision bundle stream is required", "必须上传版本压缩包")
	}
	limited := io.LimitReader(src, maxCompressedBytes+1)
	hasher := sha256.New()
	var mem bytes.Buffer
	var tempFile *os.File
	var total int64

	buf := make([]byte, 32*1024)
	cleanup := func() {
		if tempFile != nil {
			name := tempFile.Name()
			_ = tempFile.Close()
			_ = os.Remove(name)
		}
	}

	for {
		n, readErr := limited.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			total += int64(n)
			if total > maxCompressedBytes {
				cleanup()
				return nil, newPayloadTooLargeError(
					fmt.Sprintf("compressed bundle exceeds maximum size of %d bytes", maxCompressedBytes),
					fmt.Sprintf("压缩包大小超过平台上限（%d 字节）", maxCompressedBytes),
				)
			}
			_, _ = hasher.Write(chunk)
			if err := appendSpoolChunk(&mem, &tempFile, chunk, total); err != nil {
				cleanup()
				return nil, err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			cleanup()
			return nil, newBundleRejectedError("bundle_read_failed", "failed to read uploaded gzip stream", "读取上传压缩流失败")
		}
	}
	if total == 0 {
		cleanup()
		return nil, newBundleRejectedError("empty_bundle", "uploaded gzip bundle is empty", "上传的压缩包为空")
	}
	return &spooledBundle{
		sha256Hex:       hex.EncodeToString(hasher.Sum(nil)),
		compressedBytes: total,
		mem:             mem.Bytes(),
		tempFile:        tempFile,
	}, nil
}

func appendSpoolChunk(mem *bytes.Buffer, tempFile **os.File, chunk []byte, total int64) error {
	if *tempFile == nil && total > inMemorySpoolThreshold {
		created, err := os.CreateTemp("", "soha-apphosting-bundle-*.tar.gz")
		if err != nil {
			return fmt.Errorf("create bundle spool file: %w", err)
		}
		if mem.Len() > 0 {
			if _, err := created.Write(mem.Bytes()); err != nil {
				_ = created.Close()
				_ = os.Remove(created.Name())
				return fmt.Errorf("flush bundle spool file: %w", err)
			}
			mem.Reset()
		}
		*tempFile = created
	}
	if *tempFile != nil {
		_, err := (*tempFile).Write(chunk)
		return err
	}
	_, err := mem.Write(chunk)
	return err
}

func extractAndValidateStaticBundle(ctx context.Context, archive io.Reader, settings domainapphosting.Settings) (*validatedBundle, error) {
	return extractAndValidateBundle(ctx, archive, settings, true)
}

func extractAndValidateBundle(ctx context.Context, archive io.Reader, settings domainapphosting.Settings, static bool) (*validatedBundle, error) {
	gz, err := gzip.NewReader(archive)
	if err != nil {
		return nil, newBundleRejectedError("invalid_gzip", "uploaded payload is not a valid gzip stream", "上传文件不是合法的 gzip 压缩流")
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	seenPaths := make(map[string]struct{})
	files := make([]extractedBundleFile, 0, 16)
	var totalBytes int64
	var scannedTextBytes int64
	hasIndexHTML := false

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, newBundleRejectedError("invalid_tar", "uploaded gzip payload is not a valid tar archive", "压缩包内部不是合法的 tar 归档")
		}
		relPath, skipEntry, err := validateTarHeader(hdr)
		if err != nil {
			return nil, err
		}
		if skipEntry {
			continue
		}
		if _, duplicate := seenPaths[relPath]; duplicate {
			return nil, newBundleRejectedError("duplicate_bundle_path", fmt.Sprintf("bundle contains duplicate path %q", relPath), fmt.Sprintf("压缩包存在重复路径 %q", relPath))
		}
		seenPaths[relPath] = struct{}{}

		if int64(len(files)+1) > int64(settings.MaxBundleFileCount) {
			return nil, newPayloadTooLargeError(
				fmt.Sprintf("bundle exceeds maximum file count of %d", settings.MaxBundleFileCount),
				fmt.Sprintf("压缩包文件数量超过上限（%d 个）", settings.MaxBundleFileCount),
			)
		}
		data, err := readBoundedTarFile(tr, hdr.Size, settings.MaxBundleUncompressedBytes-totalBytes)
		if err != nil {
			return nil, err
		}
		totalBytes += int64(len(data))

		classify := classifyAndInspectBundleFile
		if !static {
			classify = classifyDynamicSourceFile
		}
		contentType, err := classify(ctx, relPath, data, &scannedTextBytes)
		if err != nil {
			return nil, err
		}
		if relPath == "index.html" {
			hasIndexHTML = true
		}
		files = append(files, extractedBundleFile{
			Path:        relPath,
			ContentType: contentType,
			Data:        data,
		})
	}

	if len(files) == 0 {
		return nil, newBundleRejectedError("empty_tar_bundle", "bundle contains no regular files", "压缩包未包含任何普通文件")
	}
	if static && !hasIndexHTML {
		return nil, newBundleRejectedError("missing_index_html", "static bundle must contain root index.html", "静态站点压缩包根目录必须包含 index.html")
	}
	slices.SortFunc(files, func(a, b extractedBundleFile) int {
		return strings.Compare(a.Path, b.Path)
	})
	return &validatedBundle{Files: files, TotalBytes: totalBytes}, nil
}

func validateTarHeader(hdr *tar.Header) (string, bool, error) {
	switch hdr.Typeflag {
	case tar.TypeXHeader, tar.TypeXGlobalHeader:
		return "", true, nil
	case tar.TypeDir:
		_, err := normalizeBundleRelativePath(hdr.Name, true)
		return "", true, err
	case tar.TypeReg, '\x00':
		relPath, err := normalizeBundleRelativePath(hdr.Name, false)
		if err != nil {
			return "", false, err
		}
		if reason, forbidden := checkForbiddenCredentialPath(relPath); forbidden {
			return "", false, newBundleRejectedError("forbidden_bundle_file", fmt.Sprintf("forbidden credential or VCS file %q (%s)", relPath, reason), fmt.Sprintf("禁止上传敏感凭据或版本控制文件 %q", relPath))
		}
		return relPath, false, nil
	default:
		return "", false, newBundleRejectedError("unsupported_tar_entry", fmt.Sprintf("tar entry %q uses unsupported non-regular file type", hdr.Name), fmt.Sprintf("归档条目 %q 使用了不允许的特殊文件类型（如符号链接）", hdr.Name))
	}
}

func normalizeBundleRelativePath(raw string, isDir bool) (string, error) {
	if raw == "" || strings.HasPrefix(raw, "/") || strings.ContainsRune(raw, '\\') || hasPathControlRune(raw) {
		return "", newBundleRejectedError("invalid_bundle_path", fmt.Sprintf("invalid archive entry path %q", raw), fmt.Sprintf("归档路径 %q 不合法", raw))
	}
	trimmed := raw
	if isDir {
		trimmed = strings.TrimSuffix(trimmed, "/")
		if trimmed == "" || trimmed == "." {
			return "", nil
		}
	} else if strings.HasSuffix(trimmed, "/") {
		return "", newBundleRejectedError("invalid_bundle_path", fmt.Sprintf("regular file path %q must not end with /", raw), fmt.Sprintf("普通文件路径 %q 不能以斜杠结尾", raw))
	}

	rawParts := strings.Split(trimmed, "/")
	cleanParts := make([]string, 0, len(rawParts))
	for i, part := range rawParts {
		if i == 0 && part == "." {
			continue
		}
		if part == "" || part == "." || part == ".." {
			return "", newBundleRejectedError("invalid_bundle_path", fmt.Sprintf("archive path %q contains disallowed traversal or empty segment", raw), fmt.Sprintf("归档路径 %q 包含路径穿越或空路径段", raw))
		}
		cleanParts = append(cleanParts, part)
	}
	if len(cleanParts) == 0 {
		if isDir {
			return "", nil
		}
		return "", newBundleRejectedError("invalid_bundle_path", "archive contains unnamed regular file", "归档包含无名文件")
	}
	joined := strings.Join(cleanParts, "/")
	if len(joined) > 1024 {
		return "", newBundleRejectedError("invalid_bundle_path", "archive file path exceeds 1024 characters", "归档文件路径超过 1024 字符")
	}
	return joined, nil
}

func hasPathControlRune(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func checkForbiddenCredentialPath(relPath string) (string, bool) {
	lower := strings.ToLower(relPath)
	segments := strings.Split(lower, "/")
	if slices.Contains(segments, ".git") {
		return ".git metadata", true
	}
	base := segments[len(segments)-1]
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return "dotenv file", true
	}
	if base == ".npmrc" || base == "credentials.json" || base == ".netrc" || base == ".pypirc" {
		return "credential config file", true
	}
	if strings.HasPrefix(base, "id_rsa") || strings.HasPrefix(base, "id_ed25519") || strings.HasPrefix(base, "id_ecdsa") || strings.HasPrefix(base, "id_dsa") {
		return "SSH key file", true
	}
	ext := path.Ext(base)
	if ext == ".pem" || ext == ".key" || ext == ".p12" || ext == ".pfx" {
		return "private key or certificate bundle", true
	}
	return "", false
}

func readBoundedTarFile(tr *tar.Reader, declaredSize, remainingBudget int64) ([]byte, error) {
	if declaredSize < 0 {
		return nil, newBundleRejectedError("invalid_tar_size", "negative file size in tar header", "归档文件大小异常")
	}
	if declaredSize > remainingBudget {
		return nil, newPayloadTooLargeError("uncompressed bundle size exceeds platform limit", "解压后总字节数超过平台限制")
	}
	data, err := io.ReadAll(io.LimitReader(tr, remainingBudget+1))
	if err != nil {
		return nil, newBundleRejectedError("tar_entry_read_failed", "failed reading tar entry payload", "读取归档条目失败")
	}
	if int64(len(data)) > remainingBudget {
		return nil, newPayloadTooLargeError("uncompressed bundle size exceeds platform limit", "解压后总字节数超过平台限制")
	}
	return data, nil
}

func classifyAndInspectBundleFile(ctx context.Context, relPath string, data []byte, scannedTextBytes *int64) (string, error) {
	ext := strings.ToLower(path.Ext(relPath))
	if isNestedArchivePath(relPath, ext) {
		return "", newBundleRejectedError("nested_archive_forbidden", fmt.Sprintf("nested archive %q is forbidden in static bundles", relPath), fmt.Sprintf("静态包内禁止包含嵌套压缩归档 %q", relPath))
	}
	if mime, isText := textContentTypesByExt[ext]; isText {
		if err := inspectTextBundleFile(ctx, relPath, data, scannedTextBytes); err != nil {
			return "", err
		}
		return mime, nil
	}
	if mime, isBinary := binaryContentTypesByExt[ext]; isBinary {
		if !verifyBinaryAssetSignature(ext, data) {
			return "", newBundleRejectedError("invalid_binary_asset_signature", fmt.Sprintf("file %q header does not match declared %s format", relPath, ext), fmt.Sprintf("静态资源 %q 文件头与声明格式 %s 不匹配", relPath, ext))
		}
		return mime, nil
	}
	return "", newBundleRejectedError("unsupported_bundle_file_format", fmt.Sprintf("file %q uses unsupported static asset format %q", relPath, ext), fmt.Sprintf("文件 %q 使用了不支持的静态托管格式", relPath))
}

func isNestedArchivePath(relPath, ext string) bool {
	if _, ok := nestedArchiveExts[ext]; ok {
		return true
	}
	lower := strings.ToLower(relPath)
	return strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tar.bz2") || strings.HasSuffix(lower, ".tar.xz")
}

func inspectTextBundleFile(ctx context.Context, relPath string, data []byte, scannedTextBytes *int64) error {
	if len(data) > maxDLPInspectFileBytes {
		return newBundleRejectedError("dlp_file_too_large", fmt.Sprintf("text file %q exceeds %d-byte DLP inspection limit", relPath, maxDLPInspectFileBytes), fmt.Sprintf("文本资源 %q 超过 %d 字节敏感内容检查上限", relPath, maxDLPInspectFileBytes))
	}
	if *scannedTextBytes+int64(len(data)) > maxTotalDLPTextBytes {
		return newBundleRejectedError("dlp_text_budget_exceeded", fmt.Sprintf("cumulative text assets exceed %d-byte DLP inspection budget", maxTotalDLPTextBytes), fmt.Sprintf("累计文本资源超过 %d 字节敏感内容检查上限", maxTotalDLPTextBytes))
	}
	*scannedTextBytes += int64(len(data))
	if !utf8.Valid(data) {
		return newBundleRejectedError("invalid_text_encoding", fmt.Sprintf("text asset %q must be valid UTF-8", relPath), fmt.Sprintf("文本或脚本文件 %q 必须为合法 UTF-8 编码", relPath))
	}
	if len(data) == 0 {
		return nil
	}
	for _, chunk := range splitUTF8ForDLP(data) {
		decision := appcontentdlp.Inspect(ctx, staticBundleDLPPolicy, chunk)
		if decision.Status == "unsupported" {
			decision = appcontentdlp.Inspect(ctx, staticBundleDLPPolicy, sanitizeTextControlBytes(chunk))
		}
		if decision.Status == "timeout" {
			return newBundleRejectedError("dlp_scan_timeout", fmt.Sprintf("DLP inspection timed out on %q", relPath), fmt.Sprintf("文件 %q 敏感内容扫描超时", relPath))
		}
		if decision.Status == "unsupported" {
			return newBundleRejectedError("dlp_scan_unsupported", fmt.Sprintf("DLP inspection could not classify text asset %q", relPath), fmt.Sprintf("文件 %q 无法完成敏感内容安全扫描", relPath))
		}
		if !decision.Allowed && len(decision.MatchedRuleIDs) > 0 {
			return newBundleRejectedError(
				"dlp_secret_detected",
				fmt.Sprintf("blocked sensitive content in %q (matched rules: %s)", relPath, strings.Join(decision.MatchedRuleIDs, ", ")),
				fmt.Sprintf("文件 %q 命中敏感凭据拦截规则（%s）", relPath, strings.Join(decision.MatchedRuleIDs, ", ")),
			)
		}
	}
	return nil
}

var binaryMagicPrefixes = map[string][][]byte{
	".png":   {[]byte("\x89PNG\r\n\x1a\n")},
	".jpg":   {{0xff, 0xd8, 0xff}},
	".jpeg":  {{0xff, 0xd8, 0xff}},
	".gif":   {[]byte("GIF87a"), []byte("GIF89a")},
	".ico":   {{0x00, 0x00, 0x01, 0x00}, {0x00, 0x00, 0x02, 0x00}},
	".woff":  {[]byte("wOFF")},
	".woff2": {[]byte("wOF2")},
	".ttf":   {{0x00, 0x01, 0x00, 0x00}, []byte("true")},
	".otf":   {[]byte("OTTO")},
	".wasm":  {{0x00, 'a', 's', 'm', 0x01, 0x00, 0x00, 0x00}},
}

func verifyBinaryAssetSignature(ext string, data []byte) bool {
	if prefixes, ok := binaryMagicPrefixes[ext]; ok {
		for _, prefix := range prefixes {
			if bytes.HasPrefix(data, prefix) {
				return true
			}
		}
		return false
	}
	if ext == ".webp" {
		return len(data) >= 12 && bytes.Equal(data[0:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP"))
	}
	if ext == ".avif" {
		return len(data) >= 12 && bytes.Equal(data[4:8], []byte("ftyp"))
	}
	return false
}

func splitUTF8ForDLP(data []byte) [][]byte {
	if len(data) <= domaincontentdlp.MaxTextBytes {
		return [][]byte{data}
	}
	var windows [][]byte
	start := 0
	for start < len(data) {
		end := min(start+dlpChunkWindowBytes, len(data))
		for end < len(data) && !utf8.RuneStart(data[end]) {
			end--
		}
		windows = append(windows, data[start:end])
		if end == len(data) {
			break
		}
		nextStart := max(end-dlpChunkOverlapBytes, start+1)
		for nextStart < end && !utf8.RuneStart(data[nextStart]) {
			nextStart++
		}
		start = nextStart
	}
	return windows
}

func sanitizeTextControlBytes(chunk []byte) []byte {
	out := bytes.Clone(chunk)
	for i, b := range out {
		if (b < 32 && b != '\n' && b != '\r' && b != '\t') || b == 127 {
			out[i] = ' '
		}
	}
	return out
}

func newBundleRejectedError(code, en, zh string) error {
	return wrapTypedBusinessError(domainapphosting.ErrBundleRejected, code, en, zh)
}

func newPayloadTooLargeError(en, zh string) error {
	return wrapTypedBusinessError(domainapphosting.ErrPayloadTooLarge, "bundle_too_large", en, zh)
}

func sha256HexBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
