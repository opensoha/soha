package sohacli

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	skillsReleaseManifestSchema  = "opensoha.dev/skills-release/v1"
	skillsValidationReportSchema = "opensoha.dev/skills-validation-report/v1"
	skillsCacheMarkerSchema      = "opensoha.dev/skills-cache/v1"
	maxSkillsArtifactBytes       = 64 << 20
	maxSkillsMetadataBytes       = 4 << 20
	maxSkillsExtractedBytes      = 128 << 20
	maxSkillsReleaseFiles        = 4096
)

var (
	semverPattern          = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?$`)
	sha256Pattern          = regexp.MustCompile(`^[a-f0-9]{64}$`)
	checksumPattern        = regexp.MustCompile(`^([a-f0-9]{64})\s+\*?([^\r\n]+)$`)
	githubSkillSourceRegex = regexp.MustCompile(`^github:([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)@(skills/)?v?([0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?)$`)
	rootSkillsTagRE        = regexp.MustCompile(`^skills/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	githubLatestSourceRE   = regexp.MustCompile(`^github:([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)(?:@latest)?$`)
	compatibilityRangeRE   = regexp.MustCompile(`^>=([0-9]+\.[0-9]+\.[0-9]+)\s+<([0-9]+\.[0-9]+\.[0-9]+)$`)
)

type skillsReleaseManifest struct {
	SchemaVersion string                      `json:"schemaVersion"`
	Version       string                      `json:"version"`
	Format        string                      `json:"format"`
	Artifact      string                      `json:"artifact"`
	ManifestURL   string                      `json:"manifestUrl"`
	Files         []skillsReleaseManifestFile `json:"files"`
}

type skillsReleaseManifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type skillReleaseSpec struct {
	Artifact         string
	Checksum         string
	Manifest         string
	Validation       string
	ExpectedVersion  string
	Remote           bool
	GitHubOwner      string
	GitHubRepository string
}

type githubRelease struct {
	TagName    string               `json:"tag_name"`
	Draft      bool                 `json:"draft"`
	Prerelease bool                 `json:"prerelease"`
	Assets     []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type skillReleaseMetadata struct {
	Manifest       skillsReleaseManifest
	ManifestBytes  []byte
	ArtifactSHA256 string
}

type skillCacheMarker struct {
	SchemaVersion string `json:"schemaVersion"`
	Version       string `json:"version"`
	Artifact      string `json:"artifact"`
	SHA256        string `json:"sha256"`
}

func resolveSkillSource(ctx context.Context, source string, rt Runtime) (string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", fmt.Errorf("skill source is required")
	}

	if info, err := os.Stat(source); err == nil && info.IsDir() {
		resolved := normalizeSkillSourcePath(source)
		if !isSkillSourceDir(resolved) {
			return "", fmt.Errorf("%s does not contain Soha AI Gateway skills", source)
		}
		return resolved, nil
	}

	spec, err := parseSkillReleaseSource(source)
	if err != nil {
		return "", err
	}
	if spec.GitHubOwner != "" {
		spec, err = resolveLatestGitHubSkillRelease(ctx, rt, spec.GitHubOwner, spec.GitHubRepository)
		if err != nil {
			return "", err
		}
	}
	cacheRoot, err := skillsCacheRoot()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(cacheRoot, 0o700); err != nil {
		return "", fmt.Errorf("create skills cache: %w", err)
	}
	stageDir, err := os.MkdirTemp(cacheRoot, ".resolve-")
	if err != nil {
		return "", fmt.Errorf("create skills staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stageDir) }()

	packageDir := filepath.Join(stageDir, "package")
	if err := os.MkdirAll(packageDir, 0o700); err != nil {
		return "", err
	}
	artifactName := sourceBaseName(spec.Artifact)
	checksumName := sourceBaseName(spec.Checksum)
	manifestName := sourceBaseName(spec.Manifest)
	validationName := sourceBaseName(spec.Validation)
	artifactPath := filepath.Join(packageDir, artifactName)
	checksumPath := filepath.Join(packageDir, checksumName)
	manifestPath := filepath.Join(packageDir, manifestName)
	validationPath := filepath.Join(packageDir, validationName)

	for _, item := range []struct {
		source string
		dest   string
	}{
		{source: spec.Checksum, dest: checksumPath},
		{source: spec.Manifest, dest: manifestPath},
		{source: spec.Validation, dest: validationPath},
	} {
		if err := materializeSkillReleaseAsset(ctx, rt, item.source, item.dest, maxSkillsMetadataBytes, spec.Remote); err != nil {
			return "", err
		}
	}

	metadata, err := preflightSkillRelease(checksumPath, manifestPath, validationPath, artifactName, spec.ExpectedVersion)
	if err != nil {
		return "", err
	}
	finalDir := filepath.Join(cacheRoot, "releases", metadata.Manifest.Version, metadata.ArtifactSHA256)
	if sourcePath, ok, err := cachedSkillSource(finalDir, metadata); err != nil {
		return "", err
	} else if ok {
		return sourcePath, nil
	}

	if err := materializeSkillReleaseAsset(ctx, rt, spec.Artifact, artifactPath, maxSkillsArtifactBytes, spec.Remote); err != nil {
		return "", err
	}
	actualSHA256, err := sha256File(artifactPath)
	if err != nil {
		return "", err
	}
	if actualSHA256 != metadata.ArtifactSHA256 {
		return "", fmt.Errorf("skills artifact sha256 mismatch: expected %s, got %s", metadata.ArtifactSHA256, actualSHA256)
	}

	extractRoot := filepath.Join(stageDir, "extracted")
	if err := extractVerifiedSkillsRelease(artifactPath, extractRoot, metadata); err != nil {
		return "", err
	}
	if err := validateExtractedSkillsRelease(extractRoot, metadata.Manifest.Version); err != nil {
		return "", err
	}
	marker := skillCacheMarker{
		SchemaVersion: skillsCacheMarkerSchema,
		Version:       metadata.Manifest.Version,
		Artifact:      metadata.Manifest.Artifact,
		SHA256:        metadata.ArtifactSHA256,
	}
	markerBytes, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return "", err
	}
	markerBytes = append(markerBytes, '\n')
	if err := os.WriteFile(filepath.Join(stageDir, ".verified.json"), markerBytes, 0o600); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(finalDir), 0o700); err != nil {
		return "", err
	}
	if err := os.Rename(stageDir, finalDir); err != nil {
		if sourcePath, ok, cacheErr := cachedSkillSource(finalDir, metadata); cacheErr == nil && ok {
			return sourcePath, nil
		}
		return "", fmt.Errorf("activate verified skills cache: %w", err)
	}
	return verifiedSkillSourcePath(finalDir)
}

func parseSkillReleaseSource(source string) (skillReleaseSpec, error) {
	if match := githubSkillSourceRegex.FindStringSubmatch(source); match != nil {
		owner, repository, releaseVersion := match[1], match[2], match[4]
		if match[3] != "" && (owner != "opensoha" || repository != "soha") {
			return skillReleaseSpec{}, fmt.Errorf("skills/v tags belong to github:opensoha/soha")
		}
		if owner == "." || owner == ".." || repository == "." || repository == ".." {
			return skillReleaseSpec{}, fmt.Errorf("invalid GitHub skills source %q", source)
		}
		tag := "v" + releaseVersion
		if owner == "opensoha" && repository == "soha" {
			tag = "skills/" + tag
		}
		artifactName := fmt.Sprintf("soha-skills-%s.tar.gz", releaseVersion)
		artifactURL := fmt.Sprintf(
			"https://github.com/%s/%s/releases/download/%s/%s",
			owner,
			repository,
			url.PathEscape(tag),
			artifactName,
		)
		spec, err := releaseSpecForArtifact(artifactURL, true)
		if err != nil {
			return skillReleaseSpec{}, err
		}
		spec.ExpectedVersion = releaseVersion
		return spec, nil
	}
	if match := githubLatestSourceRE.FindStringSubmatch(source); match != nil {
		owner, repository := match[1], match[2]
		if owner == "." || owner == ".." || repository == "." || repository == ".." {
			return skillReleaseSpec{}, fmt.Errorf("invalid GitHub skills source %q", source)
		}
		return skillReleaseSpec{
			Remote:           true,
			GitHubOwner:      owner,
			GitHubRepository: repository,
		}, nil
	}

	parsed, err := url.Parse(source)
	if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
			return skillReleaseSpec{}, fmt.Errorf("skills release URLs must use HTTPS outside localhost")
		}
		return releaseSpecForArtifact(source, true)
	}
	info, statErr := os.Stat(source)
	if statErr != nil {
		if strings.HasPrefix(source, "github:") {
			return skillReleaseSpec{}, fmt.Errorf("GitHub skills source must use github:owner/repo, github:owner/repo@latest, or github:owner/repo@vX.Y.Z")
		}
		return skillReleaseSpec{}, fmt.Errorf("skill source %q is not a directory, release archive, or supported URL", source)
	}
	if !info.Mode().IsRegular() {
		return skillReleaseSpec{}, fmt.Errorf("skill release source %q is not a regular file", source)
	}
	return releaseSpecForArtifact(source, false)
}

