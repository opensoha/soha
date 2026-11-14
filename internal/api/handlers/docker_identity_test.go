package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
)

type identityHandlerService struct {
	principal domainidentity.Principal
	id, kind  string
	err       error
}

func (s *identityHandlerService) GetHostIdentity(_ context.Context, p domainidentity.Principal, id string) (sohaapi.DockerResourceIdentity, error) {
	s.principal, s.id, s.kind = p, id, "docker.host"
	return sohaapi.DockerResourceIdentity{ID: id, ResourceKind: "docker.host", UID: uuid.New(), ConfigurationRevision: uuid.New()}, s.err
}
func (s *identityHandlerService) GetProjectIdentity(ctx context.Context, p domainidentity.Principal, id string) (sohaapi.DockerResourceIdentity, error) {
	item, err := s.GetHostIdentity(ctx, p, id)
	s.kind = "docker.project"
	item.ResourceKind = "docker.project"
	item.HostID = "host-1"
	return item, err
}

func TestDockerIdentityHandlersAndMissingDependency(t *testing.T) {
	for _, resource := range []string{"host", "project"} {
		for _, scenario := range []struct {
			name   string
			err    error
			status int
		}{
			{"ok", nil, 200}, {"denied", apperrors.ErrAccessDenied, 403}, {"missing", apperrors.ErrNotFound, 404}, {"unsupported", apperrors.ErrUnsupportedOperation, 501},
		} {
			t.Run(resource+"/"+scenario.name, func(t *testing.T) {
				service := &identityHandlerService{err: scenario.err}
				handler := NewDockerHandlerWithServices(DockerServices{Identities: service}, keyring.Ring{})
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Params = gin.Params{{Key: "id", Value: resource + "-1"}}
				c.Request = httptest.NewRequest(http.MethodGet, "/identity", nil)
				c.Set("principal", domainidentity.Principal{UserID: "reader"})
				if resource == "host" {
					handler.GetHostIdentity(c)
				} else {
					handler.GetProjectIdentity(c)
				}
				if w.Code != scenario.status || service.principal.UserID != "reader" || service.id != resource+"-1" || service.kind != "docker."+resource {
					t.Fatalf("status=%d identity invocation=%+v", w.Code, service)
				}
				if scenario.status == 200 {
					var result sohaapi.DockerResourceIdentityEnvelope
					if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Data.ID != resource+"-1" {
						t.Fatalf("envelope=%s err=%v", w.Body, err)
					}
				}
			})
		}
		for _, typed := range []bool{false, true} {
			t.Run(resource+"/nil/"+map[bool]string{true: "typed", false: "interface"}[typed], func(t *testing.T) {
				var dependency DockerIdentityService
				if typed {
					var service *identityHandlerService
					dependency = service
				}
				h := NewDockerHandlerWithServices(DockerServices{Identities: dependency}, keyring.Ring{})
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodGet, "/identity", nil)
				if resource == "host" {
					h.GetHostIdentity(c)
				} else {
					h.GetProjectIdentity(c)
				}
				if w.Code != 501 {
					t.Fatalf("nil identity dependency status=%d", w.Code)
				}
			})
		}
	}
}
