package virtualization

import (
	"context"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) workerToolScopes(ctx context.Context, principal domainidentity.Principal, tool string, input map[string]any) ([]map[string]string, error) {
	if tool == "virtualization.workers.assess" {
		task, err := s.GetOperation(ctx, principal, strings.TrimSpace(stringValue(input, "operationId")))
		if err != nil {
			return nil, err
		}
		if _, err := workerPoolFromTask(task); err != nil {
			return nil, err
		}
		return []map[string]string{taskCapabilityScope(task)}, nil
	}
	if tool != "virtualization.worker_pools.list" {
		pool, err := s.readWorkerPool(ctx, principal, strings.TrimSpace(stringValue(input, "workerPoolId")))
		if err != nil {
			return nil, err
		}
		return []map[string]string{workerPoolScope(pool)}, nil
	}
	if s.workerPools == nil {
		return nil, apperrors.ErrUnsupportedOperation
	}
	connection, err := s.connections.GetConnection(ctx, strings.TrimSpace(stringValue(input, "connectionId")))
	if err != nil {
		return nil, err
	}
	pools, err := s.workerPools.ListWorkerPools(ctx, connection.ID)
	if err != nil {
		return nil, err
	}
	scopes := []map[string]string{connectionCapabilityScope(connection, "")}
	for _, pool := range pools {
		scopes = append(scopes, workerPoolScope(pool))
	}
	return scopes, nil
}

func (s *Service) AssessWorkerReadiness(ctx context.Context, principal domainidentity.Principal, operationID string) (sohaapi.CapabilityAssessment, error) {
	result := sohaapi.CapabilityAssessment{Verdict: "inconclusive", Summary: "fresh evidence for the original worker is required", Evidence: []sohaapi.CapabilityEvidence{}}
	task, err := s.GetOperation(ctx, principal, operationID)
	if err != nil {
		return result, err
	}
	if err := s.authorizeWorkerPoolView(ctx, principal); err != nil {
		return result, err
	}
	pool, err := workerPoolFromTask(task)
	if err != nil {
		return result, err
	}
	if err := domain.CheckScope(ctx, taskCapabilityScope(task)); err != nil {
		return result, err
	}
	if s.workerRuntime == nil || s.workerRuntime.Observe == nil {
		return result, apperrors.ErrUnsupportedOperation
	}
	if task.VMID == "" || payloadString(task.Result, "providerEffect") != "created" {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	observed, err := s.workerRuntime.Observe(ctx, pool, task.ID)
	if err != nil {
		result.Summary = "original worker identity or cluster observations could not be verified"
		return result, nil
	}
	now := time.Now().UTC()
	fresh := !observed.ObservedAt.After(now.Add(5*time.Second)) && now.Sub(observed.ObservedAt) <= 30*time.Second && observed.ValidUntil.After(now)
	result.Summary = observed.Reason
	if result.Summary == "" {
		result.Summary = "worker readiness is not yet established"
	}
	if fresh && observed.Ready && observed.NodeUID != "" {
		result.Verdict = "satisfied"
	}
	result.Evidence = []sohaapi.CapabilityEvidence{{Kind: "worker_readiness", Source: "kubernetes.direct", ObservedAt: now, DataThrough: &observed.ObservedAt, Incomplete: !fresh || !observed.Ready, Summary: result.Summary, Resource: &sohaapi.CapabilityResourceRef{Kind: "k8s.node", ID: observed.NodeName, Scope: map[string]string{"clusterId": pool.Spec.ClusterID, "nodeUid": observed.NodeUID, "workerPoolId": pool.ID.String(), "operationId": task.ID}}, Reference: &sohaapi.CapabilityCall{ToolName: "virtualization.workers.assess", CapabilityVersion: "1", Input: map[string]any{"operationId": task.ID}}}}
	return result, nil
}

func (s *Service) FindWorkerCreation(ctx context.Context, principal domainidentity.Principal, poolID string, input sohaapi.VirtualizationWorkerCreateInput) (domain.Task, error) {
	if err := s.authorizeWorkerCreate(ctx, principal); err != nil {
		return domain.Task{}, err
	}
	pool, err := s.readWorkerPool(ctx, principal, poolID)
	if err != nil {
		return domain.Task{}, err
	}
	task, found, err := s.existingIdempotentTask(ctx, "virtualization.worker.create/"+pool.ID.String(), principal, input.IdempotencyKey, input)
	if err != nil {
		return domain.Task{}, err
	}
	if !found {
		return domain.Task{}, apperrors.ErrNotFound
	}
	return domain.WithOperationState(task, time.Now().UTC()), nil
}
