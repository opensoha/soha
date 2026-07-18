package agentharness

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ProviderPermissionAuthorizer interface {
	Authorize(context.Context, domainidentity.Principal, string) error
}

type ProviderCatalogObserver interface {
	ApplyProviderCatalog(ProviderCatalog)
}

type ProviderPersistedState struct {
	Catalog          *ProviderCatalog
	Acknowledgements []RegistryAcknowledgement
}

// ProviderStateStore persists the last-known-good catalog and the bounded
// runner acknowledgement set owned by the provider control plane.
type ProviderStateStore interface {
	LoadProviderState(context.Context, int) (ProviderPersistedState, error)
	SaveProviderCatalog(context.Context, ProviderCatalog) error
	SaveRegistryAcknowledgement(context.Context, RegistryAcknowledgement, int) error
}

type ProviderControlPlaneOption func(*ProviderControlPlane)

func WithProviderCatalogObserver(observer ProviderCatalogObserver) ProviderControlPlaneOption {
	return func(service *ProviderControlPlane) {
		service.observer = observer
	}
}

func WithProviderStateStore(store ProviderStateStore) ProviderControlPlaneOption {
	return func(service *ProviderControlPlane) {
		service.store = store
	}
}

func WithProviderAcknowledgementLimit(limit int) ProviderControlPlaneOption {
	return func(service *ProviderControlPlane) {
		if limit > 0 {
			service.ackLimit = limit
		}
	}
}

type ProviderRuntimeStatus struct {
	CatalogRevision  uint64                    `json:"catalogRevision"`
	CatalogDigest    string                    `json:"catalogDigest"`
	RunnerCount      int                       `json:"runnerCount"`
	Acknowledgements []RegistryAcknowledgement `json:"acknowledgements"`
}

// ProviderControlPlane reconciles the plugin extension registry into a
// monotonic in-process catalog and retains each runner's newest acknowledgement.
type ProviderControlPlane struct {
	mu          sync.RWMutex
	reconciler  *ProviderReconciler
	permissions ProviderPermissionAuthorizer
	observer    ProviderCatalogObserver
	store       ProviderStateStore
	catalog     ProviderCatalog
	acks        map[string]RegistryAcknowledgement
	ackLimit    int
	started     bool
	now         func() time.Time
}

const defaultProviderAcknowledgementLimit = 1000

