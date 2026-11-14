package radiusdeployment

import (
	"context"
	"crypto/subtle"
	"fmt"
	"regexp"
	"time"

	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ExecutionTasks interface {
	ClaimRadiusDeploymentExecutionTask(context.Context, string) (delivery.ExecutionTask, error)
	GetRadiusDeploymentExecutionTask(context.Context, string, string) (delivery.ExecutionTask, error)
	RecordRadiusDeploymentCallback(context.Context, string, delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error)
}
type ExecutorCredential struct{ ExecutorID, Token string }
type ExecutorService struct {
	service     *Service
	tasks       ExecutionTasks
	credentials []ExecutorCredential
}

func NewExecutorService(service *Service, tasks ExecutionTasks, credentials []ExecutorCredential) (*ExecutorService, error) {
	if service == nil || nilInterface(tasks) {
		return nil, fmt.Errorf("RADIUS executor dependencies required")
	}
	seen := map[string]bool{}
	for _, credential := range credentials {
		if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(credential.ExecutorID) || len(credential.Token) < 32 || seen[credential.ExecutorID] || seen[credential.Token] {
			return nil, apperrors.ErrInvalidArgument
		}
		seen[credential.ExecutorID], seen[credential.Token] = true, true
		service.executors[credential.ExecutorID] = true
	}
	return &ExecutorService{service: service, tasks: tasks, credentials: append([]ExecutorCredential(nil), credentials...)}, nil
}
func (e *ExecutorService) authenticate(token string) (string, error) {
	for _, credential := range e.credentials {
		if subtle.ConstantTimeCompare([]byte(token), []byte(credential.Token)) == 1 {
			return credential.ExecutorID, nil
		}
	}
	return "", apperrors.ErrUnauthorized
}
func (e *ExecutorService) validate(ctx context.Context, id string, task delivery.ExecutionTask) (domain.Command, error) {
	command, err := DecodeCommand(task)
	if err != nil {
		return command, err
	}
	state, err := e.service.repo.Get(ctx, command.PoolID)
	if err != nil {
		return command, err
	}
	if err := e.service.pinned(state); err != nil {
		return command, err
	}
	slot, ok := e.service.slots[command.Slot.ID]
	if !ok || slot.ExecutorID != id || slot.Hash() != command.SlotHash || task.ClaimedByAgentID != id || int64(state.Pool.Revision) != command.Revision || int64(state.Pool.OwnerEpoch) != command.OwnerEpoch || state.Pool.OperationID != task.ID {
		return command, apperrors.ErrConflict
	}
	if !command.Deadline.After(time.Now()) && (task.Status == "running" || task.Status == "dispatching") {
		return command, apperrors.ErrConflict
	}
	return command, nil
}
func (e *ExecutorService) Claim(ctx context.Context, token, requested string) (delivery.ExecutionTask, error) {
	id, err := e.authenticate(token)
	if err != nil {
		return delivery.ExecutionTask{}, err
	}
	if id != requested {
		return delivery.ExecutionTask{}, apperrors.ErrAccessDenied
	}
	task, err := e.tasks.ClaimRadiusDeploymentExecutionTask(ctx, id)
	if err != nil {
		return task, err
	}
	_, err = e.validate(ctx, id, task)
	return task, err
}
func (e *ExecutorService) Status(ctx context.Context, token, taskID string) (delivery.ExecutionTask, error) {
	id, err := e.authenticate(token)
	if err != nil {
		return delivery.ExecutionTask{}, err
	}
	task, err := e.tasks.GetRadiusDeploymentExecutionTask(ctx, id, taskID)
	if err != nil {
		return task, err
	}
	command, err := e.validate(ctx, id, task)
	if err != nil {
		return delivery.ExecutionTask{}, err
	}
	drain, err := e.service.repo.Drain(ctx, command.PoolID, command.Revision, time.Now().UTC())
	if err != nil {
		return delivery.ExecutionTask{}, err
	}
	if task.Result == nil {
		task.Result = map[string]any{}
	}
	task.Result["radiusDeploymentDrain"] = drain
	return task, nil
}
func (e *ExecutorService) Callback(ctx context.Context, token string, input delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error) {
	id, err := e.authenticate(token)
	if err != nil {
		return delivery.ExecutionTask{}, err
	}
	task, err := e.service.repo.TaskByToken(ctx, input.CallbackToken)
	if err != nil {
		return task, err
	}
	if _, err := e.validate(ctx, id, task); err != nil {
		return delivery.ExecutionTask{}, err
	}
	if input.Status != "running" && input.Status != "completed" && input.Status != "failed" {
		return delivery.ExecutionTask{}, apperrors.ErrInvalidArgument
	}
	return e.tasks.RecordRadiusDeploymentCallback(ctx, id, input)
}
