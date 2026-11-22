package copilot

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	contractenv "github.com/opensoha/soha/contracts/agent/environment"
	guest "github.com/opensoha/soha/contracts/agent/guest"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appapphosting "github.com/opensoha/soha/internal/application/apphosting"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Compile-time check that *Service structurally satisfies apphosting.WorkspaceBundleSource.
var _ appapphosting.WorkspaceBundleSource = (*Service)(nil)

type rawTarItem struct {
	name     string
	typeflag byte
	body     string
	linkname string
}

func buildRawExportTar(t *testing.T, items []rawTarItem) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, it := range items {
		tf := it.typeflag
		if tf == 0 {
			tf = tar.TypeReg
		}
		hdr := &tar.Header{
			Name:     it.name,
			Mode:     0o644,
			Size:     int64(len(it.body)),
			Typeflag: tf,
			Linkname: it.linkname,
		}
		if tf != tar.TypeReg && tf != '\x00' {
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if tf == tar.TypeReg || tf == '\x00' {
			if _, err := tw.Write([]byte(it.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func unpackTarGzToMap(t *testing.T, rc io.ReadCloser) map[string]string {
	t.Helper()
	defer func() { _ = rc.Close() }()
	gz, err := gzip.NewReader(rc)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	files := make(map[string]string)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("tar.Next: %v", err)
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read tar entry %s: %v", hdr.Name, err)
		}
		files[hdr.Name] = string(b)
	}
	return files
}

type containerWorkspaceBundleFixture struct {
	service       *Service
	user          domainidentity.Principal
	assistantID   string
	store         *personalFileReservationStore
	releaseCalled int
	exportArchive []byte
}

func newContainerWorkspaceBundleFixture(t *testing.T) *containerWorkspaceBundleFixture {
	t.Helper()
	service, _ := newInspectionAuthzTestService(map[string][]string{"chat": {appaccess.PermObserveAIChatUse}})
	user := domainidentity.Principal{UserID: "usr_ws_1", Roles: []string{"chat"}}
	assistant, err := service.EnsurePersonalAssistant(t.Context(), user, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	policy := domain.PersonalEnvironmentPolicy{
		TemplateID: "tpl_docker", Backend: "container", Provider: "docker", PolicyRevision: "v1",
		AgentClusterID: "cluster_1", Mode: "restricted-write", LeaseSeconds: 600,
		Quota: contractenv.Quota{DiskBytes: 64 << 20, MemoryBytes: 128 << 20, CPUUnits: 500, MaxProcesses: 32},
	}
	lease := personalEnvironmentLease(assistant.ID, user.UserID, policy, now)
	f := &containerWorkspaceBundleFixture{
		service:     service,
		user:        user,
		assistantID: assistant.ID,
		store: &personalFileReservationStore{
			personalCleanupStore: &personalCleanupStore{
				item: domain.PersonalEnvironmentRecord{
					AssistantID: assistant.ID, CreatedBy: user.UserID, TemplateID: "tpl_docker",
					Backend: "container", Provider: "docker", PolicyRevision: "v1", AgentClusterID: "cluster_1",
					DataInitialized: true, State: "ready", Version: 3, LeaseSeconds: 600, Lease: lease, UpdatedAt: now,
				},
			},
		},
	}
	service.personalEnvironment = &PersonalEnvironmentDependencies{
		Store: f.store,
		Authorize: func(_ context.Context, p domainidentity.Principal, cluster string) error {
			if p.UserID == user.UserID && cluster == "cluster_1" {
				return nil
			}
			return apperrors.ErrAccessDenied
		},
		Invoke: func(_ context.Context, _ domainidentity.Principal, _ string, req contractenv.Request) (contractenv.Result, error) {
			if req.Action == "release" {
				f.releaseCalled++
				return contractenv.Result{
					SchemaVersion: contractenv.ResultVersion, LeaseID: req.Lease.ID, RunID: req.Lease.RunID,
					State: "released", ObservedAt: time.Now().UTC(),
				}, nil
			}
			return contractenv.Result{}, apperrors.ErrUnsupportedOperation
		},
		Export: func(_ context.Context, _ domainidentity.Principal, _ string, req contractenv.Request, target io.Writer) (contractenv.ExportResult, error) {
			n, writeErr := target.Write(f.exportArchive)
			if writeErr != nil {
				return contractenv.ExportResult{}, writeErr
			}
			sum := sha256.Sum256(f.exportArchive)
			return contractenv.ExportResult{
				SchemaVersion: contractenv.ExportResultVersion, LeaseID: req.Lease.ID, RunID: req.Lease.RunID,
				OwnerID: req.Lease.OwnerID, ScopeHash: req.Lease.ScopeHash, SizeBytes: int64(n),
				SHA256: "sha256:" + hex.EncodeToString(sum[:]), ObservedAt: time.Now().UTC(),
			}, nil
		},
	}
	return f
}

func TestBundlePersonalWorkspaceContainerSleepExtractFilterAndSecurity(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	ctx := t.Context()
	f := newContainerWorkspaceBundleFixture(t)

	for _, unsafePath := range []string{"../secret", "/etc/passwd", "apps/../../etc", "bad\x00path"} {
		if _, err := f.service.BundlePersonalWorkspace(ctx, f.user, unsafePath, f.assistantID, 0, sohaapi.HostedAppKindStatic); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("unsafe sourcePath %q = %v, want ErrInvalidArgument", unsafePath, err)
		}
	}

	f.store.item.ActiveTaskID = "task_running_1"
	if _, err := f.service.BundlePersonalWorkspace(ctx, f.user, "projects/calc", f.assistantID, 0, sohaapi.HostedAppKindStatic); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("busy workspace = %v, want ErrConflict", err)
	}
	f.store.item.ActiveTaskID = ""

	f.exportArchive = buildRawExportTar(t, []rawTarItem{
		{name: ".", typeflag: tar.TypeDir},
		{name: "./projects/calc", typeflag: tar.TypeDir},
		{name: "./projects/calc/index.html", body: "<!doctype html><h1>Calculator</h1>"},
		{name: "./projects/calc/assets/app.js", body: "console.log('ok');"},
		{name: "./projects/calc/node_modules/pkg/index.js", body: "ignored"},
		{name: "./projects/calc/.git/config", body: "ignored"},
		{name: "./projects/calc/.DS_Store", body: "ignored"},
		{name: "./projects/other/secret.txt", body: "outside target dir"},
		{name: "./notes/summary.md", body: "# Sprint Summary\nAll green."},
	})

	rc, err := f.service.BundlePersonalWorkspace(ctx, f.user, "projects/calc", f.assistantID, f.store.item.Version, sohaapi.HostedAppKindStatic)
	if err != nil {
		t.Fatalf("BundlePersonalWorkspace directory: %v", err)
	}
	if f.releaseCalled != 1 || f.store.item.State != "sleeping" {
		t.Fatalf("expected ready container to sleep once (releaseCalled=%d, state=%s)", f.releaseCalled, f.store.item.State)
	}
	files := unpackTarGzToMap(t, rc)
	if len(files) != 2 || files["index.html"] != "<!doctype html><h1>Calculator</h1>" || files["assets/app.js"] != "console.log('ok');" {
		t.Fatalf("unexpected extracted bundle files: %#v", files)
	}

	singleRC, err := f.service.BundlePersonalWorkspace(ctx, f.user, "notes/summary.md", f.assistantID, 0, sohaapi.HostedAppKindStatic)
	if err != nil {
		t.Fatalf("BundlePersonalWorkspace single markdown file: %v", err)
	}
	singleFiles := unpackTarGzToMap(t, singleRC)
	if !strings.Contains(singleFiles["index.html"], "Sprint Summary") || singleFiles["summary.md"] != "# Sprint Summary\nAll green." {
		t.Fatalf("unexpected single-file bundle: %#v", singleFiles)
	}

	f.exportArchive = buildRawExportTar(t, []rawTarItem{
		{name: "./projects/calc/index.html", body: "<!doctype html>ok"},
		{name: "./projects/calc/escape", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"},
	})
	if _, err := f.service.BundlePersonalWorkspace(ctx, f.user, "projects/calc", f.assistantID, 0, sohaapi.HostedAppKindStatic); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("symlink in workspace export = %v, want ErrInvalidArgument", err)
	}
}

func TestDynamicWorkspaceBundleUsesOnlySelectedProjectIgnoreRules(t *testing.T) {
	dir := "projects/forms"
	archive := bytes.NewReader(buildRawExportTar(t, []rawTarItem{
		{name: "projects/.gitignore", body: "app.py\n"},
		{name: dir + "/app.py", body: "print('ok')"},
		{name: dir + "/.env", body: "synthetic local credential"},
		{name: dir + "/nested/keep.py", body: "print('kept')"},
		{name: dir + "/nested/drop.py", body: "not deployed"},
		{name: dir + "/nested/.gitignore", body: "!keep.py\n"},
		{name: dir + "/cache/secret.txt", body: "not deployed"},
		{name: dir + "/cache/.gitignore", typeflag: tar.TypeSymlink, linkname: "/outside"},
		{name: dir + "/cache/.gitignore", body: "duplicate ignored rules"},
		{name: dir + "/.gitignore", body: ".env\n/nested/*.py\ncache/\n"},
		{name: "projects/other/not-in-bundle.py", body: "outside"},
	}))
	rules, err := readWorkspaceSourceIgnores(archive, dir)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := repackExportedWorkspaceTar(archive, dir, rules)
	if err != nil {
		t.Fatal(err)
	}
	files := unpackTarGzToMap(t, bundle)
	if len(files) != 2 || files["app.py"] != "print('ok')" || files["nested/keep.py"] != "print('kept')" {
		t.Fatalf("unexpected source files: %v", files)
	}
}

func TestBundlePersonalWorkspaceVMSingleFileAndDirectoryGuard(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, _ := newInspectionAuthzTestService(map[string][]string{"chat": {appaccess.PermObserveAIChatUse}})
	ctx := t.Context()
	userID := uuid.NewString()
	computeRef := uuid.NewString()
	user := domainidentity.Principal{UserID: userID, Roles: []string{"chat"}}

	assistant, err := service.EnsurePersonalAssistant(ctx, user, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	vmIdentity := domain.PersonalVMIdentity{
		ProviderIdentityHash: "sha256:" + strings.Repeat("a", 64),
		DataIdentityHash:     "sha256:" + strings.Repeat("b", 64),
		DataDiskSerial:       "disk_vm_01",
		DataDiskBytes:        1 << 30,
	}
	policy := domain.PersonalEnvironmentPolicy{
		TemplateID:   "tpl_vm",
		Backend:      "vm",
		Provider:     "pve",
		Mode:         "restricted-write",
		LeaseSeconds: 600,
		Quota:        contractenv.Quota{DiskBytes: 64 << 20, MemoryBytes: 128 << 20, CPUUnits: 500, MaxProcesses: 32},
	}
	lease := personalEnvironmentLease(assistant.ID, userID, policy, now)
	store := &personalFileReservationStore{
		personalCleanupStore: &personalCleanupStore{
			item: domain.PersonalEnvironmentRecord{
				AssistantID:       assistant.ID,
				CreatedBy:         userID,
				TemplateID:        "tpl_vm",
				Backend:           "vm",
				Provider:          "pve",
				ComputeRef:        computeRef,
				ComputeGeneration: 1,
				DataRef:           uuid.NewString(),
				DataInitialized:   true,
				VMIdentity:        &vmIdentity,
				State:             "ready",
				Version:           2,
				LeaseSeconds:      600,
				Lease:             lease,
				UpdatedAt:         now,
			},
		},
	}
	service.personalEnvironment = &PersonalEnvironmentDependencies{
		Store: store,
		ObserveVM: func(_ context.Context, _ domainidentity.Principal, _ domain.PersonalVMPolicy) (domain.PersonalVMIdentity, error) {
			return vmIdentity, nil
		},
		InvokeVM: func(_ context.Context, _ domainidentity.Principal, reserved domain.PersonalEnvironmentRecord, op guest.RuntimeOperation) (guest.RuntimeResult, error) {
			obsAt := time.Now().UTC()
			return guest.RuntimeResult{
				Environment: contractenv.Result{
					SchemaVersion: contractenv.ResultVersion,
					LeaseID:       reserved.Lease.ID,
					RunID:         reserved.Lease.RunID,
					State:         "active",
					ObservedAt:    obsAt,
					Snapshot: &contractenv.Snapshot{
						SchemaVersion: contractenv.SnapshotVersion,
						LeaseID:       reserved.Lease.ID,
						RunID:         reserved.Lease.RunID,
						State:         "active",
						ObservedAt:    obsAt,
					},
				},
				File: &guest.RuntimeFileResult{
					Path:          op.File.Path,
					ContentBase64: base64.StdEncoding.EncodeToString([]byte("<!doctype html><h1>VM Tool</h1>")),
				},
			}, nil
		},
	}

	// VM directory publish returns clear 400 InvalidArgument.
	if _, err := service.BundlePersonalWorkspace(ctx, user, "projects/multi-file", assistant.ID, 0, sohaapi.HostedAppKindStatic); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("VM directory bundle = %v, want ErrInvalidArgument", err)
	}

	// VM single-file publish succeeds and produces index.html.
	rc, err := service.BundlePersonalWorkspace(ctx, user, "tools/board.html", assistant.ID, 0, sohaapi.HostedAppKindStatic)
	if err != nil {
		t.Fatalf("VM single-file BundlePersonalWorkspace: %v", err)
	}
	files := unpackTarGzToMap(t, rc)
	if files["index.html"] != "<!doctype html><h1>VM Tool</h1>" {
		t.Fatalf("unexpected VM single-file bundle: %#v", files)
	}
}

func TestBundleConversationArtifactOwnershipKnowledgeAndHTMLWrapping(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, repo := newInspectionAuthzTestService(map[string][]string{"chat": {appaccess.PermObserveAIChatUse}})
	ctx := t.Context()
	alice := domainidentity.Principal{UserID: "usr_alice", Roles: []string{"chat"}}
	bob := domainidentity.Principal{UserID: "usr_bob", Roles: []string{"chat"}}

	repo.session = domain.Session{ID: "sess_alice", CreatedBy: "usr_alice"}
	repo.messages = []domain.Message{
		{
			ID:        "msg_html",
			SessionID: "sess_alice",
			Role:      "assistant",
			Metadata: map[string]any{
				"analysisArtifacts": []domain.AnalysisArtifact{{
					Kind:  "html",
					RunID: "art_html_1",
					Title: "Ops Status",
					DataSourceSnapshot: map[string]any{
						"artifactId": "custom-snap-art-1",
						"format":     "html",
						"content":    "<!doctype html><html><body><h1>Live Ops</h1></body></html>",
					},
				}},
			},
		},
		{
			ID:        "msg_cite_revoked",
			SessionID: "sess_alice",
			Role:      "assistant",
			Metadata: map[string]any{
				"contextSnapshot": map[string]any{
					"citations": []domainknowledge.Citation{{
						KnowledgeBaseID: "kb_confidential",
						DocumentID:      "doc_1",
						ChunkID:         "chk_1",
						ContentHash:     "sha256:abc",
					}},
				},
				"analysisArtifacts": []domain.AnalysisArtifact{{
					Kind:               "report",
					RunID:              "art_cited",
					Title:              "Confidential Report",
					DataSourceSnapshot: map[string]any{"content": "# Secret"},
				}},
			},
		},
	}

	// Cross-user session access denied.
	if _, _, err := service.BundleConversationArtifact(ctx, bob, "sess_alice", "art_html_1", ""); err == nil {
		t.Fatal("expected cross-user BundleConversationArtifact to fail")
	}

	// Revoked/unverifiable knowledge citation fails closed with ErrAccessDenied.
	if _, _, err := service.BundleConversationArtifact(ctx, alice, "sess_alice", "art_cited", ""); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("artifact with unverified knowledge citation = %v, want ErrAccessDenied", err)
	}

	// Owned HTML artifact bundles directly as index.html.
	htmlRC, title, err := service.BundleConversationArtifact(ctx, alice, "sess_alice", "art_html_1", "")
	if err != nil || title != "Ops Status" {
		t.Fatalf("BundleConversationArtifact HTML = %q, %v", title, err)
	}
	htmlFiles := unpackTarGzToMap(t, htmlRC)
	if htmlFiles["index.html"] != "<!doctype html><html><body><h1>Live Ops</h1></body></html>" {
		t.Fatalf("unexpected HTML artifact bundle: %#v", htmlFiles)
	}
	snapRC, snapTitle, err := service.BundleConversationArtifact(ctx, alice, "sess_alice", "custom-snap-art-1", "")
	if err != nil || snapTitle != "Ops Status" {
		t.Fatalf("BundleConversationArtifact by snapshot artifactId = %q, %v", snapTitle, err)
	}
	_ = snapRC.Close()

	// Personal assistant artifact lookup (sessionID == "") with Markdown + XSS-escaped HTML wrapper + raw data file.
	repo.personalArtifacts = []domain.PersonalArtifact{{
		ID:        "task:run_9:0",
		SessionID: "sess_alice",
		CreatedAt: time.Now().UTC(),
		Artifact: domain.AnalysisArtifact{
			Kind:  "report",
			RunID: "art_md_9",
			Title: "Weekly <script>alert(1)</script> Digest",
			DataSourceSnapshot: map[string]any{
				"format":  "markdown",
				"content": "# Digest\n<img onerror=alert(1)>",
			},
		},
	}}
	mdRC, mdTitle, err := service.BundleConversationArtifact(ctx, alice, "", "task:run_9:0", "digest.md")
	if err != nil || !strings.HasPrefix(mdTitle, "Weekly ") {
		t.Fatalf("BundleConversationArtifact personal markdown = %q, %v", mdTitle, err)
	}
	mdFiles := unpackTarGzToMap(t, mdRC)
	if mdFiles["digest.md"] != "# Digest\n<img onerror=alert(1)>" {
		t.Fatalf("missing raw digest.md in bundle: %#v", mdFiles)
	}
	if strings.Contains(mdFiles["index.html"], "<script>alert(1)</script>") || !strings.Contains(mdFiles["index.html"], "&lt;script&gt;") {
		t.Fatalf("index.html wrapper did not HTML-escape untrusted artifact content: %s", mdFiles["index.html"])
	}
}
