package settings

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type captureSettingsStore struct {
	values     map[string]capturedSetting
	failGet    bool
	failUpsert bool
}

type capturedSetting struct {
	category  string
	value     map[string]any
	updatedBy string
}

type settingsPermissionReader struct{}

func (settingsPermissionReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{}, nil
}

type captureSettingsAudit struct {
	entries []domainaudit.Entry
	err     error
}

func (c *captureSettingsAudit) Record(_ context.Context, entry domainaudit.Entry) error {
	c.entries = append(c.entries, entry)
	return c.err
}

type captureSettingsOperations struct {
	entries []domainoperation.Entry
	err     error
}

func (c *captureSettingsOperations) Record(_ context.Context, entry domainoperation.Entry) error {
	c.entries = append(c.entries, entry)
	return c.err
}

type stubSAMLMetadataPinner struct{}

func (stubSAMLMetadataPinner) PinMetadata(_ context.Context, provider domainsettings.LoginProviderSettings) (domainsettings.LoginProviderSettings, error) {
	return provider, nil
}

func (stubSAMLMetadataPinner) ValidateMetadata(context.Context, sohaapi.SAMLMetadataInput) (sohaapi.SAMLMetadataValidation, string, error) {
	return sohaapi.SAMLMetadataValidation{Valid: true}, "", nil
}

func (s *captureSettingsStore) Get(_ context.Context, key string) (map[string]any, bool, error) {
	if s.failGet {
		return nil, false, errors.New("get failed")
	}
	item, ok := s.values[key]
	if !ok {
		return nil, false, nil
	}
	return item.value, true, nil
}

func (s *captureSettingsStore) Upsert(_ context.Context, key, category string, value map[string]any, updatedBy string) error {
	if s.failUpsert {
		return errors.New("upsert failed")
	}
	if s.values == nil {
		s.values = map[string]capturedSetting{}
	}
	s.values[key] = capturedSetting{
		category:  category,
		value:     value,
		updatedBy: updatedBy,
	}
	return nil
}

func TestIdentitySettingsKeepsDeletedLoginProvidersDeleted(t *testing.T) {
	store := &captureSettingsStore{
		values: map[string]capturedSetting{
			domainsettings.IdentityLoginProvidersSettingKey: {
				category: "identity",
				value: map[string]any{
					"defaultProviderId": "",
					"providers":         []any{},
				},
			},
		},
	}
	service := &Service{
		store:       store,
		permissions: appaccess.NewPermissionResolver(nil),
	}

	item, err := service.identitySettings(context.Background())
	if err != nil {
		t.Fatalf("identitySettings returned error: %v", err)
	}
	if len(item.Providers) != 0 {
		t.Fatalf("providers len = %d, want 0: %#v", len(item.Providers), item.Providers)
	}
	if item.DefaultProviderID != "" {
		t.Fatalf("defaultProviderID = %q, want empty", item.DefaultProviderID)
	}
}

func TestBrandingSettingsRequireViewPermission(t *testing.T) {
	service := &Service{
		store:       &captureSettingsStore{},
		permissions: appaccess.NewPermissionResolver(settingsPermissionReader{}),
	}

	if _, err := service.GetBrandingSettings(context.Background(), domainidentity.Principal{UserID: "readonly", Roles: []string{"readonly"}}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("GetBrandingSettings error = %v, want access denied", err)
	}
}

func TestResolveBrandingSettingsRemainsAvailableToAuthBootstrap(t *testing.T) {
	service := &Service{store: &captureSettingsStore{}}

	item, err := service.ResolveBrandingSettings(context.Background())
	if err != nil {
		t.Fatalf("ResolveBrandingSettings returned error: %v", err)
	}
	if item.Slogan != "Soha 是一种能力！" {
		t.Fatalf("slogan = %q, want default slogan", item.Slogan)
	}
}

