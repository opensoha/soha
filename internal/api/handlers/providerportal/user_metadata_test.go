package providerportal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type metadataProviderService struct{ ProviderService }

func (metadataProviderService) GetProviderUserMetadata(_ context.Context, actor domainidentity.Principal, providerID, userID, clientID string) (sohaapi.IdentityProviderUserMetadata, error) {
	if actor.UserID == "" {
		return sohaapi.IdentityProviderUserMetadata{}, apperrors.ErrAccessDenied
	}
	return sohaapi.IdentityProviderUserMetadata{ProviderID: providerID, UserID: userID, ClientID: clientID, Protocol: "oidc", Attributes: map[string][]string{"sub": {userID}}}, nil
}

func TestUserMetadataHTTPParametersAndCachePolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &providerHandler{service: metadataProviderService{}}
	for _, authorized := range []bool{true, false} {
		router := gin.New()
		router.Use(func(c *gin.Context) {
			if authorized {
				c.Set("principal", domainidentity.Principal{UserID: "admin"})
			}
		})
		router.GET("/identity/providers/:providerID/users/:userID/metadata", handler.GetIdentityProviderUserMetadata)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/identity/providers/provider/users/target/metadata?clientId=client", nil))
		if recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("metadata response is cacheable")
		}
		if !authorized {
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("permission error status = %d", recorder.Code)
			}
			continue
		}
		if recorder.Code != http.StatusOK {
			t.Fatalf("metadata status = %d", recorder.Code)
		}
		want := `{"data":{"attributes":{"sub":["target"]},"clientId":"client","protocol":"oidc","providerId":"provider","userId":"target"}}`
		if recorder.Body.String() != want {
			t.Fatalf("metadata response = %s", recorder.Body.String())
		}
	}
}
