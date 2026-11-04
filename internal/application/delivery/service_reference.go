package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"golang.org/x/sync/errgroup"
)

const maxReferenceTargets = 100

type referenceContext struct {
	consumer            domaincatalog.ApplicationEnvironment
	source              domaincatalog.ApplicationEnvironment
	service             domainapp.Service
	consumerEnvironment domaincatalog.Environment
	sourceEnvironment   domaincatalog.Environment
}

// PreviewEnvironmentServiceReference never creates targets/references or grants
// write control. A future save must repeat authorization and transactional CAS.
func (s *Service) PreviewEnvironmentServiceReference(ctx context.Context, principal domainidentity.Principal, consumerID string, input domaindelivery.ServiceReferencePreviewInput) (domaindelivery.ServiceReferencePreview, error) {
	preview, _, err := s.previewReferenceWithContext(ctx, principal, consumerID, input)
	return preview, err
}

func (s *Service) previewReferenceWithContext(ctx context.Context, principal domainidentity.Principal, consumerID string, input domaindelivery.ServiceReferencePreviewInput) (domaindelivery.ServiceReferencePreview, referenceContext, error) {
	return s.previewReferenceWithAccess(ctx, principal, consumerID, input, true)
}

func (s *Service) previewReferenceWithAccess(ctx context.Context, principal domainidentity.Principal, consumerID string, input domaindelivery.ServiceReferencePreviewInput, requireUpdate bool) (domaindelivery.ServiceReferencePreview, referenceContext, error) {
	var empty domaindelivery.ServiceReferencePreview
	var emptyContext referenceContext
	if err := validateReferencePreviewInput(consumerID, input); err != nil {
		return empty, emptyContext, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	state, err := s.loadReferenceContextWithAccess(ctx, principal, consumerID, input, requireUpdate)
	if err != nil {
		return empty, emptyContext, err
	}
	before, err := referenceContextSnapshot(state)
	if err != nil {
		return empty, emptyContext, err
	}
	targets, err := referenceServiceTargets(state.source, input.ServiceID)
	if err != nil {
		return empty, emptyContext, err
	}
	if err := validateReferenceTargetSelection(targets, input.SourceTargetIDs); err != nil {
		return empty, emptyContext, err
	}
	result := domaindelivery.ServiceReferencePreview{
		ApplicationID: state.consumer.ApplicationID, ServiceID: input.ServiceID,
		Consumer:              referenceScope(state.consumer, state.consumerEnvironment),
		Source:                referenceScope(state.source, state.sourceEnvironment),
		Targets:               make([]domaindelivery.ServiceReferenceTarget, len(targets)),
		CurrentLocalTargetIDs: make([]string, 0),
	}
	for _, target := range state.consumer.Targets {
		if target.ResolvedServiceID() == input.ServiceID {
			result.CurrentLocalTargetIDs = append(result.CurrentLocalTargetIDs, target.ID)
		}
	}
	if len(result.CurrentLocalTargetIDs) > maxReferenceTargets {
		return empty, emptyContext, fmt.Errorf("%w: local target group exceeds preview limit", apperrors.ErrUnsupportedOperation)
	}
	slices.Sort(result.CurrentLocalTargetIDs)
	result.Targets, err = s.observeReferenceTargets(ctx, principal, targets)
	if err != nil {
		return empty, emptyContext, err
	}
	// A preview is not an atomic cluster snapshot, but must not return a DB
	// configuration that changed while its bounded live observations ran.
	current, err := s.loadReferenceContextWithAccess(ctx, principal, consumerID, input, requireUpdate)
	if err != nil {
		return empty, emptyContext, err
	}
	after, err := referenceContextSnapshot(current)
	if err != nil {
		return empty, emptyContext, err
	}
	if string(before) != string(after) {
		return empty, emptyContext, fmt.Errorf("%w: reference configuration changed during preview", apperrors.ErrConflict)
	}
	result.SourceRevision, err = referenceSourceRevision(state, targets, result.Targets)
	if err != nil {
		return empty, emptyContext, err
	}
	if input.ExpectedSourceRevision != "" && input.ExpectedSourceRevision != result.SourceRevision {
		return empty, emptyContext, fmt.Errorf("%w: source changed; review a fresh preview", apperrors.ErrConflict)
	}
	result.ObservedAt = time.Now().UTC()
	return result, current, nil
}

func validateReferencePreviewInput(consumerID string, input domaindelivery.ServiceReferencePreviewInput) error {
	for _, id := range []string{consumerID, input.ServiceID, input.SourceApplicationEnvironmentID} {
		if strings.TrimSpace(id) == "" || strings.TrimSpace(id) != id || len(id) > 200 {
			return fmt.Errorf("%w: explicit binding and service IDs are required", apperrors.ErrInvalidArgument)
		}
	}
	if consumerID == input.SourceApplicationEnvironmentID || input.ExpectedConsumerUpdatedAt.IsZero() || input.ExpectedSourceUpdatedAt.IsZero() {
		return fmt.Errorf("%w: distinct bindings and both expected revisions are required", apperrors.ErrInvalidArgument)
	}
	if input.SourceTargetIDs != nil && (len(input.SourceTargetIDs) == 0 || len(input.SourceTargetIDs) > maxReferenceTargets) {
		return fmt.Errorf("%w: sourceTargetIds must contain 1 to 100 IDs", apperrors.ErrInvalidArgument)
	}
	if input.ExpectedSourceRevision != "" && !validReferenceDigest(input.ExpectedSourceRevision) {
		return fmt.Errorf("%w: expectedSourceRevision must be a lowercase SHA-256", apperrors.ErrInvalidArgument)
	}
	return nil
}

func (s *Service) loadReferenceContext(ctx context.Context, principal domainidentity.Principal, consumerID string, input domaindelivery.ServiceReferencePreviewInput) (referenceContext, error) {
	return s.loadReferenceContextWithAccess(ctx, principal, consumerID, input, true)
}

func (s *Service) loadReferenceContextWithAccess(ctx context.Context, principal domainidentity.Principal, consumerID string, input domaindelivery.ServiceReferencePreviewInput, requireUpdate bool) (referenceContext, error) {
	var state referenceContext
	var err error
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationEnvView); err != nil {
		return state, err
	}
	state.consumer, err = s.catalog.GetApplicationEnvironment(ctx, principal, consumerID)
	if err != nil {
		return state, err
	}
	if requireUpdate {
		permission := appaccess.ManagedActionPermission(appaccess.PermDeliveryApplicationEnvManage, "update")
		if _, err = s.catalog.AuthorizeApplicationEnvironmentPermission(ctx, principal, consumerID, permission); err != nil {
			return state, err
		}
	}
	state.source, err = s.catalog.GetApplicationEnvironment(ctx, principal, input.SourceApplicationEnvironmentID)
	if err != nil {
		return state, err
	}
	if state.consumer.ID != consumerID || state.source.ID != input.SourceApplicationEnvironmentID || state.consumer.ApplicationID == "" || state.source.ApplicationID != state.consumer.ApplicationID {
		return state, fmt.Errorf("%w: source must belong to the same application", apperrors.ErrConflict)
	}
	if !state.consumer.UpdatedAt.Equal(input.ExpectedConsumerUpdatedAt) || !state.source.UpdatedAt.Equal(input.ExpectedSourceUpdatedAt) {
		return state, fmt.Errorf("%w: application environment changed; reload before preview", apperrors.ErrConflict)
	}
	state.service, err = s.authorizeReferenceServiceWithAccess(ctx, principal, state.consumer.ApplicationID, input.ServiceID, requireUpdate)
	if err != nil {
		return state, err
	}
	state.consumerEnvironment, state.sourceEnvironment, err = s.referenceEnvironments(ctx, principal, state.consumer.EnvironmentID, state.source.EnvironmentID)
	return state, err
}

