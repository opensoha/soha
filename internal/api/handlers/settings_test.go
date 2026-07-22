package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
)

type stubSettingsService struct {
	ai                   domainsettings.AISettings
	identity             domainsettings.IdentitySettings
	updatedLocalPassword *bool
}

func (s stubSettingsService) GetIdentitySettings(context.Context, domainidentity.Principal) (domainsettings.IdentitySettings, error) {
	return s.identity, nil
}

func (s stubSettingsService) UpdateLoginProvidersSettings(_ context.Context, _ domainidentity.Principal, _ []domainsettings.LoginProviderSettings, _ string, localPasswordEnabled bool) (domainsettings.IdentitySettings, error) {
	if s.updatedLocalPassword != nil {
		*s.updatedLocalPassword = localPasswordEnabled
	}
	return domainsettings.IdentitySettings{}, nil
}

func (s stubSettingsService) GetAISettings(context.Context, domainidentity.Principal) (domainsettings.AISettings, error) {
	return s.ai, nil
}

func (s stubSettingsService) UpdateAIWorkbenchModelSettings(context.Context, domainidentity.Principal, domainsettings.AIWorkbenchModelSettings) (domainsettings.AISettings, error) {
	return s.ai, nil
}

func (s stubSettingsService) UpdateAISkillsRegistry(context.Context, domainidentity.Principal, []domainsettings.AISkillSettings) (domainsettings.AISettings, error) {
	return s.ai, nil
}

func (s stubSettingsService) GetBrandingSettings(context.Context, domainidentity.Principal) (domainsettings.BrandingSettings, error) {
	return domainsettings.BrandingSettings{}, nil
}

func (s stubSettingsService) UpdateBrandingSettings(context.Context, domainidentity.Principal, domainsettings.BrandingSettings) (domainsettings.BrandingSettings, error) {
	return domainsettings.BrandingSettings{}, nil
}

func TestGetAISettingsDoesNotSerializeLegacyProviderFields(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/ai", nil)
	handler := NewSettingsHandler(stubSettingsService{ai: domainsettings.AISettings{
		WorkbenchModel: domainsettings.AIWorkbenchModelSettings{
			DefaultPublicModel: "gpt-public",
			DefaultRouteID:     "route-openai",
			DefaultEndpoint:    "chat/completions",
			Enabled:            true,
		},
	}}, nil)

	handler.GetAISettings(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "workbenchModel") || !strings.Contains(body, "gpt-public") {
		t.Fatalf("expected workbench model response, got %s", body)
	}
	for _, forbidden := range []string{"apiKey", "baseUrl", "providers", "provider", "defaultProviderId", "secret-key", "list-secret"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("GET /settings/ai leaked %q: %s", forbidden, body)
		}
	}
}

func TestUpdateLoginProvidersPreservesLocalPasswordSettingWhenOmitted(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/v1/settings/identity/providers", strings.NewReader(`{"providers":[]}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	updatedLocalPassword := true
	handler := NewSettingsHandler(stubSettingsService{
		identity:             domainsettings.IdentitySettings{LocalPasswordLoginEnabled: false},
		updatedLocalPassword: &updatedLocalPassword,
	}, nil)

	handler.UpdateLoginProvidersSettings(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if updatedLocalPassword {
		t.Fatal("omitted localPasswordLoginEnabled unexpectedly re-enabled password login")
	}
}
