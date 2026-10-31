package workflow

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/idempotency"
)

type TriggerRequestRepository interface {
	ClaimTriggerRequest(context.Context, domainworkflow.TriggerRequest) (domainworkflow.TriggerRequest, bool, error)
	GetTriggerRequest(context.Context, string, string) (domainworkflow.TriggerRequest, error)
	FinishTriggerRequest(context.Context, string, string, string) error
}

var workflowRequestKey = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

func (s *Service) SubmitWorkflowRequest(ctx context.Context, principal domainidentity.Principal, key string, input domainworkflow.Input) (domainworkflow.TriggerReceipt, error) {
	if !workflowRequestKey.MatchString(key) || principal.UserID == "" {
		return domainworkflow.TriggerReceipt{}, apperrors.ErrInvalidArgument
	}
	if err := s.authorizeWorkflowRequest(ctx, principal, input.ApplicationID, input.ApplicationEnvironmentID); err != nil {
		return domainworkflow.TriggerReceipt{}, err
	}
	repo, ok := s.repo.(TriggerRequestRepository)
	if !ok {
		return domainworkflow.TriggerReceipt{}, apperrors.ErrUnsupportedOperation
	}
	id, digest, err := idempotency.Derive("workflow-request-v1", principal.UserID, key, input)
	if err != nil {
		return domainworkflow.TriggerReceipt{}, apperrors.ErrInvalidArgument
	}
	now := time.Now().UTC()
	request, created, err := repo.ClaimTriggerRequest(ctx, domainworkflow.TriggerRequest{ID: id, ActorID: principal.UserID, Key: key, InputDigest: digest, ApplicationID: input.ApplicationID, BindingID: input.ApplicationEnvironmentID, RunID: "workflow:" + id, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		return domainworkflow.TriggerReceipt{}, err
	}
	if !created {
		return s.workflowRequestReceipt(ctx, principal, request), nil
	}
	// The durable fence is committed before Trigger. Interrupted submissions are
	// deliberately unknown; neither a duplicate POST nor GET can enqueue again.
	input.RequestedRunID = request.RunID
	run, triggerErr := s.Trigger(ctx, principal, input)
	if triggerErr == nil && run.ID != request.RunID {
		triggerErr = apperrors.ErrServiceUnavailable
	}
	status, code := "accepted", ""
	if triggerErr != nil {
		status, code = workflowRequestFailure(triggerErr)
	}
	if status != "unknown" {
		finishCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = repo.FinishTriggerRequest(finishCtx, request.ID, status, code)
		stop()
		if err == nil {
			request.Status, request.ErrorCode = status, code
		}
	}
	return s.workflowRequestReceipt(ctx, principal, request), nil
}

func (s *Service) GetWorkflowRequest(ctx context.Context, principal domainidentity.Principal, key string) (domainworkflow.TriggerReceipt, error) {
	if !workflowRequestKey.MatchString(key) || principal.UserID == "" {
		return domainworkflow.TriggerReceipt{}, apperrors.ErrInvalidArgument
	}
	repo, ok := s.repo.(TriggerRequestRepository)
	if !ok {
		return domainworkflow.TriggerReceipt{}, apperrors.ErrUnsupportedOperation
	}
	request, err := repo.GetTriggerRequest(ctx, principal.UserID, key)
	if err != nil {
		return domainworkflow.TriggerReceipt{}, err
	}
	if err := s.authorizeWorkflowRequest(ctx, principal, request.ApplicationID, request.BindingID); err != nil {
		return domainworkflow.TriggerReceipt{}, err
	}
	return s.workflowRequestReceipt(ctx, principal, request), nil
}

func (s *Service) authorizeWorkflowRequest(ctx context.Context, principal domainidentity.Principal, application, binding string) error {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowsTrigger); err != nil {
		return err
	}
	if application == "" {
		return apperrors.ErrInvalidArgument
	}
	app, err := s.apps.Get(ctx, application)
	if err != nil {
		return err
	}
	return s.authorize(ctx, principal, domainaccess.ActionTrigger, app, application, binding, "")
}

func (s *Service) workflowRequestReceipt(ctx context.Context, principal domainidentity.Principal, request domainworkflow.TriggerRequest) domainworkflow.TriggerReceipt {
	receipt := domainworkflow.TriggerReceipt{ID: request.ID, RequestKey: request.Key, RunID: request.RunID, SubmissionStatus: request.Status, ErrorCode: request.ErrorCode}
	if run, err := s.Get(ctx, principal, request.RunID); err == nil {
		receipt.Run = &run
	}
	return receipt
}

func workflowRequestFailure(err error) (string, string) {
	switch {
	case errors.Is(err, apperrors.ErrInvalidArgument):
		return "rejected", "invalid_argument"
	case errors.Is(err, apperrors.ErrAccessDenied):
		return "rejected", "permission_denied"
	case errors.Is(err, apperrors.ErrConflict):
		return "rejected", "conflict"
	case errors.Is(err, apperrors.ErrUnsupportedOperation):
		return "rejected", "unsupported"
	default:
		return "unknown", ""
	}
}

func requestedWorkflowRunID(input domainworkflow.Input) string {
	if input.RequestedRunID != "" {
		return input.RequestedRunID
	}
	return "workflow:" + uuid.NewString()
}
