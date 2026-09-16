package delivery

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/helmrelease"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) historicalHelmPreparation(ctx context.Context, principal domainidentity.Principal, snapshot sohaapi.HelmDeliverySnapshot) (helmPreparation, error) {
	// ponytail: scan at most 1000 successful applies per release; add an indexed
	// revision lookup if release history retention grows beyond this bound.
	tasks, err := s.repository.ListExecutionTasks(ctx, domaindelivery.ExecutionTaskFilter{
		ApplicationID: snapshot.ApplicationID, ApplicationEnvironmentID: snapshot.ApplicationEnvironmentID,
		QueueKey:  "helm:" + snapshot.ClusterID + ":" + snapshot.Namespace + ":" + snapshot.ReleaseName,
		TaskKinds: []string{"helm_apply"}, Status: "completed", Limit: 1000,
	})
	if err != nil {
		return helmPreparation{}, err
	}
	for _, task := range tasks {
		payload, err := decodeHelmPayload(task.Payload)
		previous := payload.Snapshot
		if err != nil || task.TaskKind != "helm_apply" || task.Status != "completed" || previous.ExpectedRevision+1 != snapshot.RollbackRevision ||
			previous.ApplicationID != snapshot.ApplicationID || previous.ApplicationEnvironmentID != snapshot.ApplicationEnvironmentID || previous.ServiceID != snapshot.ServiceID || previous.TargetID != snapshot.TargetID ||
			previous.ClusterID != snapshot.ClusterID || previous.Namespace != snapshot.Namespace || previous.ReleaseName != snapshot.ReleaseName {
			continue
		}
		var result sohaapi.HelmExecutionTaskResult
		encoded, err := json.Marshal(task.Result["helm"])
		if err != nil || json.Unmarshal(encoded, &result) != nil || helmrelease.ValidateCompletedResult(payload, result) != nil {
			continue
		}
		plan, err := s.repository.GetDeliveryPlan(ctx, previous.DeliveryPlanID)
		if err != nil {
			return helmPreparation{}, err
		}
		prepared, err := s.helmPlanPreparation(ctx, principal, plan, previous)
		if err != nil {
			return prepared, err
		}
		if _, err := s.manifestArtifacts(ctx, principal, snapshot.ApplicationID, snapshot.ServiceID, previous.ReleaseBundleID); err != nil {
			return prepared, err
		}
		snapshot.Chart, snapshot.ChartVersion, snapshot.ChartDigest = previous.Chart, previous.ChartVersion, previous.ChartDigest
		snapshot.ValuesDigest, snapshot.RenderedDigest, snapshot.ReleaseBundleID = previous.ValuesDigest, previous.RenderedDigest, previous.ReleaseBundleID
		prepared.Payload.Snapshot = snapshot
		return prepared, nil
	}
	return helmPreparation{}, fmt.Errorf("%w: selected Helm revision has no verified delivery history", apperrors.ErrConflict)
}
