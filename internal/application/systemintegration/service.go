package systemintegration

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)

type AuditRecorder interface {
	Record(context.Context, domainaudit.Entry) error
}
type OperationRecorder interface {
	Record(context.Context, domainoperation.Entry) error
}

type SourceAdapter interface {
	TestConnection(context.Context) error
	ListRepositories(context.Context, string, string, int) ([]sohaapi.SourceRepository, string, error)
	ListRepositoryBranches(context.Context, string, string, int) ([]sohaapi.SourceBranch, error)
	ListRepositoryTags(context.Context, string, string, int) ([]sohaapi.SourceTag, error)
	GetRepositoryFile(context.Context, string, string, string) (sohaapi.SourceFile, error)
}

type SourceAdapterFactory interface {
	Build(domain.Integration, map[string]string) (SourceAdapter, error)
}

type Service struct {
	repo        domain.Repository
	permissions *appaccess.PermissionResolver
	audit       AuditRecorder
	operations  OperationRecorder
	keys        keyring.Ring
	adapters    map[string]SourceAdapterFactory
	now         func() time.Time
}

type LegacyGitLabConfig struct {
	Enabled bool
	BaseURL string
	Token   string
	GroupID string
	PerPage int
	Timeout time.Duration
}

func New(repo domain.Repository, permissions *appaccess.PermissionResolver, audit AuditRecorder, operations OperationRecorder, keys keyring.Ring) *Service {
	return &Service{repo: repo, permissions: permissions, audit: audit, operations: operations, keys: keys, adapters: map[string]SourceAdapterFactory{}, now: time.Now}
}

func (s *Service) RegisterSourceAdapter(providerType string, factory SourceAdapterFactory) {
	providerType = strings.ToLower(strings.TrimSpace(providerType))
	if providerType != "" && factory != nil {
		s.adapters[providerType] = factory
	}
}

func (s *Service) ImportLegacyGitLab(ctx context.Context, config LegacyGitLabConfig) error {
	if strings.TrimSpace(config.Token) == "" || strings.TrimSpace(config.BaseURL) == "" {
		return nil
	}
	items, err := s.repo.List(ctx, domain.Filter{Category: domain.CategorySourceControl, ProviderType: domain.ProviderGitLab})
	if err != nil || len(items) > 0 {
		return err
	}
	request := sohaapi.SystemIntegrationCreateRequest{
		Category: sohaapi.SystemIntegrationCategorySourceControl, ProviderType: domain.ProviderGitLab,
		Name: "GitLab", Description: "Imported from legacy config.yaml", Enabled: config.Enabled,
		Configuration: []sohaapi.SystemIntegrationConfigurationField{
			{Key: "base_url", Value: strings.TrimSpace(config.BaseURL)},
			{Key: "group_id", Value: strings.TrimSpace(config.GroupID)},
			{Key: "per_page", Value: strconv.Itoa(config.PerPage)},
			{Key: "timeout", Value: config.Timeout.String()},
		},
		Credentials: []sohaapi.SystemIntegrationCredentialInput{{Key: "token", Value: config.Token}},
	}
	item, credentials, err := s.normalizeCreate(request, "system")
	if err != nil {
		return fmt.Errorf("normalize legacy gitlab integration: %w", err)
	}
	_, err = s.repo.Create(ctx, item, credentials)
	return err
}

func (s *Service) List(ctx context.Context, principal domainidentity.Principal, filter domain.Filter) ([]sohaapi.SystemIntegration, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsSystemIntegrationsView); err != nil {
		return nil, err
	}
	items, err := s.repo.List(ctx, normalizeFilter(filter))
	if err != nil {
		return nil, err
	}
	result := make([]sohaapi.SystemIntegration, 0, len(items))
	for _, item := range items {
		result = append(result, publicIntegration(item))
	}
	return result, nil
}

func (s *Service) Get(ctx context.Context, principal domainidentity.Principal, id string) (sohaapi.SystemIntegration, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsSystemIntegrationsView); err != nil {
		return sohaapi.SystemIntegration{}, err
	}
	item, err := s.repo.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return sohaapi.SystemIntegration{}, err
	}
	return publicIntegration(item), nil
}

func (s *Service) Create(ctx context.Context, principal domainidentity.Principal, request sohaapi.SystemIntegrationCreateRequest) (sohaapi.SystemIntegration, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsSystemIntegrationsManage); err != nil {
		return sohaapi.SystemIntegration{}, err
	}
	item, credentials, err := s.normalizeCreate(request, principal.UserID)
	if err != nil {
		return sohaapi.SystemIntegration{}, err
	}
	created, err := s.repo.Create(ctx, item, credentials)
	if err != nil {
		return sohaapi.SystemIntegration{}, err
	}
	s.recordMutation(ctx, principal, "settings.system-integration.create", created, "created system integration")
	return publicIntegration(created), nil
}

