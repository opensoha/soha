package sohacli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func TestHostBundleDeterministicAndIgnoresMetadataDirs(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()

	writeBundleFixture := func(root string, mtime time.Time, mode os.FileMode) {
		if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, ".git", "objects"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0o755); err != nil {
			t.Fatal(err)
		}
		files := map[string]string{
			"index.html":                  "<!doctype html><html><body>Hello</body></html>\n",
			"assets/app.js":               "console.log('ready');\n",
			".DS_Store":                   "ignored-macos-metadata",
			".git/config":                 "[core]\n",
			"node_modules/pkg/.env":       "IGNORED_IN_NODE_MODULES=1\n",
			"node_modules/pkg/index.js":   "module.exports = {};\n",
			"node_modules/pkg/secret.key": "ignored-node-modules-key",
		}
		for rel, content := range files {
			full := filepath.Join(root, filepath.FromSlash(rel))
			if err := os.WriteFile(full, []byte(content), mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(full, mtime, mtime); err != nil {
				t.Fatal(err)
			}
		}
	}

	writeBundleFixture(dirA, time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC), 0o644)
	writeBundleFixture(dirB, time.Date(2026, 11, 20, 9, 8, 7, 0, time.UTC), 0o755)
	// Also verify worktree-style .git regular file is ignored cleanly.
	gitFileDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(gitFileDir, ".git"), []byte("gitdir: /tmp/worktree"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitFileDir, "index.html"), []byte("<h1>ok</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := buildHostStaticBundle(gitFileDir); err != nil {
		t.Fatalf("expected .git worktree file to be ignored, got %v", err)
	}

	bundleA, err := buildHostStaticBundle(dirA)
	if err != nil {
		t.Fatalf("buildHostStaticBundle(dirA): %v", err)
	}
	bundleB, err := buildHostStaticBundle(dirB)
	if err != nil {
		t.Fatalf("buildHostStaticBundle(dirB): %v", err)
	}
	if !bytes.Equal(bundleA.Archive, bundleB.Archive) || bundleA.BundleSHA256 != bundleB.BundleSHA256 {
		t.Fatalf("expected deterministic tar.gz output, shaA=%s shaB=%s", bundleA.BundleSHA256, bundleB.BundleSHA256)
	}
	if bundleA.FileCount != 2 || !bundleA.HasIndexHTML {
		t.Fatalf("unexpected bundle summary: %+v", bundleA)
	}

	entries := readTarGzMemberNames(t, bundleA.Archive)
	wantEntries := []string{"assets/app.js", "index.html"}
	if strings.Join(entries, ",") != strings.Join(wantEntries, ",") {
		t.Fatalf("archive members = %v, want %v", entries, wantEntries)
	}
}

func TestHostBundleRejectsUnsafeAndSensitiveInputs(t *testing.T) {
	t.Run("missing directory", func(t *testing.T) {
		_, err := buildHostStaticBundle(filepath.Join(t.TempDir(), "does-not-exist"))
		if err == nil {
			t.Fatal("expected missing directory error")
		}
	})

	t.Run("file instead of directory", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "index.html")
		if err := os.WriteFile(f, []byte("<h1>x</h1>"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := buildHostStaticBundle(f); err == nil || !strings.Contains(err.Error(), "not a directory") {
			t.Fatalf("expected not a directory error, got %v", err)
		}
	})

	t.Run("missing root index.html", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "sub", "index.html"), []byte("<h1>nested</h1>"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := buildHostStaticBundle(dir); err == nil || !strings.Contains(err.Error(), "missing root index.html") {
			t.Fatalf("expected missing root index.html error, got %v", err)
		}
	})

	for _, sensitiveName := range []string{
		".env",
		".env.production",
		"certs/server.pem",
		"tls/private.key",
		"id_rsa",
		"id_rsa.pub",
		"config/credentials.json",
	} {
		t.Run("sensitive-"+sensitiveName, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>ok</h1>"), 0o644); err != nil {
				t.Fatal(err)
			}
			full := filepath.Join(dir, filepath.FromSlash(sensitiveName))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte("placeholder"), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := buildHostStaticBundle(dir)
			if err == nil || !strings.Contains(err.Error(), "sensitive file") || !strings.Contains(err.Error(), sensitiveName) {
				t.Fatalf("expected actionable sensitive file error for %s, got %v", sensitiveName, err)
			}
		})
	}

	if runtime.GOOS != "windows" {
		t.Run("symlink root directory rejected", func(t *testing.T) {
			realDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(realDir, "index.html"), []byte("<h1>ok</h1>"), 0o644); err != nil {
				t.Fatal(err)
			}
			linkDir := filepath.Join(t.TempDir(), "linked-dist")
			if err := os.Symlink(realDir, linkDir); err != nil {
				t.Fatal(err)
			}
			if _, err := buildHostStaticBundle(linkDir); err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("expected symlink directory rejection, got %v", err)
			}
		})

		t.Run("symlink file inside bundle rejected", func(t *testing.T) {
			dir := t.TempDir()
			outside := filepath.Join(t.TempDir(), "outside.txt")
			if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>ok</h1>"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(dir, "leak.txt")); err != nil {
				t.Fatal(err)
			}
			if _, err := buildHostStaticBundle(dir); err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("expected symlink member rejection, got %v", err)
			}
		})
	}

	for _, unsafeRel := range []string{"../escape.html", "sub/../../escape.html", "/abs.html", `win\path.html`, "bad\x00name.html"} {
		if err := validateHostBundleRelPath(unsafeRel); err == nil {
			t.Fatalf("expected unsafe path %q to be rejected", unsafeRel)
		}
	}
}

