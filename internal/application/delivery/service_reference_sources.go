package delivery

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type referenceHelmReader interface {
	helmWorkloadReader
	GetHelmReleaseDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.HelmReleaseDetailView, error)
}

func (s *Service) previewReferenceTarget(ctx context.Context, principal domainidentity.Principal, target domaincatalog.ReleaseTarget) (domaindelivery.ServiceReferenceTarget, error) {
	result := domaindelivery.ServiceReferenceTarget{
		TargetID: target.ID, ClusterID: target.ClusterID, Namespace: target.Namespace,
		WorkloadKind: target.WorkloadKind, WorkloadName: target.WorkloadName,
		OwnershipMode: metadataString(target.Metadata, "ownershipMode"),
		Resources:     make([]domaindelivery.ServiceReferenceResource, 0),
	}
	if target.ClusterID == "" || target.Namespace == "" || target.WorkloadName == "" {
		return result, fmt.Errorf("%w: source requires an explicit Kubernetes location", apperrors.ErrConflict)
	}
	if target.WorkloadKind == "HelmRelease" {
		return s.previewReferenceHelmTarget(ctx, principal, target, result)
	}
	if metadataString(target.Metadata, "importSource") != "kubernetes" {
		return result, fmt.Errorf("%w: source has no stable resource identity; reassociation requires review", apperrors.ErrConflict)
	}
	expected, err := importedTargetSource(target)
	if err != nil {
		return result, err
	}
	reader, ok := s.targets.(KubernetesImportSourceReader)
	if !ok {
		return result, fmt.Errorf("%w: stable source reads are unavailable", apperrors.ErrUnsupportedOperation)
	}
	current, err := reader.GetImportSource(ctx, principal, target.ClusterID, target.Namespace, target.WorkloadKind, target.WorkloadName, expected.CRDName)
	if err != nil {
		return result, err
	}
	if !sameReferenceResource(expected.Resource, current.Resource) || expected.CRDName != current.CRDName || expected.Plural != current.Plural {
		return result, fmt.Errorf("%w: source identity changed; reassociation requires review", apperrors.ErrConflict)
	}
	resource, err := referenceResourceIdentity(current)
	if err != nil {
		return result, err
	}
	result.Resources = append(result.Resources, resource)
	return result, nil
}

func (s *Service) previewReferenceHelmTarget(ctx context.Context, principal domainidentity.Principal, target domaincatalog.ReleaseTarget, result domaindelivery.ServiceReferenceTarget) (domaindelivery.ServiceReferenceTarget, error) {
	if target.TargetKind != "helm_release" || metadataString(target.Metadata, "importSource") != "helm" {
		return result, fmt.Errorf("%w: Helm source association requires identity review", apperrors.ErrConflict)
	}
	reader, ok := s.targets.(referenceHelmReader)
	if !ok {
		return result, fmt.Errorf("%w: authorized Helm source reads are unavailable", apperrors.ErrUnsupportedOperation)
	}
	if target.Helm != nil && target.Helm.ReleaseName != target.WorkloadName {
		return result, fmt.Errorf("%w: Helm source name is ambiguous", apperrors.ErrConflict)
	}
	before, err := reader.GetHelmReleaseDetail(ctx, principal, target.ClusterID, target.Namespace, target.WorkloadName)
	if err != nil {
		return result, err
	}
	revision, err := referenceHelmRevision(before, target)
	if err != nil {
		return result, err
	}
	sources, err := reader.GetHelmReleaseWorkloadSources(ctx, principal, target.ClusterID, target.Namespace, target.WorkloadName)
	if err != nil {
		return result, err
	}
	if len(sources) > maxReferenceTargets {
		return result, fmt.Errorf("%w: Helm observation exceeds the reference preview limit", apperrors.ErrUnsupportedOperation)
	}
	seen := map[string]bool{}
	for _, source := range sources {
		if !validHelmRuntimeSource(source, target) {
			return result, fmt.Errorf("%w: Helm workload identity does not belong to this source", apperrors.ErrConflict)
		}
		item, err := referenceResourceIdentity(source)
		if err != nil {
			return result, err
		}
		key := item.Resource.APIVersion + "/" + item.Resource.Kind + "/" + item.Resource.Name
		if seen[key] {
			return result, fmt.Errorf("%w: duplicate Helm workload identity", apperrors.ErrConflict)
		}
		seen[key] = true
		result.Resources = append(result.Resources, item)
	}
	after, err := reader.GetHelmReleaseDetail(ctx, principal, target.ClusterID, target.Namespace, target.WorkloadName)
	if err != nil {
		return result, err
	}
	afterRevision, err := referenceHelmRevision(after, target)
	if err != nil || revision != afterRevision {
		return result, fmt.Errorf("%w: Helm source changed during preview", apperrors.ErrConflict)
	}
	result.HelmRevision = revision
	slices.SortFunc(result.Resources, func(a, b domaindelivery.ServiceReferenceResource) int {
		return strings.Compare(a.Resource.APIVersion+"/"+a.Resource.Kind+"/"+a.Resource.Name, b.Resource.APIVersion+"/"+b.Resource.Kind+"/"+b.Resource.Name)
	})
	return result, nil
}

func referenceHelmRevision(release domainresource.HelmReleaseDetailView, target domaincatalog.ReleaseTarget) (int, error) {
	revision, err := strconv.Atoi(release.Revision)
	if err != nil || revision <= 0 || release.Name != target.WorkloadName || release.Namespace != target.Namespace || release.Status != "deployed" {
		return 0, fmt.Errorf("%w: Helm source has no stable deployed revision", apperrors.ErrConflict)
	}
	return revision, nil
}

func sameReferenceResource(expected, current domainresource.ResourceRef) bool {
	return expected.UID != "" && expected.UID == current.UID && expected.ClusterID == current.ClusterID && expected.Namespace == current.Namespace && expected.Name == current.Name && expected.APIVersion == current.APIVersion && expected.Kind == current.Kind && expected.ScopeMode == current.ScopeMode
}

func referenceResourceIdentity(source domainresource.ResourceSourceSnapshot) (domaindelivery.ServiceReferenceResource, error) {
	r := source.Resource
	if r.UID == "" || r.ClusterID == "" || r.Namespace == "" || r.Name == "" || r.APIVersion == "" || r.Kind == "" || r.ScopeMode != domainresource.ResourceScopeModeNamespace || !validReferenceDigest(source.SpecDigest) {
		return domaindelivery.ServiceReferenceResource{}, fmt.Errorf("%w: source has incomplete stable identity", apperrors.ErrConflict)
	}
	// ResourceVersion and observation/status fields are deliberately excluded:
	// ordinary status refreshes do not invalidate a reference configuration.
	r.ResourceVersion = ""
	return domaindelivery.ServiceReferenceResource{Resource: r, SpecDigest: source.SpecDigest, CRDName: source.CRDName, Plural: source.Plural}, nil
}