func (s *Service) authorizeReferenceService(ctx context.Context, principal domainidentity.Principal, applicationID, serviceID string) (domainapp.Service, error) {
	return s.authorizeReferenceServiceWithAccess(ctx, principal, applicationID, serviceID, true)
}

func (s *Service) authorizeReferenceServiceWithAccess(ctx context.Context, principal domainidentity.Principal, applicationID, serviceID string, requireUpdate bool) (domainapp.Service, error) {
	if _, err := s.applications.Get(ctx, principal, applicationID); err != nil {
		return domainapp.Service{}, err
	}
	if requireUpdate {
		apps, ok := s.applications.(importApplicationAuthorizer)
		if !ok {
			return domainapp.Service{}, fmt.Errorf("%w: service association authorization is unavailable", apperrors.ErrUnsupportedOperation)
		}
		if err := apps.AuthorizeServiceImport(ctx, principal, applicationID, domainapp.UpsertInput{}, []domainapp.ServiceInput{{ID: serviceID}}); err != nil {
			return domainapp.Service{}, err
		}
	}
	services, err := s.applications.ListServices(ctx, principal, applicationID)
	if err != nil {
		return domainapp.Service{}, err
	}
	for _, service := range services {
		if service.ID == serviceID && service.ApplicationID == applicationID && service.Enabled {
			return service, nil
		}
	}
	return domainapp.Service{}, fmt.Errorf("%w: selected service is not available in this application", apperrors.ErrConflict)
}