func resolveLatestGitHubSkillRelease(ctx context.Context, rt Runtime, owner, repository string) (skillReleaseSpec, error) {
	var release githubRelease
	if owner == "opensoha" && repository == "soha" {
		var err error
		release, err = latestRootSkillsRelease(ctx, rt)
		if err != nil {
			return skillReleaseSpec{}, err
		}
	} else {
		apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repository)
		raw, err := readGitHubSkillsMetadata(ctx, rt, apiURL, owner, repository)
		if err != nil {
			return skillReleaseSpec{}, err
		}
		if err := json.Unmarshal(raw, &release); err != nil {
			return skillReleaseSpec{}, fmt.Errorf("decode latest GitHub skills release metadata: %w", err)
		}
	}
	if release.Draft || release.Prerelease {
		return skillReleaseSpec{}, fmt.Errorf("latest GitHub skills release for %s/%s is not a stable published release", owner, repository)
	}
	releaseVersion := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(release.TagName), "skills/"), "v")
	if !semverPattern.MatchString(releaseVersion) {
		return skillReleaseSpec{}, fmt.Errorf("latest GitHub skills release tag %q is not a supported semantic version", release.TagName)
	}

	artifactName := fmt.Sprintf("soha-skills-%s.tar.gz", releaseVersion)
	wanted := map[string]struct{}{
		artifactName:             {},
		artifactName + ".sha256": {},
		"soha-skills-" + releaseVersion + ".manifest.json":          {},
		"soha-skills-" + releaseVersion + ".validation-report.json": {},
	}
	resolved := make(map[string]string, len(wanted))
	for _, asset := range release.Assets {
		if _, ok := wanted[asset.Name]; !ok {
			continue
		}
		if _, duplicate := resolved[asset.Name]; duplicate {
			return skillReleaseSpec{}, fmt.Errorf("latest GitHub skills release contains duplicate asset %q", asset.Name)
		}
		if err := validateRemoteSkillReleaseURL(asset.BrowserDownloadURL); err != nil {
			return skillReleaseSpec{}, fmt.Errorf("invalid GitHub skills release asset %q: %w", asset.Name, err)
		}
		resolved[asset.Name] = asset.BrowserDownloadURL
	}
	for assetName := range wanted {
		if resolved[assetName] == "" {
			return skillReleaseSpec{}, fmt.Errorf("latest GitHub skills release v%s is missing asset %q", releaseVersion, assetName)
		}
	}

	return skillReleaseSpec{
		Artifact:        resolved[artifactName],
		Checksum:        resolved[artifactName+".sha256"],
		Manifest:        resolved["soha-skills-"+releaseVersion+".manifest.json"],
		Validation:      resolved["soha-skills-"+releaseVersion+".validation-report.json"],
		ExpectedVersion: releaseVersion,
		Remote:          true,
	}, nil
}

