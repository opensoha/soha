package docker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	protocol "github.com/opensoha/soha/contracts/docker/runtime"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appdlp "github.com/opensoha/soha/internal/application/contentdlp"
	domaindlp "github.com/opensoha/soha/internal/domain/contentdlp"
	domain "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type runtimeReadDLPGuard struct {
	snapshot                 appdlp.Snapshot
	prepareErr, checkErr     error
	prepareCalls, checkCalls int
	file                     domain.ProjectVolumeFileContent
	decision                 *domaindlp.Decision
	afterCheck               func()
}

func withDisabledRuntimeReadDLP(service *Service, project domain.Project) {
	WithContentDLP(&runtimeReadDLPGuard{snapshot: appdlp.Snapshot{Project: project, Policy: domaindlp.DefaultPolicy(project.ID)}})(service)
}

func (g *runtimeReadDLPGuard) Prepare(context.Context, domainidentity.Principal, string) (appdlp.Snapshot, error) {
	g.prepareCalls++
	return g.snapshot, g.prepareErr
}

func (g *runtimeReadDLPGuard) Check(_ context.Context, _ domainidentity.Principal, _ appdlp.Snapshot, file domain.ProjectVolumeFileContent, _ ...domaindlp.TransferAccess) (*domaindlp.Decision, error) {
	g.checkCalls++
	g.file = file
	if g.afterCheck != nil {
		g.afterCheck()
	}
	return g.decision, g.checkErr
}

func TestBoundRuntimeFileDLPFailuresNeverReleaseContent(t *testing.T) {
	for _, tc := range []struct {
		name       string
		change     func(*runtimeReadDLPGuard, *Service, *readFenceRepo)
		want       error
		agentCalls int32
	}{
		{"missing guard", func(_ *runtimeReadDLPGuard, s *Service, _ *readFenceRepo) { s.contentDLP = nil }, apperrors.ErrServiceUnavailable, 0},
		{"prepare unavailable", func(g *runtimeReadDLPGuard, _ *Service, _ *readFenceRepo) {
			g.prepareErr = apperrors.ErrServiceUnavailable
		}, apperrors.ErrServiceUnavailable, 0},
		{"different project", func(g *runtimeReadDLPGuard, _ *Service, _ *readFenceRepo) { g.snapshot.Project.ID = "other" }, apperrors.ErrConflict, 0},
		{"recreated project", func(g *runtimeReadDLPGuard, _ *Service, _ *readFenceRepo) {
			g.snapshot.Project.ResourceUID = "33333333-3333-4333-8333-333333333333"
		}, apperrors.ErrConflict, 0},
		{"changed revision", func(g *runtimeReadDLPGuard, _ *Service, _ *readFenceRepo) {
			g.snapshot.Project.ConfigurationRevision = "changed-revision"
		}, apperrors.ErrConflict, 0},
		{"changed host", func(g *runtimeReadDLPGuard, _ *Service, _ *readFenceRepo) { g.snapshot.Project.HostID = "other" }, apperrors.ErrConflict, 0},
		{"changed project", func(g *runtimeReadDLPGuard, _ *Service, _ *readFenceRepo) { g.snapshot.Project.UpdatedAt = time.Now() }, apperrors.ErrConflict, 0},
		{"scan audit failure", func(g *runtimeReadDLPGuard, _ *Service, _ *readFenceRepo) {
			g.checkErr = apperrors.ErrServiceUnavailable
		}, apperrors.ErrServiceUnavailable, 1},
		{"block", func(g *runtimeReadDLPGuard, _ *Service, _ *readFenceRepo) {
			g.decision = &domaindlp.Decision{Allowed: false, Mode: "block"}
		}, apperrors.ErrAccessDenied, 1},
		{"warn requires workflow", func(g *runtimeReadDLPGuard, _ *Service, _ *readFenceRepo) {
			g.decision = &domaindlp.Decision{Allowed: false, Mode: "warn"}
		}, apperrors.ErrAccessDenied, 1},
		{"missing enabled decision", func(g *runtimeReadDLPGuard, _ *Service, _ *readFenceRepo) { g.decision = nil }, apperrors.ErrServiceUnavailable, 1},
		{"disabled policy changed while reading", func(g *runtimeReadDLPGuard, _ *Service, _ *readFenceRepo) {
			g.snapshot.Policy.Published = nil
			g.checkErr = apperrors.ErrConflict
		}, apperrors.ErrConflict, 1},
		{"revoked after scan", func(g *runtimeReadDLPGuard, s *Service, _ *readFenceRepo) {
			g.afterCheck = func() {
				s.permissions = appaccess.NewPermissionResolver(dockerTestRoleReader{matrix: map[string][]string{"reader": nil}})
			}
		}, apperrors.ErrAccessDenied, 1},
		{"registry changed after scan", func(g *runtimeReadDLPGuard, _ *Service, r *readFenceRepo) {
			g.afterCheck = func() {
				p := r.projects["project-1"]
				p.ConfigurationRevision = "changed-revision"
				r.projects[p.ID] = p
			}
		}, apperrors.ErrConflict, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var req protocol.ReadRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					return
				}
				out := readReply(req)
				out.Content, out.SizeBytes, out.ReturnedBytes, out.Complete, out.Truncated = "private fixture", 15, 15, true, false
				_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
			}))
			defer agent.Close()
			service, repo, actor := boundReadFixture(agent.URL)
			guard := &runtimeReadDLPGuard{snapshot: appdlp.Snapshot{Project: repo.projects["project-1"], Policy: domaindlp.Policy{ProjectID: "project-1", Published: &domaindlp.Content{Enabled: true}}}, decision: &domaindlp.Decision{Allowed: true}}
			WithContentDLP(guard)(service)
			tc.change(guard, service, repo)
			result, err := service.ReadProjectRuntime(context.Background(), actor, "project-1", RuntimeReadInput{Action: "files.read", ServiceName: "web", Target: "/data", Path: "file.txt", LimitBytes: 32})
			if !errors.Is(err, tc.want) || result.Content != "" || calls.Load() != tc.agentCalls {
				t.Fatalf("DLP failure released data or accessed the wrong stage: error=%v calls=%d", err, calls.Load())
			}
		})
	}
}