func TestBrandingSettingsPersistSlogan(t *testing.T) {
	store := &captureSettingsStore{}
	service := New(store, appaccess.NewPermissionResolver(settingsPermissionReader{}), nil, nil)

	item, err := service.UpdateBrandingSettings(
		context.Background(),
		domainidentity.Principal{UserID: "admin", Roles: []string{"admin"}},
		domainsettings.BrandingSettings{AppTitle: "Soha", SidebarTitle: "Soha", Slogan: "  让平台协作更简单  "},
	)
	if err != nil {
		t.Fatalf("UpdateBrandingSettings returned error: %v", err)
	}
	if item.Slogan != "让平台协作更简单" {
		t.Fatalf("slogan = %q, want trimmed slogan", item.Slogan)
	}
}

func TestLoginProviderSecretsAreRedactedAndPreserved(t *testing.T) {
	store := &captureSettingsStore{values: map[string]capturedSetting{
		domainsettings.IdentityLoginProvidersSettingKey: {value: map[string]any{
			"defaultProviderId": "oauth-main",
			"providers": []any{map[string]any{
				"id": "oauth-main", "name": "OAuth", "type": "oauth2", "enabled": false,
				"clientSecret": "stored-secret", "certificate": "stored-certificate",
			}},
			"localPasswordLoginEnabled": true,
		}},
	}}
	service := New(store, appaccess.NewPermissionResolver(settingsPermissionReader{}), nil, nil)
	item, err := service.UpdateLoginProvidersSettings(context.Background(), domainidentity.Principal{UserID: "admin", Roles: []string{"admin"}}, []domainsettings.LoginProviderSettings{{
		ID: "oauth-main", Name: "OAuth", Type: "oauth2", Enabled: false,
	}}, "oauth-main", true)
	if err != nil {
		t.Fatalf("UpdateLoginProvidersSettings returned error: %v", err)
	}
	providers, ok := store.values[domainsettings.IdentityLoginProvidersSettingKey].value["providers"].([]map[string]any)
	if !ok || len(providers) == 0 {
		t.Fatalf("stored providers have unexpected shape: %#v", store.values[domainsettings.IdentityLoginProvidersSettingKey].value["providers"])
	}
	stored := providers[0]
	if stored["clientSecret"] != "stored-secret" || stored["certificate"] != "stored-certificate" {
		t.Fatalf("stored sensitive fields were not preserved: %#v", stored)
	}
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal identity settings: %v", err)
	}
	for _, forbidden := range []string{"clientSecret", "stored-secret", "certificate", "stored-certificate"} {
		if stringContains(raw, forbidden) {
			t.Fatalf("identity settings JSON leaked %q: %s", forbidden, raw)
		}
	}
}