func TestHostDeployLifecyclePreservesVisibilityAndSupportsDryRun(t *testing.T) {
	now := time.Date(2026, 11, 20, 15, 0, 0, 0, time.UTC)
	bundleDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bundleDir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "index.html"), []byte("<!doctype html><title>Ops</title>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "assets", "main.js"), []byte("console.log('ops');"), 0o644); err != nil {
		t.Fatal(err)
	}

	var (
		createCalls     int
		visibilityCalls int
		preflightCalls  int
		uploadCalls     int
		allowPreflight  = true
		rejectUpload    bool
		lastCreateReq   sohaapi.CreateHostedAppRequest
		lastUploadQuery string
	)

	existingOrgApp := sohaapi.HostedApp{
		ID:                "app_existing",
		Name:              "Shared Board",
		Kind:              sohaapi.HostedAppKindStatic,
		DNSLabel:          "shared-board-9f8e7d",
		Host:              "shared-board-9f8e7d.apps.soha.internal",
		URL:               "https://shared-board-9f8e7d.apps.soha.internal",
		SpaFallback:       true,
		OwnerUserID:       "u-1",
		MaintainerUserIDs: []string{"u-1"},
		Visibility:        sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModeOrganization},
		Status:            sohaapi.HostedAppStatusOnline,
		ActiveRevisionID:  "rev_1",
		RouteGeneration:   3,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/app-hosting/apps":
			createCalls++
			if err := json.NewDecoder(r.Body).Decode(&lastCreateReq); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			writeJSON(t, w, sohaapi.HostedAppResponse{
				Data: sohaapi.HostedApp{
					ID:                "app_new",
					Name:              lastCreateReq.Name,
					Kind:              sohaapi.HostedAppKindStatic,
					DNSLabel:          "ops-new-1a2b3c",
					Host:              "ops-new-1a2b3c.apps.soha.internal",
					URL:               "https://ops-new-1a2b3c.apps.soha.internal",
					SpaFallback:       lastCreateReq.SpaFallback != nil && *lastCreateReq.SpaFallback,
					OwnerUserID:       "u-1",
					MaintainerUserIDs: []string{"u-1"},
					Visibility:        sohaapi.HostedAppVisibilityScope{Mode: lastCreateReq.Visibility.Mode, TeamIDs: lastCreateReq.Visibility.TeamIDs, UserIDs: lastCreateReq.Visibility.UserIDs},
					Status:            sohaapi.HostedAppStatusDraft,
					RouteGeneration:   1,
					CreatedAt:         now,
					UpdatedAt:         now,
				},
			})
		case "GET /api/v1/app-hosting/apps/app_existing":
			writeJSON(t, w, sohaapi.HostedAppResponse{Data: existingOrgApp})
		case "PUT /api/v1/app-hosting/apps/app_existing/visibility":
			visibilityCalls++
			var req sohaapi.UpdateHostedAppVisibilityRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			existingOrgApp.Visibility = sohaapi.HostedAppVisibilityScope{
				Mode:    req.Visibility.Mode,
				TeamIDs: req.Visibility.TeamIDs,
				UserIDs: req.Visibility.UserIDs,
			}
			existingOrgApp.RouteGeneration++
			writeJSON(t, w, sohaapi.HostedAppResponse{Data: existingOrgApp})
		case "POST /api/v1/app-hosting/apps/app_new/deployments/preflight",
			"POST /api/v1/app-hosting/apps/app_existing/deployments/preflight":
			preflightCalls++
			appID := "app_new"
			url := "https://ops-new-1a2b3c.apps.soha.internal"
			if strings.Contains(r.URL.Path, "app_existing") {
				appID = "app_existing"
				url = existingOrgApp.URL
			}
			blockers := []string{}
			if !allowPreflight {
				blockers = []string{"owner quota exceeded (20/20 static apps)"}
			}
			writeJSON(t, w, sohaapi.HostedAppPreflightResponse{
				Data: sohaapi.HostedAppPreflightResult{
					Allowed:              allowPreflight,
					AppID:                appID,
					URL:                  url,
					Blockers:             blockers,
					Warnings:             []string{"large bundle warning"},
					MaxCompressedBytes:   52428800,
					MaxUncompressedBytes: 157286400,
					MaxFileCount:         2000,
				},
			})
		case "POST /api/v1/app-hosting/apps/app_new/revisions",
			"POST /api/v1/app-hosting/apps/app_existing/revisions":
			uploadCalls++
			lastUploadQuery = r.URL.RawQuery
			bundle, intent := readHostUpload(t, r)
			rawGz, err := io.ReadAll(bundle)
			if err != nil {
				t.Fatal(err)
			}
			members := readTarGzMemberNames(t, rawGz)
			if strings.Join(members, ",") != "assets/main.js,index.html" {
				t.Fatalf("uploaded tar members = %v", members)
			}
			appOut := existingOrgApp
			if strings.Contains(r.URL.Path, "app_new") {
				appOut = sohaapi.HostedApp{
					ID:                "app_new",
					Name:              lastCreateReq.Name,
					Kind:              sohaapi.HostedAppKindStatic,
					DNSLabel:          "ops-new-1a2b3c",
					Host:              "ops-new-1a2b3c.apps.soha.internal",
					URL:               "https://ops-new-1a2b3c.apps.soha.internal",
					OwnerUserID:       "u-1",
					MaintainerUserIDs: []string{"u-1"},
					Visibility:        sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModePrivate},
					Status:            sohaapi.HostedAppStatusOnline,
					ActiveRevisionID:  "rev_2",
					RouteGeneration:   2,
					CreatedAt:         now,
					UpdatedAt:         now,
				}
			}
			if intent != nil {
				appOut.Visibility = sohaapi.HostedAppVisibilityScope{
					Mode: intent.Visibility.Mode, TeamIDs: intent.Visibility.TeamIDs, UserIDs: intent.Visibility.UserIDs,
				}
			}
			if rejectUpload {
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			w.WriteHeader(http.StatusCreated)
			writeJSON(t, w, sohaapi.HostedAppDeploymentResponse{
				Data: sohaapi.HostedAppDeploymentReceipt{
					App: appOut,
					Revision: sohaapi.HostedAppRevision{
						ID:             "rev_2",
						AppID:          appOut.ID,
						RevisionNumber: 2,
						Kind:           sohaapi.HostedAppKindStatic,
						Status:         sohaapi.HostedAppRevisionStatusReady,
						BundleSha256:   strings.Repeat("b", 64),
						FileCount:      2,
						TotalBytes:     64,
						IdempotencyKey: r.URL.Query().Get("idempotencyKey"),
						CreatedBy:      "u-1",
						CreatedAt:      now,
					},
				},
			})
		default:
			t.Fatalf("unexpected HTTP request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	cfgPath := writeTestConfig(t, server.URL)

	t.Run("non-interactive without --yes declines before calling server", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		code := Run(context.Background(), []string{"host", "deploy", bundleDir, "--name", "Ops"}, Runtime{
			In:         strings.NewReader(""),
			Out:        &out,
			Err:        &errBuf,
			ConfigPath: cfgPath,
		})
		if code != 1 || createCalls != 0 || uploadCalls != 0 {
			t.Fatalf("code=%d createCalls=%d uploadCalls=%d err=%s", code, createCalls, uploadCalls, errBuf.String())
		}
	})

	t.Run("dry-run runs preflight without uploading", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		code := Run(context.Background(), []string{"host", "deploy", bundleDir, "--app-id", "app_existing", "--dry-run"}, Runtime{
			Out:        &out,
			Err:        &errBuf,
			ConfigPath: cfgPath,
		})
		if code != 0 || uploadCalls != 0 || preflightCalls != 1 {
			t.Fatalf("code=%d preflightCalls=%d uploadCalls=%d out=%s err=%s", code, preflightCalls, uploadCalls, out.String(), errBuf.String())
		}
		if !strings.Contains(out.String(), "Preflight: allowed") || !strings.Contains(out.String(), "app_existing") {
			t.Fatalf("unexpected dry-run output: %s", out.String())
		}
	})

	t.Run("blocked preflight exits 1 without uploading", func(t *testing.T) {
		allowPreflight = false
		defer func() { allowPreflight = true }()
		var out, errBuf bytes.Buffer
		code := Run(context.Background(), []string{"host", "deploy", bundleDir, "--app-id", "app_existing", "--yes"}, Runtime{
			Out:        &out,
			Err:        &errBuf,
			ConfigPath: cfgPath,
		})
		if code != 1 || uploadCalls != 0 {
			t.Fatalf("expected blocked preflight exit 1 without upload, code=%d uploadCalls=%d", code, uploadCalls)
		}
		if !strings.Contains(out.String(), "owner quota exceeded") {
			t.Fatalf("expected blocker in stdout, got %s", out.String())
		}
	})

	t.Run("create new app defaults visibility to private and streams gzip bundle", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		code := Run(context.Background(), []string{"host", "deploy", bundleDir, "--name", "Ops Tool", "--slug", "ops-tool", "--spa", "--idempotency-key", "idem-new-1", "--yes"}, Runtime{
			Out:        &out,
			Err:        &errBuf,
			ConfigPath: cfgPath,
		})
		if code != 0 {
			t.Fatalf("deploy failed (%d): %s", code, errBuf.String())
		}
		if lastCreateReq.Visibility == nil || lastCreateReq.Visibility.Mode != sohaapi.HostedAppVisibilityModePrivate {
			t.Fatalf("expected default private visibility on new app, got %+v", lastCreateReq.Visibility)
		}
		if !strings.Contains(lastUploadQuery, "idempotencyKey=idem-new-1") || !strings.Contains(lastUploadQuery, "spaFallback=true") {
			t.Fatalf("unexpected upload query: %s", lastUploadQuery)
		}
		for _, want := range []string{"App ID: app_new", "Revision: rev_2 (r2)", "Visibility: private", "Status: online", "URL: https://ops-new-1a2b3c.apps.soha.internal"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("deploy summary missing %q:\n%s", want, out.String())
			}
		}
	})

	t.Run("updating existing app without --visibility preserves existing visibility and spaFallback", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		code := Run(context.Background(), []string{"host", "deploy", bundleDir, "--app-id", "app_existing", "--yes", "--output", "json"}, Runtime{
			Out:        &out,
			Err:        &errBuf,
			ConfigPath: cfgPath,
		})
		if code != 0 || visibilityCalls != 0 {
			t.Fatalf("code=%d visibilityCalls=%d err=%s", code, visibilityCalls, errBuf.String())
		}
		if !strings.Contains(lastUploadQuery, "spaFallback=true") {
			t.Fatalf("expected existing spaFallback=true to be preserved, query=%s", lastUploadQuery)
		}
		var receipt sohaapi.HostedAppDeploymentReceipt
		if err := json.Unmarshal(out.Bytes(), &receipt); err != nil {
			t.Fatalf("unmarshal JSON receipt: %v", err)
		}
		if receipt.App.Visibility.Mode != sohaapi.HostedAppVisibilityModeOrganization {
			t.Fatalf("visibility = %q, want organization", receipt.App.Visibility.Mode)
		}
	})

	t.Run("updating existing app attaches the audience to the upload", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		code := Run(context.Background(), []string{"host", "deploy", bundleDir, "--app-id", "app_existing", "--visibility", "selected", "--team", "team_fin", "--user", "usr_carol", "--yes"}, Runtime{
			Out:        &out,
			Err:        &errBuf,
			ConfigPath: cfgPath,
		})
		if code != 0 || visibilityCalls != 0 {
			t.Fatalf("code=%d visibilityCalls=%d err=%s", code, visibilityCalls, errBuf.String())
		}
		if !strings.Contains(out.String(), "selected (teams=team_fin users=usr_carol)") {
			t.Fatalf("unexpected visibility summary: %s", out.String())
		}
	})
	t.Run("rejected upload does not call the visibility endpoint", func(t *testing.T) {
		rejectUpload = true
		var out, errBuf bytes.Buffer
		code := Run(context.Background(), []string{"host", "deploy", bundleDir, "--app-id", "app_existing", "--visibility", "private", "--idempotency-key", "publish-rejected", "--yes"}, Runtime{
			Out: &out, Err: &errBuf, ConfigPath: cfgPath,
		})
		if code == 0 || visibilityCalls != 0 {
			t.Fatalf("code=%d visibilityCalls=%d output=%s", code, visibilityCalls, out.String())
		}
	})
}