func (s *Service) referenceEnvironments(ctx context.Context, principal domainidentity.Principal, consumerEnvironmentID, sourceEnvironmentID string) (domaincatalog.Environment, domaincatalog.Environment, error) {
	var consumer, source domaincatalog.Environment
	environments, err := s.catalog.ListEnvironments(ctx, principal)
	if err != nil {
		return consumer, source, err
	}
	for _, environment := range environments {
		if environment.ID == consumerEnvironmentID {
			consumer = environment
		}
		if environment.ID == sourceEnvironmentID {
			source = environment
		}
	}
	if !validReferenceEnvironment(consumer) || !validReferenceEnvironment(source) || consumer.Tier != source.Tier || consumer.IsProduction != source.IsProduction || consumer.StageLevel != source.StageLevel {
		return consumer, source, fmt.Errorf("%w: source and consumer require matching, enabled and consistent environment classifications", apperrors.ErrConflict)
	}
	return consumer, source, nil
}

func validReferenceEnvironment(environment domaincatalog.Environment) bool {
	if environment.ID == "" || environment.Key == "" || !environment.Enabled || environment.StageLevel <= 0 || strings.TrimSpace(environment.Tier) == "" || strings.TrimSpace(environment.Tier) != environment.Tier {
		return false
	}
	switch environment.Tier {
	case "dev", "test", "pre":
		return !environment.IsProduction
	case "prod", "local-prod":
		return environment.IsProduction && environment.RequiresApproval
	default:
		return !environment.IsProduction || environment.RequiresApproval
	}
}

func referenceScope(binding domaincatalog.ApplicationEnvironment, environment domaincatalog.Environment) domaindelivery.ServiceReferenceScope {
	return domaindelivery.ServiceReferenceScope{ApplicationEnvironmentID: binding.ID, EnvironmentID: environment.ID, EnvironmentKey: environment.Key, Tier: environment.Tier, StageLevel: environment.StageLevel, IsProduction: environment.IsProduction, UpdatedAt: binding.UpdatedAt}
}