// Root latest can refer to Core; select the highest stable skills version across pages.
func latestRootSkillsRelease(ctx context.Context, rt Runtime) (githubRelease, error) {
	var selected githubRelease
	var selectedVersion [3]int
	for page := 1; page <= 100; page++ {
		apiURL := fmt.Sprintf("https://api.github.com/repos/opensoha/soha/releases?per_page=100&page=%d", page)
		raw, err := readGitHubSkillsMetadata(ctx, rt, apiURL, "opensoha", "soha")
		if err != nil {
			return githubRelease{}, err
		}
		var releases []githubRelease
		if err := json.Unmarshal(raw, &releases); err != nil {
			return githubRelease{}, fmt.Errorf("decode GitHub skills release page: %w", err)
		}
		for _, release := range releases {
			if release.Draft || release.Prerelease || !rootSkillsTagRE.MatchString(release.TagName) {
				continue
			}
			version, ok := parseSemverTriple(strings.TrimPrefix(release.TagName, "skills/"))
			if ok && (selected.TagName == "" || compareSemverTriple(version, selectedVersion) > 0) {
				selected, selectedVersion = release, version
			}
		}
		if len(releases) < 100 {
			if selected.TagName == "" {
				return githubRelease{}, fmt.Errorf("no published stable skills/v release found for github:opensoha/soha")
			}
			return selected, nil
		}
	}
	return githubRelease{}, fmt.Errorf("GitHub skills release listing exceeds the 100-page limit")
}

