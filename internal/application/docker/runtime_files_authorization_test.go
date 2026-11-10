package docker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func runtimeVolumeReads() map[string]func(context.Context, *Service, domainidentity.Principal) error {
	return map[string]func(context.Context, *Service, domainidentity.Principal) error{
		"mount metadata": func(ctx context.Context, s *Service, p domainidentity.Principal) error {
			_, err := s.ListProjectVolumes(ctx, p, "project-1", "web")
			return err
		},
		"directory": func(ctx context.Context, s *Service, p domainidentity.Principal) error {
			_, err := s.ListProjectVolumeFiles(ctx, p, "project-1", domaindocker.ProjectVolumeFileListInput{ServiceName: "web", Target: "/data", Path: "/"})
			return err
		},
		"content": func(ctx context.Context, s *Service, p domainidentity.Principal) error {
			_, err := s.ReadProjectVolumeFile(ctx, p, "project-1", domaindocker.ProjectVolumeFileReadInput{ServiceName: "web", Target: "/data", Path: "/private.txt"})
			return err
		},
	}
}

func TestRuntimeVolumeFilePermissionAndCredentialCeiling(t *testing.T) {
	var calls atomic.Int32
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/api/v1/docker/runtime/volumes" {
			_, _ = io.WriteString(w, `{"data":[]}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"projectId":"project-1","serviceName":"web","target":"/data","path":"/private.txt","items":[],"content":"synthetic private file","sizeBytes":22}}`)
	}))
	defer agent.Close()
	for _, tc := range []struct {
		name               string
		role, ceiling      []string
		metadata, contents bool
		missingResolver    bool
	}{
		{name: "view does not grant files", role: []string{"docker.services.view"}, metadata: true},
		{name: "files does not grant inventory", role: []string{"docker.services.files"}, contents: true},
		{name: "explicit grants", role: []string{"docker.services.view", "docker.services.files"}, metadata: true, contents: true},
		{name: "legacy manage does not grant files", role: []string{"docker.services.manage"}},
		{name: "legacy actions does not grant files", role: []string{"docker.services.actions"}},
		{name: "credential capped at view", role: []string{"docker.services.view", "docker.services.files"}, ceiling: []string{"docker.services.view"}, metadata: true},
		{name: "credential cannot add file role grant", role: []string{"docker.services.view"}, ceiling: []string{"docker.services.files"}},
		{name: "credential files only", role: []string{"docker.services.view", "docker.services.files"}, ceiling: []string{"docker.services.files"}, contents: true},
		{name: "legacy manage credential cannot grant files", role: []string{"docker.services.view", "docker.services.files"}, ceiling: []string{"docker.services.manage"}},
		{name: "legacy actions credential cannot grant files", role: []string{"docker.services.view", "docker.services.files"}, ceiling: []string{"docker.services.actions"}},
		{name: "missing resolver", missingResolver: true},
	} {
		for name, read := range runtimeVolumeReads() {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				s := newDockerRuntimeProxyTestService(agent.URL)
				s.permissions = appaccess.NewPermissionResolver(dockerTestRoleReader{matrix: map[string][]string{"reader": tc.role}})
				if tc.missingResolver {
					s.permissions = nil
				}
				principal := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}, PermissionKeys: tc.ceiling}
				before := calls.Load()
				err := read(context.Background(), s, principal)
				allowed := tc.contents
				if name == "mount metadata" {
					allowed = tc.metadata
				}
				if allowed {
					if err != nil || calls.Load() != before+1 {
						t.Fatalf("authorized read: err=%v calls=%d", err, calls.Load()-before)
					}
				} else if !errors.Is(err, apperrors.ErrAccessDenied) || calls.Load() != before {
					t.Fatalf("denied read: err=%v Agent calls=%d", err, calls.Load()-before)
				}
			})
		}
	}
}