func readHostUpload(t *testing.T, r *http.Request) (io.Reader, *sohaapi.UpdateHostedAppVisibilityRequest) {
	t.Helper()
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	if contentType == "application/gzip" {
		return r.Body, nil
	}
	reader, err := r.MultipartReader()
	if err != nil {
		t.Fatal(err)
	}
	meta, err := reader.NextPart()
	if err != nil || meta.FormName() != "publishVisibility" {
		t.Fatalf("visibility part: %v", err)
	}
	var intent sohaapi.UpdateHostedAppVisibilityRequest
	if err := json.NewDecoder(meta).Decode(&intent); err != nil {
		t.Fatal(err)
	}
	bundle, err := reader.NextPart()
	if err != nil || bundle.FormName() != "bundle" || bundle.Header.Get("Content-Type") != "application/gzip" {
		t.Fatalf("bundle part: %v", err)
	}
	return bundle, &intent
}

func TestHostManagementAndAudienceSubcommands(t *testing.T) {
	now := time.Date(2026, 11, 20, 16, 0, 0, 0, time.UTC)
	sampleApp := sohaapi.HostedApp{
		ID:                "app_42",
		Name:              "Finance Tracker",
		Kind:              sohaapi.HostedAppKindStatic,
		DNSLabel:          "finance-tracker-a1b2c3",
		Host:              "finance-tracker-a1b2c3.apps.soha.internal",
		URL:               "https://finance-tracker-a1b2c3.apps.soha.internal",
		OwnerUserID:       "u-1",
		MaintainerUserIDs: []string{"u-1"},
		Visibility:        sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModePrivate},
		Status:            sohaapi.HostedAppStatusOnline,
		ActiveRevisionID:  "rev_3",
		RouteGeneration:   5,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/app-hosting/apps":
			if r.URL.Query().Get("scope") != "mine" || r.URL.Query().Get("status") != "online" {
				t.Fatalf("unexpected list query: %s", r.URL.RawQuery)
			}
			writeJSON(t, w, sohaapi.HostedAppListResponse{Data: sohaapi.HostedAppList{Items: []sohaapi.HostedApp{sampleApp}, Total: 1}})
		case "GET /api/v1/app-hosting/apps/app_42":
			writeJSON(t, w, sohaapi.HostedAppResponse{Data: sampleApp})
		case "GET /api/v1/app-hosting/apps/app_42/revisions":
			writeJSON(t, w, sohaapi.HostedAppRevisionListResponse{
				Data: sohaapi.HostedAppRevisionList{
					Items: []sohaapi.HostedAppRevision{{
						ID:             "rev_3",
						AppID:          "app_42",
						RevisionNumber: 3,
						Kind:           sohaapi.HostedAppKindStatic,
						Status:         sohaapi.HostedAppRevisionStatusReady,
						BundleSha256:   strings.Repeat("c", 64),
						FileCount:      5,
						TotalBytes:     2048,
						CreatedBy:      "u-1",
						CreatedAt:      now,
					}},
					Total: 1,
				},
			})
		case "GET /api/v1/app-hosting/apps/app_42/revisions/rev_3":
			writeJSON(t, w, sohaapi.HostedAppRevisionResponse{
				Data: sohaapi.HostedAppRevision{
					ID:             "rev_3",
					AppID:          "app_42",
					RevisionNumber: 3,
					Kind:           sohaapi.HostedAppKindStatic,
					Status:         sohaapi.HostedAppRevisionStatusReady,
					BundleSha256:   strings.Repeat("c", 64),
					FileCount:      5,
					TotalBytes:     2048,
					CreatedBy:      "u-1",
					CreatedAt:      now,
				},
			})
		case "POST /api/v1/app-hosting/apps/app_42/rollback":
			var req sohaapi.HostedAppRollbackRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req.TargetRevisionID != "rev_1" || req.IdempotencyKey != "rbk-1" {
				t.Fatalf("unexpected rollback req: %+v", req)
			}
			rolled := sampleApp
			rolled.ActiveRevisionID = "rev_1"
			writeJSON(t, w, sohaapi.HostedAppDeploymentResponse{
				Data: sohaapi.HostedAppDeploymentReceipt{
					App: rolled,
					Revision: sohaapi.HostedAppRevision{
						ID:             "rev_1",
						AppID:          "app_42",
						RevisionNumber: 1,
						Kind:           sohaapi.HostedAppKindStatic,
						Status:         sohaapi.HostedAppRevisionStatusReady,
						BundleSha256:   strings.Repeat("a", 64),
						CreatedBy:      "u-1",
						CreatedAt:      now,
					},
				},
			})
		case "PUT /api/v1/app-hosting/apps/app_42/visibility":
			var req sohaapi.UpdateHostedAppVisibilityRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			updated := sampleApp
			updated.Visibility = sohaapi.HostedAppVisibilityScope{Mode: req.Visibility.Mode, TeamIDs: req.Visibility.TeamIDs, UserIDs: req.Visibility.UserIDs}
			updated.RouteGeneration = 6
			writeJSON(t, w, sohaapi.HostedAppResponse{Data: updated})
		case "POST /api/v1/app-hosting/apps/app_42/offline":
			offline := sampleApp
			offline.Status = sohaapi.HostedAppStatusOffline
			offline.RouteGeneration = 7
			writeJSON(t, w, sohaapi.HostedAppResponse{Data: offline})
		case "POST /api/v1/app-hosting/apps/app_42/online":
			writeJSON(t, w, sohaapi.HostedAppResponse{Data: sampleApp})
		case "GET /api/v1/app-hosting/audience-candidates":
			if r.URL.Query().Get("query") != "fin" || r.URL.Query().Get("limit") != "10" {
				t.Fatalf("unexpected audience query: %s", r.URL.RawQuery)
			}
			writeJSON(t, w, sohaapi.AppHostingAudienceCandidatesResponse{
				Data: sohaapi.AppHostingAudienceCandidates{
					Teams: []sohaapi.AppHostingAudienceTeam{{ID: "team_fin", Name: "Finance", Slug: "finance"}},
					Users: []sohaapi.AppHostingAudienceUser{{ID: "usr_carol", Username: "carol", DisplayName: "Carol Wu", Department: "Finance"}},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	cfgPath := writeTestConfig(t, server.URL)
	runOK := func(args ...string) string {
		t.Helper()
		var out, errBuf bytes.Buffer
		code := Run(context.Background(), args, Runtime{Out: &out, Err: &errBuf, ConfigPath: cfgPath})
		if code != 0 {
			t.Fatalf("Run(%v) = %d, stderr=%s", args, code, errBuf.String())
		}
		return out.String()
	}

	if got := runOK("host", "list", "--status", "online"); !strings.Contains(got, "app_42") {
		t.Fatalf("list output: %s", got)
	}
	if got := runOK("host", "get", "app_42"); !strings.Contains(got, "URL: https://finance-tracker-a1b2c3.apps.soha.internal") {
		t.Fatalf("get output: %s", got)
	}
	if got := runOK("host", "revisions", "app_42", "--limit", "10"); !strings.Contains(got, "rev_3\tr3") {
		t.Fatalf("revisions output: %s", got)
	}
	if got := runOK("host", "revisions", "app_42", "--revision", "rev_3"); !strings.Contains(got, "Revision ID: rev_3") {
		t.Fatalf("revision detail output: %s", got)
	}
	if got := runOK("host", "rollback", "app_42", "--revision", "rev_1", "--idempotency-key", "rbk-1", "--reason", "regression", "--yes"); !strings.Contains(got, "Revision: rev_1 (r1)") {
		t.Fatalf("rollback output: %s", got)
	}
	if got := runOK("host", "visibility", "app_42", "--mode", "private", "--yes"); !strings.Contains(got, "immediately denied") {
		t.Fatalf("visibility output: %s", got)
	}
	if got := runOK("host", "offline", "app_42", "--reason", "maintenance", "--yes"); !strings.Contains(got, "Status: offline") {
		t.Fatalf("offline output: %s", got)
	}
	if got := runOK("host", "online", "app_42", "--yes"); !strings.Contains(got, "Status: online") {
		t.Fatalf("online output: %s", got)
	}
	audOut := runOK("host", "audience", "--query", "fin", "--limit", "10")
	if !strings.Contains(audOut, "team\tteam_fin\tFinance\tfinance") || !strings.Contains(audOut, "user\tusr_carol\tcarol\tCarol Wu\tFinance") {
		t.Fatalf("audience output: %s", audOut)
	}

	// Verify usage validation errors map to exit code 2.
	for _, badArgs := range [][]string{
		{"host"},
		{"host", "unknown"},
		{"host", "visibility", "app_42", "--mode", "selected", "--yes"},
		{"host", "visibility", "app_42", "--mode", "private", "--user", "usr_1", "--yes"},
		{"host", "rollback", "app_42", "--yes"},
	} {
		var out, errBuf bytes.Buffer
		if code := Run(context.Background(), badArgs, Runtime{Out: &out, Err: &errBuf, ConfigPath: cfgPath}); code != 2 {
			t.Fatalf("Run(%v) = %d, want 2 (stderr=%s)", badArgs, code, errBuf.String())
		}
	}
}

func TestHostClientTokenRefreshAndSecretRedaction(t *testing.T) {
	now := time.Date(2026, 11, 20, 17, 0, 0, 0, time.UTC)
	staleAccess := "stale-secret-access-token-12345"
	freshAccess := "fresh-secret-access-token-67890"
	refreshTok := "stored-refresh-token-abcdef"

	var refreshCalls, uploadCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			refreshCalls++
			if got := r.Header.Get("Authorization"); got != "" {
				t.Fatalf("refresh request must not carry Authorization header, got %q", got)
			}
			writeJSON(t, w, map[string]any{
				"data": map[string]any{
					"tokens": map[string]any{
						"accessToken":  freshAccess,
						"refreshToken": refreshTok,
						"expiresIn":    3600,
					},
					"user": map[string]any{"userId": "u-1", "userName": "Ada"},
				},
			})
		case "/api/v1/app-hosting/apps/app_secret":
			if r.Header.Get("Authorization") == "Bearer "+staleAccess {
				w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if r.Header.Get("Authorization") != "Bearer "+freshAccess {
				t.Fatalf("expected refreshed token, got %q", r.Header.Get("Authorization"))
			}
			writeJSON(t, w, sohaapi.HostedAppResponse{
				Data: sohaapi.HostedApp{
					ID:                "app_secret",
					Name:              "Refreshed App",
					Kind:              sohaapi.HostedAppKindStatic,
					DNSLabel:          "refreshed-112233",
					Host:              "refreshed-112233.apps.soha.internal",
					URL:               "https://refreshed-112233.apps.soha.internal",
					OwnerUserID:       "u-1",
					MaintainerUserIDs: []string{"u-1"},
					Visibility:        sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModePrivate},
					Status:            sohaapi.HostedAppStatusOnline,
					CreatedAt:         now,
					UpdatedAt:         now,
				},
			})
		case "/api/v1/app-hosting/apps/app_secret/revisions":
			uploadCalls++
			rawBundle, intent := readHostUpload(t, r)
			bundle, err := io.ReadAll(rawBundle)
			if err != nil || string(bundle) != "gzip fixture" || intent == nil || intent.Reason != "deploy review" {
				t.Errorf("multipart upload changed on replay: %+v, %v", intent, err)
			}
			if r.Header.Get("Authorization") == "Bearer "+staleAccess {
				w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if r.Header.Get("Authorization") != "Bearer "+freshAccess {
				t.Error("upload retry did not use refreshed token")
			}
			writeJSON(t, w, sohaapi.HostedAppDeploymentResponse{
				Data: sohaapi.HostedAppDeploymentReceipt{Revision: sohaapi.HostedAppRevision{ID: "rev_1"}},
			})
		case "/api/v1/app-hosting/apps/app_leak":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "upstream failure echoing "+freshAccess+" and "+refreshTok)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfgPath := writeTestConfigWithProfile(t, ProfileConfig{
		ServerURL:    server.URL,
		AccessToken:  staleAccess,
		RefreshToken: refreshTok,
		UserID:       "u-1",
		UserName:     "Ada",
	})

	var out, errBuf bytes.Buffer
	code := Run(context.Background(), []string{"host", "get", "app_secret"}, Runtime{Out: &out, Err: &errBuf, ConfigPath: cfgPath})
	if code != 0 || refreshCalls != 1 {
		t.Fatalf("code=%d refreshCalls=%d err=%s", code, refreshCalls, errBuf.String())
	}

	out.Reset()
	errBuf.Reset()
	code = Run(context.Background(), []string{"host", "get", "app_leak"}, Runtime{Out: &out, Err: &errBuf, ConfigPath: cfgPath})
	if code != 1 {
		t.Fatalf("expected error code 1, got %d", code)
	}
	if strings.Contains(errBuf.String(), freshAccess) || strings.Contains(errBuf.String(), refreshTok) {
		t.Fatalf("stderr leaked credential: %s", errBuf.String())
	}

	stalePath := writeTestConfigWithProfile(t, ProfileConfig{
		ServerURL: server.URL, AccessToken: staleAccess, RefreshToken: refreshTok, UserID: "u-1", UserName: "Ada",
	})
	client, err := loadHostClient(t.Context(), Runtime{ConfigPath: stalePath}, "")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := client.UploadRevision(t.Context(), "app_secret", strings.NewReader("gzip fixture"),
		sohaapi.UploadHostedAppRevisionParams{IdempotencyKey: "same-on-retry"}, &sohaapi.UpdateHostedAppVisibilityRequest{
			Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModePrivate},
			Reason:     "deploy review",
		})
	if err != nil || receipt.Revision.ID != "rev_1" || uploadCalls != 2 || refreshCalls != 2 {
		t.Fatalf("multipart retry: revision=%s err=%v uploads=%d refreshes=%d", receipt.Revision.ID, err, uploadCalls, refreshCalls)
	}
}

func TestHostHelpCompletionAndDocsMetadata(t *testing.T) {
	var helpOut bytes.Buffer
	if code := Run(context.Background(), []string{"help", "host"}, Runtime{Out: &helpOut, Err: &bytes.Buffer{}}); code != 0 {
		t.Fatalf("help host returned %d", code)
	}
	for _, sub := range []string{"deploy", "list", "get", "revisions", "rollback", "visibility", "maintainers", "offline", "online", "delete", "audience"} {
		if !strings.Contains(helpOut.String(), sub) {
			t.Fatalf("help host missing %q: %s", sub, helpOut.String())
		}
	}

	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		var compOut bytes.Buffer
		if code := Run(context.Background(), []string{"completion", shell}, Runtime{Out: &compOut, Err: &bytes.Buffer{}}); code != 0 {
			t.Fatalf("completion %s returned %d", shell, code)
		}
		for _, want := range []string{"host", "maintainers", "delete", "audience"} {
			if !strings.Contains(compOut.String(), want) {
				t.Fatalf("completion %s missing %q: %s", shell, want, compOut.String())
			}
		}
	}
}

func TestHostMaintainersInspectUpdateAndClear(t *testing.T) {
	now := time.Date(2026, 11, 21, 4, 0, 0, 0, time.UTC)
	alice := sohaapi.HostedAppUserSummary{UserID: "usr_alice", DisplayName: "Alice Chen", TeamNames: []string{"Growth Analytics"}}
	bob := sohaapi.HostedAppUserSummary{UserID: "usr_bob", DisplayName: "Bob Li", TeamNames: []string{"Platform"}}
	carol := sohaapi.HostedAppUserSummary{UserID: "usr_carol", DisplayName: "Carol Wu", TeamNames: []string{"Finance"}}

	currentApp := sohaapi.HostedApp{
		ID:                "app_collab",
		Name:              "Growth Board",
		Kind:              sohaapi.HostedAppKindStatic,
		DNSLabel:          "growth-board-1a2b3c",
		Host:              "growth-board-1a2b3c.apps.soha.internal",
		URL:               "https://growth-board-1a2b3c.apps.soha.internal",
		OwnerUserID:       "usr_alice",
		OwnerProfile:      &alice,
		MaintainerUserIDs: []string{"usr_bob"},
		Maintainers:       []sohaapi.HostedAppUserSummary{bob},
		Visibility:        sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModePrivate},
		Status:            sohaapi.HostedAppStatusOnline,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	var lastPutBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/app-hosting/apps/app_collab":
			writeJSON(t, w, sohaapi.HostedAppResponse{Data: currentApp})
		case "PUT /api/v1/app-hosting/apps/app_collab/maintainers":
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			lastPutBody = strings.TrimSpace(string(raw))
			var req sohaapi.UpdateHostedAppMaintainersRequest
			if err := json.Unmarshal(raw, &req); err != nil {
				t.Fatal(err)
			}
			currentApp.MaintainerUserIDs = req.MaintainerUserIDs
			if len(req.MaintainerUserIDs) == 0 {
				currentApp.Maintainers = nil
			} else {
				currentApp.Maintainers = []sohaapi.HostedAppUserSummary{bob, carol}
			}
			writeJSON(t, w, sohaapi.HostedAppResponse{Data: currentApp})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	cfgPath := writeTestConfig(t, server.URL)

	var out, errBuf bytes.Buffer
	if code := Run(context.Background(), []string{"host", "maintainers", "app_collab"}, Runtime{Out: &out, Err: &errBuf, ConfigPath: cfgPath}); code != 0 {
		t.Fatalf("inspect maintainers failed (%d): %s", code, errBuf.String())
	}
	for _, want := range []string{"Owner: Alice Chen (usr_alice) [teams=Growth Analytics]", "Maintainers (1):", "usr_bob\tBob Li\tPlatform", "协作者仅具备更新版本与回滚权限，访问仍由可见范围控制"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("inspect maintainers missing %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	if code := Run(context.Background(), []string{"host", "maintainers", "app_collab", "--users", "usr_bob,usr_carol,usr_bob"}, Runtime{Out: &out, Err: &errBuf, ConfigPath: cfgPath}); code != 0 {
		t.Fatalf("update maintainers failed (%d): %s", code, errBuf.String())
	}
	if !strings.Contains(lastPutBody, `"maintainerUserIds":["usr_bob","usr_carol"]`) || !strings.Contains(out.String(), "Carol Wu\tFinance") {
		t.Fatalf("unexpected update maintainers result: body=%s out=%s", lastPutBody, out.String())
	}

	out.Reset()
	if code := Run(context.Background(), []string{"host", "maintainers", "app_collab", "--clear"}, Runtime{Out: &out, Err: &errBuf, ConfigPath: cfgPath}); code != 0 {
		t.Fatalf("clear maintainers failed (%d): %s", code, errBuf.String())
	}
	if lastPutBody != `{"maintainerUserIds":[]}` || !strings.Contains(out.String(), "Maintainers: none") {
		t.Fatalf("expected non-null empty maintainerUserIds array, got body=%s out=%s", lastPutBody, out.String())
	}

	for _, badArgs := range [][]string{
		{"host", "maintainers"},
		{"host", "maintainers", "app_collab", "--users", "usr_bob", "--clear"},
		{"host", "maintainers", "app_collab", "--users", ""},
	} {
		if code := Run(context.Background(), badArgs, Runtime{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, ConfigPath: cfgPath}); code != 2 {
			t.Fatalf("Run(%v) = %d, want 2", badArgs, code)
		}
	}
}

func TestHostDeleteRequiresExactNameConfirmation(t *testing.T) {
	var deleteCalls int
	var gotReq sohaapi.DeleteHostedAppRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/app-hosting/apps/app_del/delete" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		deleteCalls++
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatal(err)
		}
		if gotReq.ConfirmationName != "Quarterly Sales Dashboard" {
			w.WriteHeader(http.StatusConflict)
			writeJSON(t, w, map[string]any{"error": map[string]any{"message": "confirmation name mismatch"}})
			return
		}
		writeJSON(t, w, sohaapi.DeleteHostedAppResponse{
			Data: sohaapi.DeleteHostedAppResult{AppID: "app_del", Deleted: true},
		})
	}))
	defer server.Close()
	cfgPath := writeTestConfig(t, server.URL)

	for _, missingConfirm := range [][]string{
		{"host", "delete", "app_del"},
		{"host", "delete", "app_del", "--yes"},
		{"host", "delete", "app_del", "--confirm", "   "},
	} {
		var out, errBuf bytes.Buffer
		code := Run(context.Background(), missingConfirm, Runtime{Out: &out, Err: &errBuf, ConfigPath: cfgPath})
		if code != 2 || deleteCalls != 0 {
			t.Fatalf("Run(%v) = %d, deleteCalls=%d, stderr=%s", missingConfirm, code, deleteCalls, errBuf.String())
		}
		if !strings.Contains(errBuf.String(), "--confirm") || !strings.Contains(errBuf.String(), "需传入精确应用名称二次确认") {
			t.Fatalf("expected actionable --confirm error, got %s", errBuf.String())
		}
	}

	var out, errBuf bytes.Buffer
	if code := Run(context.Background(), []string{"host", "delete", "app_del", "--confirm", "Wrong Name"}, Runtime{Out: &out, Err: &errBuf, ConfigPath: cfgPath}); code != 1 {
		t.Fatalf("expected server name mismatch exit 1, got %d", code)
	}

	out.Reset()
	errBuf.Reset()
	code := Run(context.Background(), []string{"host", "delete", "app_del", "--confirm", "Quarterly Sales Dashboard", "--reason", "retired report"}, Runtime{
		Out:        &out,
		Err:        &errBuf,
		ConfigPath: cfgPath,
	})
	if code != 0 || gotReq.Reason != "retired report" {
		t.Fatalf("delete failed (%d): reason=%q err=%s", code, gotReq.Reason, errBuf.String())
	}
	for _, want := range []string{"App ID: app_del", "Confirmed Name: Quarterly Sales Dashboard", "Deleted: true"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("delete output missing %q:\n%s", want, out.String())
		}
	}
}

