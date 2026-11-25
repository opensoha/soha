package delivery

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	cr "github.com/opensoha/soha/contracts/resource"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	dc "github.com/opensoha/soha/internal/domain/catalog"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	di "github.com/opensoha/soha/internal/domain/identity"
	dr "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"golang.org/x/sync/errgroup"
)

type deliveryWorkloadPodReader interface {
	GetDeliveryWorkloadPods(context.Context, di.Principal, dr.ResourceSourceSnapshot, cr.RelationConfig) ([]dr.PodView, error)
}

const deliveryPodProjectionLimit = 200

var projectedPodUID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func (s *Service) GetApplicationServicePods(ctx context.Context, principal di.Principal, applicationID, bindingID, serviceID string) (dd.ApplicationServicePods, error) {
	applicationID, bindingID, serviceID = strings.TrimSpace(applicationID), strings.TrimSpace(bindingID), strings.TrimSpace(serviceID)
	service, err := s.applications.GetService(ctx, principal, applicationID, serviceID)
	if err != nil {
		return dd.ApplicationServicePods{}, err
	}
	binding, err := s.catalog.GetApplicationEnvironment(ctx, principal, bindingID)
	if err != nil {
		return dd.ApplicationServicePods{}, err
	}
	if binding.ID != bindingID || binding.ApplicationID != applicationID || service.ApplicationID != applicationID || service.ID != serviceID {
		return dd.ApplicationServicePods{}, apperrors.ErrNotFound
	}

	result := dd.ApplicationServicePods{Status: "unconfigured", Items: []dd.ApplicationServicePod{}}
	var targets []dc.ReleaseTarget
	for _, target := range binding.Targets {
		if assignedDeliveryPodTarget(target, binding.ID, service) {
			targets = append(targets, target)
		}
	}
	if len(targets) == 0 {
		return s.unconfiguredDeliveryServicePods(ctx, binding, service.ID, result), nil
	}
	reader, ok := s.targets.(deliveryWorkloadPodReader)
	if !ok {
		result.Status, result.Reason = "unavailable", "provider_unsupported"
		return result, nil
	}
	observeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	observeCtx = s.withDeliveryObservation(observeCtx)
	items, observed, observationErr := s.observeDeliveryServicePods(observeCtx, principal, binding, targets, reader)
	if err := ctx.Err(); err != nil {
		return dd.ApplicationServicePods{}, err
	}
	result.Items = items
	result.Status, result.Reason = deliveryServicePodStatus(observed, len(items), observationErr)
	return result, nil
}

func assignedDeliveryPodTarget(target dc.ReleaseTarget, bindingID string, service domainapp.Service) bool {
	if target.ApplicationEnvironmentID != "" && target.ApplicationEnvironmentID != bindingID {
		return false
	}
	importSource := metadataString(target.Metadata, "importSource")
	if !target.Enabled && importSource != "kubernetes" && importSource != "helm" {
		return false
	}
	if id := target.ResolvedServiceID(); id != "" {
		return id == service.ID
	}
	// Older explicit targets predate service IDs. A service key is unique
	// within the application. Live labels and unnamed targets confer no scope.
	return service.Key != "" && strings.TrimSpace(target.WorkloadName) == service.Key
}

func (s *Service) unconfiguredDeliveryServicePods(ctx context.Context, binding dc.ApplicationEnvironment, serviceID string, result dd.ApplicationServicePods) dd.ApplicationServicePods {
	store, err := s.referenceActiveStore()
	if err != nil {
		return result
	}
	reference, err := store.GetServiceReference(ctx, binding.ID, serviceID)
	switch {
	case errors.Is(err, apperrors.ErrNotFound):
		return result
	case err != nil:
		result.Status, result.Reason = "unavailable", "source_unavailable"
	case reference.ConsumerBindingID == binding.ID && reference.ServiceID == serviceID && reference.ApplicationID == binding.ApplicationID:
		result.Status, result.Reason = "relation_only", "ownership_unverified"
	default:
		result.Status, result.Reason = "unavailable", "identity_mismatch"
	}
	return result
}

type deliveryPodObservation struct {
	items    []dd.ApplicationServicePod
	observed bool
	err      error
}

type deliveryPodReadKey struct {
	resource dr.ResourceRef
	targetID string
}

