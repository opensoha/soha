package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const AgentProviderRegistrySchemaVersion = "opensoha.dev/agent-provider-registry/v1"

const (
	maxAgentProviders         = 128
	defaultConformanceTimeout = 5 * time.Second
)

type AgentProviderRegistry struct {
	SchemaVersion string                    `json:"schemaVersion"`
	Revision      uint64                    `json:"revision"`
	Digest        string                    `json:"digest,omitempty"`
	IssuedAt      time.Time                 `json:"issuedAt,omitempty"`
	Providers     []AgentProviderDefinition `json:"providers"`
	FleetTarget   AgentFleetTarget          `json:"fleetTarget,omitempty"`
}

type AgentFleetTarget struct {
	Environments  []string          `json:"environments,omitempty"`
	Platforms     []string          `json:"platforms,omitempty"`
	Architectures []string          `json:"architectures,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

type AgentFleetIdentity struct {
	RunnerID     string            `json:"runnerId"`
	Environment  string            `json:"environment,omitempty"`
	Platform     string            `json:"platform"`
	Architecture string            `json:"architecture"`
	Labels       map[string]string `json:"labels,omitempty"`
}

type AgentProviderDefinition struct {
	SchemaVersion               string                   `json:"schemaVersion"`
	ID                          string                   `json:"id"`
	Kind                        string                   `json:"kind"`
	DisplayName                 string                   `json:"displayName"`
	PluginID                    string                   `json:"pluginId,omitempty"`
	PluginVersion               string                   `json:"pluginVersion,omitempty"`
	ProviderVersion             string                   `json:"providerVersion"`
	AdapterProtocol             string                   `json:"adapterProtocol"`
	Runtime                     AgentProviderRuntime     `json:"runtime"`
	DefaultCommand              AgentProviderCommandSpec `json:"-"`
	Capabilities                []string                 `json:"capabilities"`
	RequiredGatewayCapabilities []string                 `json:"requiredGatewayCapabilities,omitempty"`
	RequiredScopes              []string                 `json:"requiredScopes,omitempty"`
	SecretRefs                  []string                 `json:"secretRefs,omitempty"`
	Draining                    bool                     `json:"draining,omitempty"`
}

type AgentProviderCommandSpec struct {
	Command          string   `json:"command"`
	Args             []string `json:"args,omitempty"`
	PromptArg        string   `json:"promptArg,omitempty"`
	SkillArg         string   `json:"skillArg,omitempty"`
	ProviderSkillArg string   `json:"providerSkillArg,omitempty"`
}

type AgentProviderRuntime struct {
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

func DefaultAgentProviderRegistry() AgentProviderRegistry {
	registry := AgentProviderRegistry{
		SchemaVersion: AgentProviderRegistrySchemaVersion,
		Revision:      1,
		Providers: []AgentProviderDefinition{
			{
				SchemaVersion:   AgentProviderDefinitionSchemaVersion,
				ID:              "hermes",
				Kind:            "hermes",
				DisplayName:     "Hermes Agent",
				ProviderVersion: "builtin-v1",
				AdapterProtocol: "opensoha.agent-provider.cli/v1",
				Runtime: AgentProviderRuntime{
					Kind:             "cli",
					Command:          "hermes",
					Args:             []string{"chat", "-Q"},
					PromptArg:        "-q",
					ProviderSkillArg: "-s",
				},
				DefaultCommand: AgentProviderCommandSpec{
					Command:          "hermes",
					Args:             []string{"chat", "-Q"},
					PromptArg:        "-q",
					ProviderSkillArg: "-s",
				},
				Capabilities:   []string{"root_cause", "release_verification", "incident_handoff"},
				RequiredScopes: []string{"application", "environment", "cluster", "namespace"},
			},
		},
	}
	registry.Digest, _ = agentProviderRegistryDigest(registry)
	return registry
}

const AgentProviderDefinitionSchemaVersion = "opensoha.dev/agent-provider-definition/v1"

func validateAgentProviderRegistry(registry AgentProviderRegistry) error {
	if registry.SchemaVersion != AgentProviderRegistrySchemaVersion {
		return fmt.Errorf("unsupported provider registry schema version %q", registry.SchemaVersion)
	}
	if registry.Revision == 0 {
		return fmt.Errorf("provider registry revision is required")
	}
	if len(registry.Providers) > maxAgentProviders {
		return fmt.Errorf("provider registry exceeds the %d provider limit", maxAgentProviders)
	}
	if strings.TrimSpace(registry.Digest) != "" {
		want, err := agentProviderRegistryDigest(registry)
		if err != nil {
			return fmt.Errorf("compute provider registry digest: %w", err)
		}
		if !strings.EqualFold(strings.TrimSpace(registry.Digest), want) {
			return fmt.Errorf("provider registry digest mismatch")
		}
	}
	providerIDs := map[string]struct{}{}
	providerKinds := map[string]struct{}{}
	for _, provider := range registry.Providers {
		if err := validateAgentProviderDefinition(provider, providerIDs, providerKinds); err != nil {
			return err
		}
	}
	return nil
}

func validateAgentProviderDefinition(provider AgentProviderDefinition, providerIDs, providerKinds map[string]struct{}) error {
	if provider.SchemaVersion != AgentProviderDefinitionSchemaVersion {
		return fmt.Errorf("provider %q schema version %q is unsupported", provider.ID, provider.SchemaVersion)
	}
	id := strings.ToLower(strings.TrimSpace(provider.ID))
	kind := strings.ToLower(strings.TrimSpace(provider.Kind))
	if id == "" {
		return fmt.Errorf("provider id is required")
	}
	if kind == "" {
		return fmt.Errorf("provider %q kind is required", provider.ID)
	}
	if _, ok := providerIDs[id]; ok {
		return fmt.Errorf("duplicate provider id %q", id)
	}
	if _, ok := providerKinds[kind]; ok {
		return fmt.Errorf("duplicate provider kind %q", kind)
	}
	providerIDs[id] = struct{}{}
	providerKinds[kind] = struct{}{}
	if strings.TrimSpace(provider.ProviderVersion) == "" {
		return fmt.Errorf("provider %q version is required", provider.ID)
	}
	if strings.TrimSpace(provider.AdapterProtocol) == "" {
		return fmt.Errorf("provider %q adapter protocol is required", provider.ID)
	}
	if err := validateAgentProviderRuntime(provider); err != nil {
		return err
	}
	return validateAgentProviderBindings(provider)
}

func validateAgentProviderRuntime(provider AgentProviderDefinition) error {
	switch provider.Runtime.Kind {
	case "cli":
		if strings.TrimSpace(provider.commandSpec().Command) == "" {
			return fmt.Errorf("provider %q default command is required", provider.ID)
		}
	case "container":
		if strings.TrimSpace(provider.Runtime.Image) == "" {
			return fmt.Errorf("provider %q container image is required", provider.ID)
		}
	case "remote":
		endpoint, err := url.Parse(provider.Runtime.Endpoint)
		if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil {
			return fmt.Errorf("provider %q remote endpoint must be an HTTPS URL without user info", provider.ID)
		}
	default:
		return fmt.Errorf("provider %q runtime %q is unsupported", provider.ID, provider.Runtime.Kind)
	}
	return nil
}

func validateAgentProviderBindings(provider AgentProviderDefinition) error {
	capabilityIDs := map[string]struct{}{}
	for _, capability := range provider.Capabilities {
		capabilityID := strings.ToLower(strings.TrimSpace(capability))
		if capabilityID == "" {
			return fmt.Errorf("provider %q capability id is required", provider.ID)
		}
		if _, ok := capabilityIDs[capabilityID]; ok {
			return fmt.Errorf("provider %q duplicate capability %q", provider.ID, capabilityID)
		}
		capabilityIDs[capabilityID] = struct{}{}
	}
	for _, secretRef := range provider.SecretRefs {
		if !strings.HasPrefix(secretRef, "secret:") || len(secretRef) > 247 {
			return fmt.Errorf("provider %q secret values must use bounded secret refs", provider.ID)
		}
	}
	return nil
}

func agentProviderRegistryDigest(registry AgentProviderRegistry) (string, error) {
	registry.Digest = ""
	data, err := json.Marshal(registry)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

type AgentProviderRegistryApplyResult struct {
	Accepted          bool                             `json:"accepted"`
	Targeted          bool                             `json:"targeted"`
	Revision          uint64                           `json:"revision"`
	DesiredRevision   uint64                           `json:"desiredRevision"`
	ActiveRevision    uint64                           `json:"activeRevision"`
	LKGRevision       uint64                           `json:"lkgRevision"`
	PreviousRevision  uint64                           `json:"previousRevision,omitempty"`
	RolloutState      string                           `json:"rolloutState"`
	RolledBack        bool                             `json:"rolledBack,omitempty"`
	Reason            string                           `json:"reason,omitempty"`
	ConformanceChecks []AgentProviderConformanceResult `json:"conformanceChecks,omitempty"`
	ObservedAt        time.Time                        `json:"observedAt"`
}

type AgentProviderConformanceResult struct {
	ProviderID string `json:"providerId"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
}

