package systemintegration

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/contracts/plugins/storagedriver"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// StorageDriverRegistry is assembled before API startup and stays immutable
// while serving. A hosted driver handles process replacement behind its port.
type StorageDriverRegistry struct {
	builtin storagedriver.Driver
	plugins map[string]storageDriver
}

type storageDriver struct {
	id         string
	name       string
	schema     sohaapi.JSONSchema
	fields     map[string]storageField
	validation *jsonschema.Schema
	driver     storagedriver.Driver
}

type storageField struct {
	kind      string
	secretRef bool
}

func NewStorageDriverRegistry(builtin storagedriver.Driver) *StorageDriverRegistry {
	return &StorageDriverRegistry{builtin: builtin, plugins: make(map[string]storageDriver)}
}

// RegisterPlugin accepts the descriptor of an already verified release. B7
// supports flat string, boolean and integer form fields.
func (r *StorageDriverRegistry) RegisterPlugin(id, name string, rawSchema []byte, driver storagedriver.Driver) error {
	if !validKey(id) || id == domain.ProviderS3 || strings.TrimSpace(name) == "" || driver == nil {
		return fmt.Errorf("invalid storage driver descriptor")
	}
	if _, exists := r.plugins[id]; exists {
		return fmt.Errorf("duplicate storage driver %q", id)
	}
	descriptor, err := parseStorageSchema(rawSchema)
	if err != nil {
		return fmt.Errorf("storage driver %q: %w", id, err)
	}
	descriptor.id, descriptor.name, descriptor.driver = id, name, driver
	r.plugins[id] = descriptor
	return nil
}

func parseStorageSchema(raw []byte) (storageDriver, error) {
	invalid := fmt.Errorf("invalid standard storage configuration schema")
	var fields sohaapi.JSONSchema
	if len(raw) > 64<<10 || json.Unmarshal(raw, &fields) != nil ||
		fields["type"] != "object" || fields["additionalProperties"] != false ||
		fields["x-soha-test-connection-action"] != "storage.testConnection" {
		return storageDriver{}, invalid
	}
	properties, ok := fields["properties"].(map[string]any)
	if !ok || len(properties) == 0 || len(properties) > 32 {
		return storageDriver{}, invalid
	}
	result := storageDriver{schema: fields, fields: make(map[string]storageField)}
	for key, property := range properties {
		rules, ok := property.(map[string]any)
		if !ok || !validKey(key) {
			return storageDriver{}, invalid
		}
		kind, _ := rules["type"].(string)
		widget, _ := rules["x-soha-widget"].(string)
		if kind != "string" && kind != "boolean" && kind != "integer" ||
			widget != "" && (widget != "secret-ref" || kind != "string" || strings.ContainsAny(key, ".-")) {
			return storageDriver{}, invalid
		}
		result.fields[key] = storageField{kind: kind, secretRef: widget == "secret-ref"}
	}
	if result.fields["container"] != (storageField{kind: "string"}) {
		return storageDriver{}, invalid
	}
	if prefix, exists := result.fields["prefix"]; exists && prefix != (storageField{kind: "string"}) {
		return storageDriver{}, invalid
	}
	const schemaURL = "https://schemas.invalid/storage-config"
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(jsonschema.SchemeURLLoader{})
	if err := compiler.AddResource(schemaURL, map[string]any(fields)); err != nil {
		return storageDriver{}, invalid
	}
	var err error
	result.validation, err = compiler.Compile(schemaURL)
	if err != nil {
		return storageDriver{}, invalid
	}
	return result, nil
}

type storageSecrets interface {
	PinReferences(context.Context, domainidentity.Principal, map[string]string, domainsecret.Target) ([]domainsecret.Reference, error)
	ResolvePinnedReferences(context.Context, domainidentity.Principal, []domainsecret.Reference, domainsecret.Target) (map[string]string, error)
}

func (s *Service) SetStorageDrivers(registry *StorageDriverRegistry, secrets storageSecrets) {
	s.storageDrivers, s.storageSecrets = registry, secrets
}