func readGitHubSkillsMetadata(ctx context.Context, rt Runtime, apiURL, owner, repository string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create latest GitHub skills release request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	response, err := (APIClient{Client: rt.HTTPClient, Timeout: rt.HTTPTimeout}).httpClient().Do(request)
	if err != nil {
		return nil, fmt.Errorf("resolve latest GitHub skills release for %s/%s: %w", owner, repository, err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.Request != nil && response.Request.URL.Scheme == "http" && !isLoopbackHost(response.Request.URL.Hostname()) {
		return nil, fmt.Errorf("resolve latest GitHub skills release for %s/%s: redirected to insecure HTTP", owner, repository)
	}
	if response.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no published skills release found for github:%s/%s", owner, repository)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("resolve latest GitHub skills release for %s/%s: unexpected HTTP status %s", owner, repository, response.Status)
	}
	if response.ContentLength > maxSkillsMetadataBytes {
		return nil, fmt.Errorf("latest GitHub skills release metadata exceeds the %d-byte limit", maxSkillsMetadataBytes)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxSkillsMetadataBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read latest GitHub skills release metadata: %w", err)
	}
	if int64(len(raw)) > maxSkillsMetadataBytes {
		return nil, fmt.Errorf("latest GitHub skills release metadata exceeds the %d-byte limit", maxSkillsMetadataBytes)
	}

	return raw, nil
}

func releaseSpecForArtifact(artifact string, remote bool) (skillReleaseSpec, error) {
	if remote {
		if err := validateRemoteSkillReleaseURL(artifact); err != nil {
			return skillReleaseSpec{}, err
		}
	}
	if !strings.HasSuffix(sourcePathComponent(artifact), ".tar.gz") {
		return skillReleaseSpec{}, fmt.Errorf("skills release artifact must end with .tar.gz: %s", artifact)
	}
	base := strings.TrimSuffix(artifact, ".tar.gz")
	return skillReleaseSpec{
		Artifact:   artifact,
		Checksum:   artifact + ".sha256",
		Manifest:   base + ".manifest.json",
		Validation: base + ".validation-report.json",
		Remote:     remote,
	}, nil
}

func validateRemoteSkillReleaseURL(source string) error {
	parsed, err := url.Parse(source)
	if err != nil {
		return fmt.Errorf("parse skills release URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("skills release URL must use HTTP or HTTPS")
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("skills release URL must include a host")
	}
	if parsed.Scheme == "http" && !isLoopbackHost(parsed.Hostname()) {
		return fmt.Errorf("skills release URLs must use HTTPS outside localhost")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("skills release URLs must not contain a query or fragment")
	}
	return nil
}

func sourcePathComponent(source string) string {
	// Drive-letter paths are local files, even though net/url interprets the
	// drive as a URI scheme. Normalize local separators before path.Base.
	if filepath.IsAbs(source) {
		return filepath.ToSlash(source)
	}
	if parsed, err := url.Parse(source); err == nil && parsed.Scheme != "" {
		return parsed.Path
	}
	return filepath.ToSlash(source)
}

func sourceBaseName(source string) string {
	return path.Base(sourcePathComponent(source))
}

func skillsCacheRoot() (string, error) {
	if configured := strings.TrimSpace(env("SOHA_SKILLS_CACHE")); configured != "" {
		return configured, nil
	}
	root, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache directory: %w", err)
	}
	return filepath.Join(root, "soha", "skills"), nil
}

func materializeSkillReleaseAsset(ctx context.Context, rt Runtime, source, dest string, maxBytes int64, remote bool) error {
	if remote {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return fmt.Errorf("create skills release request: %w", err)
		}
		request.Header.Set("Accept", "application/octet-stream, application/json;q=0.9, text/plain;q=0.8")
		response, err := (APIClient{Client: rt.HTTPClient, Timeout: rt.HTTPTimeout}).httpClient().Do(request)
		if err != nil {
			return fmt.Errorf("download skills release asset %s: %w", source, err)
		}
		defer func() { _ = response.Body.Close() }()
		if response.Request.URL.Scheme == "http" && !isLoopbackHost(response.Request.URL.Hostname()) {
			return fmt.Errorf("download skills release asset %s: redirected to insecure HTTP", source)
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("download skills release asset %s: unexpected HTTP status %s", source, response.Status)
		}
		if response.ContentLength > maxBytes {
			return fmt.Errorf("skills release asset %s exceeds the %d-byte limit", source, maxBytes)
		}
		if err := writeBoundedFile(dest, response.Body, maxBytes); err != nil {
			return fmt.Errorf("download skills release asset %s: %w", source, err)
		}
		return nil
	}

	// #nosec G304 -- source is the explicit local release asset selected by the user.
	file, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open skills release asset %s: %w", source, err)
	}
	defer func() { _ = file.Close() }()
	if err := writeBoundedFile(dest, file, maxBytes); err != nil {
		return fmt.Errorf("copy skills release asset %s: %w", source, err)
	}
	return nil
}