type AgentProviderConformanceProbe interface {
	Check(ctx context.Context, provider AgentProviderDefinition) error
}

type AgentProviderRuntimeStatus struct {
	ProviderID      string    `json:"providerId"`
	ProviderVersion string    `json:"providerVersion"`
	CatalogRevision uint64    `json:"catalogRevision"`
	Health          string    `json:"health"`
	Draining        bool      `json:"draining"`
	ActiveRuns      int       `json:"activeRuns"`
	Reason          string    `json:"reason,omitempty"`
	ObservedAt      time.Time `json:"observedAt"`
}

// DynamicAgentProviderRegistry atomically activates validated catalog snapshots.
// Rejected snapshots never replace the last-known-good revision.
type DynamicAgentProviderRegistry struct {
	mu       sync.RWMutex
	current  AgentProviderRegistry
	previous *AgentProviderRegistry
	statuses map[string]AgentProviderRuntimeStatus
}

func NewDynamicAgentProviderRegistry(initial AgentProviderRegistry) (*DynamicAgentProviderRegistry, error) {
	if err := validateAgentProviderRegistry(initial); err != nil {
		return nil, err
	}
	registry := &DynamicAgentProviderRegistry{statuses: map[string]AgentProviderRuntimeStatus{}}
	registry.activateLocked(initial, time.Now().UTC())
	return registry, nil
}

