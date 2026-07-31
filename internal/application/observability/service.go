package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainobservability "github.com/opensoha/soha/internal/domain/observability"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/requestctx"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
	"github.com/opensoha/soha/internal/platform/telemetry"
)

const (
	dataSourceKindLogs = "logs"
	logsAdapterID      = "logs.v1"
)

type DataSourceStore interface {
	ListDataSources(context.Context) ([]domainobservability.DataSource, error)
	GetDataSource(context.Context, string) (domainobservability.DataSource, error)
	CreateDataSource(context.Context, domainobservability.DataSource) (domainobservability.DataSource, error)
	UpdateDataSource(context.Context, string, domainobservability.DataSourceInput) (domainobservability.DataSource, error)
	UpdateDataSourceValidation(context.Context, string, string, string, time.Time) (domainobservability.DataSource, error)
}

type LogRegistry interface {
	Validate(string, map[string]any) error
	Search(context.Context, string, string, map[string]any, telemetry.LogSearchQuery) (telemetry.LogSearchResult, error)
}

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}

type Dependencies struct {
	DataSources DataSourceStore
	Permissions *appaccess.PermissionResolver
	Logs        LogRegistry
	Audit       AuditRecorder
	Keys        keyring.Ring
}

type Service struct {
	dataSources DataSourceStore
	permissions *appaccess.PermissionResolver
	logs        LogRegistry
	audit       AuditRecorder
	keys        keyring.Ring
	now         func() time.Time
	// ponytail: one collection lock keeps confirmation single-use in-process; split by cluster if installs need concurrency.
	collectionMu sync.Mutex
	collection   CollectionDependencies
}

func New(deps Dependencies) (*Service, error) {
	if deps.DataSources == nil || deps.Permissions == nil || deps.Logs == nil {
		return nil, fmt.Errorf("observability data sources, permissions, and log registry are required")
	}
	return &Service{dataSources: deps.DataSources, permissions: deps.Permissions, logs: deps.Logs, audit: deps.Audit, keys: deps.Keys, now: time.Now}, nil
}

func (s *Service) ListDataSources(ctx context.Context, principal domainidentity.Principal) ([]sohaapi.ObservabilityDataSource, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveLogDataSourcesView); err != nil {
		return nil, err
	}
	items, err := s.dataSources.ListDataSources(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]sohaapi.ObservabilityDataSource, 0, len(items))
	for _, item := range items {
		if item.SourceKind != dataSourceKindLogs {
			continue
		}
		result = append(result, s.publicDataSource(item))
	}
	return result, nil
}

func (s *Service) CreateDataSource(ctx context.Context, principal domainidentity.Principal, input sohaapi.ObservabilityDataSourceInput) (sohaapi.ObservabilityDataSource, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveLogDataSourcesManage); err != nil {
		return sohaapi.ObservabilityDataSource{}, err
	}
	item, err := s.normalizeInput(input, "")
	if err != nil {
		return sohaapi.ObservabilityDataSource{}, err
	}
	credentials, err := normalizeCredentialChanges(nil, input.Credentials, input.ClearCredentialKeys)
	if err != nil {
		return sohaapi.ObservabilityDataSource{}, err
	}
	item.CredentialRef, err = s.encryptCredentials(credentials)
	if err != nil {
		return sohaapi.ObservabilityDataSource{}, fmt.Errorf("%w: encrypt data-source credentials", apperrors.ErrInvalidArgument)
	}
	now := s.now().UTC()
	created, err := s.dataSources.CreateDataSource(ctx, domainobservability.DataSource{
		ID: item.ID, Name: item.Name, SourceKind: item.SourceKind, BackendType: item.BackendType, Enabled: item.Enabled,
		CredentialRef: item.CredentialRef, Scope: item.Scope, QueryBudget: item.QueryBudget, RedactionPolicy: item.RedactionPolicy,
		MCPAdapter: item.MCPAdapter, Config: item.Config, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return sohaapi.ObservabilityDataSource{}, err
	}
	s.recordMutation(ctx, principal, created, "observability.data-source.create", "created log data source")
	return s.publicDataSource(created), nil
}