func writeBoundedFile(dest string, reader io.Reader, maxBytes int64) error {
	// #nosec G304 -- dest is a staging path created under the private skills cache.
	file, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(reader, maxBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > maxBytes {
		return fmt.Errorf("asset exceeds the %d-byte limit", maxBytes)
	}
	return nil
}

func preflightSkillRelease(checksumPath, manifestPath, validationPath, artifactName, expectedVersion string) (skillReleaseMetadata, error) {
	checksumBytes, err := readBoundedFile(checksumPath, maxSkillsMetadataBytes)
	if err != nil {
		return skillReleaseMetadata{}, err
	}
	match := checksumPattern.FindStringSubmatch(strings.TrimSpace(string(checksumBytes)))
	if match == nil {
		return skillReleaseMetadata{}, fmt.Errorf("%s must contain '<sha256>  <artifact>'", checksumPath)
	}
	if strings.TrimSpace(match[2]) != artifactName {
		return skillReleaseMetadata{}, fmt.Errorf("%s names %q, expected %q", checksumPath, strings.TrimSpace(match[2]), artifactName)
	}

	manifestBytes, err := readBoundedFile(manifestPath, maxSkillsMetadataBytes)
	if err != nil {
		return skillReleaseMetadata{}, err
	}
	var manifest skillsReleaseManifest
	if err := decodeStrictJSON(manifestBytes, &manifest); err != nil {
		return skillReleaseMetadata{}, fmt.Errorf("decode skills release manifest: %w", err)
	}
	if err := validateSkillsReleaseManifest(manifest, artifactName, expectedVersion); err != nil {
		return skillReleaseMetadata{}, err
	}
	if err := validateSkillsReleaseReport(validationPath, manifest.Version, match[1]); err != nil {
		return skillReleaseMetadata{}, err
	}
	return skillReleaseMetadata{
		Manifest:       manifest,
		ManifestBytes:  manifestBytes,
		ArtifactSHA256: match[1],
	}, nil
}

func validateSkillsReleaseManifest(manifest skillsReleaseManifest, artifactName, expectedVersion string) error {
	if manifest.SchemaVersion != skillsReleaseManifestSchema {
		return fmt.Errorf("unsupported skills release manifest schema %q", manifest.SchemaVersion)
	}
	if !semverPattern.MatchString(manifest.Version) {
		return fmt.Errorf("invalid skills release version %q", manifest.Version)
	}
	if expectedVersion != "" && manifest.Version != expectedVersion {
		return fmt.Errorf("skills release version %q does not match requested version %q", manifest.Version, expectedVersion)
	}
	if manifest.Format != "tar.gz" {
		return fmt.Errorf("unsupported skills release format %q", manifest.Format)
	}
	if manifest.Artifact != artifactName {
		return fmt.Errorf("skills release manifest names artifact %q, expected %q", manifest.Artifact, artifactName)
	}
	if manifest.Artifact != "soha-skills-"+manifest.Version+".tar.gz" {
		return fmt.Errorf("skills release artifact %q does not match manifest version %q", manifest.Artifact, manifest.Version)
	}
	if strings.TrimSpace(manifest.ManifestURL) == "" {
		return fmt.Errorf("skills release manifest URL is required")
	}
	if len(manifest.Files) == 0 || len(manifest.Files) > maxSkillsReleaseFiles {
		return fmt.Errorf("skills release manifest file count must be between 1 and %d", maxSkillsReleaseFiles)
	}
	seen := make(map[string]struct{}, len(manifest.Files))
	for _, item := range manifest.Files {
		if !isSafeReleasePath(item.Path) {
			return fmt.Errorf("unsafe skills release path %q", item.Path)
		}
		if _, ok := seen[item.Path]; ok {
			return fmt.Errorf("duplicate skills release path %q", item.Path)
		}
		seen[item.Path] = struct{}{}
		if !sha256Pattern.MatchString(item.SHA256) {
			return fmt.Errorf("invalid sha256 for skills release path %q", item.Path)
		}
	}
	return nil
}

func validateSkillsReleaseReport(reportPath, releaseVersion, artifactSHA256 string) error {
	reportBytes, err := readBoundedFile(reportPath, maxSkillsMetadataBytes)
	if err != nil {
		return err
	}
	var report map[string]any
	if err := json.Unmarshal(reportBytes, &report); err != nil {
		return fmt.Errorf("decode skills validation report: %w", err)
	}
	if report["schemaVersion"] != skillsValidationReportSchema {
		return fmt.Errorf("unsupported skills validation report schema %q", report["schemaVersion"])
	}
	if report["repository"] != "soha-skills" {
		return fmt.Errorf("skills validation report repository must be soha-skills")
	}
	if report["releaseVersion"] != releaseVersion {
		return fmt.Errorf("skills validation report version does not match release version %q", releaseVersion)
	}
	if report["status"] != "passed" {
		return fmt.Errorf("skills validation report status is %q, expected passed", report["status"])
	}
	checks, ok := report["checks"].([]any)
	if !ok || len(checks) == 0 {
		return fmt.Errorf("skills validation report must contain checks")
	}
	for _, rawCheck := range checks {
		check, ok := rawCheck.(map[string]any)
		if !ok {
			return fmt.Errorf("skills validation report contains an invalid check")
		}
		status, ok := check["status"].(string)
		if !ok || (status != "passed" && status != "skipped") {
			return fmt.Errorf("skills validation report contains a %q check status", check["status"])
		}
	}
	summary, ok := report["summary"].(map[string]any)
	if !ok {
		return fmt.Errorf("skills validation report must contain a summary")
	}
	foundPackageSHA := false
	for _, key := range []string{"packageOutput", "packageVerify", "packageDryRun"} {
		item, ok := summary[key].(map[string]any)
		if !ok {
			continue
		}
		value, ok := item["sha256"].(string)
		if !ok {
			continue
		}
		foundPackageSHA = true
		if value != artifactSHA256 {
			return fmt.Errorf("skills validation report artifact sha256 does not match checksum file")
		}
	}
	if !foundPackageSHA {
		return fmt.Errorf("skills validation report does not include the package sha256")
	}
	return nil
}

func cachedSkillSource(finalDir string, metadata skillReleaseMetadata) (string, bool, error) {
	info, err := os.Stat(finalDir)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !info.IsDir() {
		return "", false, fmt.Errorf("verified skills cache path %s is not a directory", finalDir)
	}
	markerBytes, err := readBoundedFile(filepath.Join(finalDir, ".verified.json"), maxSkillsMetadataBytes)
	if err != nil {
		return "", false, fmt.Errorf("read verified skills cache marker: %w", err)
	}
	var marker skillCacheMarker
	if err := decodeStrictJSON(markerBytes, &marker); err != nil {
		return "", false, fmt.Errorf("decode verified skills cache marker: %w", err)
	}
	if marker.SchemaVersion != skillsCacheMarkerSchema ||
		marker.Version != metadata.Manifest.Version ||
		marker.Artifact != metadata.Manifest.Artifact ||
		marker.SHA256 != metadata.ArtifactSHA256 {
		return "", false, fmt.Errorf("verified skills cache marker does not match release %s", metadata.Manifest.Version)
	}
	if err := verifyCachedSkillsRelease(finalDir, metadata); err != nil {
		return "", false, fmt.Errorf("verified skills cache %s failed integrity validation; remove that cache directory and retry: %w", finalDir, err)
	}
	sourcePath, err := verifiedSkillSourcePath(finalDir)
	if err != nil {
		return "", false, err
	}
	return sourcePath, true, nil
}

func verifyCachedSkillsRelease(cacheDir string, metadata skillReleaseMetadata) error {
	extractRoot := filepath.Join(cacheDir, "extracted")
	root := filepath.Join(extractRoot, "soha-skills")
	manifestName := strings.TrimSuffix(metadata.Manifest.Artifact, ".tar.gz") + ".manifest.json"
	expectedFiles := make(map[string]string, len(metadata.Manifest.Files)+1)
	expectedFiles[manifestName] = ""
	for _, item := range metadata.Manifest.Files {
		expectedFiles[item.Path] = item.SHA256
	}
	expectedDirs := map[string]struct{}{
		".": {},
	}
	for filePath := range expectedFiles {
		for parent := path.Dir(filePath); parent != "."; parent = path.Dir(parent) {
			expectedDirs[parent] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(expectedFiles))
	err := filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if _, ok := expectedDirs[relative]; !ok {
				return fmt.Errorf("unexpected cached skills directory %q", relative)
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("cached skills member %q is not a regular file", relative)
		}
		expectedSHA, ok := expectedFiles[relative]
		if !ok {
			return fmt.Errorf("unexpected cached skills file %q", relative)
		}
		if _, ok := seen[relative]; ok {
			return fmt.Errorf("duplicate cached skills file %q", relative)
		}
		if relative == manifestName {
			raw, err := readBoundedFile(filePath, maxSkillsMetadataBytes)
			if err != nil {
				return err
			}
			if string(raw) != string(metadata.ManifestBytes) {
				return fmt.Errorf("cached embedded manifest differs from the verified manifest")
			}
		} else {
			actualSHA, err := sha256File(filePath)
			if err != nil {
				return err
			}
			if actualSHA != expectedSHA {
				return fmt.Errorf("cached skills file %q failed sha256 verification", relative)
			}
		}
		seen[relative] = struct{}{}
		return nil
	})
	if err != nil {
		return err
	}
	if len(seen) != len(expectedFiles) {
		return fmt.Errorf("cached skills release is missing %d files", len(expectedFiles)-len(seen))
	}
	return validateExtractedSkillsRelease(extractRoot, metadata.Manifest.Version)
}