func NewProviderControlPlane(reconciler *ProviderReconciler, permissions ProviderPermissionAuthorizer, options ...ProviderControlPlaneOption) (*ProviderControlPlane, error) {
	if reconciler == nil {
		return nil, fmt.Errorf("agent provider reconciler is required")
	}
	if permissions == nil {
		return nil, fmt.Errorf("agent provider permission authorizer is required")
	}
	emptyDigest, err := providerDefinitionsDigest([]ProviderDefinition{})
	if err != nil {
		return nil, fmt.Errorf("initialize agent provider catalog: %w", err)
	}
	now := time.Now().UTC()
	service := &ProviderControlPlane{
		reconciler:  reconciler,
		permissions: permissions,
		catalog: ProviderCatalog{
			SchemaVersion: ProviderCatalogSchemaVersion,
			Revision:      1,
			Digest:        emptyDigest,
			CreatedAt:     now,
			Providers:     []ProviderDefinition{},
		},
		acks:     map[string]RegistryAcknowledgement{},
		ackLimit: defaultProviderAcknowledgementLimit,
		now:      time.Now,
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	if service.store != nil {
		state, err := service.store.LoadProviderState(context.Background(), service.ackLimit)
		if err != nil {
			return nil, fmt.Errorf("load agent provider state: %w", err)
		}
		if state.Catalog != nil {
			if err := validatePersistedProviderCatalog(*state.Catalog); err != nil {
				return nil, fmt.Errorf("load agent provider catalog: %w", err)
			}
			service.catalog = cloneProviderCatalog(*state.Catalog)
		}
		for _, ack := range state.Acknowledgements {
			if ack.RunnerID == "" || ack.Revision == 0 || ack.Revision > service.catalog.Revision {
				continue
			}
			current, exists := service.acks[ack.RunnerID]
			if !exists || acknowledgementIsNewer(ack, current) {
				service.acks[ack.RunnerID] = cloneRegistryAcknowledgement(ack)
			}
		}
		service.pruneAcknowledgementsLocked()
	}
	if err := service.reconcileLocked(); err != nil {
		return nil, err
	}
	if service.store != nil {
		if err := service.store.SaveProviderCatalog(context.Background(), service.catalog); err != nil {
			return nil, fmt.Errorf("persist initial agent provider catalog: %w", err)
		}
	}
	if service.observer != nil {
		service.observer.ApplyProviderCatalog(cloneProviderCatalog(service.catalog))
	}
	service.started = true
	return service, nil
}

func (s *ProviderControlPlane) Catalog(ctx context.Context, principal domainidentity.Principal) (ProviderCatalog, error) {
	if err := s.permissions.Authorize(ctx, principal, appaccess.PermAIAgentProvidersView); err != nil {
		return ProviderCatalog{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reconcileLocked(); err != nil {
		return ProviderCatalog{}, err
	}
	return cloneProviderCatalog(s.catalog), nil
}

func (s *ProviderControlPlane) RegistrySnapshot(runnerID string) (ProviderRegistrySnapshot, error) {
	runnerID = strings.TrimSpace(runnerID)
	if runnerID == "" || len(runnerID) > 160 {
		return ProviderRegistrySnapshot{}, fmt.Errorf("%w: runnerId is required and must be at most 160 characters", apperrors.ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reconcileLocked(); err != nil {
		return ProviderRegistrySnapshot{}, err
	}
	return ProjectRegistrySnapshot(s.catalog, FleetTarget{}, s.catalog.CreatedAt)
}

func (s *ProviderControlPlane) Acknowledge(input RegistryAcknowledgement) (RegistryAcknowledgement, error) {
	input, err := prepareRegistryAcknowledgement(input, s.now)
	if err != nil {
		return RegistryAcknowledgement{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reconcileLocked(); err != nil {
		return RegistryAcknowledgement{}, err
	}
	if input.Revision > s.catalog.Revision {
		return RegistryAcknowledgement{}, fmt.Errorf("%w: acknowledgement revision exceeds current catalog revision", apperrors.ErrConflict)
	}
	current, exists := s.acks[input.RunnerID]
	if exists && !acknowledgementIsNewer(input, current) {
		return cloneRegistryAcknowledgement(current), nil
	}
	input = cloneRegistryAcknowledgement(input)
	if s.store != nil {
		if err := s.store.SaveRegistryAcknowledgement(context.Background(), input, s.ackLimit); err != nil {
			return RegistryAcknowledgement{}, fmt.Errorf("persist agent provider acknowledgement: %w", err)
		}
	}
	s.acks[input.RunnerID] = input
	s.pruneAcknowledgementsLocked()
	return cloneRegistryAcknowledgement(input), nil
}

func prepareRegistryAcknowledgement(input RegistryAcknowledgement, now func() time.Time) (RegistryAcknowledgement, error) {
	input.RunnerID = strings.TrimSpace(input.RunnerID)
	input.Reason = strings.TrimSpace(input.Reason)
	input.RolloutState = strings.TrimSpace(input.RolloutState)
	if err := validateRegistryAcknowledgementHeader(input); err != nil {
		return RegistryAcknowledgement{}, err
	}
	if err := normalizeConformanceChecks(input.ConformanceChecks); err != nil {
		return RegistryAcknowledgement{}, err
	}
	if err := normalizeProviderStatuses(input.ProviderStatuses); err != nil {
		return RegistryAcknowledgement{}, err
	}
	if input.ObservedAt.IsZero() {
		input.ObservedAt = now().UTC()
	} else {
		input.ObservedAt = input.ObservedAt.UTC()
	}
	return input, nil
}

func validateRegistryAcknowledgementHeader(input RegistryAcknowledgement) error {
	if input.RunnerID == "" || len(input.RunnerID) > 160 || input.Revision == 0 {
		return fmt.Errorf("%w: runnerId and revision are required", apperrors.ErrInvalidArgument)
	}
	if len(input.Reason) > 2048 {
		return fmt.Errorf("%w: acknowledgement reason is too long", apperrors.ErrInvalidArgument)
	}
	if len(input.ProviderStatuses) > 256 {
		return fmt.Errorf("%w: too many provider runtime statuses", apperrors.ErrInvalidArgument)
	}
	if len(input.ConformanceChecks) > 256 {
		return fmt.Errorf("%w: too many provider conformance checks", apperrors.ErrInvalidArgument)
	}
	if len(input.RolloutState) > 64 {
		return fmt.Errorf("%w: rollout state is too long", apperrors.ErrInvalidArgument)
	}
	return nil
}

func normalizeConformanceChecks(checks []ProviderConformanceResult) error {
	for i := range checks {
		check := &checks[i]
		check.ProviderID = strings.TrimSpace(check.ProviderID)
		check.Status = strings.TrimSpace(check.Status)
		check.Reason = strings.TrimSpace(check.Reason)
		if check.ProviderID == "" || len(check.ProviderID) > 160 || (check.Status != "passed" && check.Status != "failed") || len(check.Reason) > 2048 {
			return fmt.Errorf("%w: invalid provider conformance check", apperrors.ErrInvalidArgument)
		}
	}
	return nil
}

func normalizeProviderStatuses(statuses []RunnerProviderStatus) error {
	for i := range statuses {
		status := &statuses[i]
		status.ProviderID = strings.TrimSpace(status.ProviderID)
		status.ProviderVersion = strings.TrimSpace(status.ProviderVersion)
		status.Health = strings.TrimSpace(status.Health)
		status.Reason = strings.TrimSpace(status.Reason)
		if status.ProviderID == "" || len(status.ProviderID) > 160 || len(status.Reason) > 2048 || status.ActiveRuns < 0 {
			return fmt.Errorf("%w: invalid provider runtime status", apperrors.ErrInvalidArgument)
		}
		switch status.Health {
		case "unknown", "healthy", "unhealthy":
		default:
			return fmt.Errorf("%w: invalid provider health", apperrors.ErrInvalidArgument)
		}
		status.ObservedAt = status.ObservedAt.UTC()
	}
	return nil
}

func (s *ProviderControlPlane) RuntimeStatus(ctx context.Context, principal domainidentity.Principal) (ProviderRuntimeStatus, error) {
	if err := s.permissions.Authorize(ctx, principal, appaccess.PermAIAgentProvidersView); err != nil {
		return ProviderRuntimeStatus{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.reconcileLocked(); err != nil {
		return ProviderRuntimeStatus{}, err
	}
	acks := make([]RegistryAcknowledgement, 0, len(s.acks))
	for _, ack := range s.acks {
		acks = append(acks, cloneRegistryAcknowledgement(ack))
	}
	sortAcknowledgements(acks)
	return ProviderRuntimeStatus{
		CatalogRevision:  s.catalog.Revision,
		CatalogDigest:    s.catalog.Digest,
		RunnerCount:      len(acks),
		Acknowledgements: acks,
	}, nil
}

func cloneRegistryAcknowledgement(input RegistryAcknowledgement) RegistryAcknowledgement {
	out := input
	out.ProviderStatuses = append([]RunnerProviderStatus(nil), input.ProviderStatuses...)
	out.ConformanceChecks = append([]ProviderConformanceResult(nil), input.ConformanceChecks...)
	return out
}

func (s *ProviderControlPlane) reconcileLocked() error {
	next, changed, err := s.reconciler.Reconcile(s.catalog, s.now().UTC())
	if err != nil {
		// The extension registry is the desired-state source, but an invalid
		// update must not displace the last-known-good catalog served to runners.
		return nil
	}
	if changed && s.store != nil && s.started {
		if err := s.store.SaveProviderCatalog(context.Background(), next); err != nil {
			return fmt.Errorf("persist agent provider catalog: %w", err)
		}
	}
	s.catalog = cloneProviderCatalog(next)
	if changed && s.observer != nil && s.started {
		s.observer.ApplyProviderCatalog(cloneProviderCatalog(next))
	}
	return nil
}

func validatePersistedProviderCatalog(catalog ProviderCatalog) error {
	if catalog.SchemaVersion != ProviderCatalogSchemaVersion || catalog.Revision == 0 || catalog.CreatedAt.IsZero() {
		return fmt.Errorf("persisted catalog metadata is invalid")
	}
	if err := validateProviderDefinitions(catalog.Providers); err != nil {
		return err
	}
	digest, err := providerDefinitionsDigest(catalog.Providers)
	if err != nil {
		return err
	}
	if digest != catalog.Digest {
		return fmt.Errorf("persisted catalog digest does not match providers")
	}
	return nil
}

func acknowledgementIsNewer(candidate, current RegistryAcknowledgement) bool {
	if candidate.Revision != current.Revision {
		return candidate.Revision > current.Revision
	}
	return candidate.ObservedAt.After(current.ObservedAt)
}

func (s *ProviderControlPlane) pruneAcknowledgementsLocked() {
	for len(s.acks) > s.ackLimit {
		var oldestID string
		var oldest RegistryAcknowledgement
		for runnerID, ack := range s.acks {
			if oldestID == "" || ack.ObservedAt.Before(oldest.ObservedAt) || (ack.ObservedAt.Equal(oldest.ObservedAt) && runnerID > oldestID) {
				oldestID, oldest = runnerID, ack
			}
		}
		delete(s.acks, oldestID)
	}
}

func cloneProviderCatalog(input ProviderCatalog) ProviderCatalog {
	out := input
	out.Providers = make([]ProviderDefinition, len(input.Providers))
	for i, provider := range input.Providers {
		out.Providers[i] = provider
		out.Providers[i].Runtime.Args = append([]string(nil), provider.Runtime.Args...)
		out.Providers[i].Capabilities = append([]string(nil), provider.Capabilities...)
		out.Providers[i].RequiredGatewayCapabilities = append([]string(nil), provider.RequiredGatewayCapabilities...)
		out.Providers[i].RequiredScopes = append([]string(nil), provider.RequiredScopes...)
		out.Providers[i].SecretRefs = append([]string(nil), provider.SecretRefs...)
	}
	return out
}

func sortAcknowledgements(items []RegistryAcknowledgement) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].RunnerID < items[j-1].RunnerID; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}
