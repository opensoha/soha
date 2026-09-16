package delivery

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/helmrelease"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

type HelmChartLoader interface {
	LoadChart(context.Context, sohaapi.DeploymentTemplateHelmSource, map[string]string) (sohaapi.HelmChartInspection, []byte, error)
}

type HelmDeliveryRuntime interface {
	PrepareHelmDelivery(context.Context, domainidentity.Principal, sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskPayload, error)
	ExecuteHelmDelivery(context.Context, domainidentity.Principal, sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskResult, error)
	AuthorizeHelmDelivery(context.Context, domainidentity.Principal, sohaapi.HelmDeliverySnapshot, bool) (string, error)
}

type HelmSecretResolver interface {
	PinReferences(context.Context, domainidentity.Principal, map[string]string, domainsecret.Target) ([]domainsecret.Reference, error)
	ResolvePinnedReferences(context.Context, domainidentity.Principal, []domainsecret.Reference, domainsecret.Target) (map[string]string, error)
}

type HelmDeliveryDependencies struct {
	Charts  HelmChartLoader
	Runtime HelmDeliveryRuntime
	Secrets HelmSecretResolver
	Keys    keyring.Ring
}

type helmPreparation struct {
	Payload    sohaapi.HelmExecutionTaskPayload `json:"payload"`
	References []domainsecret.Reference         `json:"references"`
}

func (s *Service) SetHelmDelivery(dependencies HelmDeliveryDependencies) {
	s.helm = dependencies
}

func (s *Service) InspectApplicationHelmChart(ctx context.Context, principal domainidentity.Principal, applicationID string, input sohaapi.HelmChartInspectionInput) (sohaapi.HelmChartInspection, error) {
	if err := s.authorizeRuntimeScope(ctx, principal, applicationID, input.ApplicationEnvironmentID); err != nil {
		return sohaapi.HelmChartInspection{}, err
	}
	binding, err := s.catalog.GetApplicationEnvironment(ctx, principal, input.ApplicationEnvironmentID)
	if err != nil {
		return sohaapi.HelmChartInspection{}, err
	}
	if s.helm.Runtime == nil {
		return sohaapi.HelmChartInspection{}, fmt.Errorf("%w: Helm delivery is unavailable", apperrors.ErrClusterUnready)
	}
	if _, err := s.helm.Runtime.AuthorizeHelmDelivery(ctx, principal, sohaapi.HelmDeliverySnapshot{ClusterID: binding.ClusterID, Namespace: binding.Namespace}, false); err != nil {
		return sohaapi.HelmChartInspection{}, err
	}
	inspection, _, _, err := s.loadHelmDeliveryChart(ctx, principal, applicationID, input.Source)
	return inspection, err
}