func verifiedSkillSourcePath(cacheDir string) (string, error) {
	sourcePath := filepath.Join(cacheDir, "extracted", "soha-skills", defaultSkillSource)
	if !isSkillSourceDir(sourcePath) {
		return "", fmt.Errorf("verified skills release does not contain %s", defaultSkillSource)
	}
	return sourcePath, nil
}

func extractVerifiedSkillsRelease(artifactPath, extractRoot string, metadata skillReleaseMetadata) error {
	if err := os.MkdirAll(extractRoot, 0o700); err != nil {
		return err
	}
	// #nosec G304 -- artifactPath is a checksum-verified file in the private staging directory.
	artifact, err := os.Open(artifactPath)
	if err != nil {
		return err
	}
	defer func() { _ = artifact.Close() }()
	compressed, err := gzip.NewReader(artifact)
	if err != nil {
		return fmt.Errorf("open skills release gzip stream: %w", err)
	}
	defer func() { _ = compressed.Close() }()

	expected := make(map[string]string, len(metadata.Manifest.Files)+1)
	manifestMember := "soha-skills/" + strings.TrimSuffix(metadata.Manifest.Artifact, ".tar.gz") + ".manifest.json"
	expected[manifestMember] = ""
	for _, item := range metadata.Manifest.Files {
		expected["soha-skills/"+item.Path] = item.SHA256
	}
	seen := make(map[string]struct{}, len(expected))
	var totalBytes int64
	archive := tar.NewReader(compressed)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read skills release archive: %w", err)
		}
		if len(seen) >= maxSkillsReleaseFiles+1 {
			return fmt.Errorf("skills release archive exceeds the %d-file limit", maxSkillsReleaseFiles+1)
		}
		if header.Typeflag != tar.TypeReg {
			return fmt.Errorf("skills release member %q is not a regular file", header.Name)
		}
		if !isSafeReleasePath(header.Name) || !strings.HasPrefix(header.Name, "soha-skills/") {
			return fmt.Errorf("unsafe skills release member %q", header.Name)
		}
		expectedSHA, ok := expected[header.Name]
		if !ok {
			return fmt.Errorf("unexpected skills release member %q", header.Name)
		}
		if _, duplicate := seen[header.Name]; duplicate {
			return fmt.Errorf("duplicate skills release member %q", header.Name)
		}
		if header.Size < 0 || header.Size > maxSkillsExtractedBytes || totalBytes+header.Size > maxSkillsExtractedBytes {
			return fmt.Errorf("skills release exceeds the %d-byte extraction limit", maxSkillsExtractedBytes)
		}
		totalBytes += header.Size
		destPath, err := safeReleaseDestination(extractRoot, header.Name)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0o700); err != nil {
			return err
		}
		// #nosec G304 -- destPath passed safeReleaseDestination containment checks.
		file, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		hash := sha256.New()
		written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(archive, header.Size))
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if written != header.Size {
			return fmt.Errorf("skills release member %q is truncated", header.Name)
		}
		if header.Name == manifestMember {
			// #nosec G304 -- destPath passed safeReleaseDestination containment checks.
			memberBytes, err := os.ReadFile(destPath)
			if err != nil {
				return err
			}
			if string(memberBytes) != string(metadata.ManifestBytes) {
				return fmt.Errorf("embedded skills release manifest differs from external manifest")
			}
		} else if hex.EncodeToString(hash.Sum(nil)) != expectedSHA {
			return fmt.Errorf("skills release member %q failed sha256 verification", header.Name)
		}
		seen[header.Name] = struct{}{}
	}
	if len(seen) != len(expected) {
		missing := make([]string, 0)
		for name := range expected {
			if _, ok := seen[name]; !ok {
				missing = append(missing, name)
			}
		}
		return fmt.Errorf("skills release is missing %d expected members", len(missing))
	}
	return nil
}