func (s *Service) Update(ctx context.Context, principal domainidentity.Principal, id string, input domain.UpdateInput) (sohaapi.SystemIntegration, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsSystemIntegrationsManage); err != nil {
		return sohaapi.SystemIntegration{}, err
	}
	current, err := s.repo.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return sohaapi.SystemIntegration{}, err
	}
	item, credentials, clearKeys, err := s.normalizeUpdate(current, input, principal.UserID)
	if err != nil {
		return sohaapi.SystemIntegration{}, err
	}
	updated, err := s.repo.Update(ctx, item, input.ExpectedVersion, credentials, clearKeys)
	if err != nil {
		return sohaapi.SystemIntegration{}, err
	}
	s.recordMutation(ctx, principal, "settings.system-integration.update", updated, "updated system integration")
	return publicIntegration(updated), nil
}

func (s *Service) Delete(ctx context.Context, principal domainidentity.Principal, id string) error {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsSystemIntegrationsManage); err != nil {
		return err
	}
	item, err := s.repo.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, item.ID); err != nil {
		return err
	}
	s.recordMutation(ctx, principal, "settings.system-integration.delete", item, "deleted system integration")
	return nil
}

func (s *Service) Test(ctx context.Context, principal domainidentity.Principal, id string) (sohaapi.SystemIntegrationTestResult, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsSystemIntegrationsManage); err != nil {
		return sohaapi.SystemIntegrationTestResult{}, err
	}
	item, adapter, err := s.sourceAdapter(ctx, strings.TrimSpace(id), false)
	if err != nil {
		return sohaapi.SystemIntegrationTestResult{}, err
	}
	started := s.now().UTC()
	err = adapter.TestConnection(ctx)
	checkedAt := s.now().UTC()
	result := sohaapi.SystemIntegrationTestResult{IntegrationID: item.ID, CheckedAt: checkedAt, LatencyMs: max(0, checkedAt.Sub(started).Milliseconds()), Capabilities: sourceCapabilities()}
	if err != nil {
		result.Status = sohaapi.SystemIntegrationTestStatusFailed
		result.Message = "connection test failed"
		_ = s.repo.UpdateHealth(ctx, item.ID, domain.HealthUnhealthy, redactProviderError(err), checkedAt)
		return result, nil
	}
	result.Status = sohaapi.SystemIntegrationTestStatusSucceeded
	_ = s.repo.UpdateHealth(ctx, item.ID, domain.HealthHealthy, "", checkedAt)
	return result, nil
}

func (s *Service) normalizeCreate(request sohaapi.SystemIntegrationCreateRequest, actor string) (domain.Integration, map[string]string, error) {
	category := strings.ToLower(strings.TrimSpace(string(request.Category)))
	providerType := strings.ToLower(strings.TrimSpace(request.ProviderType))
	name := strings.TrimSpace(request.Name)
	description := strings.TrimSpace(request.Description)
	if name == "" || len(name) > 200 || len(description) > 1000 || !validKey(providerType) || !validCategory(category) {
		return domain.Integration{}, nil, fmt.Errorf("%w: invalid system integration identity", apperrors.ErrInvalidArgument)
	}
	configuration, err := normalizeConfiguration(request.Configuration)
	if err != nil {
		return domain.Integration{}, nil, err
	}
	credentials, err := s.encryptCredentialInputs(request.Credentials)
	if err != nil {
		return domain.Integration{}, nil, err
	}
	if err := validateProviderConfiguration(category, providerType, request.Enabled, configuration, credentialKeySet(credentials)); err != nil {
		return domain.Integration{}, nil, err
	}
	now := s.now().UTC()
	return domain.Integration{ID: uuid.NewString(), Category: category, ProviderType: providerType, Name: name, Description: description, Enabled: request.Enabled, Configuration: configuration, HealthStatus: domain.HealthUnknown, Version: 1, CreatedBy: actor, UpdatedBy: actor, CreatedAt: now, UpdatedAt: now}, credentials, nil
}

