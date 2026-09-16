package resource

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"sigs.k8s.io/yaml"
)

// ResolveCreateTargets performs no dry-run or writes. Gateway uses the actual
// manifest targets, including every namespace, before evaluating its policy.
func (s *ResourceCreation) ResolveCreateTargets(ctx context.Context, principal domainidentity.Principal, clusterID string, request domainresource.ResourceCreateRequest) ([]domainresource.ResourceCreateRef, error) {
	request, err := normalizeResourceCreateRequest(request)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeResourceCreationEntry(ctx, principal, request.Source); err != nil {
		return nil, err
	}
	_, manifests, err := s.resolveCreateManifests(ctx, clusterID, request.Content)
	if err != nil {
		return nil, err
	}
	if isScopedResourceCreateSource(request.Source) && len(manifests) != 1 {
		return nil, apperrors.ErrInvalidArgument
	}
	if err := resolveCreateManifestTargets(manifests, request); err != nil {
		return nil, err
	}
	refs := make([]domainresource.ResourceCreateRef, len(manifests))
	for i, manifest := range manifests {
		if err := s.authorizeCreateRef(ctx, principal, clusterID, manifest.Ref); err != nil {
			return nil, err
		}
		refs[i] = manifest.Ref
	}
	return refs, nil
}

func (s *ResourceCreation) authorizeCreateRef(ctx context.Context, principal domainidentity.Principal, clusterID string, ref domainresource.ResourceCreateRef) error {
	group, version, qualified := strings.Cut(ref.APIVersion, "/")
	if !qualified {
		group, version = "", ref.APIVersion
	}
	if version == "" || strings.Contains(version, "/") {
		return apperrors.ErrAccessDenied
	}
	// Select only an existing product permission family. Unknown group/kind
	// combinations remain extensions; this name is never used for API routing.
	resource := strings.ToLower(ref.Kind)
	switch {
	case strings.HasSuffix(resource, "s"):
		resource += "es"
	case strings.HasSuffix(resource, "y"):
		resource = strings.TrimSuffix(resource, "y") + "ies"
	default:
		resource += "s"
	}
	family := resourceGroupForResolvedResource(group, resource, ref.Kind)
	_, _, err := s.authorizeResourceGroup(ctx, principal, clusterID, ref.Namespace, family, ref.Kind, domainaccess.ActionCreate)
	return err
}

func (s *ResourceCreation) authorizeCreateBatch(ctx context.Context, principal domainidentity.Principal, clusterID string, batch domainresource.ResourceCreateBatch) error {
	if principal.UserID == "" || batch.ActorID != principal.UserID || batch.ClusterID != clusterID {
		return apperrors.ErrNotFound
	}
	for _, document := range batch.Documents {
		if err := s.authorizeCreateRef(ctx, principal, clusterID, document.Resource); err != nil {
			return err
		}
	}
	return nil
}

func (s *ResourceCreation) GetCreate(ctx context.Context, principal domainidentity.Principal, clusterID, operationID string) (domainresource.ResourceCreateExecution, error) {
	if s.batches == nil {
		return domainresource.ResourceCreateExecution{}, apperrors.ErrUnsupportedOperation
	}
	batch, err := s.batches.Get(ctx, strings.TrimSpace(operationID))
	if err != nil {
		return domainresource.ResourceCreateExecution{}, err
	}
	if err := s.authorizeCreateBatch(ctx, principal, clusterID, batch); err != nil {
		return domainresource.ResourceCreateExecution{}, err
	}
	return executionFromResourceCreateBatch(batch), nil
}

// FindCreate recovers the exact original receipt without rerunning preflight
// (which rejects now-existing objects), creating resources, or taking a lease.
func (s *ResourceCreation) FindCreate(ctx context.Context, principal domainidentity.Principal, clusterID string, request domainresource.ResourceCreateRequest) (domainresource.ResourceCreateExecution, error) {
	if s.batches == nil {
		return domainresource.ResourceCreateExecution{}, apperrors.ErrUnsupportedOperation
	}
	request, err := normalizeResourceCreateRequest(request)
	if err != nil {
		return domainresource.ResourceCreateExecution{}, err
	}
	if request.RequestID == "" {
		return domainresource.ResourceCreateExecution{}, apperrors.ErrInvalidArgument
	}
	if err := s.authorizeResourceCreationEntry(ctx, principal, request.Source); err != nil {
		return domainresource.ResourceCreateExecution{}, err
	}
	item, found, err := s.findExistingCreateBatch(ctx, principal, clusterID, request)
	if err == nil && !found {
		err = apperrors.ErrNotFound
	}
	return item, err
}

