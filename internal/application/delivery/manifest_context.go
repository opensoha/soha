package delivery

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type manifestControlValidator interface {
	ValidateDeliveryControl(context.Context, domainidentity.Principal, domainmanifest.DeliverySnapshot) error
}

func (s *Service) validateManifestApprovalControl(ctx context.Context, principal domainidentity.Principal, plan domaindelivery.DeliveryPlan) error {
	if len(plan.ManifestSnapshots) == 0 {
		return nil
	}
	validator, ok := s.manifestDelivery.(manifestControlValidator)
	if !ok {
		return apperrors.ErrUnsupportedOperation
	}
	for _, snapshot := range plan.ManifestSnapshots {
		if err := validator.ValidateDeliveryControl(ctx, principal, snapshot); err != nil {
			return err
		}
	}
	return nil
}

// CurrentManifestDeliveryContext is a read-only policy projection. Publishing
// and approval retain their separate authorization at their owning entry points.
func (s *Service) CurrentManifestDeliveryContext(ctx context.Context, principal domainidentity.Principal, applicationID, environmentID, targetID string) (domainmanifest.DeliveryContext, error) {
	var empty domainmanifest.DeliveryContext
	if s.applications == nil || s.catalog == nil {
		return empty, apperrors.ErrUnsupportedOperation
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := s.applications.Get(ctx, principal, applicationID); err != nil {
		return empty, err
	}
	binding, err := s.catalog.GetApplicationEnvironment(ctx, principal, environmentID)
	if err != nil {
		return empty, err
	}
	if binding.ApplicationID != applicationID || targetID == "" {
		return empty, apperrors.ErrConflict
	}
	index := slices.IndexFunc(binding.Targets, func(target domaincatalog.ReleaseTarget) bool { return target.ID == targetID })
	if index < 0 {
		return empty, fmt.Errorf("%w: publication target is no longer associated", apperrors.ErrConflict)
	}
	target := binding.Targets[index]
	if err := validateManifestPublicationTarget(binding, target); err != nil {
		return empty, err
	}
	environment, err := s.manifestContextEnvironment(ctx, principal, binding.EnvironmentID)
	if err != nil {
		return empty, err
	}
	group, err := manifestContextTargetGroup(binding, target)
	if err != nil {
		return empty, err
	}
	serviceVersion, err := s.manifestContextServiceVersion(ctx, principal, applicationID, target.ResolvedServiceID())
	if err != nil {
		return empty, err
	}
	targetRevision, err := target.ConfigurationRevision()
	if err != nil {
		return empty, fmt.Errorf("%w: publication target identity requires review", apperrors.ErrConflict)
	}
	scopeRevision, err := domainmanifest.DeliveryContextDigest(struct {
		ApplicationID, BindingID, EnvironmentID string
		Environment                             domaincatalog.Environment
		ReleasePolicy                           domaincatalog.ReleasePolicy
		Strategy, Promotion, Artifact           string
		ServiceVersion                          int64
		Targets                                 []domaincatalog.ReleaseTargetInput
	}{applicationID, binding.ID, binding.EnvironmentID, environment, binding.ReleasePolicy, binding.StrategyProfileID, binding.PromotionPolicyID, binding.ArtifactPolicyID, serviceVersion, group})
	if err != nil {
		return empty, fmt.Errorf("encode publication policy: %w", err)
	}
	return domainmanifest.DeliveryContext{Version: 1, TargetID: target.ID, TargetRevision: targetRevision, ScopeRevision: scopeRevision}, nil
}

func validateManifestPublicationTarget(binding domaincatalog.ApplicationEnvironment, target domaincatalog.ReleaseTarget) error {
	// Missing legacy metadata is allowed; malformed control declarations are
	// never interpreted as the default publication authority.
	for _, key := range []string{"ownershipMode", "importSource"} {
		if value, present := target.Metadata[key]; present {
			if _, valid := value.(string); !valid {
				return fmt.Errorf("%w: invalid publication control metadata", apperrors.ErrConflict)
			}
		}
	}
	mode := metadataString(target.Metadata, "ownershipMode")
	if target.ApplicationEnvironmentID != binding.ID || !target.Enabled || target.ExecutorKind != "manifest_ssa" || target.ConfigRef == "" || target.ClusterID == "" || target.Namespace == "" || mode == observeOnlyOwnershipMode || mode != "" && mode != managedOwnershipMode {
		return fmt.Errorf("%w: target no longer grants Manifest publication control", apperrors.ErrConflict)
	}
	if source := metadataString(target.Metadata, "importSource"); source != "" && (source != "kubernetes" || target.WorkloadKind != "CustomResource" || mode != managedOwnershipMode) {
		return fmt.Errorf("%w: publish through the authoritative target controller", apperrors.ErrConflict)
	}
	return nil
}

func (s *Service) manifestContextEnvironment(ctx context.Context, principal domainidentity.Principal, id string) (domaincatalog.Environment, error) {
	environments, err := s.catalog.ListEnvironments(ctx, principal)
	if err != nil {
		return domaincatalog.Environment{}, err
	}
	for _, environment := range environments {
		if environment.ID == id && environment.Enabled {
			// Display ordering, names and timestamps are not publication policy.
			environment.Name, environment.Key = "", ""
			environment.SortOrder = 0
			environment.CreatedAt, environment.UpdatedAt = time.Time{}, time.Time{}
			return environment, nil
		}
	}
	return domaincatalog.Environment{}, fmt.Errorf("%w: publication environment is unavailable", apperrors.ErrConflict)
}

func manifestContextTargetGroup(binding domaincatalog.ApplicationEnvironment, selected domaincatalog.ReleaseTarget) ([]domaincatalog.ReleaseTargetInput, error) {
	group := make([]domaincatalog.ReleaseTargetInput, 0)
	for _, target := range binding.Targets {
		if target.ID != selected.ID && target.ConfigRef == selected.ConfigRef && target.ExecutorKind == "manifest_ssa" {
			return nil, fmt.Errorf("%w: Manifest binding has ambiguous publication targets", apperrors.ErrConflict)
		}
		if target.ID != selected.ID && (selected.ResolvedServiceID() == "" || target.ResolvedServiceID() != selected.ResolvedServiceID()) {
			continue
		}
		input, err := target.Input().WithCanonicalServiceID()
		if err != nil || target.ApplicationEnvironmentID != binding.ID || target.ID == "" {
			return nil, apperrors.ErrConflict
		}
		group = append(group, input)
	}
	if len(group) == 0 || len(group) > maxReferenceTargets {
		return nil, apperrors.ErrConflict
	}
	slices.SortFunc(group, func(a, b domaincatalog.ReleaseTargetInput) int { return strings.Compare(a.ID, b.ID) })
	return group, nil
}

func (s *Service) manifestContextServiceVersion(ctx context.Context, principal domainidentity.Principal, applicationID, serviceID string) (int64, error) {
	if serviceID == "" {
		return 0, nil
	}
	services, err := s.applications.ListServices(ctx, principal, applicationID)
	if err != nil {
		return 0, err
	}
	for _, service := range services {
		if service.ID == serviceID && service.ApplicationID == applicationID && service.Enabled {
			return service.Version, nil
		}
	}
	return 0, fmt.Errorf("%w: publication service is unavailable", apperrors.ErrConflict)
}
