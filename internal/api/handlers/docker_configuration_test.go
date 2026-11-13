package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
)

type configurationHandlerService struct {
	calls                int
	actor, kind, id, key string
	err                  error
}

func (s *configurationHandlerService) result(p domainidentity.Principal, kind, id, key string) (sohaapi.DockerConfigurationReceipt, error) {
	s.calls++
	s.actor, s.kind, s.id, s.key = p.UserID, kind, id, key
	return sohaapi.DockerConfigurationReceipt{ID: "receipt", RequestKey: key}, s.err
}
func (s *configurationHandlerService) RegisterHostConfiguration(_ context.Context, p domainidentity.Principal, in sohaapi.DockerHostRegistrationInput) (sohaapi.DockerConfigurationReceipt, error) {
	return s.result(p, "docker.host", "", in.IdempotencyKey)
}
func (s *configurationHandlerService) PatchConfiguration(_ context.Context, p domainidentity.Principal, kind, id string, in sohaapi.DockerConfigurationPatchInput) (sohaapi.DockerConfigurationReceipt, error) {
	return s.result(p, kind, id, in.IdempotencyKey)
}
func (s *configurationHandlerService) DeleteConfiguration(_ context.Context, p domainidentity.Principal, kind, id string, in sohaapi.DockerConfigurationDeleteInput) (sohaapi.DockerConfigurationReceipt, error) {
	return s.result(p, kind, id, in.IdempotencyKey)
}
func (s *configurationHandlerService) GetConfigurationRequest(_ context.Context, p domainidentity.Principal, key string) (sohaapi.DockerConfigurationReceipt, error) {
	return s.result(p, "", "", key)
}

func TestDockerConfigurationHandlersThinBoundary(t *testing.T) {
	for _, test := range []struct {
		name, kind string
		call       func(*DockerHandler, *gin.Context)
	}{
		{"register", "docker.host", (*DockerHandler).RegisterHostConfiguration},
		{"patch-host", "docker.host", (*DockerHandler).PatchHostConfiguration},
		{"patch-project", "docker.project", (*DockerHandler).PatchProjectConfiguration},
		{"delete-host", "docker.host", (*DockerHandler).DeleteHostConfiguration},
		{"delete-project", "docker.project", (*DockerHandler).DeleteProjectConfiguration},
		{"lookup", "", (*DockerHandler).GetConfigurationRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, status := range []struct {
				err  error
				want int
			}{{nil, 200}, {apperrors.ErrAccessDenied, 403}, {apperrors.ErrConflict, 409}, {apperrors.ErrNotFound, 404}, {apperrors.ErrUnsupportedOperation, 501}} {
				service := &configurationHandlerService{err: status.err}
				h := NewDockerHandlerWithServices(DockerServices{Configuration: service}, keyring.Ring{})
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodPost, "/configuration", strings.NewReader(`{"idempotencyKey":"original-key"}`))
				c.Set("principal", domainidentity.Principal{UserID: "actor"})
				c.Params = gin.Params{{Key: "id", Value: "target"}, {Key: "requestKey", Value: "original-key"}}
				test.call(h, c)
				if w.Code != status.want || service.calls != 1 || service.actor != "actor" || service.kind != test.kind || service.key != "original-key" || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("status=%d invocation=%+v", w.Code, service)
				}
			}
		})
	}
}

func TestDockerConfigurationHandlerRejectsAmbiguousJSON(t *testing.T) {
	for _, body := range []string{`{"idempotencyKey":"original-key","IdempotencyKey":"another-key"}`, `{"IdempotencyKey":"original-key"}`, `{"changes":[{"Field":"name","stringValue":"a"}]}`, `{"preconditions":{"ExpectedUid":"11111111-1111-4111-8111-111111111111"}}`, `{"idempotencyKey":"original-key","idempotencyKey":"another-key"}`, `{"idempotencyKey":null}`, `{"unexpected":true}`, `{"changes":[{"field":"name","stringValue":"a","stringValue":"b"}]}`, `{"changes":[{"field":"config","objectValue":{"key":null}}]}`, `{} {}`, `[]`, string([]byte{'{', '"', 'a', '"', ':', '"', 0xff, '"', '}'}), `{"idempotencyKey":"` + strings.Repeat("a", 2<<20) + `"}`, strings.Repeat(`{"a":`, 34) + `1` + strings.Repeat("}", 34)} {
		service := &configurationHandlerService{}
		h := NewDockerHandlerWithServices(DockerServices{Configuration: service}, keyring.Ring{})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPatch, "/configuration", strings.NewReader(body))
		h.PatchHostConfiguration(c)
		if w.Code != 400 || service.calls != 0 {
			t.Fatalf("ambiguous request reached service status=%d calls=%d", w.Code, service.calls)
		}
	}
}

func TestDockerConfigurationMissingServiceFailsClosed(t *testing.T) {
	for _, typed := range []bool{false, true} {
		var dependency DockerConfigurationService
		if typed {
			var value *configurationHandlerService
			dependency = value
		}
		h := NewDockerHandlerWithServices(DockerServices{Configuration: dependency}, keyring.Ring{})
		for _, call := range []func(*DockerHandler, *gin.Context){(*DockerHandler).RegisterHostConfiguration, (*DockerHandler).PatchHostConfiguration, (*DockerHandler).PatchProjectConfiguration, (*DockerHandler).DeleteHostConfiguration, (*DockerHandler).DeleteProjectConfiguration, (*DockerHandler).GetConfigurationRequest} {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/configuration", nil)
			call(h, c)
			if w.Code != 501 {
				t.Fatalf("missing dependency status=%d", w.Code)
			}
		}
	}
}

func TestDockerConfigurationCaseSensitiveMapKeysPreserved(t *testing.T) {
	service := &configurationHandlerService{}
	h := NewDockerHandlerWithServices(DockerServices{Configuration: service}, keyring.Ring{})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPatch, "/configuration", strings.NewReader(`{"changes":[{"field":"config","objectValue":{"Name":1,"name":2}}]}`))
	h.PatchHostConfiguration(c)
	if w.Code != 200 || service.calls != 1 {
		t.Fatal("case-sensitive config keys incorrectly rejected", w.Code)
	}
}
