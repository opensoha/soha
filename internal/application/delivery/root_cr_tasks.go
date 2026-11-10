package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"reflect"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type rootCRTaskValidator interface {
	ValidateRootCRExecutionTask(context.Context, domaindelivery.ExecutionTask) error
}

func (s *Service) hydrateRootCRTask(ctx context.Context, task domaindelivery.ExecutionTask) (domaindelivery.ExecutionTask, error) {
	validator, ok := s.manifestDelivery.(rootCRTaskValidator)
	if !ok {
		return task, apperrors.ErrUnsupportedOperation
	}
	var payload domainmanifest.TaskPayload
	data, err := json.Marshal(task.Payload)
	if err != nil || json.Unmarshal(data, &payload) != nil || payload.RootResource == nil {
		return task, apperrors.ErrInvalidArgument
	}
	if payload.Action != domainmanifest.TaskActionPreflight && payload.Action != domainmanifest.TaskActionObserve {
		if payload.Action != domainmanifest.TaskActionApply {
			return task, apperrors.ErrUnsupportedOperation
		}
		plan, err := s.repository.GetDeliveryPlan(ctx, payload.RootResource.DeliveryPlanID)
		if err != nil {
			return task, err
		}
		if plan.ApplicationID != task.ApplicationID || plan.ApplicationEnvironmentID != task.ApplicationEnvironmentID || (plan.Status != domaindelivery.DeliveryPlanStatusConfirming && plan.Status != domaindelivery.DeliveryPlanStatusConfirmed) || plan.RequiresApproval && !deliveryPlanApprovalGranted(plan) {
			return task, apperrors.ErrConflict
		}
		found := false
		for _, snapshot := range plan.ManifestSnapshots {
			if rootTaskMatchesSnapshot(payload, snapshot) {
				found = true
			}
		}
		if !found {
			return task, fmt.Errorf("%w: root CR task differs from approved plan", apperrors.ErrConflict)
		}
	}
	return task, validator.ValidateRootCRExecutionTask(ctx, task)
}

func (s *Service) authorizeRootPlanRead(ctx context.Context, p domainidentity.Principal, plan domaindelivery.DeliveryPlan) error {
	for _, snapshot := range plan.ManifestSnapshots {
		if snapshot.RootResource != nil || domainmanifest.SensitiveDocuments(snapshot.Documents) {
			return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, p, appaccess.PermPlatformConfigurationSecretDataView)
		}
	}
	return nil
}

func rootTaskMatchesSnapshot(payload domainmanifest.TaskPayload, snapshot domainmanifest.DeliverySnapshot) bool {
	return snapshot.TargetID == payload.RootResource.TargetID && snapshot.BindingID == payload.BindingID && snapshot.PackageID == payload.PackageID && snapshot.ClusterID == payload.ClusterID && snapshot.Namespace == payload.Namespace && snapshot.RenderedDigest == payload.RenderedDigest && reflect.DeepEqual(snapshot.RootResource, payload.RootResource) && reflect.DeepEqual(snapshot.Documents, payload.Documents)
}