func validateExtractedSkillsRelease(extractRoot, releaseVersion string) error {
	root := filepath.Join(extractRoot, "soha-skills")
	indexVersion, err := jsonStringField(filepath.Join(root, "skills", "index.json"), "version")
	if err != nil {
		return err
	}
	if indexVersion != releaseVersion {
		return fmt.Errorf("skills index version %q does not match release version %q", indexVersion, releaseVersion)
	}
	matrixPath := filepath.Join(root, "catalog", "compatibility-matrix.json")
	matrixBytes, err := readBoundedFile(matrixPath, maxSkillsMetadataBytes)
	if err != nil {
		return err
	}
	var matrix map[string]any
	if err := json.Unmarshal(matrixBytes, &matrix); err != nil {
		return fmt.Errorf("decode skills compatibility matrix: %w", err)
	}
	if matrix["schemaVersion"] != "opensoha.dev/skills-compatibility-matrix/v1" {
		return fmt.Errorf("unsupported skills compatibility matrix schema %q", matrix["schemaVersion"])
	}
	if matrix["skillsVersion"] != releaseVersion {
		return fmt.Errorf("skills compatibility matrix version does not match release version %q", releaseVersion)
	}
	supportedVersions, ok := matrix["supportedVersions"].(map[string]any)
	if !ok {
		return fmt.Errorf("skills compatibility matrix is missing supportedVersions")
	}
	cliRange, ok := supportedVersions["soha-cli"].(string)
	if !ok || strings.TrimSpace(cliRange) == "" {
		return fmt.Errorf("skills compatibility matrix is missing the soha-cli version range")
	}
	if err := validateCLIVersionRange(version, cliRange); err != nil {
		return err
	}
	return nil
}

