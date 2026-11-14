package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync/atomic"
	"testing"

	protocol "github.com/opensoha/soha/contracts/docker/runtime"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type readFenceRepo struct {
	*memoryDockerRepo
	projectReads  int
	projectChange func(int, *domain.Project)
	serviceChange func(*domain.Service)
}

func (r *readFenceRepo) GetProject(ctx context.Context, id string) (domain.Project, error) {
	item, err := r.memoryDockerRepo.GetProject(ctx, id)
	r.projectReads++
	if r.projectChange != nil {
		r.projectChange(r.projectReads, &item)
	}
	return item, err
}

func (r *readFenceRepo) GetService(ctx context.Context, id string) (domain.Service, error) {
	item, err := r.memoryDockerRepo.GetService(ctx, id)
	if r.serviceChange != nil {
		r.serviceChange(&item)
	}
	return item, err
}

func boundReadFixture(endpoint string) (*Service, *readFenceRepo, domainidentity.Principal) {
	base := newMemoryDockerRepo()
	base.hosts["host-1"] = domain.Host{ID: "host-1", Endpoint: endpoint, ResourceUID: "11111111-1111-4111-8111-111111111111"}
	base.projects["project-1"] = domain.Project{ID: "project-1", HostID: "host-1", ResourceUID: "22222222-2222-4222-8222-222222222222", RuntimeIdentityVersion: 1, ComposeContent: "private-compose-fixture", EnvContent: "PRIVATE=private-env-fixture"}
	base.services["service-1"] = domain.Service{ID: "service-1", HostID: "host-1", ProjectID: "project-1", Name: "web", ContainerID: strings.Repeat("a", 64), Config: map[string]any{"password": "private-service-fixture"}}
	repo := &readFenceRepo{memoryDockerRepo: base}
	permissions := appaccess.NewPermissionResolver(dockerTestRoleReader{matrix: map[string][]string{"reader": {"docker.services.logs", "docker.services.files"}}})
	s := New(repo, permissions, nil, WithRuntimeBearerToken("runtime-token"))
	// Identity tests use a project with DLP disabled, not a missing guard.
	withDisabledRuntimeReadDLP(s, base.projects["project-1"])
	return s, repo, domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
}

func readReply(req protocol.ReadRequest) protocol.ReadResult {
	r := protocol.ReadResult{Protocol: protocol.ReadProtocol, Action: req.Action, ProjectID: req.ProjectID, Container: req.Container, Target: req.Target, Path: req.Path, Complete: true, Volumes: []protocol.Volume{}, Files: []protocol.FileEntry{}}
	switch req.Action {
	case "logs":
		r.Encoding, r.Content = "utf-8", "ready\n"
		r.SizeBytes, r.ReturnedBytes = 6, 6
	case "volumes":
		r.Volumes = []protocol.Volume{{Target: "/data", Type: "volume", UnsupportedReason: "reader disabled"}}
	case "files.list":
		r.Files = []protocol.FileEntry{{Name: "note.txt", Path: path.Join(req.Path, "note.txt"), Kind: "file", SizeBytes: 9, ReadSupported: true}}
	case "files.read":
		r.Encoding, r.Content = "utf-8", "中"
		r.SizeBytes, r.ReturnedBytes = 9, 3
		r.Truncated, r.Complete = true, false
	}
	return r
}

func TestBoundRuntimeReadsSendOnlyOriginalIdentityAndBoundedOptions(t *testing.T) {
	for _, action := range []string{"logs", "volumes", "files.list", "files.read"} {
		t.Run(action, func(t *testing.T) {
			var calls atomic.Int32
			agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				raw, _ := io.ReadAll(r.Body)
				if strings.Contains(string(raw), "private-") || strings.Contains(string(raw), "composeContent") || strings.Contains(string(raw), "envContent") || strings.Contains(string(raw), `"config"`) {
					t.Error("private configuration forwarded to reader")
				}
				var req protocol.ReadRequest
				if json.Unmarshal(raw, &req) != nil || req.ProjectID != "project-1" || req.Container.ContainerID != strings.Repeat("a", 64) || req.Container.Validate() != nil || r.Header.Get("Authorization") != "Bearer runtime-token" {
					t.Error("wrong original runtime identity or credential")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": readReply(req)})
			}))
			defer agent.Close()
			s, _, actor := boundReadFixture(agent.URL)
			input := RuntimeReadInput{Action: action, ServiceName: "web"}
			if strings.HasPrefix(action, "files.") {
				input.Target = "/data"
				input.Path = "."
			}
			if action == "files.read" {
				input.LimitBytes = 4
			}
			result, err := s.ReadProjectRuntime(context.Background(), actor, "project-1", input)
			if err != nil || calls.Load() != 1 || result.Container.ServiceName != "web" || result.ProjectID != "project-1" {
				t.Fatal(result, err, calls.Load())
			}
		})
	}
}

