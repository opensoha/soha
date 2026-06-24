package settings

import (
	"context"
	"errors"
	"fmt"
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

func TestOpenAICompatibleReplyFromBody(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "chat completion json",
			body: `{"choices":[{"message":{"content":"pong"}}]}`,
			want: "pong",
		},
		{
			name: "sse completion",
			body: "data: {\"choices\":[{\"delta\":{\"content\":\"po\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"ng\"}}]}\n\ndata: [DONE]\n",
			want: "pong",
		},
		{
			name: "sse completion preserves spaces across chunks",
			body: "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\ndata: [DONE]\n",
			want: "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := openAICompatibleReplyFromBody([]byte(tt.body))
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestIdentitySettingsMigratesLegacyOIDCToLoginProviders(t *testing.T) {
	store := &captureSettingsStore{
		values: map[string]capturedSetting{
			domainsettings.IdentityOIDCSettingKey: {
				category: "identity",
				value: map[string]any{
					"enabled":      true,
					"providerName": "Legacy OIDC",
					"issuer":       "https://issuer.example.com",
					"clientId":     "client-id",
					"clientSecret": "client-secret",
					"redirectUrl":  "https://soha.example.com/auth/callback",
					"scopes":       []any{"openid", "profile"},
					"defaultRoles": []any{"admin"},
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
	if len(item.Providers) != 1 {
		t.Fatalf("providers len = %d, want 1", len(item.Providers))
	}
	if item.Providers[0].ID != "Legacy OIDC" && item.Providers[0].ID != "oidc-default" {
		t.Fatalf("unexpected provider id: %#v", item.Providers[0])
	}
	upserted, ok := store.values[domainsettings.IdentityLoginProvidersSettingKey]
	if !ok {
		t.Fatal("expected identity.login_providers to be migrated")
	}
	if upserted.updatedBy != "system" {
		t.Fatalf("updatedBy = %q, want system", upserted.updatedBy)
	}
	if got := fmt.Sprint(upserted.value["defaultProviderId"]); got == "" {
		t.Fatal("expected defaultProviderId to be written")
	}
}