// AssessCreate proves only that all original objects still exist. Workload
// readiness, endpoints and application health need their own assessments.
func (s *ResourceCreation) AssessCreate(ctx context.Context, principal domainidentity.Principal, clusterID, operationID string) (sohaapi.CapabilityAssessment, error) {
	item, err := s.GetCreate(ctx, principal, clusterID, operationID)
	result := sohaapi.CapabilityAssessment{Verdict: "inconclusive", Summary: "Original resource creation is not confirmed", Evidence: []sohaapi.CapabilityEvidence{}}
	if err != nil {
		return result, err
	}
	if item.Status != "succeeded" || len(item.Documents) == 0 || s.yaml == nil {
		return result, nil
	}
	result.Verdict = "satisfied"
	result.Summary = "Every original Kubernetes object is present; workload health and reachability are not assessed"
	for _, document := range item.Documents {
		ref := document.Resource
		evidence := sohaapi.CapabilityEvidence{Kind: "k8s.resource.identity", Source: "kubernetes.api", ObservedAt: time.Now().UTC(), Incomplete: true,
			Summary: "Original UID or current observation unavailable", Resource: &sohaapi.CapabilityResourceRef{Kind: "k8s.resource", ID: ref.Name, Version: ref.UID, Scope: map[string]string{"clusterId": clusterID, "namespace": ref.Namespace, "resourceKind": ref.Kind}}}
		verdict := sohaapi.CapabilityAssessmentVerdict("inconclusive")
		if ref.UID != "" {
			current, readErr := s.yaml.GetResourceYAML(ctx, principal, clusterID, ref.Namespace, ref.Kind, ref.Name)
			if readErr == nil {
				verdict, evidence.Summary = assessCreatedIdentity(ref, current.Content)
				evidence.Incomplete = verdict == "inconclusive"
			}
		}
		evidence.ObservedAt = time.Now().UTC()
		result.Evidence = append(result.Evidence, evidence)
		if verdict == "unsatisfied" || (verdict == "inconclusive" && result.Verdict != "unsatisfied") {
			result.Verdict = verdict
		}
	}
	if result.Verdict != "satisfied" {
		result.Summary = "One or more original objects could not be verified; inspect the original receipt"
	}
	return result, nil
}

func assessCreatedIdentity(ref domainresource.ResourceCreateRef, content string) (sohaapi.CapabilityAssessmentVerdict, string) {
	var object struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name              string `json:"name"`
			Namespace         string `json:"namespace"`
			UID               string `json:"uid"`
			DeletionTimestamp string `json:"deletionTimestamp"`
		} `json:"metadata"`
	}
	if yaml.Unmarshal([]byte(content), &object) != nil || object.Metadata.UID == "" {
		return "inconclusive", "Current object identity unavailable"
	}
	if object.APIVersion != ref.APIVersion || object.Kind != ref.Kind || object.Metadata.Name != ref.Name || object.Metadata.Namespace != ref.Namespace {
		return "inconclusive", "Observed object does not match the original target"
	}
	if object.Metadata.UID != ref.UID || object.Metadata.DeletionTimestamp != "" {
		return "unsatisfied", "The original object was replaced or is being deleted"
	}
	return "satisfied", fmt.Sprintf("Original %s object UID is present", ref.Kind)
}

// Only the original create response can establish ownership of the result.
func resourceCreateResponseUID(content string) string {
	var metadata struct {
		Metadata struct {
			UID string `json:"uid"`
		} `json:"metadata"`
	}
	if yaml.Unmarshal([]byte(content), &metadata) != nil {
		return ""
	}
	return metadata.Metadata.UID
}