func (s *Service) normalizeUpdate(current domain.Integration, input domain.UpdateInput, actor string) (domain.Integration, map[string]string, []string, error) {
	if input.ExpectedVersion < 1 {
		return domain.Integration{}, nil, nil, fmt.Errorf("%w: expectedVersion is required", apperrors.ErrInvalidArgument)
	}
	item := current
	if input.Name != nil {
		item.Name = strings.TrimSpace(*input.Name)
	}
	if input.Description != nil {
		item.Description = strings.TrimSpace(*input.Description)
	}
	if input.Enabled != nil {
		item.Enabled = *input.Enabled
	}
	if input.Configuration != nil {
		configuration, err := normalizeConfiguration(*input.Configuration)
		if err != nil {
			return domain.Integration{}, nil, nil, err
		}
		item.Configuration = configuration
	}
	if item.Name == "" || len(item.Name) > 200 || len(item.Description) > 1000 {
		return domain.Integration{}, nil, nil, fmt.Errorf("%w: integration name is required", apperrors.ErrInvalidArgument)
	}
	credentials, err := s.encryptCredentialInputs(input.Credentials)
	if err != nil {
		return domain.Integration{}, nil, nil, err
	}
	clearKeys, err := normalizeKeys(input.ClearCredentialKeys)
	if err != nil {
		return domain.Integration{}, nil, nil, err
	}
	configuredKeys := make(map[string]struct{}, len(current.CredentialKeys)+len(credentials))
	for _, key := range current.CredentialKeys {
		configuredKeys[key] = struct{}{}
	}
	for _, key := range clearKeys {
		delete(configuredKeys, key)
	}
	// Repository updates clear old values before writing replacements. Mirror that
	// ordering so clearing and replacing a credential in one request is valid.
	for key := range credentials {
		configuredKeys[key] = struct{}{}
	}
	if err := validateProviderConfiguration(item.Category, item.ProviderType, item.Enabled, item.Configuration, configuredKeys); err != nil {
		return domain.Integration{}, nil, nil, err
	}
	item.UpdatedBy = actor
	item.UpdatedAt = s.now().UTC()
	return item, credentials, clearKeys, nil
}

func (s *Service) encryptCredentialInputs(inputs []sohaapi.SystemIntegrationCredentialInput) (map[string]string, error) {
	if len(inputs) > 20 {
		return nil, fmt.Errorf("%w: too many system integration credentials", apperrors.ErrInvalidArgument)
	}
	credentials := make(map[string]string, len(inputs))
	for _, input := range inputs {
		key := strings.ToLower(strings.TrimSpace(input.Key))
		value := strings.TrimSpace(input.Value)
		if !validKey(key) || value == "" || len(value) > 16384 {
			return nil, fmt.Errorf("%w: invalid system integration credential", apperrors.ErrInvalidArgument)
		}
		if _, exists := credentials[key]; exists {
			return nil, fmt.Errorf("%w: duplicate credential key %s", apperrors.ErrInvalidArgument, key)
		}
		encrypted, err := secretcrypto.EncryptStringWithKeyring(s.keys, value)
		if err != nil {
			return nil, fmt.Errorf("%w: encrypt system integration credential", apperrors.ErrInvalidArgument)
		}
		credentials[key] = encrypted
	}
	return credentials, nil
}

func (s *Service) sourceAdapter(ctx context.Context, id string, requireEnabled bool) (domain.Integration, SourceAdapter, error) {
	item, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.Integration{}, nil, err
	}
	if item.Category != domain.CategorySourceControl || (requireEnabled && !item.Enabled) {
		return domain.Integration{}, nil, fmt.Errorf("%w: source connection is unavailable", apperrors.ErrAccessDenied)
	}
	factory := s.adapters[item.ProviderType]
	if factory == nil {
		return domain.Integration{}, nil, fmt.Errorf("%w: source provider is unsupported", apperrors.ErrInvalidArgument)
	}
	credentials, err := s.decryptCredentials(ctx, item.ID)
	if err != nil {
		return domain.Integration{}, nil, err
	}
	adapter, err := factory.Build(item, credentials)
	if err != nil {
		return domain.Integration{}, nil, err
	}
	return item, adapter, nil
}

func (s *Service) decryptCredentials(ctx context.Context, id string) (map[string]string, error) {
	encrypted, err := s.repo.Credentials(ctx, id)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(encrypted))
	for key, value := range encrypted {
		plain, err := secretcrypto.DecryptStringWithKeyring(s.keys, value)
		if err != nil {
			return nil, fmt.Errorf("decrypt system integration credential %s: %w", key, err)
		}
		result[key] = plain
	}
	return result, nil
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, permission string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permission)
}

