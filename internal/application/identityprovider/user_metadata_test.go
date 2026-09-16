package identityprovider

import (
	"context"
	"errors"
	"reflect"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	"github.com/opensoha/soha/internal/platform/apperrors"
	userrepo "github.com/opensoha/soha/internal/repository/user"
)

type metadataUsers struct {
	memoryUsers
	reads    int
	inactive bool
}

func (u *metadataUsers) GetByID(_ context.Context, id string) (userrepo.User, error) {
	u.reads++
	status := "active"
	if u.inactive {
		status = "disabled"
	}
	return userrepo.User{ID: id, DisplayName: "Target User", Email: "target@example.com", Status: status}, nil
}

func TestProviderUserMetadataPermissionsMappingsAndNoSessions(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	users := &metadataUsers{}
	audit := &oidcSecretAuditRecorder{}
	permissions := appaccess.NewPermissionResolver(identityProviderRolePermissions{matrix: map[string][]string{
		"both":     {appaccess.PermIdentityProvidersView, appaccess.PermAccessUsersView},
		"provider": {appaccess.PermIdentityProvidersView},
		"users":    {appaccess.PermAccessUsersView},
	}})
	service := New(repo, users, permissions, audit, "test-encryption-key-32-bytes-long")
	actor := domainidentity.Principal{UserID: "admin", Roles: []string{"both"}}
	for _, role := range []string{"provider", "users", "none"} {
		_, err := service.GetProviderUserMetadata(ctx, domainidentity.Principal{UserID: "viewer", Roles: []string{role}}, repo.provider.ID, "target", repo.client.ID)
		if !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("role %s: %v", role, err)
		}
	}
	if users.reads != 0 {
		t.Fatal("read user before both permissions were checked")
	}
	repo.client.AllowedScopes = []string{"openid", "email"}
	// Configuration preview remains available while login is disabled.
	repo.provider.Enabled = false
	repo.client.Status = domainprovider.OIDCClientStatusDisabled
	metadata, err := service.GetProviderUserMetadata(ctx, actor, repo.provider.ID, "target", repo.client.ID)
	ssoNoError(t, err)
	want := map[string][]string{"sub": {"target"}, "email": {"target@example.com"}}
	if !reflect.DeepEqual(metadata.Attributes, want) || metadata.UserID != "target" {
		t.Fatalf("OIDC preview = %#v", metadata)
	}
	if len(repo.codes)+len(repo.sessions)+len(repo.refreshTokens) != 0 {
		t.Fatal("preview created login state")
	}
	if len(audit.entries) != 1 || audit.entries[0].ActorID != "admin" || audit.entries[0].Metadata["targetUserId"] != "target" {
		t.Fatalf("preview audit = %#v", audit.entries)
	}
	if _, ok := audit.entries[0].Metadata["attributes"]; ok {
		t.Fatal("audit contains attribute values")
	}
	repo.client.ProviderID = "other-provider"
	_, err = service.GetProviderUserMetadata(ctx, actor, repo.provider.ID, "target", repo.client.ID)
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("foreign client = %v", err)
	}
	_, err = service.GetProviderUserMetadata(ctx, actor, repo.provider.ID, "target", "")
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("missing client = %v", err)
	}
	repo.provider.Type = domainprovider.ProviderTypeSAML
	repo.samlSP = domainprovider.SAMLServiceProvider{ProviderID: repo.provider.ID, NameIDFormat: "emailAddress", AttributeMappings: map[string]string{"email": "mail"}}
	metadata, err = service.GetProviderUserMetadata(ctx, actor, repo.provider.ID, "target", "")
	ssoNoError(t, err)
	if metadata.Subject != "target@example.com" || !reflect.DeepEqual(metadata.Attributes, map[string][]string{"mail": {"target@example.com"}}) {
		t.Fatalf("SAML preview = %#v", metadata)
	}
	repo.provider.Type = domainprovider.ProviderTypeProxy
	repo.provider.Config = map[string]any{"headerMappings": map[string]string{"email": "X-Auth-Request-Email"}}
	metadata, err = service.GetProviderUserMetadata(ctx, actor, repo.provider.ID, "target", "")
	ssoNoError(t, err)
	if !reflect.DeepEqual(metadata.Attributes["X-Auth-Request-Email"], []string{"target@example.com"}) || metadata.Attributes["X-Soha-Email"] != nil {
		t.Fatalf("Proxy preview = %#v", metadata)
	}
	users.inactive = true
	_, err = service.GetProviderUserMetadata(ctx, actor, repo.provider.ID, "target", "")
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("inactive target must not return actor auth error: %v", err)
	}
}
