package providerportal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appprovider "github.com/opensoha/soha/internal/application/identityprovider"
	appportal "github.com/opensoha/soha/internal/application/providerportal"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	providerrepo "github.com/opensoha/soha/internal/repository/identityprovider"
	portalrepo "github.com/opensoha/soha/internal/repository/providerportal"
	"gorm.io/gorm"
)

func TestApplicationOnboardingHTTPWithPostgres(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := ssoTestDatabase(t)
	repo := portalrepo.New(db)
	permissions := appaccess.NewPermissionResolver(onboardingTestPermissions{})
	audit := &onboardingTestAudit{t: t, db: db}
	applications := appportal.New(repo, permissions, audit)
	key, err := keyring.NewKey("sso-test", "test-encryption-key-32-bytes-long", time.Now(), nil)
	ssoNoError(t, err)
	keys, err := keyring.New(key, nil)
	ssoNoError(t, err)
	providers := appprovider.NewWithEncryptionKeys(providerrepo.New(db), nil, permissions, audit, keys)
	service := appportal.NewOnboarding(repo, applications, providers)
	ctx := context.Background()
	principal := domainidentity.Principal{UserID: "test-admin", Roles: []string{"onboarding"}}
	for _, kind := range []string{"link", "oidc", "saml", "proxy"} {
		t.Run(kind, func(t *testing.T) {
			input := onboardingTestInput(kind)
			audit.slug = input.Application.Slug
			before := len(audit.entries)
			handler := New(Services{ApplicationOnboarding: service})
			router := gin.New()
			router.Use(func(c *gin.Context) { c.Set("principal", principal) })
			router.POST("/onboard", handler.OnboardIdentityApplication)
			body, _ := json.Marshal(input)
			request := httptest.NewRequest("POST", "/onboard", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusCreated {
				_, serviceErr := service.OnboardApplication(ctx, principal, input)
				t.Fatalf("create status=%d: %s; service error: %v", recorder.Code, recorder.Body.String(), serviceErr)
			}
			var envelope struct {
				Data json.RawMessage `json:"data"`
			}
			ssoNoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
			ssoCheck(t, recorder.Header().Get("Cache-Control") == "no-store", "onboarding secret response is cacheable")
			validateSSOResponse(t, "IdentityApplicationOnboarding", envelope.Data)
			var result appportal.OnboardingResult
			ssoNoError(t, json.Unmarshal(envelope.Data, &result))
			t.Cleanup(func() { _ = repo.DeleteApplication(ctx, result.Application.ID) })
			ssoCheck(t, result.Application.Status == "disabled" && len(result.Application.Assignments) == 1, "onboarding did not preserve restricted access and disabled status")
			ssoCheck(t, kind == "link" || result.Provider != nil && result.Application.ProviderID == result.Provider.ID, "missing committed binding")
			if kind == "oidc" {
				ssoCheck(t, result.OIDCClient != nil && result.OIDCClient.ClientSecret != "", "missing first client secret")
				encoded, _ := json.Marshal(audit.entries[before:])
				ssoCheck(t, !(bytes.Contains(encoded, []byte(result.OIDCClient.ClientSecret))), "secret leaked into audit")
			}
			ssoCheck(t, len(audit.entries) != before, "missing committed audit")
			duplicate, err := service.OnboardApplication(ctx, principal, input)
			ssoCheck(t, errors.Is(err, apperrors.ErrConflict) && duplicate.OIDCClient == nil && duplicate.Application.ID == "", "duplicate was not a redacted conflict: %v", err)
		})
	}
	t.Run("rollback and retry", func(t *testing.T) {
		input := onboardingTestInput("oidc")
		audit.slug = input.Application.Slug
		before := len(audit.entries)
		input.OIDCClient.RedirectURIs = nil
		result, err := service.OnboardApplication(ctx, principal, input)
		ssoCheck(t, errors.Is(err, apperrors.ErrInvalidArgument) && result.Application.ID == "", "invalid late client: %v", err)
		assertOnboardingAbsent(t, db, input.Application.Slug)
		ssoCheck(t, len(audit.entries) == before, "rolled-back create emitted success audit")
		input.OIDCClient.RedirectURIs = []string{"https://app.example.com/callback"}
		result, err = service.OnboardApplication(ctx, principal, input)
		ssoNoError(t, err)
		t.Cleanup(func() { _ = repo.DeleteApplication(ctx, result.Application.ID) })
	})
	t.Run("rollback after every SAML write", func(t *testing.T) {
		input := onboardingTestInput("saml")
		audit.slug = input.Application.Slug
		before := len(audit.entries)
		failing := appportal.NewOnboarding(failingOnboardingTransaction{repo}, applications, providers)
		result, err := failing.OnboardApplication(ctx, principal, input)
		ssoCheck(t, err != nil && result.Application.ID == "", "injected failure committed")
		assertOnboardingAbsent(t, db, input.Application.Slug)
		ssoCheck(t, len(audit.entries) == before, "rollback emitted success audit")
	})
	t.Run("permission intersection and explicit access", func(t *testing.T) {
		input := onboardingTestInput("oidc")
		audit.slug = input.Application.Slug
		limited := domainidentity.Principal{UserID: "test-admin", Roles: []string{"application-only"}}
		if _, err := service.OnboardApplication(ctx, limited, input); !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatalf("provider permission: %v", err)
		}
		assertOnboardingAbsent(t, db, input.Application.Slug)
		input.Provider, input.OIDCClient = nil, nil
		result, err := service.OnboardApplication(ctx, limited, input)
		ssoCheck(t, err == nil && result.Application.ProviderID == "", "application-only draft: %v", err)
		t.Cleanup(func() { _ = repo.DeleteApplication(ctx, result.Application.ID) })
		input = onboardingTestInput("link")
		input.AccessMode = ""
		if _, err := service.OnboardApplication(ctx, principal, input); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("implicit access accepted: %v", err)
		}
		input.AccessMode = "restricted"
		input.Application.Assignments = nil
		if _, err := service.OnboardApplication(ctx, principal, input); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("empty restriction accepted: %v", err)
		}
	})
}

