package bootstrap

import (
	"context"
	"github.com/gin-gonic/gin"
	handler "github.com/opensoha/soha/internal/api/handlers/radiusdeployment"
	access "github.com/opensoha/soha/internal/application/access"
	deployment "github.com/opensoha/soha/internal/application/radiusdeployment"
	"github.com/opensoha/soha/internal/domain/identity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type catalogRadiusRoleReader struct{ matrix map[string][]string }

func (r catalogRadiusRoleReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return r.matrix, nil
}

type noRadiusWrites struct{ deployment.Repository }

func TestRadiusCreateHTTPRejectsCatalogViewOnlyAndLegacySitesRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { access.SetRolePermissionMatrix(nil) })
	for _, keys := range [][]string{{access.PermNetworkAccessNASBindingsView}, {access.PermNetworkAccessSitesView, access.PermNetworkAccessSitesUpdate}} {
		canonical, e := access.CanonicalAssignablePermissionKeys(keys)
		if e != nil {
			t.Fatalf("real catalog rejected role: %v", e)
		}
		scope := &radiusScope{allow: true}
		authority := radiusAuthority{permissions: access.NewPermissionResolver(catalogRadiusRoleReader{matrix: map[string][]string{"assigned": canonical}}), scope: scope}
		service, e := deployment.New(&noRadiusWrites{}, authority, nil)
		if e != nil {
			t.Fatal(e)
		}
		h := handler.New(service, nil)
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Set("principal", identity.Principal{UserID: "user", Roles: []string{"assigned"}})
		})
		r.POST("/network-access/radius-pools", h.Create)
		body := `{"name":"RADIUS","siteId":"site","nasBindingId":"binding","slotId":"slot","configurationRef":"approved","requestId":"request","createApprovedBinding":true}`
		request := httptest.NewRequest(http.MethodPost, "/network-access/radius-pools", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		r.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("missing create grant status=%d body=%s", response.Code, response.Body)
		}
		if scope.calls != 0 {
			t.Fatal("missing catalog grant reached scope/database mutation")
		}
	}
}
