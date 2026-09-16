package deliverytrigger

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	domain "github.com/opensoha/soha/internal/domain/deliverytrigger"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) process(ctx context.Context, event domain.StoredEvent) {
	workCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	status, reason, err := s.dispatch(workCtx, &event)
	cancel()
	if err != nil && !errors.Is(err, apperrors.ErrUnauthorized) && !errors.Is(err, apperrors.ErrAccessDenied) && !errors.Is(err, apperrors.ErrNotFound) && !errors.Is(err, apperrors.ErrInvalidArgument) && !errors.Is(err, apperrors.ErrConflict) {
		// Leave the bounded lease recoverable when persistence or a remote reply is uncertain.
		return
	}
	if err != nil {
		status, reason = "failed", dispatchErrorReason(err)
	}
	event.Status, event.Reason, event.UpdatedAt = sohaapi.DeliveryTriggerEventStatus(status), reason, time.Now().UTC()
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	if err := s.repo.Finish(finishCtx, event); err != nil {
		return
	}
	s.record(finishCtx, domainidentity.Principal{UserID: "delivery-trigger", UserName: "Delivery trigger"}, "delivery.trigger.dispatch", event.TriggerID, status, map[string]any{"eventId": event.ID, "providerEventId": event.EventID, "triggerRevision": event.TriggerRevision, "reason": reason, "batchId": event.BatchID, "syncRunId": event.SyncRunID, "resolvedCommit": event.ResolvedCommit})
}

func dispatchErrorReason(err error) string {
	switch {
	case errors.Is(err, apperrors.ErrUnauthorized), errors.Is(err, apperrors.ErrAccessDenied):
		return "authorization_revoked"
	case errors.Is(err, apperrors.ErrNotFound):
		return "target_unavailable"
	case errors.Is(err, apperrors.ErrConflict):
		return "configuration_changed"
	case errors.Is(err, apperrors.ErrInvalidArgument):
		return "invalid_configuration"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "dispatch_interrupted"
	default:
		return "dispatch_failed"
	}
}

func (s *Service) actors(ctx context.Context, item domain.StoredTrigger) (domainidentity.Principal, domainidentity.Principal, error) {
	issuer, err := s.identity.CurrentExecutionPrincipal(ctx, item.UpdatedBy, item.UpdatedByTokenID)
	if err != nil {
		return issuer, domainidentity.Principal{}, err
	}
	action := "update"
	if item.Revision == 1 {
		action = "create"
	}
	if err := s.permission(ctx, issuer, action); err != nil {
		return issuer, domainidentity.Principal{}, err
	}
	executor, err := s.identity.CurrentExecutionPrincipal(ctx, "service_account:"+item.ServiceAccountID, item.ExecutionTokenID)
	return issuer, executor, err
}

func (s *Service) checkTarget(ctx context.Context, item domain.StoredTrigger, issuer, executor domainidentity.Principal) error {
	for _, principal := range []domainidentity.Principal{issuer, executor} {
		current, err := s.target(ctx, principal, item, true)
		if err != nil {
			return err
		}
		if current != item.TargetDigest {
			return fmt.Errorf("%w: target definition changed", apperrors.ErrConflict)
		}
	}
	return nil
}

func (s *Service) dispatch(ctx context.Context, event *domain.StoredEvent) (string, string, error) {
	item, err := s.repo.Get(ctx, event.TriggerID)
	if err != nil {
		return "", "", err
	}
	if !item.Enabled || item.Revision != event.TriggerRevision {
		return "skipped", "trigger_changed", nil
	}
	issuer, executor, err := s.actors(ctx, item)
	if err != nil {
		return "", "", err
	}
	// A previous attempt may have committed a Batch before losing its response.
	if event.PreparedAt != nil && item.TargetKind == "workflow" {
		batch, err := s.workflows.FindDeliveryBatch(ctx, executor, batchInput(item, *event))
		if err == nil {
			event.BatchID = batch.ID
			return "succeeded", "batch_accepted", nil
		}
		if !errors.Is(err, apperrors.ErrNotFound) {
			return "", "", err
		}
	}
	if err := s.checkTarget(ctx, item, issuer, executor); err != nil {
		return "", "", err
	}
	if event.PreparedAt == nil {
		status, reason, err := s.prepareEvent(ctx, item, event, executor)
		if err != nil || status != "" {
			return status, reason, err
		}
	}

	if item.TargetKind == "template_source" {
		return s.syncSource(ctx, item, event, executor)
	}
	batch, err := s.workflows.CreateDeliveryBatch(ctx, executor, batchInput(item, *event))
	if err != nil {
		return "", "", err
	}
	event.BatchID = batch.ID
	return "succeeded", "batch_accepted", nil
}