func (s *Service) ListStorageDrivers(ctx context.Context, principal domainidentity.Principal) ([]sohaapi.StorageDriverDescriptor, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsSystemIntegrationsView); err != nil {
		return nil, err
	}
	result := make([]sohaapi.StorageDriverDescriptor, 0)
	if s.storageDrivers != nil {
		for _, plugin := range s.storageDrivers.plugins {
			result = append(result, sohaapi.StorageDriverDescriptor{ID: plugin.id, Name: plugin.name, ConfigSchema: plugin.schema})
		}
		sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	}
	return result, nil
}

func (s *Service) pluginDriver(id string) (storageDriver, error) {
	if s.storageDrivers != nil {
		if descriptor, ok := s.storageDrivers.plugins[id]; ok {
			return descriptor, nil
		}
	}
	return storageDriver{}, fmt.Errorf("%w: storage driver unavailable", apperrors.ErrUnsupportedOperation)
}

func (d storageDriver) binding(id string, fields []sohaapi.SystemIntegrationConfigurationField) (storagedriver.Binding, map[string]string, error) {
	errInvalid := fmt.Errorf("%w: invalid storage driver configuration", apperrors.ErrInvalidArgument)
	settings := make(map[string]any)
	values := make(map[string]any)
	refs := make(map[string]string)
	binding := storagedriver.Binding{ID: id}
	for _, field := range fields {
		spec, exists := d.fields[field.Key]
		if !exists {
			return storagedriver.Binding{}, nil, errInvalid
		}
		value, err := storageFieldValue(spec, field.Value)
		if err != nil {
			return storagedriver.Binding{}, nil, errInvalid
		}
		values[field.Key] = value
		switch {
		case field.Key == "container":
			binding.Container = field.Value
		case field.Key == "prefix":
			binding.Prefix = field.Value
		case spec.secretRef:
			refs[field.Key] = field.Value
		default:
			settings[field.Key] = value
		}
	}
	if d.validation.Validate(values) != nil {
		return storagedriver.Binding{}, nil, errInvalid
	}
	if binding.Prefix != "" && !strings.HasSuffix(binding.Prefix, "/") {
		binding.Prefix += "/"
	}
	binding.Settings, _ = json.Marshal(settings)
	if err := binding.Validate(); err != nil {
		return storagedriver.Binding{}, nil, errInvalid
	}
	return binding, refs, nil
}

func storageFieldValue(field storageField, input string) (any, error) {
	switch field.kind {
	case "boolean":
		if input != "true" && input != "false" {
			return nil, apperrors.ErrInvalidArgument
		}
		return input == "true", nil
	case "integer":
		return strconv.ParseInt(input, 10, 53)
	default:
		if field.secretRef && !strings.HasPrefix(input, "soha://secrets/") {
			return nil, apperrors.ErrInvalidArgument
		}
		return input, nil
	}
}

func (s *Service) storageSecretRefs(ctx context.Context, principal domainidentity.Principal, id string, refs map[string]string) ([]domainsecret.Reference, domainsecret.Target, error) {
	target := domainsecret.Target{Type: "capability", Ref: "storage.driver." + id}
	if len(refs) == 0 {
		return nil, target, nil
	}
	if s.storageSecrets == nil {
		return nil, target, apperrors.ErrServiceUnavailable
	}
	aliases := make(map[string]string, len(refs))
	for field, ref := range refs {
		aliases[strings.ToUpper(field)] = ref
	}
	pinned, err := s.storageSecrets.PinReferences(ctx, principal, aliases, target)
	return pinned, target, err
}

// ResolveStorageDriver resolves a fresh invocation binding under the current caller.
// Internal consumers first authorize their own operation and pass an explicit connection ID.
func (s *Service) ResolveStorageDriver(ctx context.Context, principal domainidentity.Principal, id string) (storagedriver.Driver, storagedriver.Binding, error) {
	if err := s.authorize(ctx, principal, appaccess.PermSettingsSystemIntegrationsView); err != nil {
		return nil, storagedriver.Binding{}, err
	}
	if strings.TrimSpace(id) == "" {
		return nil, storagedriver.Binding{}, apperrors.ErrInvalidArgument
	}
	item, err := s.repo.Get(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, storagedriver.Binding{}, err
	}
	if item.Category != domain.CategoryStorage || !item.Enabled {
		return nil, storagedriver.Binding{}, apperrors.ErrServiceUnavailable
	}
	return s.resolveDriverBinding(ctx, principal, item)
}

