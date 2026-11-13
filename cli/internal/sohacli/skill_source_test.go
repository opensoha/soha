package sohacli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestRunSkillListUsesLatestPublishedGitHubReleaseByDefault(t *testing.T) {
	releaseDir := t.TempDir()
	artifactPath := writeTestSkillsRelease(t, releaseDir, "0.1.0", "")
	artifactName := filepath.Base(artifactPath)
	fileServer := http.FileServer(http.Dir(releaseDir))
	assetServer := httptest.NewServer(fileServer)
	defer assetServer.Close()

	assetNames := []string{
		artifactName,
		artifactName + ".sha256",
		"soha-skills-0.1.0.manifest.json",
		"soha-skills-0.1.0.validation-report.json",
	}
	assets := make([]map[string]string, 0, len(assetNames))
	for _, name := range assetNames {
		assets = append(assets, map[string]string{
			"name":                 name,
			"browser_download_url": assetServer.URL + "/" + name,
		})
	}
	releaseBody := mustJSON(t, map[string]any{
		"tag_name":   "v0.1.0",
		"draft":      false,
		"prerelease": false,
		"assets":     assets,
	})

	apiRequests := 0
	apiPath := ""
	apiAuthorization := ""
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "api.github.com" {
			apiRequests++
			apiPath = request.URL.Path
			apiAuthorization = request.Header.Get("Authorization")
			return &http.Response{
				StatusCode:    http.StatusOK,
				Status:        "200 OK",
				Header:        make(http.Header),
				Body:          io.NopCloser(bytes.NewReader(releaseBody)),
				ContentLength: int64(len(releaseBody)),
				Request:       request,
			}, nil
		}
		return http.DefaultTransport.RoundTrip(request)
	})}

	t.Setenv("SOHA_SKILLS_SOURCE", "")
	t.Setenv("SOHA_SKILLS_CACHE", t.TempDir())
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), []string{"skill", "list"}, Runtime{
		Out:        &out,
		Err:        &errOut,
		HTTPClient: client,
	}); code != 0 {
		t.Fatalf("skill list returned %d: %s", code, errOut.String())
	}
	if out.String() != "delivery-developer\n" {
		t.Fatalf("unexpected skill list output %q", out.String())
	}
	if apiRequests != 1 || apiPath != "/repos/opensoha/soha-skills/releases/latest" {
		t.Fatalf("latest release API requests = %d at %q", apiRequests, apiPath)
	}
	if apiAuthorization != "" {
		t.Fatalf("GitHub release request included Authorization header %q", apiAuthorization)
	}
}

func TestSkillsVersionFlagIsRemoved(t *testing.T) {
	for _, args := range [][]string{
		{"skill", "list", "--skills-version", "0.1.0"},
		{"add", "codex", "--skills-version", "0.1.0"},
	} {
		var errOut bytes.Buffer
		if code := Run(context.Background(), args, Runtime{Out: &bytes.Buffer{}, Err: &errOut}); code == 0 {
			t.Fatalf("%v unexpectedly accepted --skills-version", args)
		}
		if !strings.Contains(errOut.String(), "flag provided but not defined: -skills-version") {
			t.Fatalf("%v returned unexpected error %q", args, errOut.String())
		}
	}
}

func TestRunSkillInstallFromVerifiedHTTPReleaseAndCache(t *testing.T) {
	releaseDir := t.TempDir()
	artifactPath := writeTestSkillsRelease(t, releaseDir, "0.1.0", "")
	artifactName := filepath.Base(artifactPath)
	artifactRequests := 0
	fileServer := http.FileServer(http.Dir(releaseDir))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimPrefix(r.URL.Path, "/") == artifactName {
			artifactRequests++
		}
		fileServer.ServeHTTP(w, r)
	}))
	defer server.Close()

	t.Setenv("SOHA_SKILLS_CACHE", t.TempDir())
	source := server.URL + "/" + artifactName
	var listOut, listErr bytes.Buffer
	if code := Run(context.Background(), []string{"skill", "list", "--source", source}, Runtime{
		Out: &listOut,
		Err: &listErr,
	}); code != 0 {
		t.Fatalf("skill list returned %d: %s", code, listErr.String())
	}
	if listOut.String() != "delivery-developer\n" {
		t.Fatalf("unexpected skill list output %q", listOut.String())
	}

	dest := t.TempDir()
	var installErr bytes.Buffer
	if code := Run(context.Background(), []string{
		"skill", "install", "--source", source, "--dest", dest, "delivery-developer",
	}, Runtime{Out: &bytes.Buffer{}, Err: &installErr}); code != 0 {
		t.Fatalf("skill install returned %d: %s", code, installErr.String())
	}
	if artifactRequests != 1 {
		t.Fatalf("artifact downloaded %d times, want one verified cache fill", artifactRequests)
	}
	raw, err := os.ReadFile(filepath.Join(dest, "delivery-developer", "SKILL.md"))
	if err != nil {
		t.Fatalf("read installed skill: %v", err)
	}
	if string(raw) != "# Delivery Developer\n" {
		t.Fatalf("unexpected installed skill content %q", raw)
	}
}