func batchInput(item domain.StoredTrigger, event domain.StoredEvent) domainworkflow.DeliveryBatchInput {
	input := domainworkflow.DeliveryBatchInput{IdempotencyKey: "trigger:" + event.ID, WorkflowID: item.TargetID, WorkflowVersion: int64(item.WorkflowVersion), TriggerAuthorizerID: item.UpdatedBy, TriggerAuthorizerTokenID: item.UpdatedByTokenID}
	if item.Webhook != nil {
		input.SourceCommit = &sohaapi.DeliverySourceCommit{RepositoryID: item.Webhook.RepositoryID, RefType: sohaapi.DeliverySourceCommitRefType(item.Webhook.RefType), RefName: item.Webhook.RefValue, Commit: event.ResolvedCommit}
	}
	return input
}

func (s *Service) syncSource(ctx context.Context, item domain.StoredTrigger, event *domain.StoredEvent, executor domainidentity.Principal) (string, string, error) {
	key := "trigger:" + event.ID
	run, err := s.sources.Sync(ctx, executor, item.TargetID, domaindocument.SyncInput{IdempotencyKey: key, ExpectedGeneration: event.SourceGeneration, ResolvedCommit: event.ResolvedCommit})
	if err != nil {
		return "", "", err
	}
	event.SyncRunID = run.ID
	if run.Status == "applied" {
		return "succeeded", "drafts_applied", nil
	}
	if run.Status != "ready" || run.Preview == nil {
		return "failed", "source_" + run.Status, nil
	}
	// Import can follow a long Git read. Recheck both delegation subjects before writing drafts.
	issuer, executor, err := s.actors(ctx, item)
	if err != nil {
		return "", "", err
	}
	if err := s.checkTarget(ctx, item, issuer, executor); err != nil {
		return "", "", err
	}
	if err := s.repo.Checkpoint(ctx, *event); err != nil {
		return "", "", err
	}
	run, err = s.sources.Apply(ctx, executor, item.TargetID, run.ID, domaindocument.SyncApplyInput{ExpectedGeneration: event.SourceGeneration, CandidateDigest: run.Preview.CandidateDigest, IdempotencyKey: key})
	if err != nil {
		return "", "", err
	}
	if run.Status != "applied" {
		return "failed", "source_" + run.Status, nil
	}
	return "succeeded", "drafts_applied", nil
}

func terminalBatch(status string) bool {
	switch status {
	case "completed", "partially_completed", "failed", "canceled":
		return true
	default:
		return false
	}
}

func (s *Service) prepareEvent(ctx context.Context, item domain.StoredTrigger, event *domain.StoredEvent, executor domainidentity.Principal) (string, string, error) {
	if event.EventType != "webhook" && time.Since(event.OccurredAt) >= time.Minute {
		return "skipped", "missed_schedule", nil
	}
	if item.LastBatchID != "" {
		batch, err := s.workflows.GetDeliveryBatch(ctx, executor, item.LastBatchID)
		if err != nil {
			return "", "", err
		}
		if !terminalBatch(batch.Status) {
			return "skipped", "previous_batch_active", nil
		}
	}
	if item.Webhook != nil {
		repository, err := s.repositories.GetRepository(ctx, executor, item.Webhook.RepositoryID)
		if err != nil {
			return "", "", err
		}
		head, err := s.git.ResolveDeliveryCommit(ctx, repository, string(item.Webhook.RefType), item.Webhook.RefValue)
		if err != nil {
			return "", "", err
		}
		if head != event.ResolvedCommit {
			return "skipped", "stale_commit", nil
		}
	}
	if item.TargetKind == "template_source" {
		source, err := s.sources.Get(ctx, executor, item.TargetID)
		if err != nil {
			return "", "", err
		}
		event.SourceGeneration = source.Generation
		if event.ResolvedCommit == "" {
			repository, err := s.repositories.GetRepository(ctx, executor, source.RepositoryID)
			if err != nil {
				return "", "", err
			}
			event.ResolvedCommit, err = s.git.ResolveDeliveryCommit(ctx, repository, string(source.RefType), source.RefValue)
			if err != nil {
				return "", "", err
			}
		}
		if source.ResolvedCommit == event.ResolvedCommit {
			return "skipped", "source_unchanged", nil
		}
	}
	now := time.Now().UTC()
	event.PreparedAt = &now
	if err := s.repo.Checkpoint(ctx, *event); err != nil {
		return "", "", err
	}
	return "", "", nil
}