func TestBoundRuntimeFileDLPScansBeforeUTF8PrefixTruncation(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		limit         int64
		want          string
	}{
		{"ASCII prefix", "prefix beyond the requested byte", 1, "p"},
		{"UTF8 boundary", "中a", 1, ""},
		{"UTF8 complete rune", "中a", 3, "中"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requested atomic.Int64
			agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req protocol.ReadRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					return
				}
				requested.Store(req.LimitBytes)
				out := readReply(req)
				out.Content, out.SizeBytes, out.ReturnedBytes, out.Complete, out.Truncated = tc.content, int64(len(tc.content)), int64(len(tc.content)), true, false
				_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
			}))
			defer agent.Close()
			service, repo, actor := boundReadFixture(agent.URL)
			guard := &runtimeReadDLPGuard{snapshot: appdlp.Snapshot{Project: repo.projects["project-1"], Policy: domaindlp.Policy{ProjectID: "project-1", Published: &domaindlp.Content{Enabled: true}}}, decision: &domaindlp.Decision{Allowed: true}}
			WithContentDLP(guard)(service)
			input := RuntimeReadInput{Action: "files.read", ServiceName: "web", Target: "/data", Path: "nested/file.txt", LimitBytes: tc.limit}
			result, err := service.ReadProjectRuntime(context.Background(), actor, "project-1", input)
			if err != nil || requested.Load() != domaindlp.MaxTextBytes || guard.file.Content != tc.content || guard.file.Path != "/nested/file.txt" || guard.checkCalls != 1 || result.Content != tc.want || result.ReturnedBytes != int64(len(tc.want)) || !result.Truncated || result.Complete {
				t.Fatalf("scan/truncation order or metadata invalid: error=%v requestLimit=%d checks=%d", err, requested.Load(), guard.checkCalls)
			}
		})
	}
}

func TestBoundRuntimeFileReadDLPRejectsProtectedContent(t *testing.T) {
	var calls atomic.Int32
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req protocol.ReadRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		result := readReply(req)
		result.Content = "protected fixture"
		result.SizeBytes, result.ReturnedBytes = int64(len(result.Content)), int64(len(result.Content))
		result.Truncated, result.Complete = false, true
		_ = json.NewEncoder(w).Encode(map[string]any{"data": result})
	}))
	defer agent.Close()
	service, repo, actor := boundReadFixture(agent.URL)
	guard := &runtimeReadDLPGuard{
		snapshot: appdlp.Snapshot{Project: repo.projects["project-1"], Policy: domaindlp.Policy{ProjectID: "project-1", Published: &domaindlp.Content{Enabled: true}}},
		checkErr: apperrors.ErrAccessDenied,
	}
	WithContentDLP(guard)(service)
	result, err := service.ReadProjectRuntime(context.Background(), actor, "project-1", RuntimeReadInput{Action: "files.read", ServiceName: "web", Target: "/data", Path: "file.txt", LimitBytes: 32})
	if !errors.Is(err, apperrors.ErrAccessDenied) || result.Content != "" || guard.prepareCalls != 1 || guard.checkCalls != 1 || calls.Load() != 1 {
		t.Fatalf("protected runtime file must pass Core DLP before release: error=%v prepare=%d check=%d agent=%d", err, guard.prepareCalls, guard.checkCalls, calls.Load())
	}
}