func newApprovalFixtureApp(now time.Time) sohaapi.HostedApp {
	alice := sohaapi.HostedAppUserSummary{UserID: "usr_alice", DisplayName: "Alice Chen", TeamNames: []string{"Growth"}}
	bob := sohaapi.HostedAppUserSummary{UserID: "usr_bob", DisplayName: "Bob Li", TeamNames: []string{"Platform"}}
	return sohaapi.HostedApp{
		ID:                "app_org",
		Name:              "Quarterly Metrics",
		Kind:              sohaapi.HostedAppKindStatic,
		DNSLabel:          "quarterly-metrics-1a2b3c",
		Host:              "quarterly-metrics-1a2b3c.apps.soha.internal",
		URL:               "https://quarterly-metrics-1a2b3c.apps.soha.internal",
		OwnerUserID:       "usr_alice",
		OwnerProfile:      &alice,
		MaintainerUserIDs: []string{"usr_bob"},
		Maintainers:       []sohaapi.HostedAppUserSummary{bob},
		Visibility:        sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModeSelected, TeamIDs: []string{"team_ops"}},
		Status:            sohaapi.HostedAppStatusOnline,
		ActiveRevisionID:  "rev_2",
		RouteGeneration:   3,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

func TestHostVisibilityPendingApprovalWithdrawAndDetailListEnhancements(t *testing.T) {
	now := time.Date(2026, 11, 21, 4, 15, 0, 0, time.UTC)
	appState := newApprovalFixtureApp(now)
	var lastVisReason, lastWithdrawReason string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "PUT /api/v1/app-hosting/apps/app_org/visibility":
			var req sohaapi.UpdateHostedAppVisibilityRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatal(err)
			}
			lastVisReason = req.Reason
			appState.PendingVisibilityApproval = &sohaapi.HostedAppVisibilityApproval{
				ID:                  "appr_77",
				AppID:               "app_org",
				AppName:             appState.Name,
				AppDNSLabel:         appState.DNSLabel,
				RequesterUserID:     "usr_alice",
				CurrentVisibility:   appState.Visibility,
				RequestedVisibility: sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModeOrganization},
				BaseRouteGeneration: 3,
				PolicyVersion:       2,
				Status:              sohaapi.HostedAppVisibilityApprovalStatusPending,
				Reason:              req.Reason,
				CreatedAt:           now,
				UpdatedAt:           now,
			}
			writeJSON(t, w, sohaapi.HostedAppResponse{Data: appState})
		case "GET /api/v1/app-hosting/apps/app_org":
			writeJSON(t, w, sohaapi.HostedAppResponse{Data: appState})
		case "GET /api/v1/app-hosting/apps":
			writeJSON(t, w, sohaapi.HostedAppListResponse{Data: sohaapi.HostedAppList{Items: []sohaapi.HostedApp{appState}, Total: 1}})
		case "POST /api/v1/app-hosting/apps/app_org/visibility-approval/withdraw":
			var req sohaapi.HostedAppLifecycleActionRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			lastWithdrawReason = req.Reason
			appState.PendingVisibilityApproval = nil
			writeJSON(t, w, sohaapi.HostedAppResponse{Data: appState})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	cfgPath := writeTestConfig(t, server.URL)

	var out, errBuf bytes.Buffer
	if code := Run(context.Background(), []string{"host", "visibility", "app_org", "--mode", "organization", "--reason", "Open to all staff", "--yes"}, Runtime{Out: &out, Err: &errBuf, ConfigPath: cfgPath}); code != 0 {
		t.Fatalf("visibility approval request failed (%d): %s", code, errBuf.String())
	}
	if lastVisReason != "Open to all staff" || strings.Contains(out.String(), "Visibility: organization") || strings.Contains(out.String(), "visibility is now organization") {
		t.Fatalf("must not claim organization visibility before approval:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "已提交全公司公开审批申请（单号 appr_77），审批通过前继续维持当前可见范围 selected") {
		t.Fatalf("missing pending approval notice:\n%s", out.String())
	}

	out.Reset()
	if code := Run(context.Background(), []string{"host", "get", "app_org"}, Runtime{Out: &out, Err: &errBuf, ConfigPath: cfgPath}); code != 0 {
		t.Fatalf("host get failed (%d): %s", code, errBuf.String())
	}
	for _, want := range []string{"Pending Visibility Approval: appr_77 (status=pending requested=organization current=selected reason=\"Open to all staff\")", "Maintainers: Bob Li (usr_bob) [teams=Platform]"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("host get missing %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	if code := Run(context.Background(), []string{"host", "list"}, Runtime{Out: &out, Err: &errBuf, ConfigPath: cfgPath}); code != 0 {
		t.Fatalf("host list failed (%d): %s", code, errBuf.String())
	}
	for _, want := range []string{"maintainers=Bob Li(usr_bob)", "pendingApproval=appr_77(pending->organization)"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("host list missing %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	if code := Run(context.Background(), []string{"host", "visibility", "app_org", "--withdraw", "--reason", "revise scope"}, Runtime{Out: &out, Err: &errBuf, ConfigPath: cfgPath}); code != 0 {
		t.Fatalf("withdraw failed (%d): %s", code, errBuf.String())
	}
	if lastWithdrawReason != "revise scope" || strings.Contains(out.String(), "Pending Visibility Approval:") || !strings.Contains(out.String(), "withdrawn; effective visibility remains selected") {
		t.Fatalf("unexpected withdraw output:\n%s", out.String())
	}

	if code := Run(context.Background(), []string{"host", "visibility", "app_org", "--withdraw", "--mode", "private"}, Runtime{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, ConfigPath: cfgPath}); code != 2 {
		t.Fatalf("expected withdraw+mode exit 2, got %d", code)
	}
}

func TestHostDeployOrganizationPendingApprovalNotice(t *testing.T) {
	now := time.Date(2026, 11, 21, 4, 30, 0, 0, time.UTC)
	bundleDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundleDir, "index.html"), []byte("<!doctype html><title>Board</title>"), 0o644); err != nil {
		t.Fatal(err)
	}

	pending := &sohaapi.HostedAppVisibilityApproval{
		ID:                  "appr_deploy_1",
		AppID:               "app_pending_org",
		AppName:             "Company Board",
		AppDNSLabel:         "company-board-1a2b3c",
		RequesterUserID:     "usr_alice",
		CurrentVisibility:   sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModePrivate},
		RequestedVisibility: sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModeOrganization},
		BaseRouteGeneration: 1,
		PolicyVersion:       2,
		Status:              sohaapi.HostedAppVisibilityApprovalStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	appRecord := sohaapi.HostedApp{
		ID:                        "app_pending_org",
		Name:                      "Company Board",
		Kind:                      sohaapi.HostedAppKindStatic,
		DNSLabel:                  "company-board-1a2b3c",
		Host:                      "company-board-1a2b3c.apps.soha.internal",
		URL:                       "https://company-board-1a2b3c.apps.soha.internal",
		OwnerUserID:               "usr_alice",
		MaintainerUserIDs:         []string{},
		Visibility:                sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModePrivate},
		PendingVisibilityApproval: pending,
		Status:                    sohaapi.HostedAppStatusOnline,
		ActiveRevisionID:          "rev_1",
		RouteGeneration:           1,
		CreatedAt:                 now,
		UpdatedAt:                 now,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/app-hosting/apps":
			var req sohaapi.CreateHostedAppRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Visibility == nil || req.Visibility.Mode != sohaapi.HostedAppVisibilityModeOrganization || !req.DeferVisibilityUntilPublication {
				t.Errorf("draft creation must defer organization visibility: %+v, %v", req, err)
			}
			draft := appRecord
			draft.Status = sohaapi.HostedAppStatusDraft
			draft.PendingVisibilityApproval = nil
			draft.ActiveRevisionID = ""
			w.WriteHeader(http.StatusCreated)
			writeJSON(t, w, sohaapi.HostedAppResponse{Data: draft})
		case "POST /api/v1/app-hosting/apps/app_pending_org/deployments/preflight":
			writeJSON(t, w, sohaapi.HostedAppPreflightResponse{
				Data: sohaapi.HostedAppPreflightResult{Allowed: true, AppID: appRecord.ID, URL: appRecord.URL, MaxCompressedBytes: 1 << 20, MaxUncompressedBytes: 4 << 20, MaxFileCount: 100},
			})
		case "POST /api/v1/app-hosting/apps/app_pending_org/revisions":
			_, intent := readHostUpload(t, r)
			if intent == nil || intent.Visibility.Mode != sohaapi.HostedAppVisibilityModeOrganization {
				t.Errorf("organization intent must belong to the upload: %+v", intent)
			}
			w.WriteHeader(http.StatusCreated)
			writeJSON(t, w, sohaapi.HostedAppDeploymentResponse{
				Data: sohaapi.HostedAppDeploymentReceipt{
					App: appRecord,
					Revision: sohaapi.HostedAppRevision{
						ID:             "rev_1",
						AppID:          appRecord.ID,
						RevisionNumber: 1,
						Kind:           sohaapi.HostedAppKindStatic,
						Status:         sohaapi.HostedAppRevisionStatusReady,
						BundleSha256:   strings.Repeat("d", 64),
						FileCount:      1,
						TotalBytes:     36,
						CreatedBy:      "usr_alice",
						CreatedAt:      now,
					},
				},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	cfgPath := writeTestConfig(t, server.URL)

	var out, errBuf bytes.Buffer
	code := Run(context.Background(), []string{"host", "deploy", bundleDir, "--name", "Company Board", "--visibility", "organization", "--yes"}, Runtime{
		Out:        &out,
		Err:        &errBuf,
		ConfigPath: cfgPath,
	})
	if code != 0 {
		t.Fatalf("deploy failed (%d): %s", code, errBuf.String())
	}
	if strings.Contains(out.String(), "Visibility: organization") {
		t.Fatalf("deploy output must not claim organization visibility while pending:\n%s", out.String())
	}
	for _, want := range []string{
		"Visibility: private",
		"Pending Visibility Approval: appr_deploy_1",
		"已提交全公司公开审批申请（单号 appr_deploy_1），审批通过前继续维持当前可见范围 private",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("deploy output missing %q:\n%s", want, out.String())
		}
	}
}