func (r *DynamicAgentProviderRegistry) Apply(snapshot AgentProviderRegistry, now time.Time) AgentProviderRegistryApplyResult {
	return r.ApplyDesired(context.Background(), snapshot, AgentFleetIdentity{
		Platform: runtime.GOOS, Architecture: runtime.GOARCH,
	}, structuralConformanceProbe{}, defaultConformanceTimeout, now)
}

type structuralConformanceProbe struct{}

func (structuralConformanceProbe) Check(context.Context, AgentProviderDefinition) error { return nil }

func (r *DynamicAgentProviderRegistry) ApplyDesired(
	ctx context.Context,
	snapshot AgentProviderRegistry,
	identity AgentFleetIdentity,
	probe AgentProviderConformanceProbe,
	timeout time.Duration,
	now time.Time,
) AgentProviderRegistryApplyResult {
	result := AgentProviderRegistryApplyResult{
		Targeted: true, Revision: snapshot.Revision, DesiredRevision: snapshot.Revision,
		RolloutState: "rejected", ObservedAt: now.UTC(),
	}
	if err := validateAgentProviderRegistry(snapshot); err != nil {
		result.Reason = err.Error()
		r.populateRevisions(&result)
		return result
	}
	if !snapshot.FleetTarget.Matches(identity) {
		result.Accepted = true
		result.Targeted = false
		result.RolloutState = "skipped_not_targeted"
		result.Reason = "snapshot does not target this runner"
		r.populateRevisions(&result)
		return result
	}
	checks, ok := runProviderConformance(ctx, snapshot.Providers, probe, timeout)
	result.ConformanceChecks = checks
	if !ok {
		result.RolloutState = "conformance_failed"
		result.Reason = "provider conformance failed"
		r.populateRevisions(&result)
		return result
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.populateRevisionsLocked(&result)
	if snapshot.Revision == r.current.Revision && strings.EqualFold(snapshot.Digest, r.current.Digest) {
		result.Accepted = true
		result.RolloutState = "active"
		return result
	}
	if snapshot.Revision <= r.current.Revision {
		result.Reason = fmt.Sprintf("revision %d is not newer than active revision %d", snapshot.Revision, r.current.Revision)
		return result
	}
	if reason := r.activeRunTransitionBlockerLocked(snapshot); reason != "" {
		result.Reason = reason
		result.RolloutState = "blocked_draining"
		return result
	}
	previous := cloneAgentProviderRegistry(r.current)
	r.previous = &previous
	r.activateLocked(snapshot, now.UTC())
	result.Accepted = true
	result.RolloutState = "active"
	result.ActiveRevision = snapshot.Revision
	result.LKGRevision = snapshot.Revision
	result.PreviousRevision = previous.Revision
	return result
}

func (r *DynamicAgentProviderRegistry) Rollback(now time.Time) AgentProviderRegistryApplyResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := AgentProviderRegistryApplyResult{
		Targeted: true, RolloutState: "rollback_rejected", ObservedAt: now.UTC(),
	}
	r.populateRevisionsLocked(&result)
	if r.previous == nil {
		result.Reason = "no previous validated provider registry is available"
		return result
	}
	if reason := r.activeRunTransitionBlockerLocked(*r.previous); reason != "" {
		result.Reason = reason
		result.RolloutState = "blocked_draining"
		return result
	}
	rollback := cloneAgentProviderRegistry(*r.previous)
	replaced := cloneAgentProviderRegistry(r.current)
	r.previous = &replaced
	r.activateLocked(rollback, now.UTC())
	result.Accepted = true
	result.RolledBack = true
	result.Revision = rollback.Revision
	result.DesiredRevision = rollback.Revision
	result.ActiveRevision = rollback.Revision
	result.LKGRevision = rollback.Revision
	result.PreviousRevision = replaced.Revision
	result.RolloutState = "rolled_back"
	return result
}