func TestSettingsMutationsRecordRedactedAuditAndOperations(t *testing.T) {
	store := &captureSettingsStore{}
	audit := &captureSettingsAudit{}
	operations := &captureSettingsOperations{}
	service := New(store, appaccess.NewPermissionResolver(settingsPermissionReader{}), audit, operations)
	service.SetSAMLMetadataPinner(stubSAMLMetadataPinner{})
	principal := domainidentity.Principal{UserID: "admin", UserName: "Admin", Roles: []string{"admin"}}
	ctx := context.Background()

	if _, err := service.UpdateLoginProvidersSettings(ctx, principal, nil, "", true); err != nil {
		t.Fatalf("update identity settings: %v", err)
	}
	if _, err := service.UpdateBrandingSettings(ctx, principal, domainsettings.BrandingSettings{AppTitle: "Soha"}); err != nil {
		t.Fatalf("update branding settings: %v", err)
	}
	if _, err := service.UpdateAIWorkbenchModelSettings(ctx, principal, domainsettings.AIWorkbenchModelSettings{Enabled: true}); err != nil {
		t.Fatalf("update AI workbench settings: %v", err)
	}
	if _, err := service.UpdateAISkillsRegistry(ctx, principal, nil); err != nil {
		t.Fatalf("update AI skills settings: %v", err)
	}
	if _, err := service.ValidateSAMLMetadata(ctx, principal, sohaapi.SAMLMetadataInput{Source: sohaapi.XML, XML: "sensitive-xml"}); err != nil {
		t.Fatalf("validate SAML metadata: %v", err)
	}

	wantActions := []string{
		"settings.identity.update",
		"settings.branding.update",
		"settings.ai.workbench_model.update",
		"settings.ai.skills.update",
		"settings.identity.saml.validate",
	}
	if len(audit.entries) != len(wantActions) || len(operations.entries) != len(wantActions) {
		t.Fatalf("audit/operation counts = %d/%d, want %d", len(audit.entries), len(operations.entries), len(wantActions))
	}
	for index, action := range wantActions {
		if audit.entries[index].Action != action || operations.entries[index].OperationType != action {
			t.Fatalf("action %d = %q/%q, want %q", index, audit.entries[index].Action, operations.entries[index].OperationType, action)
		}
	}
	raw, err := json.Marshal([]any{audit.entries, operations.entries})
	if err != nil {
		t.Fatalf("marshal audit evidence: %v", err)
	}
	for _, forbidden := range []string{"clientSecret", "certificate", "sensitive-xml"} {
		if stringContains(raw, forbidden) {
			t.Fatalf("settings evidence leaked %q: %s", forbidden, raw)
		}
	}
}

func TestSettingsRecorderFailuresAreObservable(t *testing.T) {
	audit := &captureSettingsAudit{err: errors.New("audit unavailable")}
	operations := &captureSettingsOperations{err: errors.New("operations unavailable")}
	service := New(&captureSettingsStore{}, appaccess.NewPermissionResolver(settingsPermissionReader{}), audit, operations)
	core, logs := observer.New(zap.WarnLevel)
	service.SetInstrumentation(zap.New(core))

	if _, err := service.UpdateBrandingSettings(context.Background(), domainidentity.Principal{UserID: "admin", Roles: []string{"admin"}}, domainsettings.BrandingSettings{AppTitle: "private-brand"}); err != nil {
		t.Fatalf("UpdateBrandingSettings returned error: %v", err)
	}
	if logs.FilterMessage("settings evidence record failed").Len() != 2 {
		t.Fatalf("warning logs = %d, want 2", logs.Len())
	}
	raw, _ := json.Marshal(logs.All())
	if strings.Contains(string(raw), "private-brand") {
		t.Fatalf("warning logs leaked settings values: %s", raw)
	}
}

func TestLoginProviderProfileFieldsRoundTrip(t *testing.T) {
	provider := normalizeLoginProvider(domainsettings.LoginProviderSettings{
		ID:          "feishu-main",
		Name:        "Feishu",
		Type:        "feishu",
		PhoneField:  "contact.mobile",
		AvatarField: "avatar.url",
	}, 0)
	stored := loginProvidersToMaps([]domainsettings.LoginProviderSettings{provider})[0]
	if stored["phoneField"] != "contact.mobile" || stored["avatarField"] != "avatar.url" {
		t.Fatalf("profile field mappings not persisted: %#v", stored)
	}
}

