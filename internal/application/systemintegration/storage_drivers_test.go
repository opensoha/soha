package systemintegration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/contracts/plugins/storagedriver"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/stretchr/testify/require"
)

const testStorageSchema = `{
  "type":"object",
  "additionalProperties":false,
  "x-soha-test-connection-action":"storage.testConnection",
  "properties":{
    "container":{"type":"string","pattern":"^[a-z]+$"},
    "prefix":{"type":"string"},
    "account":{"type":"string"},
    "sas_token":{"type":"string","x-soha-widget":"secret-ref"}
  },
  "required":["container","account","sas_token"]
}`

type captureDriver struct {
	storagedriver.Driver
	binding storagedriver.Binding
	calls   int
}

func (d *captureDriver) TestConnection(_ context.Context, binding storagedriver.Binding) error {
	d.binding = binding
	d.calls++
	return nil
}

type storageSecretStub struct {
	allowed string
	next    string
	calls   int
}

func (s *storageSecretStub) PinReferences(_ context.Context, principal domainidentity.Principal, refs map[string]string, target domainsecret.Target) ([]domainsecret.Reference, error) {
	if principal.UserID != s.allowed || target.Type != "capability" || target.Ref != "storage.driver.example.blob" || refs["SAS_TOKEN"] != "soha://secrets/test-secret" {
		return nil, apperrors.ErrAccessDenied
	}
	return []domainsecret.Reference{{Alias: "SAS_TOKEN", SecretID: "test-secret", Version: 1}}, nil
}

func (s *storageSecretStub) ResolvePinnedReferences(_ context.Context, _ domainidentity.Principal, refs []domainsecret.Reference, _ domainsecret.Target) (map[string]string, error) {
	s.calls++
	return map[string]string{refs[0].Alias: s.next}, nil
}

func TestPluginStorageBindingIsScopedAndSecretsResolvePerCall(t *testing.T) {
	ctx := t.Context()
	repo := newMemoryIntegrationRepository()
	service := testIntegrationService(t, repo)
	driver := &captureDriver{}
	registry := NewStorageDriverRegistry(nil)
	require.NoError(t, registry.RegisterPlugin("example.blob", "Blob", []byte(testStorageSchema), driver))
	actor := adminPrincipal()
	secrets := &storageSecretStub{allowed: actor.UserID, next: "synthetic-one"}
	service.SetStorageDrivers(registry, secrets)
	request := sohaapi.SystemIntegrationCreateRequest{
		Category: "storage", ProviderType: "example.blob", Name: "Blob test", Enabled: true,
		Configuration: []sohaapi.SystemIntegrationConfigurationField{
			{Key: "container", Value: "abc"}, {Key: "prefix", Value: "scoped"},
			{Key: "account", Value: "fixture"}, {Key: "sas_token", Value: "soha://secrets/test-secret"},
		},
	}
	require.ErrorIs(t, func() error {
		modified := request
		modified.Configuration = append([]sohaapi.SystemIntegrationConfigurationField(nil), request.Configuration...)
		modified.Configuration[3].Value = secrets.next
		_, err := service.Create(ctx, actor, modified)
		return err
	}(), apperrors.ErrInvalidArgument)
	item, err := service.Create(ctx, actor, request)
	require.NoError(t, err)
	require.Empty(t, repo.credentials[item.ID])
	require.Zero(t, secrets.calls)
	require.Zero(t, driver.calls)
	result, err := service.Test(ctx, actor, item.ID)
	require.NoError(t, err)
	require.Equal(t, sohaapi.SystemIntegrationTestStatusSucceeded, result.Status)
	require.Equal(t, item.ID, driver.binding.ID)
	require.Equal(t, "scoped/", driver.binding.Prefix)
	require.JSONEq(t, `{"account":"fixture"}`, string(driver.binding.Settings))
	require.Equal(t, map[string]string{"sas_token": "synthetic-one"}, driver.binding.Credentials)
	secrets.next = "synthetic-two"
	_, next, err := service.ResolveStorageDriver(ctx, actor, item.ID)
	require.NoError(t, err)
	require.Equal(t, "synthetic-two", next.Credentials["sas_token"])
	_, _, err = service.ResolveStorageDriver(ctx, domainidentity.Principal{UserID: "denied"}, item.ID)
	require.ErrorIs(t, err, apperrors.ErrAccessDenied)
	other := actor
	other.UserID = "different-admin"
	_, err = service.Test(ctx, other, item.ID)
	require.ErrorIs(t, err, apperrors.ErrAccessDenied)
	require.Equal(t, 1, driver.calls)
	require.Equal(t, 2, secrets.calls)
	for _, public := range []any{item, result} {
		raw, err := json.Marshal(public)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "synthetic-")
	}
}

func TestStorageDriverRegistryRejectsUnknownConfiguration(t *testing.T) {
	registry := NewStorageDriverRegistry(nil)
	driver := &captureDriver{}
	require.Error(t, registry.RegisterPlugin("s3", "Reserved", []byte(testStorageSchema), driver))
	require.Error(t, registry.RegisterPlugin("example.blob", "Bad", []byte(strings.Replace(testStorageSchema, "secret-ref", "password", 1)), driver))
	require.NoError(t, registry.RegisterPlugin("example.blob", "Blob", []byte(testStorageSchema), driver))
	require.Error(t, registry.RegisterPlugin("example.blob", "Duplicate", []byte(testStorageSchema), driver))
	_, _, err := registry.plugins["example.blob"].binding("fixture", []sohaapi.SystemIntegrationConfigurationField{{Key: "extra", Value: "secret"}})
	require.ErrorIs(t, err, apperrors.ErrInvalidArgument)
}
