package settings

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
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
