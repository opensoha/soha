package resourcebackend

import (
	"context"
	"errors"
	"fmt"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	helmruntime "github.com/opensoha/soha-contracts/helmrelease/runtime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (d *Direct) PrepareHelmDelivery(ctx context.Context, input sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskPayload, error) {
	cfg, err := d.helmActionConfig(ctx, input.Snapshot.ClusterID, input.Snapshot.Namespace)
	if err != nil {
		return input, err
	}
	result, err := helmruntime.Prepare(ctx, cfg, input)
	return result, helmDeliveryError(err)
}

func (d *Direct) ExecuteHelmDelivery(ctx context.Context, input sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskResult, error) {
	cfg, err := d.helmActionConfig(ctx, input.Snapshot.ClusterID, input.Snapshot.Namespace)
	if err != nil {
		return sohaapi.HelmExecutionTaskResult{Stopped: true}, err
	}
	result, err := helmruntime.Execute(ctx, cfg, input)
	return result, helmDeliveryError(err)
}

func helmDeliveryError(err error) error {
	switch {
	case errors.Is(err, helmruntime.ErrConflict):
		return fmt.Errorf("%w: %v", apperrors.ErrConflict, err)
	case errors.Is(err, helmruntime.ErrInvalidArgument):
		return fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	case errors.Is(err, helmruntime.ErrUnavailable):
		return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	default:
		return err
	}
}
