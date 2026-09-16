package delivery

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

func (s *Service) freezeHelmDeployment(ctx context.Context, principal domainidentity.Principal, service domainapp.Service, binding domaincatalog.ApplicationEnvironment, target domaincatalog.ReleaseTarget, snapshot *domainworkflow.DeliveryTargetSnapshot) error {
	if s.helm.Runtime == nil {
		return apperrors.ErrClusterUnready
	}
	if err := s.authorizeApplicationDeliveryAction(ctx, principal, binding.ID, domaindelivery.ApplicationDeliveryActionDeploy); err != nil {
		return err
	}
	if snapshot.Target.HelmRevision == 0 && snapshot.Target.Action == "config_update" && snapshot.Target.ReleaseBundleID == "" && len(target.Helm.ImageMappings) > 0 {
		bundleID, err := s.deployedHelmBundle(ctx, principal, binding, target)
		if err != nil {
			return err
		}
		snapshot.Target.ReleaseBundleID = bundleID
	}
	input := domaindelivery.DeliveryPlanInput{ID: "freeze:" + uuid.NewString(), Action: domaindelivery.ApplicationDeliveryActionDeploy, ReleaseBundleID: snapshot.Target.ReleaseBundleID, HelmRevision: snapshot.Target.HelmRevision}
	if snapshot.Target.Action == "build_deploy" {
		// Reserve identities before building; the final render must have the same
		// resource keys after verified digests replace these inert placeholders.
		input.HelmImagePlaceholders = map[string]string{}
		for _, container := range service.Containers {
			image, err := domaindelivery.ImmutableImageReference(container.ImageRepository, "sha256:"+strings.Repeat("0", 64))
			if err == nil {
				input.HelmImagePlaceholders[container.Name] = image
			}
		}
	} else if input.HelmRevision == 0 && input.ReleaseBundleID == "" && len(target.Helm.ImageMappings) > 0 {
		return fmt.Errorf("%w: select a verified artifact for Helm image mappings", apperrors.ErrInvalidArgument)
	}
	prepared, err := s.prepareHelmTarget(ctx, principal, binding, target, input)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(prepared)
	if err != nil {
		return err
	}
	ciphertext, err := secretcrypto.EncryptStringWithKeyring(s.helm.Keys, string(encoded))
	if err != nil {
		return err
	}
	snapshot.FrozenHelm, snapshot.FrozenHelmCiphertext = &prepared.Payload.Snapshot, ciphertext
	snapshot.Target.ReleaseBundleID = prepared.Payload.Snapshot.ReleaseBundleID
	snapshot.FrozenReleaseTarget, snapshot.Target.ReleaseTargetID = &target, target.ID
	return nil
}