func (s *Service) observeDeliveryServicePods(ctx context.Context, principal di.Principal, binding dc.ApplicationEnvironment, targets []dc.ReleaseTarget, reader deliveryWorkloadPodReader) ([]dd.ApplicationServicePod, bool, error) {
	const maxTargets = 24
	var coverageErr error
	if len(targets) > maxTargets {
		targets = targets[:maxTargets]
		coverageErr = dr.ErrDeliveryPodObservationLimit
	}
	batch := deliveryPodReadBatch{results: make(map[deliveryPodReadKey]func() ([]dr.PodView, error))}
	observations := make([]deliveryPodObservation, len(targets))
	var group errgroup.Group
	group.SetLimit(4)
	for i, target := range targets {
		group.Go(func() error {
			observations[i] = s.observeDeliveryPodTarget(ctx, principal, binding, target, reader, &batch)
			return nil
		})
	}
	_ = group.Wait()
	return mergeDeliveryPodObservations(observations, errors.Join(coverageErr, ctx.Err()))
}

type deliveryPodReadBatch struct {
	mu      sync.Mutex
	results map[deliveryPodReadKey]func() ([]dr.PodView, error)
}

func (b *deliveryPodReadBatch) read(ctx context.Context, principal di.Principal, reader deliveryWorkloadPodReader, target dc.ReleaseTarget, source dr.ResourceSourceSnapshot, config cr.RelationConfig) ([]dr.PodView, error) {
	key := deliveryPodReadKey{resource: source.Resource}
	key.resource.ResourceVersion, key.resource.ScopeMode = "", ""
	if source.CRDName != "" {
		key.targetID = target.ID // A CR target can configure extra controller kinds.
	}
	b.mu.Lock()
	read, exists := b.results[key]
	if !exists {
		read = sync.OnceValues(func() ([]dr.PodView, error) {
			return reader.GetDeliveryWorkloadPods(ctx, principal, source, config)
		})
		b.results[key] = read
	}
	b.mu.Unlock()
	return read()
}

func (s *Service) observeDeliveryPodTarget(ctx context.Context, principal di.Principal, binding dc.ApplicationEnvironment, target dc.ReleaseTarget, reader deliveryWorkloadPodReader, batch *deliveryPodReadBatch) deliveryPodObservation {
	sources, config, err := s.deliveryPodSources(ctx, principal, binding, target)
	result := deliveryPodObservation{observed: len(sources) == 0 && err == nil, err: err}
	const maxSources = 24
	if len(sources) > maxSources {
		sources = sources[:maxSources]
		result.err = errors.Join(result.err, dr.ErrDeliveryPodObservationLimit)
	}
	for _, source := range sources {
		if ctx.Err() != nil {
			result.err = errors.Join(result.err, ctx.Err())
			break
		}
		pods, err := batch.read(ctx, principal, reader, target, source, config)
		result.err = errors.Join(result.err, err)
		result.observed = result.observed || err == nil
		for _, pod := range pods {
			ref := source.Resource
			if !projectedPodUID.MatchString(pod.UID) || pod.Namespace != ref.Namespace || pod.Name == "" {
				result.err = errors.Join(result.err, apperrors.ErrConflict)
				continue
			}
			result.items = append(result.items, dd.ApplicationServicePod{
				ClusterID: ref.ClusterID, TargetID: target.ID, WorkloadKind: ref.Kind, WorkloadName: ref.Name, Pod: pod,
			})
		}
	}
	return result
}

func mergeDeliveryPodObservations(observations []deliveryPodObservation, observationErr error) ([]dd.ApplicationServicePod, bool, error) {
	items := []dd.ApplicationServicePod{}
	seen := make(map[[2]string]bool)
	observed := false
	for _, result := range observations {
		observed = observed || result.observed
		observationErr = errors.Join(observationErr, result.err)
		for _, item := range result.items {
			key := [2]string{item.ClusterID, item.Pod.UID}
			if seen[key] {
				continue
			}
			if len(items) == deliveryPodProjectionLimit {
				observationErr = errors.Join(observationErr, dr.ErrDeliveryPodObservationLimit)
				break
			}
			seen[key] = true
			items = append(items, item)
		}
	}
	slices.SortFunc(items, func(a, b dd.ApplicationServicePod) int {
		return strings.Compare(a.ClusterID+"/"+a.Pod.Namespace+"/"+a.Pod.Name+"/"+a.Pod.UID, b.ClusterID+"/"+b.Pod.Namespace+"/"+b.Pod.Name+"/"+b.Pod.UID)
	})
	return items, observed, observationErr
}