func (s *Service) resolveDriverBinding(ctx context.Context, principal domainidentity.Principal, item domain.Integration) (storagedriver.Driver, storagedriver.Binding, error) {
	if item.ProviderType == domain.ProviderS3 {
		return s.resolveBuiltinStorageDriver(ctx, item)
	}
	descriptor, err := s.pluginDriver(item.ProviderType)
	if err != nil {
		return nil, storagedriver.Binding{}, err
	}
	binding, refs, err := descriptor.binding(item.ID, item.Configuration)
	if err != nil {
		return nil, storagedriver.Binding{}, err
	}
	pinned, target, err := s.storageSecretRefs(ctx, principal, descriptor.id, refs)
	if err == nil && len(pinned) != 0 {
		var credentials map[string]string
		credentials, err = s.storageSecrets.ResolvePinnedReferences(ctx, principal, pinned, target)
		binding.Credentials = make(map[string]string, len(refs))
		for field := range refs {
			value, exists := credentials[strings.ToUpper(field)]
			if !exists && err == nil {
				err = apperrors.ErrServiceUnavailable
			}
			binding.Credentials[field] = value
		}
	}
	if err != nil {
		return nil, storagedriver.Binding{}, err
	}
	if err := binding.Validate(); err != nil {
		return nil, storagedriver.Binding{}, apperrors.ErrInvalidArgument
	}
	return descriptor.driver, binding, nil
}

func (s *Service) resolveBuiltinStorageDriver(ctx context.Context, item domain.Integration) (storagedriver.Driver, storagedriver.Binding, error) {
	if s.storageDrivers == nil || s.storageDrivers.builtin == nil {
		return nil, storagedriver.Binding{}, apperrors.ErrUnsupportedOperation
	}
	config := configurationMap(item.Configuration)
	binding := storagedriver.Binding{ID: item.ID, Container: config["bucket"]}
	if prefix := strings.Trim(config["prefix"], "/"); prefix != "" {
		binding.Prefix = prefix + "/"
	}
	delete(config, "bucket")
	delete(config, "prefix")
	binding.Settings, _ = json.Marshal(config)
	var err error
	binding.Credentials, err = s.decryptCredentials(ctx, item.ID)
	if err != nil {
		return nil, storagedriver.Binding{}, err
	}
	if binding.Validate() != nil {
		return nil, storagedriver.Binding{}, apperrors.ErrInvalidArgument
	}
	return s.storageDrivers.builtin, binding, nil
}

func (s *Service) validateIntegrationProvider(ctx context.Context, principal domainidentity.Principal, item domain.Integration, credentials map[string]struct{}) error {
	if item.Category != domain.CategoryStorage || item.ProviderType == domain.ProviderS3 {
		return validateProviderConfiguration(item.Category, item.ProviderType, item.Enabled, item.Configuration, credentials)
	}
	if len(credentials) != 0 {
		return fmt.Errorf("%w: use secret references for plugin storage credentials", apperrors.ErrInvalidArgument)
	}
	driver, err := s.pluginDriver(item.ProviderType)
	if err != nil {
		return fmt.Errorf("%w: unknown storage driver", apperrors.ErrInvalidArgument)
	}
	_, refs, err := driver.binding(item.ID, item.Configuration)
	if err != nil {
		return err
	}
	_, _, err = s.storageSecretRefs(ctx, principal, driver.id, refs)
	return err
}

type driverConnectionTest struct {
	driver  storagedriver.Driver
	binding storagedriver.Binding
}

func (t driverConnectionTest) TestConnection(ctx context.Context) error {
	return t.driver.TestConnection(ctx, t.binding)
}