func referenceServiceTargets(binding domaincatalog.ApplicationEnvironment, serviceID string) ([]domaincatalog.ReleaseTarget, error) {
	targets := make([]domaincatalog.ReleaseTarget, 0)
	seen := map[string]bool{}
	for _, target := range binding.Targets {
		if target.ResolvedServiceID() != serviceID {
			continue
		}
		if _, err := target.Input().WithCanonicalServiceID(); err != nil {
			return nil, fmt.Errorf("%w: source service identity requires review", apperrors.ErrConflict)
		}
		// Observe-only imports deliberately have execution disabled. This flag
		// must not turn an observational association into publication control.
		mode := metadataString(target.Metadata, "ownershipMode")
		if mode != observeOnlyOwnershipMode && (mode != managedOwnershipMode || !target.Enabled) {
			return nil, fmt.Errorf("%w: source publication control requires review", apperrors.ErrConflict)
		}
		if target.ID == "" || seen[target.ID] || target.ApplicationEnvironmentID != binding.ID {
			return nil, fmt.Errorf("%w: source target group is disabled or ambiguous", apperrors.ErrConflict)
		}
		seen[target.ID] = true
		targets = append(targets, target)
	}
	if len(targets) == 0 || len(targets) > maxReferenceTargets {
		return nil, fmt.Errorf("%w: source requires a complete local group of 1 to 100 targets", apperrors.ErrConflict)
	}
	slices.SortFunc(targets, func(a, b domaincatalog.ReleaseTarget) int { return strings.Compare(a.ID, b.ID) })
	return targets, nil
}

func validateReferenceTargetSelection(targets []domaincatalog.ReleaseTarget, selected []string) error {
	if selected == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, id := range selected {
		if id == "" || len(id) > 200 || strings.TrimSpace(id) != id || seen[id] {
			return fmt.Errorf("%w: source target IDs must be distinct", apperrors.ErrInvalidArgument)
		}
		seen[id] = true
	}
	if len(seen) != len(targets) {
		return fmt.Errorf("%w: select the complete source service target group", apperrors.ErrConflict)
	}
	for _, target := range targets {
		if !seen[target.ID] {
			return fmt.Errorf("%w: selected targets do not match the complete source group", apperrors.ErrConflict)
		}
	}
	return nil
}

func referenceContextSnapshot(state referenceContext) ([]byte, error) {
	// Includes membership/control and environment policy changes, even if a legacy
	// writer failed to advance a parent timestamp. No context leaves this method.
	value := struct {
		Consumer            domaincatalog.ApplicationEnvironment
		Source              domaincatalog.ApplicationEnvironment
		Service             domainapp.Service
		ConsumerEnvironment domaincatalog.Environment
		SourceEnvironment   domaincatalog.Environment
	}{Consumer: state.consumer, Source: state.source, Service: state.service, ConsumerEnvironment: state.consumerEnvironment, SourceEnvironment: state.sourceEnvironment}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode reference configuration: %w", err)
	}
	return raw, nil
}

func referenceSourceRevision(state referenceContext, targets []domaincatalog.ReleaseTarget, observations []domaindelivery.ServiceReferenceTarget) (string, error) {
	// Only non-secret identities and durable revision times enter a public digest.
	type targetRevision struct {
		TargetID                                              string
		UpdatedAt                                             time.Time
		ExecutorKind, ConfigRef, GroupKey, WaveKey, RegionKey string
	}
	revisions := make([]targetRevision, 0, len(targets))
	for _, target := range targets {
		revisions = append(revisions, targetRevision{TargetID: target.ID, UpdatedAt: target.UpdatedAt, ExecutorKind: target.ExecutorKind, ConfigRef: target.ConfigRef, GroupKey: target.GroupKey, WaveKey: target.WaveKey, RegionKey: target.RegionKey})
	}
	value := struct {
		ApplicationID, ServiceID string
		ServiceVersion           int64
		ServiceUpdatedAt         time.Time
		Source                   domaindelivery.ServiceReferenceScope
		EnvironmentUpdatedAt     time.Time
		RequiresApproval         bool
		Targets                  []targetRevision
		Observations             []domaindelivery.ServiceReferenceTarget
	}{ApplicationID: state.source.ApplicationID, ServiceID: state.service.ID, ServiceVersion: state.service.Version,
		ServiceUpdatedAt: state.service.UpdatedAt, Source: referenceScope(state.source, state.sourceEnvironment),
		EnvironmentUpdatedAt: state.sourceEnvironment.UpdatedAt, RequiresApproval: state.sourceEnvironment.RequiresApproval || state.source.ReleasePolicy.RequiresApproval,
		Targets: revisions, Observations: observations}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode reference revision: %w", err)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func validReferenceDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && strings.ToLower(value) == value
}