func TestAISettingsIgnoresLegacyProviderSecrets(t *testing.T) {
	store := &captureSettingsStore{
		values: map[string]capturedSetting{
			"ai.provider": {
				category: "ai",
				value: map[string]any{
					"enabled":           true,
					"model":             "legacy-model",
					"baseUrl":           "https://api.example.com/v1",
					"apiKey":            "secret-key",
					"defaultProviderId": "legacy-provider",
					"provider": map[string]any{
						"apiKey":  "provider-secret",
						"baseUrl": "https://provider.example.com/v1",
					},
					"providers": []any{
						map[string]any{
							"id":     "legacy-provider",
							"apiKey": "list-secret",
						},
					},
					"skillsRegistry": []any{
						map[string]any{
							"id":      "skill-1",
							"name":    "Skill One",
							"enabled": true,
						},
					},
				},
			},
		},
	}
	service := &Service{
		store:       store,
		permissions: appaccess.NewPermissionResolver(nil),
	}

	item, err := service.aiSettings(context.Background())
	if err != nil {
		t.Fatalf("aiSettings returned error: %v", err)
	}
	if item.WorkbenchModel.DefaultPublicModel != "" {
		t.Fatalf("legacy ai.provider model must not map to workbench model, got %#v", item.WorkbenchModel)
	}
	if len(item.SkillsRegistry) != 0 {
		t.Fatalf("legacy ai.provider skills must not be read, got %#v", item.SkillsRegistry)
	}
	raw, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal AI settings: %v", err)
	}
	for _, forbidden := range []string{"apiKey", "baseUrl", "providers", "provider", "defaultProviderId", "secret-key"} {
		if stringContains(raw, forbidden) {
			t.Fatalf("AI settings JSON leaked %q: %s", forbidden, raw)
		}
	}
}

func TestPersistAISettingsDoesNotWriteLegacyProviderKeys(t *testing.T) {
	store := &captureSettingsStore{}
	service := &Service{
		store:       store,
		permissions: appaccess.NewPermissionResolver(nil),
	}

	_, err := service.persistAISettings(
		context.Background(),
		"user-1",
		domainsettings.AIWorkbenchModelSettings{
			DefaultPublicModel: "gpt-public",
			DefaultRouteID:     "route-openai",
			DefaultEndpoint:    "responses",
			Enabled:            true,
		},
		[]map[string]any{{"id": "skill-1", "name": "Skill One", "enabled": true}},
	)
	if err != nil {
		t.Fatalf("persistAISettings returned error: %v", err)
	}
	upserted, ok := store.values[domainsettings.AISettingsKey]
	if !ok {
		t.Fatal("expected AI settings to be upserted")
	}
	for _, forbidden := range []string{"apiKey", "baseUrl", "provider", "providers", "defaultProviderId", "model"} {
		if _, exists := upserted.value[forbidden]; exists {
			t.Fatalf("persisted AI settings must not include %q: %#v", forbidden, upserted.value)
		}
	}
	if _, ok := upserted.value["workbenchModel"]; !ok {
		t.Fatalf("expected workbenchModel to be persisted: %#v", upserted.value)
	}
}

func TestResolveAIWorkbenchSettingsAndSkillsRegistryUseWorkbenchKey(t *testing.T) {
	store := &captureSettingsStore{
		values: map[string]capturedSetting{
			domainsettings.AISettingsKey: {
				category: "ai",
				value: map[string]any{
					"workbenchModel": map[string]any{
						"defaultPublicModel": "gpt-public",
						"defaultRouteId":     "route-openai",
						"defaultEndpoint":    "responses",
						"enabled":            true,
					},
					"skillsRegistry": []any{
						map[string]any{
							"id":      "skill-1",
							"name":    "Skill One",
							"enabled": true,
						},
					},
				},
			},
		},
	}
	service := &Service{
		store:       store,
		permissions: appaccess.NewPermissionResolver(nil),
	}

	model, err := service.ResolveAIWorkbenchSettings(context.Background())
	if err != nil {
		t.Fatalf("ResolveAIWorkbenchSettings returned error: %v", err)
	}
	if model.DefaultPublicModel != "gpt-public" || model.DefaultRouteID != "route-openai" || model.DefaultEndpoint != "responses" || !model.Enabled {
		t.Fatalf("unexpected workbench model settings: %#v", model)
	}
	skills, err := service.ResolveAISkillsRegistry(context.Background())
	if err != nil {
		t.Fatalf("ResolveAISkillsRegistry returned error: %v", err)
	}
	if len(skills) != 1 || skills[0].ID != "skill-1" {
		t.Fatalf("unexpected skills registry: %#v", skills)
	}
}

func stringContains(raw []byte, needle string) bool {
	return strings.Contains(string(raw), needle)
}