func validateCLIVersionRange(cliVersion, supportedRange string) error {
	if strings.Contains(strings.ToLower(cliVersion), "dev") || strings.TrimSpace(cliVersion) == "" {
		return nil
	}
	current, ok := parseSemverTriple(cliVersion)
	if !ok {
		return fmt.Errorf("cannot validate CLI version %q against skills compatibility range", cliVersion)
	}
	match := compatibilityRangeRE.FindStringSubmatch(strings.TrimSpace(supportedRange))
	if match == nil {
		return fmt.Errorf("unsupported soha-cli compatibility range %q", supportedRange)
	}
	minimum, _ := parseSemverTriple(match[1])
	maximum, _ := parseSemverTriple(match[2])
	if compareSemverTriple(current, minimum) < 0 || compareSemverTriple(current, maximum) >= 0 {
		return fmt.Errorf("soha CLI %s is not supported by skills compatibility range %s", cliVersion, supportedRange)
	}
	return nil
}

func parseSemverTriple(value string) ([3]int, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if index := strings.IndexAny(value, "-+"); index >= 0 {
		value = value[:index]
	}
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	patch, patchErr := strconv.Atoi(parts[2])
	if majorErr != nil || minorErr != nil || patchErr != nil || major < 0 || minor < 0 || patch < 0 {
		return [3]int{}, false
	}
	return [3]int{major, minor, patch}, true
}

func compareSemverTriple(left, right [3]int) int {
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}

func jsonStringField(filePath, field string) (string, error) {
	raw, err := readBoundedFile(filePath, maxSkillsMetadataBytes)
	if err != nil {
		return "", err
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("decode %s: %w", filePath, err)
	}
	result, ok := value[field].(string)
	if !ok || strings.TrimSpace(result) == "" {
		return "", fmt.Errorf("%s is missing string field %q", filePath, field)
	}
	return result, nil
}

func isSafeReleasePath(value string) bool {
	if value == "" || strings.Contains(value, "\\") || strings.ContainsRune(value, '\x00') || path.IsAbs(value) {
		return false
	}
	cleaned := path.Clean(value)
	return cleaned == value && cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

func safeReleaseDestination(root, member string) (string, error) {
	dest := filepath.Join(root, filepath.FromSlash(member))
	relative, err := filepath.Rel(root, dest)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe skills release destination %q", member)
	}
	return dest, nil
}

func readBoundedFile(filePath string, maxBytes int64) ([]byte, error) {
	// #nosec G304 -- filePath is a verified release metadata path bounded by maxBytes.
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maxBytes {
		return nil, fmt.Errorf("%s exceeds the %d-byte limit", filePath, maxBytes)
	}
	return raw, nil
}

func decodeStrictJSON(raw []byte, value any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON content")
		}
		return err
	}
	return nil
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func sha256File(filePath string) (string, error) {
	// #nosec G304 -- filePath is a verified release asset path selected by the resolver.
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