func (r *DynamicAgentProviderRegistry) populateRevisions(result *AgentProviderRegistryApplyResult) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.populateRevisionsLocked(result)
}

func (r *DynamicAgentProviderRegistry) populateRevisionsLocked(result *AgentProviderRegistryApplyResult) {
	result.ActiveRevision = r.current.Revision
	result.LKGRevision = r.current.Revision
	if r.previous != nil {
		result.PreviousRevision = r.previous.Revision
	}
}

func (target AgentFleetTarget) Matches(identity AgentFleetIdentity) bool {
	if !normalizedListMatches(target.Environments, identity.Environment) ||
		!normalizedListMatches(target.Platforms, identity.Platform) ||
		!normalizedListMatches(target.Architectures, identity.Architecture) {
		return false
	}
	labels := normalizedLabels(identity.Labels)
	for key, value := range target.Labels {
		if labels[normalizeFleetValue(key)] != normalizeFleetValue(value) {
			return false
		}
	}
	return true
}

func normalizedListMatches(allowed []string, actual string) bool {
	if len(allowed) == 0 {
		return true
	}
	actual = normalizeFleetValue(actual)
	for _, candidate := range allowed {
		if normalizeFleetValue(candidate) == actual {
			return true
		}
	}
	return false
}

func normalizedLabels(input map[string]string) map[string]string {
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[normalizeFleetValue(key)] = normalizeFleetValue(value)
	}
	return out
}

func normalizeFleetValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func runProviderConformance(
	ctx context.Context,
	providers []AgentProviderDefinition,
	probe AgentProviderConformanceProbe,
	timeout time.Duration,
) ([]AgentProviderConformanceResult, bool) {
	if timeout <= 0 {
		timeout = defaultConformanceTimeout
	}
	results := make([]AgentProviderConformanceResult, 0, len(providers))
	if probe == nil {
		for _, provider := range providers {
			results = append(results, AgentProviderConformanceResult{
				ProviderID: provider.ID,
				Status:     "failed",
				Reason:     "conformance probe unavailable",
			})
		}
		return results, false
	}
	passed := true
	for _, provider := range providers {
		result := AgentProviderConformanceResult{ProviderID: provider.ID, Status: "passed"}
		checkCtx, cancel := context.WithTimeout(ctx, timeout)
		err := probe.Check(checkCtx, cloneAgentProviderDefinition(provider))
		cancel()
		if err != nil {
			result.Status = "failed"
			result.Reason = "conformance check failed"
			passed = false
		}
		results = append(results, result)
		if ctx.Err() != nil {
			passed = false
			break
		}
	}
	return results, passed
}

func (r *DynamicAgentProviderRegistry) activeRunTransitionBlockerLocked(snapshot AgentProviderRegistry) string {
	nextVersions := make(map[string]string, len(snapshot.Providers))
	for _, provider := range snapshot.Providers {
		nextVersions[provider.ID] = provider.ProviderVersion
	}
	for providerID, status := range r.statuses {
		if status.ActiveRuns == 0 {
			continue
		}
		if nextVersions[providerID] != status.ProviderVersion {
			return fmt.Sprintf("provider %q has %d active runs; drain before replacing version %q", providerID, status.ActiveRuns, status.ProviderVersion)
		}
	}
	return ""
}

func (r *DynamicAgentProviderRegistry) Snapshot() AgentProviderRegistry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneAgentProviderRegistry(r.current)
}

func (r *DynamicAgentProviderRegistry) Resolve(id, version string) (AgentProviderDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, provider := range r.current.Providers {
		if strings.EqualFold(provider.ID, strings.TrimSpace(id)) && (version == "" || provider.ProviderVersion == version) {
			return cloneAgentProviderDefinition(provider), true
		}
	}
	return AgentProviderDefinition{}, false
}

func (r *DynamicAgentProviderRegistry) Acquire(id, version string, revision ...uint64) (AgentProviderDefinition, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(revision) > 0 && (revision[0] == 0 || revision[0] != r.current.Revision) {
		return AgentProviderDefinition{}, fmt.Errorf("selected agent catalog revision is no longer active")
	}
	for _, provider := range r.current.Providers {
		if !strings.EqualFold(provider.ID, strings.TrimSpace(id)) || (version != "" && provider.ProviderVersion != version) {
			continue
		}
		status := r.statuses[provider.ID]
		if provider.Draining || status.Draining {
			return AgentProviderDefinition{}, fmt.Errorf("provider %q is draining", provider.ID)
		}
		if status.Health != "healthy" {
			return AgentProviderDefinition{}, fmt.Errorf("provider %q is not healthy", provider.ID)
		}
		status.ActiveRuns++
		status.ObservedAt = time.Now().UTC()
		r.statuses[provider.ID] = status
		return cloneAgentProviderDefinition(provider), nil
	}
	return AgentProviderDefinition{}, fmt.Errorf("provider %q version %q is not active", id, version)
}

func (r *DynamicAgentProviderRegistry) Release(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	status, ok := r.statuses[id]
	if !ok {
		return
	}
	if status.ActiveRuns > 0 {
		status.ActiveRuns--
	}
	status.ObservedAt = time.Now().UTC()
	r.statuses[id] = status
}