func TestHostDynamicDeployKindAndBuildStatus(t *testing.T) {
	now := time.Date(2026, 11, 21, 5, 0, 0, 0, time.UTC)
	sourceDir := t.TempDir()
	files := map[string]string{
		"soha-host.json":           `{"template":"node","database":false}`,
		"server.js":                "require('http').createServer((_, res) => res.end('ok')).listen(8080)\n",
		"package.json":             `{"name":"ops-api","version":"1.0.0"}`,
		"package-lock.json":        `{"lockfileVersion":3}`,
		".venv/lib/ignored.txt":    "ignored",
		"__pycache__/ignored.pyc":  "ignored",
		"node_modules/pkg/ignored": "ignored",
	}
	for rel, content := range files {
		full := filepath.Join(sourceDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	dynamicApp := sohaapi.HostedApp{
		ID:                "app_dyn",
		Name:              "Ops API",
		Kind:              sohaapi.HostedAppKindDynamic,
		DNSLabel:          "ops-api-1a2b3c",
		Host:              "ops-api-1a2b3c.apps.soha.internal",
		URL:               "https://ops-api-1a2b3c.apps.soha.internal",
		OwnerUserID:       "u-1",
		MaintainerUserIDs: []string{"u-1"},
		Visibility:        sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModePrivate},
		Status:            sohaapi.HostedAppStatusOnline,
		RouteGeneration:   1,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	staticApp := dynamicApp
	staticApp.ID = "app_static"
	staticApp.Name = "Static Board"
	staticApp.Kind = sohaapi.HostedAppKindStatic

	var (
		createCalls    int
		preflightCalls int
		uploadCalls    int
		lastCreate     sohaapi.CreateHostedAppRequest
		lastPreflight  sohaapi.HostedAppPreflightRequest
		lastUpload     string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/app-hosting/apps":
			createCalls++
			if err := json.NewDecoder(r.Body).Decode(&lastCreate); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			writeJSON(t, w, sohaapi.HostedAppResponse{Data: dynamicApp})
		case "GET /api/v1/app-hosting/apps/app_static":
			writeJSON(t, w, sohaapi.HostedAppResponse{Data: staticApp})
		case "POST /api/v1/app-hosting/apps/app_dyn/deployments/preflight":
			preflightCalls++
			if err := json.NewDecoder(r.Body).Decode(&lastPreflight); err != nil {
				t.Fatal(err)
			}
			writeJSON(t, w, sohaapi.HostedAppPreflightResponse{Data: sohaapi.HostedAppPreflightResult{
				Allowed: true, AppID: dynamicApp.ID, URL: dynamicApp.URL, MaxCompressedBytes: 1 << 20, MaxUncompressedBytes: 4 << 20, MaxFileCount: 100,
			}})
		case "POST /api/v1/app-hosting/apps/app_dyn/revisions":
			uploadCalls++
			lastUpload = r.URL.RawQuery
			revisionStatus := sohaapi.HostedAppRevisionStatusValidating
			if r.URL.Query().Get("idempotencyKey") == "replay-failed-key" {
				revisionStatus = sohaapi.HostedAppRevisionStatusFailed
			}
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			members := readTarGzMemberNames(t, raw)
			if strings.Join(members, ",") != "package-lock.json,package.json,server.js,soha-host.json" {
				t.Fatalf("dynamic upload members = %v", members)
			}
			appOut := dynamicApp
			appOut.ActiveRevisionID = "rev_dyn"
			w.WriteHeader(http.StatusCreated)
			writeJSON(t, w, sohaapi.HostedAppDeploymentResponse{Data: sohaapi.HostedAppDeploymentReceipt{
				App: appOut,
				Revision: sohaapi.HostedAppRevision{
					ID:             "rev_dyn",
					AppID:          dynamicApp.ID,
					RevisionNumber: 1,
					Kind:           sohaapi.HostedAppKindDynamic,
					Status:         revisionStatus,
					BuildPhase:     "building",
					ImageRef:       "registry.local/soha/ops-api@sha256:abc",
					BundleSha256:   strings.Repeat("e", 64),
					FileCount:      4,
					TotalBytes:     256,
					CreatedBy:      "u-1",
					CreatedAt:      now,
				},
			}})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	cfgPath := writeTestConfig(t, server.URL)

	t.Run("dry-run without app-id stays local without server calls", func(t *testing.T) {
		beforeCreate, beforePreflight, beforeUpload := createCalls, preflightCalls, uploadCalls
		var out, errBuf bytes.Buffer
		code := Run(context.Background(), []string{"host", "deploy", sourceDir, "--kind", "dynamic", "--dry-run", "--output", "json"}, Runtime{
			Out: &out, Err: &errBuf, ConfigPath: filepath.Join(t.TempDir(), "missing-config.json"),
		})
		if code != 0 {
			t.Fatalf("local dry-run failed (%d): %s", code, errBuf.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
			t.Fatalf("decode dry-run json: %v\n%s", err, out.String())
		}
		if payload["dryRun"] != "local_only" || payload["serverPreflight"] != "not_run" {
			t.Fatalf("unexpected dry-run payload: %+v", payload)
		}
		if _, ok := payload["allowed"]; ok {
			t.Fatalf("local dry-run must not report an Allowed verdict: %+v", payload)
		}
		if fileCount, _ := payload["fileCount"].(float64); fileCount <= 0 {
			t.Fatalf("expected local scan file count, got %+v", payload)
		}
		if createCalls != beforeCreate || preflightCalls != beforePreflight || uploadCalls != beforeUpload {
			t.Fatalf("unexpected server calls: create=%d preflight=%d upload=%d", createCalls, preflightCalls, uploadCalls)
		}
	})

	t.Run("rejects --spa for dynamic before server calls", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		code := Run(context.Background(), []string{"host", "deploy", sourceDir, "--kind", "dynamic", "--spa", "--yes"}, Runtime{Out: &out, Err: &errBuf, ConfigPath: cfgPath})
		if code != 2 || !strings.Contains(errBuf.String(), "--spa is not supported for dynamic apps") {
			t.Fatalf("code=%d err=%s", code, errBuf.String())
		}
		if createCalls != 0 || preflightCalls != 0 || uploadCalls != 0 {
			t.Fatalf("unexpected server calls: create=%d preflight=%d upload=%d", createCalls, preflightCalls, uploadCalls)
		}
	})

	t.Run("rejects kind mismatch before mutations", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		code := Run(context.Background(), []string{"host", "deploy", sourceDir, "--app-id", "app_static", "--kind", "dynamic", "--yes"}, Runtime{Out: &out, Err: &errBuf, ConfigPath: cfgPath})
		if code != 1 || !strings.Contains(errBuf.String(), "has kind \"static\", not \"dynamic\"") {
			t.Fatalf("code=%d err=%s", code, errBuf.String())
		}
		if createCalls != 0 || preflightCalls != 0 || uploadCalls != 0 {
			t.Fatalf("unexpected server calls: create=%d preflight=%d upload=%d", createCalls, preflightCalls, uploadCalls)
		}
	})

	t.Run("creates dynamic app and reports validating revision without claiming ready", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		code := Run(context.Background(), []string{"host", "deploy", sourceDir, "--kind", "dynamic", "--name", "Ops API", "--yes"}, Runtime{Out: &out, Err: &errBuf, ConfigPath: cfgPath})
		if code != 0 {
			t.Fatalf("deploy failed (%d): %s", code, errBuf.String())
		}
		if lastCreate.Kind != sohaapi.HostedAppKindDynamic || lastCreate.SpaFallback != nil || !strings.HasPrefix(lastCreate.IdempotencyKey, "host-app-dyn-") {
			t.Fatalf("unexpected create request: %+v", lastCreate)
		}
		if lastPreflight.Kind != sohaapi.HostedAppKindDynamic || lastPreflight.SpaFallback != nil {
			t.Fatalf("unexpected preflight request: %+v", lastPreflight)
		}
		if strings.Contains(lastUpload, "spaFallback") {
			t.Fatalf("dynamic upload must not send spaFallback: %s", lastUpload)
		}
		for _, want := range []string{
			"Revision: rev_dyn (r1)",
			"Kind: dynamic",
			"Revision Status: validating",
			"Build Phase: building",
			"Image: registry.local/soha/ops-api@sha256:abc",
			"Deployment: revision is validating, not ready.",
			"Check: soha host revisions app_dyn --revision rev_dyn --output json",
		} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("deploy output missing %q:\n%s", want, out.String())
			}
		}
		if strings.Contains(out.String(), "Revision Status: ready") {
			t.Fatalf("output must not claim ready while validating:\n%s", out.String())
		}
	})

	t.Run("fails when the upload receipt reports a failed revision", func(t *testing.T) {
		var out, errBuf bytes.Buffer
		args := []string{"host", "deploy", sourceDir, "--kind", "dynamic", "--name", "Ops API", "--idempotency-key", "replay-failed-key", "--yes", "--output", "json"}
		code := Run(context.Background(), args, Runtime{Out: &out, Err: &errBuf, ConfigPath: cfgPath})
		if code != 1 || !strings.Contains(errBuf.String(), "new --idempotency-key") {
			t.Fatalf("code=%d err=%s", code, errBuf.String())
		}
		var receipt sohaapi.HostedAppDeploymentReceipt
		if err := json.Unmarshal(out.Bytes(), &receipt); err != nil || receipt.Revision.Status != sohaapi.HostedAppRevisionStatusFailed {
			t.Fatalf("expected failed revision receipt, got %s: %v", out.String(), err)
		}
	})
}

func readTarGzMemberNames(t *testing.T, archive []byte) []string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		names = append(names, hdr.Name)
	}
	return names
}