func (s *Service) deliveryPodSources(ctx context.Context, principal di.Principal, binding dc.ApplicationEnvironment, target dc.ReleaseTarget) ([]dr.ResourceSourceSnapshot, cr.RelationConfig, error) {
	if target.Docker != nil || target.ExecutorKind == "docker" {
		return nil, cr.RelationConfig{}, apperrors.ErrUnsupportedOperation
	}
	switch {
	case metadataString(target.Metadata, "importSource") == "kubernetes":
		source, err := importedTargetSource(target)
		if err != nil {
			return nil, cr.RelationConfig{}, err
		}
		config, err := importedTargetRelationConfig(target)
		if err != nil {
			return nil, cr.RelationConfig{}, err
		}
		return []dr.ResourceSourceSnapshot{source}, config, nil
	case target.Helm != nil || target.WorkloadKind == "HelmRelease" || metadataString(target.Metadata, "importSource") == "helm":
		items, err := s.deliveryHelmPodSources(ctx, principal, target)
		return items, cr.RelationConfig{}, err
	case target.ExecutorKind == "manifest_ssa":
		items, err := s.deliveryManifestPodSources(ctx, principal, binding, target)
		return items, cr.RelationConfig{}, err
	}
	apiVersion := deliveryNativeControllerVersion(target.WorkloadKind)
	if apiVersion == "" || target.ClusterID == "" || target.Namespace == "" || target.WorkloadName == "" {
		return nil, cr.RelationConfig{}, dr.ErrDeliveryPodOwnershipUnverified
	}
	source := dr.ResourceSourceSnapshot{Resource: dr.ResourceRef{
		APIVersion: apiVersion, Kind: target.WorkloadKind, Name: target.WorkloadName,
		ClusterID: target.ClusterID, Namespace: target.Namespace,
	}}
	return []dr.ResourceSourceSnapshot{source}, cr.RelationConfig{}, nil
}

func deliveryNativeControllerVersion(kind string) string {
	switch kind {
	case "Deployment", "StatefulSet", "DaemonSet":
		return "apps/v1"
	case "Job", "CronJob":
		return "batch/v1"
	default:
		return ""
	}
}

func (s *Service) deliveryHelmPodSources(ctx context.Context, principal di.Principal, target dc.ReleaseTarget) ([]dr.ResourceSourceSnapshot, error) {
	reader, ok := s.targets.(helmWorkloadReader)
	if !ok {
		return nil, apperrors.ErrUnsupportedOperation
	}
	name := target.WorkloadName
	if target.Helm != nil {
		name = target.Helm.ReleaseName
	}
	sources, err := reader.GetHelmReleaseWorkloadSources(ctx, principal, target.ClusterID, target.Namespace, name)
	sources = slices.DeleteFunc(slices.Clone(sources), func(source dr.ResourceSourceSnapshot) bool {
		if validHelmRuntimeSource(source, target) {
			return false
		}
		err = errors.Join(err, apperrors.ErrConflict)
		return true
	})
	return sources, err
}

func (s *Service) deliveryManifestPodSources(ctx context.Context, principal di.Principal, binding dc.ApplicationEnvironment, target dc.ReleaseTarget) ([]dr.ResourceSourceSnapshot, error) {
	if s.manifestDelivery == nil {
		return nil, apperrors.ErrUnsupportedOperation
	}
	deployment, err := s.manifestDelivery.DeliveryDeployment(ctx, principal, binding.ApplicationID, binding.ID, target)
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil, nil // Configured manifest has yet to be applied.
	}
	if err != nil {
		return nil, err
	}
	var sources []dr.ResourceSourceSnapshot
	for _, item := range deployment.Status.Inventory {
		if item.Namespace != target.Namespace || deliveryNativeControllerVersion(item.Kind) != item.APIVersion || item.APIVersion == "" {
			continue
		}
		if item.UID == "" || item.Name == "" {
			err = errors.Join(err, apperrors.ErrConflict)
			continue
		}
		sources = append(sources, dr.ResourceSourceSnapshot{Resource: dr.ResourceRef{
			APIVersion: item.APIVersion, Kind: item.Kind, Name: item.Name, UID: item.UID,
			ClusterID: target.ClusterID, Namespace: item.Namespace,
		}})
	}
	return sources, err
}

func deliveryServicePodStatus(observed bool, count int, err error) (string, string) {
	if err == nil {
		return "available", ""
	}
	reason := deliveryPodObservationReason(err)
	if observed || count > 0 {
		return "partial", reason
	}
	if reason == "ownership_unverified" {
		return "relation_only", reason
	}
	return "unavailable", reason
}

func deliveryPodObservationReason(err error) string {
	switch {
	case errors.Is(err, apperrors.ErrAccessDenied), errors.Is(err, apperrors.ErrUnauthorized):
		return "forbidden"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, apperrors.ErrConflict), errors.Is(err, apperrors.ErrNotFound):
		return "identity_mismatch"
	case errors.Is(err, apperrors.ErrUnsupportedOperation):
		return "provider_unsupported"
	case errors.Is(err, dr.ErrDeliveryPodObservationLimit):
		return "observation_limit"
	case errors.Is(err, dr.ErrDeliveryPodOwnershipUnverified):
		return "ownership_unverified"
	default:
		return "source_unavailable"
	}
}