func (r *DynamicAgentProviderRegistry) SetHealth(id, health, reason string, now time.Time) error {
	if health != "healthy" && health != "degraded" && health != "unhealthy" {
		return fmt.Errorf("unsupported provider health %q", health)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	status, ok := r.statuses[id]
	if !ok {
		return fmt.Errorf("provider %q is not active", id)
	}
	status.Health = health
	status.Reason = strings.TrimSpace(reason)
	status.ObservedAt = now.UTC()
	r.statuses[id] = status
	return nil
}

func (r *DynamicAgentProviderRegistry) Statuses() []AgentProviderRuntimeStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AgentProviderRuntimeStatus, 0, len(r.statuses))
	for _, status := range r.statuses {
		out = append(out, status)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProviderID < out[j].ProviderID })
	return out
}

func (r *DynamicAgentProviderRegistry) activateLocked(snapshot AgentProviderRegistry, now time.Time) {
	previous := r.statuses
	r.current = cloneAgentProviderRegistry(snapshot)
	r.statuses = make(map[string]AgentProviderRuntimeStatus, len(snapshot.Providers))
	for _, provider := range snapshot.Providers {
		status := previous[provider.ID]
		previousVersion := status.ProviderVersion
		status.ProviderID = provider.ID
		status.ProviderVersion = provider.ProviderVersion
		status.CatalogRevision = snapshot.Revision
		status.Draining = provider.Draining
		status.ObservedAt = now
		if status.Health == "" || previousVersion != provider.ProviderVersion {
			status.Health = "healthy"
		}
		r.statuses[provider.ID] = status
	}
}

func cloneAgentProviderRegistry(input AgentProviderRegistry) AgentProviderRegistry {
	out := input
	out.Providers = make([]AgentProviderDefinition, len(input.Providers))
	for i, provider := range input.Providers {
		out.Providers[i] = cloneAgentProviderDefinition(provider)
	}
	out.FleetTarget.Environments = append([]string(nil), input.FleetTarget.Environments...)
	out.FleetTarget.Platforms = append([]string(nil), input.FleetTarget.Platforms...)
	out.FleetTarget.Architectures = append([]string(nil), input.FleetTarget.Architectures...)
	out.FleetTarget.Labels = make(map[string]string, len(input.FleetTarget.Labels))
	for key, value := range input.FleetTarget.Labels {
		out.FleetTarget.Labels[key] = value
	}
	return out
}

func cloneAgentProviderDefinition(input AgentProviderDefinition) AgentProviderDefinition {
	out := input
	out.DefaultCommand.Args = append([]string(nil), input.DefaultCommand.Args...)
	out.Runtime.Args = append([]string(nil), input.Runtime.Args...)
	out.Capabilities = append([]string(nil), input.Capabilities...)
	out.RequiredGatewayCapabilities = append([]string(nil), input.RequiredGatewayCapabilities...)
	out.RequiredScopes = append([]string(nil), input.RequiredScopes...)
	out.SecretRefs = append([]string(nil), input.SecretRefs...)
	return out
}

func (p AgentProviderDefinition) commandSpec() AgentProviderCommandSpec {
	if strings.TrimSpace(p.Runtime.Command) != "" {
		return AgentProviderCommandSpec{
			Command: p.Runtime.Command, Args: append([]string(nil), p.Runtime.Args...), PromptArg: p.Runtime.PromptArg,
			SkillArg: p.Runtime.SkillArg, ProviderSkillArg: p.Runtime.ProviderSkillArg,
		}
	}
	return p.DefaultCommand
}

func defaultAgentProviderDefinition(providerKey string) (AgentProviderDefinition, bool) {
	providerKey = strings.ToLower(strings.TrimSpace(providerKey))
	if providerKey == "" {
		return AgentProviderDefinition{}, false
	}
	for _, provider := range DefaultAgentProviderRegistry().Providers {
		if strings.EqualFold(provider.ID, providerKey) || strings.EqualFold(provider.Kind, providerKey) {
			return provider, true
		}
	}
	return AgentProviderDefinition{}, false
}

func providerCapabilityIDs(provider AgentProviderDefinition) []string {
	out := make([]string, 0, len(provider.Capabilities))
	for _, capability := range provider.Capabilities {
		if capabilityID := strings.TrimSpace(capability); capabilityID != "" {
			out = append(out, capabilityID)
		}
	}
	sort.Strings(out)
	return out
}