func TestBoundRuntimeReadsEnforceRoleCredentialCeilingAndScope(t *testing.T) {
	for _, tc := range []struct {
		name, action                 string
		role, ceiling                []string
		scopeDenied, missingResolver bool
		allowed                      bool
	}{
		{name: "logs", action: "logs", role: []string{"docker.services.logs"}, allowed: true},
		{name: "files", action: "volumes", role: []string{"docker.services.files"}, allowed: true},
		{name: "view is not files", action: "volumes", role: []string{"docker.services.view"}},
		{name: "ceiling", action: "volumes", role: []string{"docker.services.files", "docker.services.view"}, ceiling: []string{"docker.services.view"}},
		{name: "ceiling cannot grant role", action: "volumes", role: []string{"docker.services.view"}, ceiling: []string{"docker.services.files"}},
		{name: "scope", action: "logs", role: []string{"docker.services.logs"}, scopeDenied: true},
		{name: "missing resolver", action: "logs", role: []string{"docker.services.logs"}, missingResolver: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var req protocol.ReadRequest
				_ = json.NewDecoder(r.Body).Decode(&req)
				_ = json.NewEncoder(w).Encode(map[string]any{"data": readReply(req)})
			}))
			defer agent.Close()
			s, _, actor := boundReadFixture(agent.URL)
			s.permissions = appaccess.NewPermissionResolver(dockerTestRoleReader{matrix: map[string][]string{"reader": tc.role}})
			if tc.missingResolver {
				s.permissions = nil
			}
			actor.PermissionKeys = tc.ceiling
			ctx := context.Background()
			if tc.scopeDenied {
				ctx = domain.WithScopeCheck(ctx, func(map[string]string) error { return apperrors.ErrAccessDenied })
			}
			_, err := s.ReadProjectRuntime(ctx, actor, "project-1", RuntimeReadInput{Action: tc.action, ServiceName: "web"})
			if tc.allowed {
				if err != nil || calls.Load() != 1 {
					t.Fatal(err, calls.Load())
				}
			} else if !errors.Is(err, apperrors.ErrAccessDenied) || calls.Load() != 0 {
				t.Fatal("denied read touched Agent", err, calls.Load())
			}
		})
	}
}

func TestBoundRuntimeReadRejectsOldAgentWrongIdentityAndRegistryChange(t *testing.T) {
	for _, mode := range []string{"old-agent", "redirect", "wrong-CID", "wrong-UID", "recreated-project", "replaced-container", "revoked-scope", "legacy-project", "provider-error"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if mode == "old-agent" {
					w.WriteHeader(404)
					_, _ = io.WriteString(w, "old agent")
					return
				}
				if mode == "redirect" {
					w.Header().Set("Location", "/legacy-unsafe")
					w.WriteHeader(307)
					return
				}
				if mode == "provider-error" {
					w.WriteHeader(500)
					_, _ = io.WriteString(w, "private-provider-token")
					return
				}
				var req protocol.ReadRequest
				_ = json.NewDecoder(r.Body).Decode(&req)
				out := readReply(req)
				if mode == "wrong-CID" {
					out.Container.ContainerID = strings.Repeat("b", 64)
				}
				if mode == "wrong-UID" {
					out.Container.ProjectUID = "33333333-3333-4333-8333-333333333333"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
			}))
			defer agent.Close()
			s, repo, actor := boundReadFixture(agent.URL)
			if mode == "legacy-project" {
				item := repo.projects["project-1"]
				item.RuntimeIdentityVersion = 0
				repo.projects["project-1"] = item
			}
			if mode == "recreated-project" {
				repo.projectChange = func(n int, p *domain.Project) {
					if n > 1 {
						p.ResourceUID = "33333333-3333-4333-8333-333333333333"
					}
				}
			}
			if mode == "replaced-container" {
				repo.serviceChange = func(s *domain.Service) { s.ContainerID = strings.Repeat("b", 64) }
			}
			ctx := context.Background()
			if mode == "revoked-scope" {
				checks := 0
				ctx = domain.WithScopeCheck(ctx, func(map[string]string) error {
					checks++
					if checks > 2 {
						return apperrors.ErrAccessDenied
					}
					return nil
				})
			}
			out, err := s.ReadProjectRuntime(ctx, actor, "project-1", RuntimeReadInput{Action: "logs", ServiceName: "web"})
			if err == nil || out.Content != "" || strings.Contains(err.Error(), "private-provider-token") {
				t.Fatal("unconfirmed read returned data", out, err)
			}
			want := 1
			if mode == "legacy-project" {
				want = 0
			}
			if calls.Load() != int32(want) {
				t.Fatal("runtime read replayed or fell back", calls.Load(), want)
			}
		})
	}
}

func TestBoundProjectPlanRejectsMultipleReplicasBeforeEnqueue(t *testing.T) {
	s, repo, actor := boundReadFixture("http://127.0.0.1:1")
	s.permissions = dockerTestPermissions()
	actor.Roles = []string{"admin"}
	item := repo.projects["project-1"]
	item.ComposeContent = "services:\n  web:\n    image: fixture\n    deploy:\n      replicas: 2\n"
	repo.projects[item.ID] = item
	if _, err := s.PlanProjectDeploy(context.Background(), actor, item.ID, domain.ProjectDeployInput{}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatal("multi-replica plan was ready", err)
	}
}