func TestProjectRuntimeScopeIsCheckedBeforeAgentAccess(t *testing.T) {
	var calls atomic.Int32
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	defer agent.Close()
	reads := runtimeVolumeReads()
	reads["logs"] = func(ctx context.Context, s *Service, p domainidentity.Principal) error {
		_, err := s.GetProjectLogs(ctx, p, "project-1", "web", 20)
		return err
	}
	reads["log stream"] = func(ctx context.Context, s *Service, p domainidentity.Principal) error {
		return s.StreamProjectLogs(ctx, p, "project-1", "web", 20, io.Discard)
	}
	reads["terminal"] = func(ctx context.Context, s *Service, p domainidentity.Principal) error {
		return s.StreamProjectTerminal(ctx, p, "project-1", "web", "/bin/sh", strings.NewReader(""), io.Discard, io.Discard)
	}
	query := domainresource.LogQuery{Selector: &domainresource.LogSourceSelector{DockerService: "web"}, Direction: sohaapi.LogDirectionForward}
	reads["log query"] = func(ctx context.Context, s *Service, p domainidentity.Principal) error {
		_, err := s.QueryProjectLogs(ctx, p, "project-1", query)
		return err
	}
	reads["log ticket"] = func(ctx context.Context, s *Service, p domainidentity.Principal) error {
		issuer := &recordingDockerTicketIssuer{}
		s.logStreamTickets = issuer
		_, err := s.IssueProjectLogStreamTicket(ctx, p, domainidentity.AccessContext{TokenKind: "session_access"}, "project-1", query)
		if issuer.request.Path != "" {
			return errors.New("scope rejection must not issue a stream ticket")
		}
		return err
	}
	reads["ticket events"] = func(ctx context.Context, s *Service, p domainidentity.Principal) error {
		emitted := 0
		accessCtx := domainidentity.AccessContext{TokenKind: "stream_ticket", Metadata: map[string]any{dockerLogProjectMetadataKey: "project-1", dockerLogQueryMetadataKey: query}}
		err := s.StreamProjectLogEventsFromTicket(ctx, p, accessCtx, "project-1", func(domainresource.LogStreamEvent) error { emitted++; return nil })
		if emitted != 0 {
			return errors.New("scope rejection must not emit stream events")
		}
		return err
	}
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			s := newDockerRuntimeProxyTestService(agent.URL)
			s.permissions = appaccess.NewPermissionResolver(dockerTestRoleReader{matrix: map[string][]string{"reader": {"docker.services.view", "docker.services.files", "docker.services.logs", "docker.services.terminal"}}})
			checked := 0
			ctx := domaindocker.WithScopeCheck(context.Background(), func(scope map[string]string) error {
				checked++
				if scope["hostId"] != "host-1" || scope["projectId"] != "project-1" || len(scope) != 2 {
					t.Fatalf("scope must use resolved project/host: %#v", scope)
				}
				return apperrors.ErrAccessDenied
			})
			before := calls.Load()
			err := read(ctx, s, domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}})
			if !errors.Is(err, apperrors.ErrAccessDenied) || checked != 1 || calls.Load() != before {
				t.Fatalf("scope rejection: err=%v checks=%d Agent calls=%d", err, checked, calls.Load()-before)
			}
		})
	}
}

func TestRuntimeVolumeScopeRevocationIsRechecked(t *testing.T) {
	var calls atomic.Int32
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/api/v1/docker/runtime/volumes" {
			_, _ = io.WriteString(w, `{"data":[]}`)
		} else {
			_, _ = io.WriteString(w, `{"data":{"projectId":"project-1","serviceName":"web","target":"/data","path":"/private.txt","items":[],"content":"fixture","sizeBytes":7}}`)
		}
	}))
	defer agent.Close()
	for name, read := range runtimeVolumeReads() {
		t.Run(name, func(t *testing.T) {
			s := newDockerRuntimeProxyTestService(agent.URL)
			s.permissions = appaccess.NewPermissionResolver(dockerTestRoleReader{matrix: map[string][]string{"reader": {"docker.services.view", "docker.services.files"}}})
			allowed := true
			checks := 0
			ctx := domaindocker.WithScopeCheck(context.Background(), func(scope map[string]string) error {
				checks++
				if !allowed || scope["hostId"] != "host-1" || scope["projectId"] != "project-1" {
					return apperrors.ErrAccessDenied
				}
				return nil
			})
			principal := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
			before := calls.Load()
			if err := read(ctx, s, principal); err != nil || calls.Load() != before+1 {
				t.Fatalf("in-scope read failed: %v", err)
			}
			allowed = false
			if err := read(ctx, s, principal); !errors.Is(err, apperrors.ErrAccessDenied) || calls.Load() != before+1 || checks != 2 {
				t.Fatalf("revoked scope: err=%v calls=%d checks=%d", err, calls.Load()-before, checks)
			}
		})
	}
}
