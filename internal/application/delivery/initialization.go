package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/contracts/helmrelease"
	helmruntime "github.com/opensoha/soha/contracts/helmrelease/runtime"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type initializationHistory interface {
	HasTargetWriteHistory(context.Context, string, string, []string, string, string) (bool, error)
}

func (s *Service) checkHelmInitialization(ctx context.Context, snapshot sohaapi.HelmDeliverySnapshot) error {
	if snapshot.Operation != sohaapi.HelmDeliverySnapshotOperationInstall || snapshot.ExpectedRevision != 0 || snapshot.RollbackRevision != 0 {
		return fmt.Errorf("%w: 已有部署但运行基线不明，不能按首次发布初始化", apperrors.ErrConflict)
	}
	reader, ok := s.repository.(initializationHistory)
	if !ok {
		return fmt.Errorf("%w: initialization history unavailable", apperrors.ErrConflict)
	}
	previous, err := reader.HasTargetWriteHistory(ctx, snapshot.ClusterID, snapshot.Namespace, domainworkflow.HelmResourceKeys(snapshot), snapshot.ReleaseName, snapshot.DeliveryPlanID)
	if err != nil {
		return err
	}
	if previous {
		return fmt.Errorf("%w: 目标存在历史写入，必须核实运行基线", apperrors.ErrConflict)
	}
	return nil
}

// Only a complete original successful apply receipt can authorize observation.
// If the process died before persisting that receipt, current UIDs are insufficient.
func (s *Service) initialHelmReceipt(ctx context.Context, snapshot sohaapi.HelmDeliverySnapshot) ([]sohaapi.ManifestResourceInventory, error) {
	tasks, err := s.repository.ListExecutionTasks(ctx, domaindelivery.ExecutionTaskFilter{ApplicationID: snapshot.ApplicationID, ApplicationEnvironmentID: snapshot.ApplicationEnvironmentID, QueueKey: "helm:" + snapshot.ClusterID + ":" + snapshot.Namespace + ":" + snapshot.ReleaseName, TaskKinds: []string{"helm_apply"}, Status: "completed", Limit: 1000})
	if err != nil {
		return nil, err
	}
	for _, task := range tasks {
		payload, err := decodeHelmPayload(task.Payload)
		if err != nil {
			return nil, err
		}
		if payload.Action != sohaapi.Apply || task.TaskKind != "helm_apply" || !reflect.DeepEqual(payload.Snapshot, snapshot) {
			continue
		}
		var result sohaapi.HelmExecutionTaskResult
		data, _ := json.Marshal(task.Result["helm"])
		if json.Unmarshal(data, &result) != nil || helmrelease.ValidateCompletedResult(payload, result) != nil || helmruntime.ValidateInitializationResources(result.Resources, result.Resources) != nil {
			return nil, apperrors.ErrConflict
		}
		return result.Resources, nil
	}
	return nil, apperrors.ErrNotFound
}

func (s *Service) hydrateHelmInitialization(ctx context.Context, payload sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskPayload, error) {
	if !helmrelease.IsInitialization(payload.Snapshot) {
		return payload, nil
	}
	original, err := s.initialHelmReceipt(ctx, payload.Snapshot)
	if payload.Action == sohaapi.Observe {
		if err != nil || !reflect.DeepEqual(original, payload.InitializationResources) {
			return payload, apperrors.ErrConflict
		}
	} else if err == nil {
		payload.InitializationResources = original
	} else if !errors.Is(err, apperrors.ErrNotFound) {
		return payload, err
	}
	return payload, nil
}
