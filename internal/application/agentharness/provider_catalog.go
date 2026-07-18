package agentharness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
)

const ProviderCatalogSchemaVersion = "opensoha.dev/agent-provider-catalog/v1"

type ProviderCatalog struct {
	SchemaVersion string               `json:"schemaVersion"`
	Revision      uint64               `json:"revision"`
	Digest        string               `json:"digest"`
	CreatedAt     time.Time            `json:"createdAt"`
	Providers     []ProviderDefinition `json:"providers"`
}

type ProviderDefinition struct {
	SchemaVersion               string            `json:"schemaVersion"`
	ID                          string            `json:"id"`
	Kind                        string            `json:"kind"`
	DisplayName                 string            `json:"displayName"`
	PluginID                    string            `json:"pluginId"`
	PluginVersion               string            `json:"pluginVersion"`
	ProviderVersion             string            `json:"providerVersion"`
	AdapterProtocol             string            `json:"adapterProtocol"`
	Runtime                     RuntimeDefinition `json:"runtime"`
	Capabilities                []string          `json:"capabilities"`
	RequiredGatewayCapabilities []string          `json:"requiredGatewayCapabilities,omitempty"`
	RequiredScopes              []string          `json:"requiredScopes,omitempty"`
	SecretRefs                  []string          `json:"secretRefs,omitempty"`
	Draining                    bool              `json:"draining,omitempty"`
}

type RuntimeDefinition struct {
	Kind             string   `json:"kind"`
	Command          string   `json:"command,omitempty"`
	Args             []string `json:"args,omitempty"`
	PromptArg        string   `json:"promptArg,omitempty"`
	SkillArg         string   `json:"skillArg,omitempty"`
	ProviderSkillArg string   `json:"providerSkillArg,omitempty"`
	Image            string   `json:"image,omitempty"`
	Endpoint         string   `json:"endpoint,omitempty"`
	HealthPath       string   `json:"healthPath,omitempty"`
}

type ExtensionSource interface {
	List(scope string) []domainplugin.ExtensionRecord
}

type ProviderReconciler struct {
	source ExtensionSource
}

func NewProviderReconciler(source ExtensionSource) *ProviderReconciler {
	return &ProviderReconciler{source: source}
}

func (r *ProviderReconciler) Reconcile(previous ProviderCatalog, now time.Time) (ProviderCatalog, bool, error) {
	if r == nil || r.source == nil {
		return ProviderCatalog{}, false, fmt.Errorf("agent provider extension source is required")
	}
	providers := make([]ProviderDefinition, 0)
	for _, record := range r.source.List("ai") {
		if record.Point != "ai.agentProviders" || record.Status != "enabled" || !record.Configured {
			continue
		}
		provider, err := providerDefinitionFromExtension(record)
		if err != nil {
			return previous, false, fmt.Errorf("agent provider extension %q: %w", record.ID, err)
		}
		providers = append(providers, provider)
	}
	sort.Slice(providers, func(i, j int) bool {
		if providers[i].ID == providers[j].ID {
			return providers[i].ProviderVersion < providers[j].ProviderVersion
		}
		return providers[i].ID < providers[j].ID
	})
	if err := validateProviderDefinitions(providers); err != nil {
		return previous, false, err
	}
	digest, err := providerDefinitionsDigest(providers)
	if err != nil {
		return previous, false, err
	}
	if previous.Digest == digest && previous.Revision > 0 {
		return previous, false, nil
	}
	revision := previous.Revision + 1
	if revision == 0 {
		revision = 1
	}
	return ProviderCatalog{SchemaVersion: ProviderCatalogSchemaVersion, Revision: revision, Digest: digest, CreatedAt: now.UTC(), Providers: providers}, true, nil
}

func providerDefinitionFromExtension(record domainplugin.ExtensionRecord) (ProviderDefinition, error) {
	metadata := record.Metadata
	provider := ProviderDefinition{
		SchemaVersion:               "opensoha.dev/agent-provider-definition/v1",
		ID:                          firstNonEmpty(metadataString(metadata, "providerId"), record.ID),
		Kind:                        firstNonEmpty(metadataString(metadata, "kind"), record.ID),
		DisplayName:                 firstNonEmpty(metadataString(metadata, "displayName"), record.Label, record.ID),
		PluginID:                    record.PluginID,
		PluginVersion:               record.PluginVersion,
		ProviderVersion:             firstNonEmpty(metadataString(metadata, "providerVersion"), record.PluginVersion),
		AdapterProtocol:             metadataString(metadata, "adapterProtocol"),
		Capabilities:                stringSlice(metadata["capabilities"]),
		RequiredGatewayCapabilities: stringSlice(metadata["requiredGatewayCapabilities"]),
		RequiredScopes:              stringSlice(metadata["requiredScopes"]),
		SecretRefs:                  stringSlice(metadata["secretRefs"]),
		Draining:                    metadataBool(metadata, "draining"),
	}
	runtimeMap, _ := metadata["runtime"].(map[string]any)
	provider.Runtime = RuntimeDefinition{
		Kind:             firstNonEmpty(metadataString(runtimeMap, "kind"), record.RuntimeMode),
		Command:          metadataString(runtimeMap, "command"),
		Args:             stringSlice(runtimeMap["args"]),
		PromptArg:        metadataString(runtimeMap, "promptArg"),
		SkillArg:         metadataString(runtimeMap, "skillArg"),
		ProviderSkillArg: metadataString(runtimeMap, "providerSkillArg"),
		Image:            metadataString(runtimeMap, "image"),
		Endpoint:         metadataString(runtimeMap, "endpoint"),
		HealthPath:       metadataString(runtimeMap, "healthPath"),
	}
	if err := validateProviderDefinition(provider); err != nil {
		return ProviderDefinition{}, err
	}
	return provider, nil
}