func (s *Service) UpdateDataSource(ctx context.Context, principal domainidentity.Principal, dataSourceID string, input sohaapi.ObservabilityDataSourceInput) (sohaapi.ObservabilityDataSource, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveLogDataSourcesManage); err != nil {
		return sohaapi.ObservabilityDataSource{}, err
	}
	current, err := s.dataSources.GetDataSource(ctx, strings.TrimSpace(dataSourceID))
	if err != nil {
		return sohaapi.ObservabilityDataSource{}, err
	}
	if current.SourceKind != dataSourceKindLogs {
		return sohaapi.ObservabilityDataSource{}, fmt.Errorf("%w: log data source not found", apperrors.ErrNotFound)
	}
	item, err := s.normalizeInput(input, current.ID)
	if err != nil {
		return sohaapi.ObservabilityDataSource{}, err
	}
	item.CredentialRef = current.CredentialRef
	if len(input.Credentials) > 0 || len(input.ClearCredentialKeys) > 0 {
		credentials, decryptErr := s.decryptCredentials(current.CredentialRef)
		if decryptErr != nil {
			return sohaapi.ObservabilityDataSource{}, fmt.Errorf("%w: existing data-source credentials cannot be updated", apperrors.ErrInvalidArgument)
		}
		credentials, err = normalizeCredentialChanges(credentials, input.Credentials, input.ClearCredentialKeys)
		if err != nil {
			return sohaapi.ObservabilityDataSource{}, err
		}
		item.CredentialRef, err = s.encryptCredentials(credentials)
		if err != nil {
			return sohaapi.ObservabilityDataSource{}, fmt.Errorf("%w: encrypt data-source credentials", apperrors.ErrInvalidArgument)
		}
	}
	updated, err := s.dataSources.UpdateDataSource(ctx, current.ID, item)
	if err != nil {
		return sohaapi.ObservabilityDataSource{}, err
	}
	s.recordMutation(ctx, principal, updated, "observability.data-source.update", "updated log data source")
	return s.publicDataSource(updated), nil
}

func (s *Service) ValidateDataSource(ctx context.Context, principal domainidentity.Principal, dataSourceID string) (sohaapi.ObservabilityDataSource, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveLogDataSourcesManage); err != nil {
		return sohaapi.ObservabilityDataSource{}, err
	}
	item, err := s.dataSources.GetDataSource(ctx, strings.TrimSpace(dataSourceID))
	if err != nil {
		return sohaapi.ObservabilityDataSource{}, err
	}
	if item.SourceKind != dataSourceKindLogs {
		return sohaapi.ObservabilityDataSource{}, fmt.Errorf("%w: log data source not found", apperrors.ErrNotFound)
	}
	checkedAt := s.now().UTC()
	config, configErr := s.runtimeConfig(item)
	if configErr == nil {
		configErr = s.logs.Validate(item.BackendType, config)
	}
	if configErr == nil {
		queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, configErr = s.logs.Search(queryCtx, item.BackendType, item.ID, config, telemetry.LogSearchQuery{
			Scope: scopeForValidation(item.Scope), TimeFrom: checkedAt.Add(-5 * time.Minute), TimeTo: checkedAt, Limit: 1, Direction: "backward",
		})
		cancel()
	}
	status, message := "success", ""
	if configErr != nil {
		status, message = "error", "backend validation failed"
	}
	updated, err := s.dataSources.UpdateDataSourceValidation(ctx, item.ID, status, message, checkedAt)
	if err != nil {
		return sohaapi.ObservabilityDataSource{}, err
	}
	s.recordMutation(ctx, principal, updated, "observability.data-source.validate", "validated log data source")
	return s.publicDataSource(updated), nil
}

func (s *Service) normalizeInput(input sohaapi.ObservabilityDataSourceInput, id string) (domainobservability.DataSourceInput, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domainobservability.DataSourceInput{}, fmt.Errorf("%w: data source name is required", apperrors.ErrInvalidArgument)
	}
	backend := strings.ToLower(strings.TrimSpace(string(input.BackendType)))
	if backend != "loki" && backend != "elasticsearch" && backend != "clickhouse" {
		return domainobservability.DataSourceInput{}, fmt.Errorf("%w: unsupported log backend", apperrors.ErrInvalidArgument)
	}
	config := dataSourceConfigMap(input.Config)
	if err := s.logs.Validate(backend, config); err != nil {
		return domainobservability.DataSourceInput{}, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	if id == "" {
		id = "ds:" + uuid.NewString()
	}
	return domainobservability.DataSourceInput{
		ID: id, Name: name, SourceKind: dataSourceKindLogs, BackendType: backend, Enabled: input.Enabled,
		Scope: scopeMap(input.Scope), QueryBudget: budgetMap(input.QueryBudget), RedactionPolicy: redactionMap(input.RedactionPolicy),
		MCPAdapter: logsAdapterID, Config: config,
	}, nil
}

