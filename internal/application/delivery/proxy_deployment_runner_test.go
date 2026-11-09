package delivery

import (
	"context"
	"errors"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type privateProxyDeploymentRepository struct {
	stubRepository
	task domain.ExecutionTask
}

func (r privateProxyDeploymentRepository) GetExecutionTask(context.Context, string) (domain.ExecutionTask, error) {
	return r.task, nil
}
func (r privateProxyDeploymentRepository) GetExecutionTaskByCallbackToken(context.Context, string) (domain.ExecutionTask, error) {
	return r.task, nil
}

func TestGenericRunnerCannotReadOrCallbackPrivateProxyDeploymentTask(t *testing.T) {
	service := &Service{repository: privateProxyDeploymentRepository{task: domain.ExecutionTask{ID: "private", ProviderKind: "proxy_deployment.edge", CallbackToken: "secret"}}}
	if task, err := service.GetExecutionTaskForRunner(context.Background(), "private"); !errors.Is(err, apperrors.ErrAccessDenied) || task.CallbackToken != "" {
		t.Fatalf("generic status leaked private task: %+v %v", task, err)
	}
	if _, err := service.RecordCallback(context.Background(), domain.ExecutionCallbackInput{CallbackToken: "secret", Status: "completed"}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("fallback callback accepted: %v", err)
	}
}