func validateProviderDefinitions(providers []ProviderDefinition) error {
	providerIDs := map[string]struct{}{}
	providerKinds := map[string]struct{}{}
	for _, provider := range providers {
		if err := validateProviderDefinition(provider); err != nil {
			return err
		}
		id := strings.ToLower(strings.TrimSpace(provider.ID))
		if _, ok := providerIDs[id]; ok {
			return fmt.Errorf("duplicate agent provider id %q", id)
		}
		providerIDs[id] = struct{}{}
		kind := strings.ToLower(strings.TrimSpace(provider.Kind))
		if _, ok := providerKinds[kind]; ok {
			return fmt.Errorf("duplicate agent provider kind %q", kind)
		}
		providerKinds[kind] = struct{}{}
	}
	return nil
}

func validateProviderDefinition(provider ProviderDefinition) error {
	if err := validateProviderIdentity(provider); err != nil {
		return err
	}
	if err := validateProviderRuntime(provider.Runtime); err != nil {
		return err
	}
	return validateProviderSecretRefs(provider.SecretRefs)
}

func validateProviderIdentity(provider ProviderDefinition) error {
	if provider.SchemaVersion != "opensoha.dev/agent-provider-definition/v1" {
		return fmt.Errorf("provider schema version is unsupported")
	}
	missingProviderIdentity := strings.TrimSpace(provider.ID) == "" || strings.TrimSpace(provider.Kind) == ""
	missingPluginIdentity := strings.TrimSpace(provider.PluginID) == "" || strings.TrimSpace(provider.PluginVersion) == ""
	if missingProviderIdentity || missingPluginIdentity || strings.TrimSpace(provider.ProviderVersion) == "" {
		return fmt.Errorf("provider and plugin identity are required")
	}
	if strings.TrimSpace(provider.AdapterProtocol) == "" {
		return fmt.Errorf("adapter protocol is required")
	}
	if len(provider.Capabilities) == 0 {
		return fmt.Errorf("provider capabilities are required")
	}
	return nil
}

func validateProviderRuntime(runtime RuntimeDefinition) error {
	switch runtime.Kind {
	case "cli":
		if strings.TrimSpace(runtime.Command) == "" {
			return fmt.Errorf("CLI provider command is required")
		}
	case "container":
		if strings.TrimSpace(runtime.Image) == "" {
			return fmt.Errorf("container provider image is required")
		}
	case "remote":
		if strings.TrimSpace(runtime.Endpoint) == "" {
			return fmt.Errorf("remote provider endpoint is required")
		}
		return validateRemoteProviderEndpoint(runtime.Endpoint)
	default:
		return fmt.Errorf("unsupported provider runtime %q", runtime.Kind)
	}
	return nil
}

func validateRemoteProviderEndpoint(rawEndpoint string) error {
	endpoint, err := url.Parse(rawEndpoint)
	invalidEndpoint := err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil
	if invalidEndpoint {
		return fmt.Errorf("remote provider endpoint must be an HTTPS URL without user info")
	}
	return nil
}

func validateProviderSecretRefs(secretRefs []string) error {
	for _, secretRef := range secretRefs {
		if !strings.HasPrefix(secretRef, "secret:") || len(secretRef) > 247 {
			return fmt.Errorf("provider secret values must use bounded secret refs")
		}
	}
	return nil
}

func providerDefinitionsDigest(providers []ProviderDefinition) (string, error) {
	data, err := json.Marshal(providers)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func metadataString(values map[string]any, key string) string {
	if value, ok := values[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func metadataBool(values map[string]any, key string) bool {
	value, _ := values[key].(bool)
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func stringSlice(value any) []string {
	var input []string
	switch typed := value.(type) {
	case []string:
		input = typed
	case []any:
		for _, item := range typed {
			if text, ok := item.(string); ok {
				input = append(input, text)
			}
		}
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(input))
	for _, item := range input {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}