func (s *Service) publicDataSource(item domainobservability.DataSource) sohaapi.ObservabilityDataSource {
	credentialKeys := make([]sohaapi.ObservabilityDataSourceCredentialKeys, 0)
	if credentials, err := s.decryptCredentials(item.CredentialRef); err == nil {
		keys := make([]string, 0, len(credentials))
		for key := range credentials {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			credentialKeys = append(credentialKeys, sohaapi.ObservabilityDataSourceCredentialKeys(key))
		}
	}
	status := sohaapi.ObservabilityDataSourceValidationStatusUnknown
	switch item.ValidationStatus {
	case "success":
		status = sohaapi.ObservabilityDataSourceValidationStatusHealthy
	case "error":
		status = sohaapi.ObservabilityDataSourceValidationStatusFailed
	}
	backendType := item.BackendType
	if backendType == "es" {
		backendType = "elasticsearch"
	}
	return sohaapi.ObservabilityDataSource{
		ID: item.ID, Name: item.Name, BackendType: sohaapi.ObservabilityDataSourceBackendType(backendType), Enabled: item.Enabled,
		Scope: apiScope(item.Scope), QueryBudget: apiBudget(item.QueryBudget), RedactionPolicy: apiRedaction(item.RedactionPolicy),
		Config: apiConfig(item.Config), CredentialKeys: credentialKeys, ValidationStatus: status,
		ValidationMessage: item.ValidationMessage, LastValidatedAt: item.LastValidatedAt, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func (s *Service) runtimeConfig(item domainobservability.DataSource) (map[string]any, error) {
	config := make(map[string]any, len(item.Config)+3)
	for key, value := range item.Config {
		config[key] = value
	}
	if strings.TrimSpace(item.CredentialRef) == "" {
		return config, nil
	}
	credentials, err := s.decryptCredentials(item.CredentialRef)
	if err != nil {
		return nil, err
	}
	for key, value := range credentials {
		switch key {
		case "bearer_token":
			config["bearerToken"] = value
		case "username", "password":
			config[key] = value
		}
	}
	return config, nil
}

func (s *Service) encryptCredentials(credentials map[string]string) (string, error) {
	if len(credentials) == 0 {
		return "", nil
	}
	payload, err := json.Marshal(credentials)
	if err != nil {
		return "", err
	}
	return secretcrypto.EncryptStringWithKeyring(s.keys, string(payload))
}

func (s *Service) decryptCredentials(reference string) (map[string]string, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return map[string]string{}, nil
	}
	if !secretcrypto.Encrypted(reference) {
		return nil, fmt.Errorf("external credential references are not configured")
	}
	payload, err := secretcrypto.DecryptStringWithKeyring(s.keys, reference)
	if err != nil {
		return nil, err
	}
	credentials := map[string]string{}
	if err := json.Unmarshal([]byte(payload), &credentials); err != nil {
		return nil, err
	}
	return credentials, nil
}

func normalizeCredentialChanges(current map[string]string, inputs []sohaapi.SystemIntegrationCredentialInput, clear []sohaapi.ObservabilityDataSourceInputClearCredentialKeys) (map[string]string, error) {
	result := make(map[string]string, len(current)+len(inputs))
	for key, value := range current {
		result[key] = value
	}
	for _, key := range clear {
		delete(result, string(key))
	}
	seen := map[string]struct{}{}
	for _, input := range inputs {
		key, value := strings.ToLower(strings.TrimSpace(input.Key)), strings.TrimSpace(input.Value)
		if key != "bearer_token" && key != "username" && key != "password" || value == "" {
			return nil, fmt.Errorf("%w: invalid data-source credential", apperrors.ErrInvalidArgument)
		}
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("%w: duplicate data-source credential", apperrors.ErrInvalidArgument)
		}
		seen[key] = struct{}{}
		result[key] = value
	}
	return result, nil
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, permission string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permission)
}

func (s *Service) recordMutation(ctx context.Context, principal domainidentity.Principal, item domainobservability.DataSource, action, summary string) {
	if s.audit == nil {
		return
	}
	metadata := requestctx.FromContext(ctx)
	_ = s.audit.Record(ctx, domainaudit.Entry{ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams, ResourceKind: "ObservabilityDataSource", ResourceName: item.ID, Action: action, Result: "success", Summary: summary, RequestPath: metadata.Path, RequestMethod: metadata.Method, RequestID: metadata.RequestID, SourceIP: metadata.SourceIP, CreatedAt: s.now().UTC()})
}

func scopeForValidation(scope map[string]any) telemetry.LogScope {
	return telemetry.LogScope{ClusterID: firstString(scope["clusterIds"]), Namespace: firstString(scope["namespaces"])}
}
