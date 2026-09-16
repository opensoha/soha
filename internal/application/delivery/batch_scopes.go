package delivery

import (
	"context"
	"fmt"
	"maps"
	"reflect"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func deliveryTargetScope(app domainapp.App, input domainworkflow.DeliveryTargetInput, target *domaincatalog.ReleaseTarget) map[string]string {
	scope := map[string]string{"applicationId": app.ID, "applicationKey": app.Key, "serviceId": input.ServiceID}
	for key, value := range map[string]string{"businessLineId": app.BusinessLineID, "applicationEnvironmentId": input.ApplicationEnvironmentID, "releaseTargetId": input.ReleaseTargetID} {
		if value != "" {
			scope[key] = value
		}
	}
	if target != nil {
		scope["releaseTargetId"] = target.ID
		if target.ClusterID != "" {
			scope["clusterId"] = target.ClusterID
		}
		if target.Namespace != "" {
			scope["namespace"] = target.Namespace
		}
		if target.Docker != nil {
			scope["hostId"], scope["projectId"] = target.Docker.HostID, target.Docker.ProjectID
		}
	}
	return scope
}

func selectedDeliveryTarget(binding domaincatalog.ApplicationEnvironment, input domainworkflow.DeliveryTargetInput) (domaincatalog.ReleaseTarget, error) {
	var matches []domaincatalog.ReleaseTarget
	for _, target := range binding.Targets {
		if target.Enabled && target.Metadata["serviceId"] == input.ServiceID && (input.ReleaseTargetID == "" || target.ID == input.ReleaseTargetID) {
			matches = append(matches, target)
		}
	}
	if len(matches) != 1 {
		return domaincatalog.ReleaseTarget{}, fmt.Errorf("%w: select one enabled deployment target for the service environment", apperrors.ErrInvalidArgument)
	}
	return matches[0], nil
}

// ResolveDeliveryTargetScopes performs only authorized local reads. Frozen runs
// retain their original physical resource scope even after catalog edits.
func (s *Service) ResolveDeliveryTargetScopes(ctx context.Context, principal domainidentity.Principal, snapshot domainworkflow.DeliveryTargetSnapshot) ([]map[string]string, error) {
	app, _, err := s.deliveryTargetService(ctx, principal, snapshot.Target)
	if err != nil {
		return nil, err
	}
	target := snapshot.FrozenReleaseTarget
	if target == nil && snapshot.Target.Action != "build" {
		binding, err := s.catalog.GetApplicationEnvironment(ctx, principal, snapshot.Target.ApplicationEnvironmentID)
		if err != nil {
			return nil, err
		}
		if binding.ApplicationID != app.ID {
			return nil, apperrors.ErrAccessDenied
		}
		selected, err := selectedDeliveryTarget(binding, snapshot.Target)
		if err != nil {
			return nil, err
		}
		target = &selected
	}
	current := deliveryTargetScope(app, snapshot.Target, target)
	if len(snapshot.FrozenScope) > 0 && !reflect.DeepEqual(snapshot.FrozenScope, current) {
		return []map[string]string{maps.Clone(snapshot.FrozenScope), current}, nil
	}
	return []map[string]string{current}, nil
}