func (s *Service) loadHelmDeliveryChart(ctx context.Context, principal domainidentity.Principal, applicationID string, source sohaapi.DeploymentTemplateHelmSource) (sohaapi.HelmChartInspection, []byte, []domainsecret.Reference, error) {
	if s.helm.Charts == nil {
		return sohaapi.HelmChartInspection{}, nil, nil, fmt.Errorf("%w: Helm chart loader is unavailable", apperrors.ErrClusterUnready)
	}
	if err := helmrelease.ValidateSource(source); err != nil {
		return sohaapi.HelmChartInspection{}, nil, nil, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	refs := map[string]string{}
	if source.SecretRefs != nil {
		refs = *source.SecretRefs
	}
	pinned, credentials, err := s.resolveHelmReferences(ctx, principal, applicationID, refs)
	if err != nil {
		return sohaapi.HelmChartInspection{}, nil, nil, err
	}
	inspection, archive, err := s.helm.Charts.LoadChart(ctx, source, credentials)
	return inspection, archive, pinned, err
}

func (s *Service) prepareHelmDelivery(ctx context.Context, principal domainidentity.Principal, binding domaincatalog.ApplicationEnvironment, targets []domaincatalog.ReleaseTarget, input domaindelivery.DeliveryPlanInput) ([]sohaapi.HelmDeliverySnapshot, string, error) {
	if input.HelmRevision != 0 && input.Action != domaindelivery.ApplicationDeliveryActionDeploy {
		return nil, "", fmt.Errorf("%w: Helm rollback requires a deploy plan", apperrors.ErrInvalidArgument)
	}
	if input.Action == domaindelivery.ApplicationDeliveryActionBuild {
		return nil, "", nil
	}
	if input.HelmRevision < 0 || input.HelmRevision > 0 && len(targets) != 1 {
		return nil, "", fmt.Errorf("%w: helmRevision requires one Helm target", apperrors.ErrInvalidArgument)
	}
	if input.HelmRevision > 0 && input.ReleaseBundleID != "" && input.FrozenHelmCiphertext == "" {
		return nil, "", fmt.Errorf("%w: Helm rollback cannot override historical artifacts", apperrors.ErrInvalidArgument)
	}
	snapshots := []sohaapi.HelmDeliverySnapshot{}
	prepared := map[string]helmPreparation{}
	for _, target := range targets {
		if target.Helm == nil {
			continue
		}
		if input.Action != domaindelivery.ApplicationDeliveryActionDeploy || s.helm.Runtime == nil {
			return nil, "", fmt.Errorf("%w: Helm targets require an available runtime and a deploy plan", apperrors.ErrInvalidArgument)
		}
		if err := s.authorizeApplicationDeliveryAction(ctx, principal, binding.ID, input.Action); err != nil {
			return nil, "", err
		}
		item, err := s.prepareHelmTarget(ctx, principal, binding, target, input)
		if err != nil {
			return nil, "", err
		}
		prepared[target.ID] = item
		snapshots = append(snapshots, item.Payload.Snapshot)
	}
	if len(snapshots) == 0 {
		if input.HelmRevision > 0 {
			return nil, "", fmt.Errorf("%w: helmRevision requires a configured Helm target", apperrors.ErrInvalidArgument)
		}
		return nil, "", nil
	}
	encoded, err := json.Marshal(prepared)
	if err != nil {
		return nil, "", err
	}
	ciphertext, err := secretcrypto.EncryptStringWithKeyring(s.helm.Keys, string(encoded))
	return snapshots, ciphertext, err
}

func (s *Service) prepareHelmTarget(ctx context.Context, principal domainidentity.Principal, binding domaincatalog.ApplicationEnvironment, target domaincatalog.ReleaseTarget, input domaindelivery.DeliveryPlanInput) (helmPreparation, error) {
	var result helmPreparation
	if err := helmrelease.ValidateConfiguration(*target.Helm); err != nil {
		return result, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	serviceID, _ := target.Metadata["serviceId"].(string)
	service, err := s.helmTargetService(ctx, principal, binding.ApplicationID, serviceID)
	if err != nil {
		return result, err
	}
	digest, err := s.deliveryConfigurationDigest(ctx, principal, service, binding, nil, &target)
	if err != nil {
		return result, err
	}
	snapshot := sohaapi.HelmDeliverySnapshot{ApplicationID: binding.ApplicationID, ApplicationEnvironmentID: binding.ID, ServiceID: service.ID, ServiceVersion: service.Version,
		DeliveryPlanID: input.ID, TargetID: target.ID, ClusterID: target.ClusterID, Namespace: target.Namespace, ReleaseName: target.Helm.ReleaseName,
		ConfigurationDigest: digest, RollbackRevision: input.HelmRevision, ReleaseBundleID: input.ReleaseBundleID, TimeoutSeconds: target.Helm.TimeoutSeconds}
	if snapshot.TimeoutSeconds == 0 {
		snapshot.TimeoutSeconds = 300
	}
	snapshot.PreflightTaskID = "task:" + uuid.NewString()
	if node, ok := domainworkflow.NodeExecutionFrom(ctx); ok {
		snapshot.PreflightTaskID = node.ResourceID("task")
	}
	result.Payload = sohaapi.HelmExecutionTaskPayload{Action: sohaapi.Preflight, Snapshot: snapshot, Prepared: &sohaapi.HelmPreparedRelease{Values: sohaapi.TemplateParameterValues{}, Hooks: []string{}}}
	if input.FrozenHelmCiphertext != "" {
		result, err = s.restoreFrozenHelm(ctx, principal, service, target, input, snapshot)
		if err != nil {
			return result, err
		}
	} else if input.HelmRevision > 0 {
		result, err = s.historicalHelmPreparation(ctx, principal, snapshot)
		if err != nil {
			return result, err
		}
	} else {
		inspection, archive, refs, err := s.loadHelmDeliveryChart(ctx, principal, binding.ApplicationID, target.Helm.Source)
		if err != nil {
			return result, err
		}
		result.References = refs
		result.Payload.Snapshot.Chart, result.Payload.Snapshot.ChartVersion, result.Payload.Snapshot.ChartDigest = inspection.Name, inspection.Version, inspection.Digest
		result.Payload.Prepared.ChartArchive = base64.StdEncoding.EncodeToString(archive)
		values, refs, err := s.helmTargetValues(ctx, principal, service, target, input, inspection.DefaultValues)
		if err != nil {
			return result, err
		}
		result.References = append(result.References, refs...)
		result.Payload.Prepared.Values = values
	}
	intended := result.Payload.Snapshot
	result.Payload, err = s.helm.Runtime.PrepareHelmDelivery(ctx, principal, result.Payload)
	if err == nil {
		err = validateHelmPreparation(intended, result.Payload)
	}
	if err == nil {
		_, err = s.helm.Runtime.AuthorizeHelmDelivery(ctx, principal, result.Payload.Snapshot, true)
	}
	return result, err
}

func (s *Service) helmTargetService(ctx context.Context, principal domainidentity.Principal, applicationID, serviceID string) (domainapp.Service, error) {
	services, err := s.applications.ListServices(ctx, principal, applicationID)
	if err != nil {
		return domainapp.Service{}, err
	}
	for _, service := range services {
		if service.ID == serviceID && service.Enabled {
			return service, nil
		}
	}
	return domainapp.Service{}, fmt.Errorf("%w: Helm target requires an enabled service", apperrors.ErrInvalidArgument)
}

func (s *Service) resolveHelmReferences(ctx context.Context, principal domainidentity.Principal, applicationID string, refs map[string]string) ([]domainsecret.Reference, map[string]string, error) {
	if len(refs) == 0 {
		return nil, map[string]string{}, nil
	}
	if s.helm.Secrets == nil {
		return nil, nil, fmt.Errorf("%w: Helm secret resolver is unavailable", apperrors.ErrInvalidArgument)
	}
	target := domainsecret.Target{Type: "project", Ref: applicationID}
	pinned, err := s.helm.Secrets.PinReferences(ctx, principal, refs, target)
	if err != nil {
		return nil, nil, err
	}
	values, err := s.helm.Secrets.ResolvePinnedReferences(ctx, principal, pinned, target)
	return pinned, values, err
}

func (s *Service) helmTargetValues(ctx context.Context, principal domainidentity.Principal, service domainapp.Service, target domaincatalog.ReleaseTarget, input domaindelivery.DeliveryPlanInput, defaults sohaapi.TemplateParameterValues) (sohaapi.TemplateParameterValues, []domainsecret.Reference, error) {
	values := map[string]any{}
	for _, layer := range []any{defaults, target.Helm.Source.Values, target.Helm.Values} {
		encoded, err := json.Marshal(layer)
		if err != nil {
			return nil, nil, err
		}
		var overlay map[string]any
		if err := json.Unmarshal(encoded, &overlay); err != nil {
			return nil, nil, err
		}
		mergeHelmValues(values, overlay)
	}
	artifacts, err := s.manifestArtifacts(ctx, principal, service.ApplicationID, service.ID, input.ReleaseBundleID)
	if err != nil {
		return nil, nil, err
	}
	images := artifacts.ContainerImages
	if input.HelmImagePlaceholders != nil {
		images = input.HelmImagePlaceholders
	}
	values, err = helmrelease.MapImages(values, target.Helm.ImageMappings, images)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	refs := map[string]string{}
	walkHelmValues(values, func(value string) string {
		if strings.HasPrefix(value, "soha://secrets/") {
			refs[helmValueAlias(value)] = value
		}
		return value
	})
	pinned, resolved, err := s.resolveHelmReferences(ctx, principal, service.ApplicationID, refs)
	if err != nil {
		return nil, nil, err
	}
	walkHelmValues(values, func(value string) string {
		if secret, ok := resolved[helmValueAlias(value)]; ok {
			return secret
		}
		return value
	})
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, nil, err
	}
	var result sohaapi.TemplateParameterValues
	err = json.Unmarshal(encoded, &result)
	return result, pinned, err
}

func helmValueAlias(value string) string {
	return "HELM_VALUE_" + helmrelease.SHA256([]byte(value))[7:39]
}

func mergeHelmValues(target, overlay map[string]any) {
	for key, value := range overlay {
		if object, ok := value.(map[string]any); ok {
			if existing, ok := target[key].(map[string]any); ok {
				mergeHelmValues(existing, object)
				continue
			}
		}
		target[key] = value
	}
}

func walkHelmValues(value any, replace func(string) string) {
	switch item := value.(type) {
	case map[string]any:
		for key, value := range item {
			if text, ok := value.(string); ok {
				item[key] = replace(text)
			} else {
				walkHelmValues(value, replace)
			}
		}
	case []any:
		for index, value := range item {
			if text, ok := value.(string); ok {
				item[index] = replace(text)
			} else {
				walkHelmValues(value, replace)
			}
		}
	}
}

func validateHelmPreparation(intended sohaapi.HelmDeliverySnapshot, result sohaapi.HelmExecutionTaskPayload) error {
	if intended.RollbackRevision > 0 && (intended.ChartDigest != result.Snapshot.ChartDigest || intended.ValuesDigest != result.Snapshot.ValuesDigest || intended.RenderedDigest != result.Snapshot.RenderedDigest) {
		return fmt.Errorf("%w: Helm rollback revision differs from verified delivery history", apperrors.ErrConflict)
	}
	actual := result.Snapshot
	intended.Operation, intended.ExpectedRevision = actual.Operation, actual.ExpectedRevision
	intended.RenderedDigest, intended.ValuesDigest, intended.Resources = actual.RenderedDigest, actual.ValuesDigest, actual.Resources
	if intended.RollbackRevision > 0 {
		intended.Chart, intended.ChartVersion, intended.ChartDigest = actual.Chart, actual.ChartVersion, actual.ChartDigest
	}
	if !reflect.DeepEqual(intended, actual) || result.Action != sohaapi.Preflight || result.Prepared == nil || len(actual.Resources) == 0 {
		return fmt.Errorf("%w: Helm preparation changed its target or contains no resources", apperrors.ErrInvalidArgument)
	}
	return nil
}
