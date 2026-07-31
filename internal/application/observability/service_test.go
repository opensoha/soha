package observability

import (
	"context"
	"errors"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainobservability "github.com/opensoha/soha/internal/domain/observability"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	"github.com/opensoha/soha/internal/platform/telemetry"
)

type observabilityRoleReader map[string][]string

func (r observabilityRoleReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return r, nil
}

type memoryDataSources struct {
	items map[string]domainobservability.DataSource
}

func (m *memoryDataSources) ListDataSources(context.Context) ([]domainobservability.DataSource, error) {
	items := make([]domainobservability.DataSource, 0, len(m.items))
	for _, item := range m.items {
		items = append(items, item)
	}
	return items, nil
}

func (m *memoryDataSources) GetDataSource(_ context.Context, id string) (domainobservability.DataSource, error) {
	item, ok := m.items[id]
	if !ok {
		return domainobservability.DataSource{}, apperrors.ErrNotFound
	}
	return item, nil
}

func (m *memoryDataSources) CreateDataSource(_ context.Context, item domainobservability.DataSource) (domainobservability.DataSource, error) {
	m.items[item.ID] = item
	return item, nil
}

func (m *memoryDataSources) UpdateDataSource(_ context.Context, id string, input domainobservability.DataSourceInput) (domainobservability.DataSource, error) {
	current := m.items[id]
	current.Name, current.BackendType, current.Enabled = input.Name, input.BackendType, input.Enabled
	current.CredentialRef, current.Scope, current.QueryBudget = input.CredentialRef, input.Scope, input.QueryBudget
	current.RedactionPolicy, current.Config, current.UpdatedAt = input.RedactionPolicy, input.Config, time.Now().UTC()
	m.items[id] = current
	return current, nil
}

func (m *memoryDataSources) UpdateDataSourceValidation(_ context.Context, id, status, message string, validatedAt time.Time) (domainobservability.DataSource, error) {
	current := m.items[id]
	current.ValidationStatus, current.ValidationMessage, current.LastValidatedAt = status, message, &validatedAt
	m.items[id] = current
	return current, nil
}

type recordingLogRegistry struct {
	config map[string]any
	query  telemetry.LogSearchQuery
	calls  int
}

func (r *recordingLogRegistry) Validate(string, map[string]any) error { return nil }

func (r *recordingLogRegistry) Search(_ context.Context, _, _ string, config map[string]any, query telemetry.LogSearchQuery) (telemetry.LogSearchResult, error) {
	r.calls++
	r.config, r.query = config, query
	return telemetry.LogSearchResult{
		Records: []telemetry.LogRecord{{
			Timestamp: time.Date(2026, 7, 31, 8, 0, 0, 0, time.UTC), Message: "ready", Severity: "info",
			ClusterID: "cluster-a", Namespace: "team-a", Pod: "api-0", Container: "api",
			Attributes: map[string]any{"safe": "value", "token": "secret"},
		}},
		NextPageToken: "provider-page", Truncated: true,
	}, nil
}

func TestDataSourceCredentialsStayEncryptedAndCursorIsPrincipalBound(t *testing.T) {
	now := time.Date(2026, 7, 31, 8, 5, 0, 0, time.UTC)
	key, err := keyring.NewKey("observability.test", "01234567890123456789012345678901", now.Add(-time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryDataSources{items: map[string]domainobservability.DataSource{}}
	logs := &recordingLogRegistry{}
	service, err := New(Dependencies{
		DataSources: store,
		Permissions: appaccess.NewPermissionResolver(observabilityRoleReader{"ops": {appaccess.PermObserveLogDataSourcesView, appaccess.PermObserveLogDataSourcesManage}}),
		Logs:        logs, Keys: keys,
	})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	principal := domainidentity.Principal{UserID: "user-1", Roles: []string{"ops"}}
	input := sohaapi.ObservabilityDataSourceInput{
		Name: "shared logs", BackendType: sohaapi.ObservabilityDataSourceBackendTypeElasticsearch, Enabled: true,
		Config:          sohaapi.ObservabilityLogDataSourceConfig{Endpoint: "https://logs.example", Index: "app-logs"},
		Credentials:     []sohaapi.SystemIntegrationCredentialInput{{Key: "bearer_token", Value: "top-secret"}},
		Scope:           &sohaapi.ObservabilityDataSourceScope{ClusterIDs: []string{"cluster-a"}, Namespaces: []string{"team-a"}},
		RedactionPolicy: &sohaapi.ObservabilityLogRedactionPolicy{DropAttributeKeys: []string{"token"}},
	}
	created, err := service.CreateDataSource(context.Background(), principal, input)
	if err != nil {
		t.Fatalf("CreateDataSource() error = %v", err)
	}
	stored := store.items[created.ID]
	if !secretcrypto.Encrypted(stored.CredentialRef) || stored.CredentialRef == "top-secret" {
		t.Fatalf("credential reference is not encrypted: %q", stored.CredentialRef)
	}
	if len(created.CredentialKeys) != 1 || created.CredentialKeys[0] != sohaapi.ObservabilityDataSourceCredentialKeysBearerToken {
		t.Fatalf("credential keys = %#v", created.CredentialKeys)
	}

	selector := domainresource.LogSourceSelector{Namespace: "team-a", PodNames: []string{"api-0"}, Containers: []string{"api"}}
	query := domainresource.LogQuery{Selector: &selector, SourceMode: sohaapi.LogSourceModeDurable, Limit: 1}
	page, err := service.QueryDurableLogs(context.Background(), principal, "cluster-a", query)
	if err != nil {
		t.Fatalf("QueryDurableLogs() error = %v", err)
	}
	if logs.config["bearerToken"] != "top-secret" || logs.query.Scope.Pod != "api-0" || page.NextCursor == "" {
		t.Fatalf("provider config/query/page = %#v %#v %#v", logs.config, logs.query, page)
	}
	if _, exists := page.Entries[0].Attributes["token"]; exists || page.Entries[0].Attributes["safe"] != "value" {
		t.Fatalf("redacted attributes = %#v", page.Entries[0].Attributes)
	}

	query.Cursor = page.NextCursor
	if _, err := service.QueryDurableLogs(context.Background(), principal, "cluster-a", query); err != nil || logs.query.PageToken != "provider-page" {
		t.Fatalf("cursor continuation error=%v providerToken=%q", err, logs.query.PageToken)
	}
	other := domainidentity.Principal{UserID: "user-2", Roles: []string{"ops"}}
	if _, err := service.QueryDurableLogs(context.Background(), other, "cluster-a", query); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("cross-principal cursor error = %v", err)
	}
}

func TestDataSourceMutationRequiresManagePermission(t *testing.T) {
	service, err := New(Dependencies{
		DataSources: &memoryDataSources{items: map[string]domainobservability.DataSource{}},
		Permissions: appaccess.NewPermissionResolver(observabilityRoleReader{"reader": {appaccess.PermObserveLogDataSourcesView}}),
		Logs:        &recordingLogRegistry{},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CreateDataSource(context.Background(), domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}, sohaapi.ObservabilityDataSourceInput{})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("CreateDataSource() error = %v, want access denied", err)
	}
}