// observeReferenceTargets shares the existing bounded, authorized runtime reader.
func (s *Service) observeReferenceTargets(ctx context.Context, principal domainidentity.Principal, targets []domaincatalog.ReleaseTarget) ([]domaindelivery.ServiceReferenceTarget, error) {
	result := make([]domaindelivery.ServiceReferenceTarget, len(targets))
	group, queryCtx := errgroup.WithContext(ctx)
	group.SetLimit(4)
	for i, target := range targets {
		group.Go(func() error {
			var err error
			result[i], err = s.previewReferenceTarget(queryCtx, principal, target)
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	return result, nil
}

// observeSourceReferenceGroup checks the physical source independently of a
// consumer's visibility. It grants no publication or consumer read authority.
func (s *Service) observeSourceReferenceGroup(ctx context.Context, principal domainidentity.Principal, sourceID, serviceID string) (domaindelivery.ServiceReferencePreview, error) {
	var empty domaindelivery.ServiceReferencePreview
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	state, err := s.loadSourceReferenceContext(ctx, principal, sourceID, serviceID)
	if err != nil {
		return empty, err
	}
	before, err := referenceContextSnapshot(state)
	if err != nil {
		return empty, err
	}
	targets, err := referenceServiceTargets(state.source, serviceID)
	if err != nil {
		return empty, err
	}
	observed, err := s.observeReferenceTargets(ctx, principal, targets)
	if err != nil {
		return empty, err
	}
	current, err := s.loadSourceReferenceContext(ctx, principal, sourceID, serviceID)
	if err != nil {
		return empty, err
	}
	after, err := referenceContextSnapshot(current)
	if err != nil {
		return empty, err
	}
	if string(before) != string(after) {
		return empty, fmt.Errorf("%w: source configuration changed during observation", apperrors.ErrConflict)
	}
	revision, err := referenceSourceRevision(state, targets, observed)
	if err != nil {
		return empty, err
	}
	return domaindelivery.ServiceReferencePreview{ApplicationID: state.source.ApplicationID, ServiceID: serviceID,
		Consumer: referenceScope(state.source, state.sourceEnvironment), Source: referenceScope(state.source, state.sourceEnvironment),
		Targets: observed, CurrentLocalTargetIDs: []string{}, SourceRevision: revision, ObservedAt: time.Now().UTC()}, nil
}

func (s *Service) loadSourceReferenceContext(ctx context.Context, principal domainidentity.Principal, sourceID, serviceID string) (referenceContext, error) {
	var state referenceContext
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationEnvView); err != nil {
		return state, err
	}
	source, err := s.catalog.GetApplicationEnvironment(ctx, principal, sourceID)
	if err != nil {
		return state, err
	}
	if source.ID != sourceID || source.ApplicationID == "" {
		return state, fmt.Errorf("%w: source scope changed", apperrors.ErrConflict)
	}
	state.source, state.consumer = source, source
	state.service, err = s.authorizeReferenceServiceWithAccess(ctx, principal, source.ApplicationID, serviceID, false)
	if err != nil {
		return state, err
	}
	state.consumerEnvironment, state.sourceEnvironment, err = s.referenceEnvironments(ctx, principal, source.EnvironmentID, source.EnvironmentID)
	return state, err
}