func TestResolveSkillSourceRejectsArtifactChecksumMismatch(t *testing.T) {
	releaseDir := t.TempDir()
	artifactPath := writeTestSkillsRelease(t, releaseDir, "0.1.0", "")
	file, err := os.OpenFile(artifactPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatalf("open artifact for tampering: %v", err)
	}
	if _, err := file.WriteString("tampered"); err != nil {
		t.Fatalf("tamper artifact: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close tampered artifact: %v", err)
	}

	t.Setenv("SOHA_SKILLS_CACHE", t.TempDir())
	_, err = resolveSkillSource(context.Background(), artifactPath, Runtime{})
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("expected sha256 mismatch, got %v", err)
	}
}

func TestResolveSkillSourceRejectsUnsafeArchiveMember(t *testing.T) {
	releaseDir := t.TempDir()
	artifactPath := writeTestSkillsRelease(t, releaseDir, "0.1.0", "../escape")
	cacheDir := t.TempDir()
	t.Setenv("SOHA_SKILLS_CACHE", cacheDir)

	_, err := resolveSkillSource(context.Background(), artifactPath, Runtime{})
	if err == nil || !strings.Contains(err.Error(), "unsafe skills release member") {
		t.Fatalf("expected unsafe member rejection, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(cacheDir), "escape")); !os.IsNotExist(statErr) {
		t.Fatalf("unsafe archive member escaped the extraction root: %v", statErr)
	}
}

func TestResolveSkillSourceRejectsModifiedVerifiedCache(t *testing.T) {
	releaseDir := t.TempDir()
	artifactPath := writeTestSkillsRelease(t, releaseDir, "0.1.0", "")
	t.Setenv("SOHA_SKILLS_CACHE", t.TempDir())

	source, err := resolveSkillSource(context.Background(), artifactPath, Runtime{})
	if err != nil {
		t.Fatalf("resolve verified skills release: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "delivery-developer", "SKILL.md"), []byte("modified\n"), 0o644); err != nil {
		t.Fatalf("modify cached skill: %v", err)
	}
	_, err = resolveSkillSource(context.Background(), artifactPath, Runtime{})
	if err == nil || !strings.Contains(err.Error(), "failed sha256 verification") {
		t.Fatalf("expected modified cache rejection, got %v", err)
	}
}

func TestParseGitHubSkillReleaseSourceSupportsLatestAndPinnedReleases(t *testing.T) {
	spec, err := parseSkillReleaseSource("github:opensoha/soha-skills@v0.1.0")
	if err != nil {
		t.Fatalf("parse GitHub release source: %v", err)
	}
	want := "https://github.com/opensoha/soha-skills/releases/download/v0.1.0/soha-skills-0.1.0.tar.gz"
	if spec.Artifact != want || spec.ExpectedVersion != "0.1.0" || !spec.Remote {
		t.Fatalf("unexpected GitHub release spec %#v", spec)
	}
	latest, err := parseSkillReleaseSource("github:opensoha/soha-skills")
	if err != nil {
		t.Fatalf("parse latest GitHub release source: %v", err)
	}
	if latest.GitHubOwner != "opensoha" || latest.GitHubRepository != "soha-skills" || !latest.Remote {
		t.Fatalf("unexpected latest GitHub release spec %#v", latest)
	}
	explicitLatest, err := parseSkillReleaseSource("github:opensoha/soha-skills@latest")
	if err != nil {
		t.Fatalf("parse explicit latest GitHub release source: %v", err)
	}
	if explicitLatest.GitHubOwner != latest.GitHubOwner || explicitLatest.GitHubRepository != latest.GitHubRepository {
		t.Fatalf("explicit latest source %#v does not match default latest source %#v", explicitLatest, latest)
	}
	if _, err := parseSkillReleaseSource("github:opensoha/soha-skills@main"); err == nil {
		t.Fatal("expected mutable GitHub branch source to fail")
	}
	if _, err := parseSkillReleaseSource("http://example.com/soha-skills-0.1.0.tar.gz"); err == nil {
		t.Fatal("expected non-local HTTP release source to fail")
	}
}

func writeTestSkillsRelease(t *testing.T, releaseDir, releaseVersion, extraMember string) string {
	t.Helper()
	files := map[string][]byte{
		"catalog/compatibility-matrix.json": mustJSON(t, map[string]any{
			"schemaVersion": "opensoha.dev/skills-compatibility-matrix/v1",
			"skillsVersion": releaseVersion,
			"supportedVersions": map[string]any{
				"soha-cli": ">=0.1.0 <0.2.0",
			},
		}),
		"skills/ai-gateway/delivery-developer/SKILL.md": []byte("# Delivery Developer\n"),
		"skills/index.json": mustJSON(t, map[string]any{
			"schemaVersion": "opensoha.dev/skills-index/v1",
			"version":       releaseVersion,
			"skills":        []any{},
		}),
	}
	paths := make([]string, 0, len(files))
	for filePath := range files {
		paths = append(paths, filePath)
	}
	sort.Strings(paths)
	manifestFiles := make([]map[string]string, 0, len(paths))
	for _, filePath := range paths {
		digest := sha256.Sum256(files[filePath])
		manifestFiles = append(manifestFiles, map[string]string{
			"path":   filePath,
			"sha256": hex.EncodeToString(digest[:]),
		})
	}
	artifactName := fmt.Sprintf("soha-skills-%s.tar.gz", releaseVersion)
	manifestName := fmt.Sprintf("soha-skills-%s.manifest.json", releaseVersion)
	manifestBytes := mustJSON(t, map[string]any{
		"schemaVersion": "opensoha.dev/skills-release/v1",
		"version":       releaseVersion,
		"format":        "tar.gz",
		"artifact":      artifactName,
		"manifestUrl":   "https://github.com/opensoha/soha-skills/releases/download/v" + releaseVersion + "/" + manifestName,
		"files":         manifestFiles,
	})
	if err := os.WriteFile(filepath.Join(releaseDir, manifestName), manifestBytes, 0o644); err != nil {
		t.Fatalf("write release manifest: %v", err)
	}

	artifactPath := filepath.Join(releaseDir, artifactName)
	artifact, err := os.Create(artifactPath)
	if err != nil {
		t.Fatalf("create release artifact: %v", err)
	}
	compressed := gzip.NewWriter(artifact)
	archive := tar.NewWriter(compressed)
	for _, filePath := range paths {
		writeTestTarMember(t, archive, "soha-skills/"+filePath, files[filePath])
	}
	writeTestTarMember(t, archive, "soha-skills/"+manifestName, manifestBytes)
	if extraMember != "" {
		writeTestTarMember(t, archive, extraMember, []byte("escape\n"))
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close release tar: %v", err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatalf("close release gzip: %v", err)
	}
	if err := artifact.Close(); err != nil {
		t.Fatalf("close release artifact: %v", err)
	}

	artifactBytes, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("read release artifact: %v", err)
	}
	artifactDigest := sha256.Sum256(artifactBytes)
	artifactSHA := hex.EncodeToString(artifactDigest[:])
	checksum := fmt.Sprintf("%s  %s\n", artifactSHA, artifactName)
	if err := os.WriteFile(artifactPath+".sha256", []byte(checksum), 0o644); err != nil {
		t.Fatalf("write release checksum: %v", err)
	}
	report := mustJSON(t, map[string]any{
		"schemaVersion":  "opensoha.dev/skills-validation-report/v1",
		"repository":     "soha-skills",
		"generatedAt":    "2026-07-28T00:00:00Z",
		"releaseVersion": releaseVersion,
		"status":         "passed",
		"checks": []any{
			map[string]any{"name": "package", "status": "passed"},
		},
		"summary": map[string]any{
			"packageOutput": map[string]any{"sha256": artifactSHA},
		},
		"error": "",
	})
	reportPath := filepath.Join(releaseDir, fmt.Sprintf("soha-skills-%s.validation-report.json", releaseVersion))
	if err := os.WriteFile(reportPath, report, 0o644); err != nil {
		t.Fatalf("write validation report: %v", err)
	}
	return artifactPath
}

func writeTestTarMember(t *testing.T, archive *tar.Writer, name string, raw []byte) {
	t.Helper()
	header := &tar.Header{
		Name: name,
		Mode: 0o644,
		Size: int64(len(raw)),
	}
	if err := archive.WriteHeader(header); err != nil {
		t.Fatalf("write tar header %s: %v", name, err)
	}
	if _, err := archive.Write(raw); err != nil {
		t.Fatalf("write tar member %s: %v", name, err)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal JSON fixture: %v", err)
	}
	return append(raw, '\n')
}