func onboardingTestInput(kind string) appportal.OnboardingInput {
	id := "sso-test-" + uuid.NewString()
	input := appportal.OnboardingInput{Application: domainportal.ApplicationInput{Slug: id, Name: "SSO integration", ProviderType: kind, LaunchURL: "https://app.example.com", Status: "enabled", PortalVisible: true, Assignments: []domainportal.ApplicationAssignmentInput{{SubjectType: "role", SubjectID: "viewer", Effect: "allow"}}}, AccessMode: "restricted"}
	if kind != "link" {
		input.Provider = &domainprovider.ProviderInput{Name: "SSO integration provider", Type: kind, Enabled: true, Status: "enabled", Config: map[string]any{}}
	}
	switch kind {
	case "oidc":
		input.OIDCClient = &domainprovider.OIDCClientInput{ClientID: id, ClientType: "confidential", RedirectURIs: []string{"https://app.example.com/callback"}, AllowedScopes: []string{"openid", "profile"}, AllowedGrantTypes: []string{"authorization_code"}, RequirePKCE: true, Status: "enabled"}
	case "saml":
		input.Provider.Config = map[string]any{"entityId": "https://app.example.com/saml", "assertionConsumerServiceUrls": []string{"https://app.example.com/acs"}}
	case "proxy":
		input.Provider.Config = map[string]any{"mode": "forward_auth", "externalHosts": []string{"app.example.com"}}
	}
	return input
}

type onboardingTestPermissions struct{}

func (onboardingTestPermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"onboarding": {"identity.applications.create", "identity.providers.create"}, "application-only": {"identity.applications.create"}}, nil
}

type onboardingTestAudit struct {
	t       *testing.T
	db      *gorm.DB
	slug    string
	entries []domainaudit.Entry
}

func (a *onboardingTestAudit) Record(ctx context.Context, entry domainaudit.Entry) error {
	var count int64
	if err := a.db.WithContext(ctx).Table("identity_applications").Where("slug = ?", a.slug).Count(&count).Error; err != nil || count != 1 {
		a.t.Fatalf("audit before commit: count=%d err=%v", count, err)
	}
	a.entries = append(a.entries, entry)
	return nil
}

type failingOnboardingTransaction struct{ repo *portalrepo.Repository }

func (r failingOnboardingTransaction) WithinOnboardingTransaction(ctx context.Context, create func(domainportal.Repository, domainprovider.Repository) error) error {
	return r.repo.WithinOnboardingTransaction(ctx, func(apps domainportal.Repository, providers domainprovider.Repository) error {
		if err := create(apps, providers); err != nil {
			return err
		}
		return errors.New("injected failure before commit")
	})
}
func assertOnboardingAbsent(t *testing.T, db *gorm.DB, slug string) {
	t.Helper()
	var count int64
	if err := db.Table("identity_applications").Where("slug = ?", slug).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("application survived rollback: count=%d err=%v", count, err)
	}
	// FK ownership means a rolled-back application cannot leave a provider, client, SP or key.
}
