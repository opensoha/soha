package proxydeployment

import (
	"context"
	"crypto/subtle"
	"fmt"
	"regexp"
	"time"

	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ExecutionTasks interface {
	ClaimProxyDeploymentExecutionTask(context.Context, string) (delivery.ExecutionTask, error)
	GetProxyDeploymentExecutionTask(context.Context, string, string) (delivery.ExecutionTask, error)
	RecordProxyDeploymentCallback(context.Context, string, delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error)
}

type ExecutorCredential struct{ ExecutorID, Token string }

type ExecutorService struct {
	service     *Service
	tasks       ExecutionTasks
	credentials []ExecutorCredential
}

func NewExecutorService(service *Service, tasks ExecutionTasks, credentials []ExecutorCredential) (*ExecutorService, error) {
	if service == nil || missing(tasks) {
		return nil, fmt.Errorf("Proxy executor dependencies are required")
	}
	seen := map[string]bool{}
	for _, item := range credentials {
		if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(item.ExecutorID) || len(item.Token) < 32 || seen[item.ExecutorID] || seen[item.Token] {
			return nil, fmt.Errorf("invalid or duplicate Proxy executor credential")
		}
		seen[item.ExecutorID], seen[item.Token] = true, true
	}
	// Bootstrap completes this registration before exposing either service.
	for _, item := range credentials {
		service.executors[item.ExecutorID] = true
	}
	return &ExecutorService{service: service, tasks: tasks, credentials: append([]ExecutorCredential(nil), credentials...)}, nil
}

func (s *ExecutorService) authenticate(token string) (string, error) {
	for _, item := range s.credentials {
		if subtle.ConstantTimeCompare([]byte(token), []byte(item.Token)) == 1 {
			return item.ExecutorID, nil
		}
	}
	return "", apperrors.ErrUnauthorized
}

func (s *ExecutorService) Claim(ctx context.Context, token, executorID string) (delivery.ExecutionTask, error) {
	id, err := s.authenticate(token)
	if err != nil {
		return delivery.ExecutionTask{}, err
	}
	if id != executorID {
		return delivery.ExecutionTask{}, apperrors.ErrAccessDenied
	}
	task, err := s.tasks.ClaimProxyDeploymentExecutionTask(ctx, id)
	if err != nil {
		return task, err
	}
	if _, err = s.validate(ctx, id, task); err != nil {
		return delivery.ExecutionTask{}, err
	}
	return task, nil
}

func (s *ExecutorService) validate(ctx context.Context, id string, task delivery.ExecutionTask) (domain.Command, error) {
	command, err := DecodeCommand(task)
	if err != nil {
		return command, apperrors.ErrAccessDenied
	}
	if !time.Now().Before(command.Deadline) && (task.Status == "running" || task.Status == "dispatching") {
		return command, apperrors.ErrConflict
	}
	slot, ok := s.service.slots[command.Slot.ID]
	if !ok || slot.ExecutorID != id || !SameSlotPolicy(slot, command.Slot) || task.ClaimedByAgentID != id {
		return command, apperrors.ErrAccessDenied
	}
	state, err := s.service.repo.Get(ctx, command.ProxyID)
	if err != nil {
		return command, err
	}
	if _, _, err := s.service.installedPin(state); err != nil {
		return command, err
	}
	if state.Owner != "soha-managed" || state.LatestTaskID != task.ID || state.Generation != command.Generation || state.OwnerEpoch != command.OwnerEpoch || state.UID != slot.UID {
		return command, apperrors.ErrConflict
	}
	return command, nil
}

func (s *ExecutorService) Status(ctx context.Context, token, taskID string) (delivery.ExecutionTask, error) {
	id, err := s.authenticate(token)
	if err != nil {
		return delivery.ExecutionTask{}, err
	}
	task, err := s.tasks.GetProxyDeploymentExecutionTask(ctx, id, taskID)
	if err != nil {
		return task, err
	}
	command, err := s.validate(ctx, id, task)
	if err != nil {
		return delivery.ExecutionTask{}, err
	}
	drain, err := s.service.repo.DrainStatus(ctx, command.ProxyID, command.Generation, time.Now().UTC())
	if err != nil {
		return delivery.ExecutionTask{}, err
	}
	if task.Result == nil {
		task.Result = map[string]any{}
	}
	task.Result["proxyDeploymentDrain"] = drain
	return task, nil
}

func (s *ExecutorService) Callback(ctx context.Context, token string, input delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error) {
	id, err := s.authenticate(token)
	if err != nil {
		return delivery.ExecutionTask{}, err
	}
	task, err := s.service.repo.TaskByToken(ctx, input.CallbackToken)
	if err != nil {
		return delivery.ExecutionTask{}, err
	}
	if _, err = s.validate(ctx, id, task); err != nil {
		return delivery.ExecutionTask{}, err
	}
	switch input.Status {
	case "running", "completed", "failed":
	default:
		return delivery.ExecutionTask{}, apperrors.ErrInvalidArgument
	}
	return s.tasks.RecordProxyDeploymentCallback(ctx, id, input)
}