func (s *Service) restoreFrozenHelm(ctx context.Context, principal domainidentity.Principal, service domainapp.Service, target domaincatalog.ReleaseTarget, input domaindelivery.DeliveryPlanInput, snapshot sohaapi.HelmDeliverySnapshot) (helmPreparation, error) {
	var prepared helmPreparation
	plaintext, err := secretcrypto.DecryptStringWithKeyring(s.helm.Keys, input.FrozenHelmCiphertext)
	if err != nil {
		return prepared, apperrors.ErrConflict
	}
	if err := json.Unmarshal([]byte(plaintext), &prepared); err != nil {
		return prepared, apperrors.ErrConflict
	}
	previous := prepared.Payload.Snapshot
	if previous.ConfigurationDigest != snapshot.ConfigurationDigest || previous.TargetID != snapshot.TargetID || previous.RollbackRevision != snapshot.RollbackRevision || prepared.Payload.Prepared == nil {
		return prepared, fmt.Errorf("%w: Helm intent differs from the frozen batch", apperrors.ErrInvalidArgument)
	}
	if len(prepared.References) > 0 {
		if s.helm.Secrets == nil {
			return prepared, apperrors.ErrAccessDenied
		}
		if _, err := s.helm.Secrets.ResolvePinnedReferences(ctx, principal, prepared.References, domainsecret.Target{Type: "project", Ref: service.ApplicationID}); err != nil {
			return prepared, err
		}
	}
	if snapshot.RollbackRevision > 0 {
		// A rollback restores the frozen historical values, including their images.
		// Applying current mappings here would silently turn it into a new configuration.
		snapshot.Chart, snapshot.ChartVersion, snapshot.ChartDigest = previous.Chart, previous.ChartVersion, previous.ChartDigest
		snapshot.ValuesDigest, snapshot.RenderedDigest, snapshot.ReleaseBundleID = previous.ValuesDigest, previous.RenderedDigest, previous.ReleaseBundleID
		prepared.Payload.Snapshot = snapshot
		return prepared, nil
	}
	artifacts, err := s.manifestArtifacts(ctx, principal, service.ApplicationID, service.ID, input.ReleaseBundleID)
	if err != nil {
		return prepared, err
	}
	encoded, err := json.Marshal(prepared.Payload.Prepared.Values)
	if err != nil {
		return prepared, err
	}
	var values map[string]any
	if err := json.Unmarshal(encoded, &values); err != nil {
		return prepared, err
	}
	values, err = helmrelease.MapImages(values, target.Helm.ImageMappings, artifacts.ContainerImages)
	if err != nil {
		return prepared, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	encoded, err = json.Marshal(values)
	if err != nil {
		return prepared, err
	}
	if err := json.Unmarshal(encoded, &prepared.Payload.Prepared.Values); err != nil {
		return prepared, err
	}
	snapshot.Chart, snapshot.ChartVersion, snapshot.ChartDigest = previous.Chart, previous.ChartVersion, previous.ChartDigest
	prepared.Payload.Snapshot = snapshot
	return prepared, nil
}

func (s *Service) deployedHelmBundle(ctx context.Context, principal domainidentity.Principal, binding domaincatalog.ApplicationEnvironment, target domaincatalog.ReleaseTarget) (string, error) {
	serviceID, _ := target.Metadata["serviceId"].(string)
	// The native current revision decides which historical task is still deployed.
	tasks, err := s.repository.ListExecutionTasks(ctx, domaindelivery.ExecutionTaskFilter{ApplicationID: binding.ApplicationID, ApplicationEnvironmentID: binding.ID, Status: "completed", Limit: 1000})
	if err != nil {
		return "", err
	}
	for _, task := range tasks {
		if task.TaskKind != "helm_apply" {
			continue
		}
		payload, err := decodeHelmPayload(task.Payload)
		if err != nil || payload.Snapshot.TargetID != target.ID || payload.Snapshot.ServiceID != serviceID || payload.Snapshot.ReleaseBundleID == "" {
			continue
		}
		payload.Action = sohaapi.Observe
		result, err := s.helm.Runtime.ExecuteHelmDelivery(ctx, principal, payload)
		if err == nil && result.Status == "deployed" && result.Revision == payload.Snapshot.ExpectedRevision+1 {
			return payload.Snapshot.ReleaseBundleID, nil
		}
	}
	return "", fmt.Errorf("%w: no verified Helm artifact is currently deployed; select an existing artifact", apperrors.ErrConflict)
}

func (s *Service) observeBatchHelmHealth(ctx context.Context, principal domainidentity.Principal, snapshot domainworkflow.DeliveryTargetSnapshot, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	plan, err := s.repository.GetDeliveryPlan(ctx, node.DeliveryPlanID)
	if err != nil {
		return node, err
	}
	if len(plan.HelmSnapshots) != 1 || plan.HelmSnapshots[0].TargetID != snapshot.Target.ReleaseTargetID {
		return node, apperrors.ErrConflict
	}
	timeout := snapshot.HealthTimeoutSeconds
	if timeout <= 0 {
		timeout = 300
	}
	task, err := s.queueHelmTask(ctx, principal, plan.HelmSnapshots[0], sohaapi.Observe, timeout)
	if err != nil {
		return node, err
	}
	return projectBatchTask(node, task), nil
}