func (s *Service) recordMutation(ctx context.Context, principal domainidentity.Principal, operationType string, item domain.Integration, summary string) {
	metadata := requestctx.FromContext(ctx)
	if s.audit != nil {
		_ = s.audit.Record(ctx, domainaudit.Entry{ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams, ResourceKind: "SystemIntegration", ResourceName: item.ID, Action: operationType, Result: "success", Summary: summary, RequestPath: metadata.Path, RequestMethod: metadata.Method, RequestID: metadata.RequestID, SourceIP: metadata.SourceIP, CreatedAt: s.now().UTC()})
	}
	if s.operations != nil {
		_ = s.operations.Record(ctx, operationentry.New(ctx, principal, operationType, map[string]any{"resourceKind": "SystemIntegration", "resourceName": item.ID}, "success", summary, map[string]any{"providerType": item.ProviderType, "category": item.Category}))
	}
}

func publicIntegration(item domain.Integration) sohaapi.SystemIntegration {
	credentialKeys := append([]string(nil), item.CredentialKeys...)
	sort.Strings(credentialKeys)
	return sohaapi.SystemIntegration{ID: item.ID, Category: sohaapi.SystemIntegrationCategory(item.Category), ProviderType: item.ProviderType, Name: item.Name, Description: item.Description, Enabled: item.Enabled, Configuration: append([]sohaapi.SystemIntegrationConfigurationField(nil), item.Configuration...), CredentialKeys: credentialKeys, HealthStatus: sohaapi.SystemIntegrationHealthStatus(item.HealthStatus), LastCheckedAt: item.LastCheckedAt, Version: item.Version, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

func normalizeFilter(filter domain.Filter) domain.Filter {
	filter.Category = strings.ToLower(strings.TrimSpace(filter.Category))
	filter.ProviderType = strings.ToLower(strings.TrimSpace(filter.ProviderType))
	return filter
}

func normalizeConfiguration(fields []sohaapi.SystemIntegrationConfigurationField) ([]sohaapi.SystemIntegrationConfigurationField, error) {
	if len(fields) > 100 {
		return nil, fmt.Errorf("%w: too many system integration configuration fields", apperrors.ErrInvalidArgument)
	}
	result := make([]sohaapi.SystemIntegrationConfigurationField, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		key := strings.ToLower(strings.TrimSpace(field.Key))
		if !validKey(key) || len(field.Value) > 8192 {
			return nil, fmt.Errorf("%w: invalid system integration configuration", apperrors.ErrInvalidArgument)
		}
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("%w: duplicate configuration key %s", apperrors.ErrInvalidArgument, key)
		}
		seen[key] = struct{}{}
		result = append(result, sohaapi.SystemIntegrationConfigurationField{Key: key, Value: strings.TrimSpace(field.Value)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result, nil
}

func normalizeKeys(keys []string) ([]string, error) {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(keys))
	for _, value := range keys {
		key := strings.ToLower(strings.TrimSpace(value))
		if !validKey(key) {
			return nil, fmt.Errorf("%w: invalid credential key", apperrors.ErrInvalidArgument)
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	return result, nil
}

func validateProviderConfiguration(category, providerType string, enabled bool, fields []sohaapi.SystemIntegrationConfigurationField, credentials map[string]struct{}) error {
	if category != domain.CategorySourceControl || providerType != domain.ProviderGitLab {
		return nil
	}
	config := configurationMap(fields)
	baseURL := strings.TrimRight(config["base_url"], "/")
	parsed, err := url.Parse(baseURL)
	if baseURL == "" || err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("%w: gitlab base_url must be an HTTP(S) URL", apperrors.ErrInvalidArgument)
	}
	if value := config["per_page"]; value != "" {
		parsedValue, err := strconv.Atoi(value)
		if err != nil || parsedValue < 1 || parsedValue > 200 {
			return fmt.Errorf("%w: gitlab per_page must be between 1 and 200", apperrors.ErrInvalidArgument)
		}
	}
	if enabled {
		if _, ok := credentials["token"]; !ok {
			return fmt.Errorf("%w: gitlab token is required when enabled", apperrors.ErrInvalidArgument)
		}
	}
	return nil
}

func configurationMap(fields []sohaapi.SystemIntegrationConfigurationField) map[string]string {
	result := make(map[string]string, len(fields))
	for _, field := range fields {
		result[field.Key] = field.Value
	}
	return result
}

func credentialKeySet(values map[string]string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for key := range values {
		result[key] = struct{}{}
	}
	return result
}

func validKey(value string) bool {
	return value != "" && len(value) <= 128 && keyPattern.MatchString(value)
}

func validCategory(value string) bool {
	switch value {
	case "identity", "project_management", "source_control", "configuration", "ci_cd", "code_quality", "api_gateway", "monitoring", "messaging", "ai", "cloud", "other":
		return true
	default:
		return false
	}
}

func sourceCapabilities() []string { return []string{"repositories", "branches", "tags", "files"} }

func redactProviderError(err error) string {
	if err == nil {
		return ""
	}
	return "connection test failed"
}
