package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appbuild "github.com/opensoha/soha/internal/application/build"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type DeliveryRepository interface {
	FindDeliveryWorkflowCreation(context.Context, string, string, string) (domainworkflow.DeliveryWorkflow, error)
	CreateDeliveryWorkflowIdempotent(context.Context, domainworkflow.DeliveryWorkflow, string, string) (domainworkflow.DeliveryWorkflow, error)
	ListDeliveryWorkflows(context.Context) ([]domainworkflow.DeliveryWorkflow, error)
	GetDeliveryWorkflow(context.Context, string) (domainworkflow.DeliveryWorkflow, error)
	SaveDeliveryWorkflow(context.Context, domainworkflow.DeliveryWorkflow, int64) (domainworkflow.DeliveryWorkflow, error)
	CreateDeliveryBatch(context.Context, domainworkflow.DeliveryBatch, domainworkflow.Run, string, string) (domainworkflow.DeliveryBatch, domainworkflow.Run, error)
	FindDeliveryBatch(context.Context, string, string, string) (domainworkflow.DeliveryBatch, domainworkflow.Run, error)
	GetDeliveryBatch(context.Context, string) (domainworkflow.DeliveryBatch, domainworkflow.Run, error)
	ListDeliveryBatchIDs(context.Context, string, string, string, int) ([]string, error)
	ClaimManagedRun(context.Context, string, time.Duration) (domainworkflow.Run, error)
	SaveManagedRun(context.Context, domainworkflow.Run, bool) (domainworkflow.Run, error)
	StopManagedRun(context.Context, string, string, string) (domainworkflow.Run, error)
}

// DeliveryRuntime connects existing build/plan/execution services to Run nodes.
// It does not schedule work or own a second set of execution records.
type DeliveryRuntime interface {
	AssessDeliveryTarget(context.Context, domainidentity.Principal, domainworkflow.Run, domainworkflow.DeliveryTargetSnapshot, sohaapi.DeliveryBatchAssessmentInput) (sohaapi.DeliveryBatchAssessment, error)
	ResolveDeliveryTargetScopes(context.Context, domainidentity.Principal, domainworkflow.DeliveryTargetSnapshot) ([]map[string]string, error)
	ValidateDeliveryTarget(context.Context, domainidentity.Principal, domainworkflow.DeliveryTargetInput) error
	FreezeDeliveryTarget(context.Context, domainidentity.Principal, domainworkflow.DeliveryTargetInput) (domainworkflow.DeliveryTargetSnapshot, error)
	ExecuteDeliveryStage(context.Context, domainidentity.Principal, domainworkflow.Run, domainworkflow.DeliveryBatch, domainworkflow.NodeRun) (domainworkflow.NodeRun, error)
	CancelDeliveryStage(context.Context, domainworkflow.Run, domainworkflow.DeliveryBatch, domainworkflow.NodeRun) (domainworkflow.NodeRun, error)
}

type DeliveryPrincipalReader interface {
	CurrentExecutionPrincipal(context.Context, string, string) (domainidentity.Principal, error)
}

func (s *Service) SetDeliveryRuntime(runtime DeliveryRuntime, principals DeliveryPrincipalReader) {
	s.deliveryRuntime, s.deliveryPrincipals = runtime, principals
	s.configureManagedPoll()
}

func (s *Service) configureManagedPoll() {
	if (s.deliveryRuntime != nil || s.capabilityRuntime != nil) && s.deliveryPrincipals != nil {
		owner := uuid.NewString()
		s.scheduler.configurePoll(func(ctx context.Context) (dagRunTask, bool) {
			return s.claimDeliveryRun(ctx, owner)
		}, time.Second)
	}
}

func (s *Service) deliveryRepository() (DeliveryRepository, error) {
	repo, ok := s.repo.(DeliveryRepository)
	if !ok {
		return nil, fmt.Errorf("%w: durable delivery repository is not configured", apperrors.ErrInvalidArgument)
	}
	return repo, nil
}

func (s *Service) ListDeliveryWorkflows(ctx context.Context, principal domainidentity.Principal) ([]domainworkflow.DeliveryWorkflow, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowsView); err != nil {
		return nil, err
	}
	repo, err := s.deliveryRepository()
	if err != nil {
		return nil, err
	}
	items, err := repo.ListDeliveryWorkflows(ctx)
	if err != nil {
		return nil, err
	}
	visible := []domainworkflow.DeliveryWorkflow{}
	for _, item := range items {
		if s.authorizeDeliveryTargets(ctx, principal, item.Definition.Targets, domainaccess.ActionView) == nil {
			visible = append(visible, item)
		}
	}
	return visible, nil
}

func (s *Service) GetDeliveryWorkflow(ctx context.Context, principal domainidentity.Principal, id string) (domainworkflow.DeliveryWorkflow, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowsView); err != nil {
		return domainworkflow.DeliveryWorkflow{}, err
	}
	repo, err := s.deliveryRepository()
	if err != nil {
		return domainworkflow.DeliveryWorkflow{}, err
	}
	item, err := repo.GetDeliveryWorkflow(ctx, id)
	if err != nil {
		return item, err
	}
	if err := s.authorizeDeliveryTargets(ctx, principal, item.Definition.Targets, domainaccess.ActionView); err != nil {
		return item, err
	}
	return s.scopedDeliveryWorkflow(ctx, principal, item)
}

func (s *Service) SaveDeliveryWorkflow(ctx context.Context, principal domainidentity.Principal, id string, input domainworkflow.DeliveryWorkflowInput) (domainworkflow.DeliveryWorkflow, error) {
	if input.IdempotencyKey != "" {
		if id != "" {
			return domainworkflow.DeliveryWorkflow{}, fmt.Errorf("%w: idempotencyKey is only supported for workflow creation", apperrors.ErrInvalidArgument)
		}
		return s.createDeliveryWorkflowIdempotent(ctx, principal, input)
	}
	input, err := s.PrepareDeliveryWorkflow(ctx, principal, id, input)
	if err != nil {
		return domainworkflow.DeliveryWorkflow{}, err
	}
	repo, err := s.deliveryRepository()
	if err != nil {
		return domainworkflow.DeliveryWorkflow{}, err
	}
	expected := int64(0)
	if id == "" {
		id = uuid.NewString()
	} else {
		expected = *input.ExpectedVersion
	}
	item, err := s.scopedDeliveryWorkflow(ctx, principal, domainworkflow.DeliveryWorkflow{ID: id, Definition: input.Definition, CreatedBy: principal.UserID})
	if err != nil {
		return item, err
	}
	saved, err := repo.SaveDeliveryWorkflow(ctx, item, expected)
	saved.InvocationScopes = item.InvocationScopes
	return saved, err
}

// PrepareDeliveryWorkflow shares the save boundary with transactional document
// import. It validates and authorizes the definition without saving or running it.
func (s *Service) PrepareDeliveryWorkflow(ctx context.Context, principal domainidentity.Principal, id string, input domainworkflow.DeliveryWorkflowInput) (domainworkflow.DeliveryWorkflowInput, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowsTrigger); err != nil {
		return input, err
	}
	repo, err := s.deliveryRepository()
	if err != nil {
		return input, err
	}
	definition, _, err := s.resolveDeliveryRecipe(ctx, principal, input.Definition)
	if err != nil {
		return input, err
	}
	definition, err = normalizeDeliveryDefinition(definition)
	if err != nil {
		return input, err
	}
	if err := s.authorizeDeliveryTargets(ctx, principal, definition.Targets, domainaccess.ActionTrigger); err != nil {
		return input, err
	}
	if s.deliveryRuntime == nil {
		return input, fmt.Errorf("%w: delivery target validation is unavailable", apperrors.ErrInvalidArgument)
	}
	for index, target := range definition.Targets {
		if err := s.deliveryRuntime.ValidateDeliveryTarget(ctx, principal, target); err != nil {
			return input, &domainworkflow.TargetValidationError{Index: index, Err: err}
		}
	}
	if _, err := compileDeliveryDAG(definition, deliveryUnresolvedTargets(definition.Targets)); err != nil {
		return input, err
	}
	if id != "" {
		if input.ExpectedVersion == nil || *input.ExpectedVersion < 1 {
			return input, fmt.Errorf("%w: expectedVersion is required when updating a workflow", apperrors.ErrConflict)
		}
		existing, err := repo.GetDeliveryWorkflow(ctx, id)
		if err != nil {
			return input, err
		}
		if err := s.authorizeDeliveryTargets(ctx, principal, existing.Definition.Targets, domainaccess.ActionTrigger); err != nil {
			return input, err
		}
		if existing.Version != *input.ExpectedVersion {
			return input, fmt.Errorf("%w: workflow changed; preview again", apperrors.ErrConflict)
		}
	}
	input.Definition = definition
	return input, nil
}

func deliveryUnresolvedTargets(targets []domainworkflow.DeliveryTargetInput) []domainworkflow.DeliveryTargetSnapshot {
	out := make([]domainworkflow.DeliveryTargetSnapshot, len(targets))
	for i, target := range targets {
		out[i] = domainworkflow.DeliveryTargetSnapshot{Target: target, BuildNodeID: target.ID + ":build"}
	}
	return out
}

func (s *Service) authorizeDeliveryTargets(ctx context.Context, principal domainidentity.Principal, targets []domainworkflow.DeliveryTargetInput, action domainaccess.Action) error {
	for _, target := range targets {
		if err := s.authorizeDeliveryTarget(ctx, principal, target, action); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) authorizeDeliveryTarget(ctx context.Context, principal domainidentity.Principal, target domainworkflow.DeliveryTargetInput, action domainaccess.Action) error {
	app, err := s.apps.Get(ctx, target.ApplicationID)
	if err != nil {
		return err
	}
	if err := s.authorize(ctx, principal, action, app, target.ServiceID, target.ApplicationEnvironmentID, ""); err != nil {
		return err
	}
	if action != domainaccess.ActionTrigger {
		return nil
	}
	permissions := []string{}
	if target.Action == "build" || target.Action == "build_deploy" {
		permissions = append(permissions, appaccess.PermDeliveryBuildsTrigger)
	}
	if target.Action != "build" {
		permissions = append(permissions, appaccess.PermDeliveryReleasesTrigger)
	}
	for _, permission := range permissions {
		if err := s.authorizePermission(ctx, principal, permission); err != nil {
			return err
		}
		if err := s.authorize(ctx, principal, action, app, target.ServiceID, target.ApplicationEnvironmentID, permission); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) CreateDeliveryBatch(ctx context.Context, principal domainidentity.Principal, input domainworkflow.DeliveryBatchInput) (domainworkflow.DeliveryBatch, error) {
	if err := s.validateDeliveryBatchActor(ctx, principal, input); err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	repo, err := s.deliveryRepository()
	if err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	digest, err := deliveryRequestDigest(input)
	if err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	existing, existingRun, err := repo.FindDeliveryBatch(ctx, principal.UserID, input.IdempotencyKey, digest)
	if err == nil {
		if err := s.authorizeDeliveryTargets(ctx, principal, existing.Definition.Targets, domainaccess.ActionView); err != nil {
			return domainworkflow.DeliveryBatch{}, err
		}
		return s.scopedDeliveryBatch(ctx, principal, existing, existingRun)
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		return domainworkflow.DeliveryBatch{}, err
	}
	definition, recipeDigest, err := s.prepareNewDeliveryBatch(ctx, principal, repo, input)
	if err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	targets, err := s.freezeDeliveryTargets(ctx, principal, definition)
	if err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	scopes, err := s.deliverySnapshotScopes(ctx, principal, targets)
	if err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	dag, err := compileDeliveryDAG(definition, targets)
	if err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	now := time.Now().UTC()
	batch := domainworkflow.DeliveryBatch{InvocationScopes: scopes, WorkflowTemplateDigest: recipeDigest, ID: uuid.NewString(), RootRunID: "workflow:" + uuid.NewString(), Definition: definition, Targets: targets, WorkflowID: input.WorkflowID, WorkflowVersion: input.WorkflowVersion, RetryOfBatchID: input.RetryOfBatchID, Status: "queued", CreatedBy: principal.UserID, CreatedAt: now, UpdatedAt: now}
	run := domainworkflow.Run{ID: batch.RootRunID, Scope: domainworkflow.ScopeDeliveryBatch, DeliveryBatchID: batch.ID, WorkflowName: definition.Name, Status: "queued", CreatedAt: now.Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339), Metadata: deliveryDAGMetadata(dag)}
	if principal.AccessTokenID != "" {
		run.Metadata["executionTokenId"] = principal.AccessTokenID
	}
	run.GatewayAuthorization, err = s.sealDeliveryGatewayAuthorization(ctx)
	if err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	if input.TriggerAuthorizerID != "" {
		run.Metadata["triggerAuthorizerId"] = input.TriggerAuthorizerID
		run.Metadata["triggerAuthorizerTokenId"] = input.TriggerAuthorizerTokenID
	}
	run = syncRunNodeState(run, dag, initializeNodeRuns(dag))
	batch, run, err = repo.CreateDeliveryBatch(ctx, batch, run, input.IdempotencyKey, digest)
	if err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	return s.scopedDeliveryBatch(ctx, principal, batch, run)
}

// PrepareDeliveryBatch resolves the complete authorized intent without freezing
// artifacts, creating a run, or invoking a provider. Existing keys use their receipt.
func (s *Service) PrepareDeliveryBatch(ctx context.Context, principal domainidentity.Principal, input domainworkflow.DeliveryBatchInput) (domainworkflow.DeliveryWorkflowDefinition, error) {
	if err := s.validateDeliveryBatchActor(ctx, principal, input); err != nil {
		return domainworkflow.DeliveryWorkflowDefinition{}, err
	}
	existing, err := s.FindDeliveryBatch(ctx, principal, input)
	if err == nil {
		return existing.Definition, nil
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		return domainworkflow.DeliveryWorkflowDefinition{}, err
	}
	repo, err := s.deliveryRepository()
	if err != nil {
		return domainworkflow.DeliveryWorkflowDefinition{}, err
	}
	definition, _, err := s.prepareNewDeliveryBatch(ctx, principal, repo, input)
	return definition, err
}

func (s *Service) prepareNewDeliveryBatch(ctx context.Context, principal domainidentity.Principal, repo DeliveryRepository, input domainworkflow.DeliveryBatchInput) (domainworkflow.DeliveryWorkflowDefinition, string, error) {
	definition, err := s.deliveryBatchDefinition(ctx, principal, repo, input)
	if err != nil {
		return domainworkflow.DeliveryWorkflowDefinition{}, "", err
	}
	definition, recipeDigest, err := s.resolveDeliveryRecipe(ctx, principal, definition)
	if err != nil {
		return domainworkflow.DeliveryWorkflowDefinition{}, "", err
	}
	definition, err = normalizeDeliveryDefinition(definition)
	if err != nil {
		return domainworkflow.DeliveryWorkflowDefinition{}, "", err
	}
	if err := s.authorizeDeliveryTargets(ctx, principal, definition.Targets, domainaccess.ActionTrigger); err != nil {
		return domainworkflow.DeliveryWorkflowDefinition{}, "", err
	}
	if _, err := compileDeliveryDAG(definition, deliveryUnresolvedTargets(definition.Targets)); err != nil {
		return domainworkflow.DeliveryWorkflowDefinition{}, "", err
	}
	return definition, recipeDigest, nil
}

func deliveryRequestDigest(input domainworkflow.DeliveryBatchInput) (string, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

// FindDeliveryBatch restores a trigger dispatch after a crash without creating work.
func (s *Service) FindDeliveryBatch(ctx context.Context, principal domainidentity.Principal, input domainworkflow.DeliveryBatchInput) (domainworkflow.DeliveryBatch, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowsView); err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	repo, err := s.deliveryRepository()
	if err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	digest, err := deliveryRequestDigest(input)
	if err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	batch, run, err := repo.FindDeliveryBatch(ctx, principal.UserID, input.IdempotencyKey, digest)
	if err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	if err := s.authorizeDeliveryTargets(ctx, principal, batch.Definition.Targets, domainaccess.ActionView); err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	return s.scopedDeliveryBatch(ctx, principal, batch, run)
}

func (s *Service) deliveryBatchDefinition(ctx context.Context, principal domainidentity.Principal, repo DeliveryRepository, input domainworkflow.DeliveryBatchInput) (domainworkflow.DeliveryWorkflowDefinition, error) {
	if (input.Definition == nil) == (input.WorkflowID == "") {
		return domainworkflow.DeliveryWorkflowDefinition{}, fmt.Errorf("%w: choose a versioned workflow or an inline definition", apperrors.ErrInvalidArgument)
	}
	var definition domainworkflow.DeliveryWorkflowDefinition
	if input.Definition != nil {
		definition = *input.Definition
	} else {
		item, err := repo.GetDeliveryWorkflow(ctx, input.WorkflowID)
		if err != nil {
			return definition, err
		}
		if input.WorkflowVersion != item.Version {
			return definition, fmt.Errorf("%w: delivery workflow version changed", apperrors.ErrConflict)
		}
		definition = item.Definition
	}
	if input.RetryOfBatchID != "" {
		previous, run, err := repo.GetDeliveryBatch(ctx, input.RetryOfBatchID)
		if err != nil {
			return definition, err
		}
		if err := s.authorizeDeliveryTargets(ctx, principal, previous.Definition.Targets, domainaccess.ActionView); err != nil {
			return definition, err
		}
		if !deliveryRunTerminal(run.Status) {
			return definition, fmt.Errorf("%w: previous batch must have confirmed terminal state before retry", apperrors.ErrConflict)
		}
	}
	return pinDeliverySourceCommit(definition, input)
}

func (s *Service) freezeDeliveryTargets(ctx context.Context, principal domainidentity.Principal, definition domainworkflow.DeliveryWorkflowDefinition) ([]domainworkflow.DeliveryTargetSnapshot, error) {
	ctx = appbuild.WithRepositoryRefCache(ctx)
	targets, builds := make([]domainworkflow.DeliveryTargetSnapshot, 0, len(definition.Targets)), map[string]string{}
	for _, target := range definition.Targets {
		snapshot, err := s.deliveryRuntime.FreezeDeliveryTarget(ctx, principal, target)
		if err != nil {
			return nil, fmt.Errorf("target %s: %w", target.ID, err)
		}
		if target.Action == "build" || target.Action == "build_deploy" {
			if snapshot.BuildFingerprint == "" || snapshot.BuildSourceID == "" {
				return nil, fmt.Errorf("%w: target %s has no verified build fingerprint", apperrors.ErrInvalidArgument, target.ID)
			}
			key := target.ApplicationID + ":" + snapshot.BuildSourceID + ":" + snapshot.BuildFingerprint
			if builds[key] == "" {
				builds[key] = target.ID + ":build"
			}
			snapshot.BuildNodeID = builds[key]
		}
		targets = append(targets, snapshot)
	}
	return targets, validateDeliveryResourceTargets(targets)
}

func deliveryDAGMetadata(dag dagWorkflowDefinition) map[string]any {
	nodes, edges := []map[string]any{}, []map[string]any{}
	for _, node := range dag.Nodes {
		nodes = append(nodes, map[string]any{"id": node.ID, "name": node.Name, "type": node.Type, "targetId": node.TargetID, "stage": node.Stage, "failurePolicy": node.FailurePolicy})
	}
	for _, edge := range dag.Edges {
		edges = append(edges, map[string]any{"id": edge.ID, "source": edge.Source, "target": edge.Target, "condition": edge.Condition})
	}
	return map[string]any{"mode": dag.Mode, "schemaVersion": dag.SchemaVersion, "nodes": nodes, "edges": edges}
}

func (s *Service) GetDeliveryBatch(ctx context.Context, principal domainidentity.Principal, id string) (domainworkflow.DeliveryBatch, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowsView); err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	repo, err := s.deliveryRepository()
	if err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	batch, run, err := repo.GetDeliveryBatch(ctx, id)
	if err != nil {
		return batch, err
	}
	visible := s.visibleDeliveryTargets(ctx, principal, batch, "", "")
	if len(visible) == 0 {
		return domainworkflow.DeliveryBatch{}, apperrors.ErrNotFound
	}
	visibleBatch := filterDeliveryBatch(batch, visible)
	batch.InvocationScopes, err = s.deliverySnapshotScopes(ctx, principal, visibleBatch.Targets)
	if err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	return filterDeliveryBatch(projectDeliveryBatch(batch, run), visible), nil
}

// AssessDeliveryBatch keeps the private frozen target in its owning domain and
// requires the complete batch scope, just like a capability invocation.
func (s *Service) AssessDeliveryBatch(ctx context.Context, principal domainidentity.Principal, input sohaapi.DeliveryBatchAssessmentInput) (sohaapi.DeliveryBatchAssessment, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowsView); err != nil {
		return sohaapi.DeliveryBatchAssessment{}, err
	}
	if s.deliveryRuntime == nil {
		return sohaapi.DeliveryBatchAssessment{}, apperrors.ErrClusterUnready
	}
	repo, err := s.deliveryRepository()
	if err != nil {
		return sohaapi.DeliveryBatchAssessment{}, err
	}
	batch, run, err := repo.GetDeliveryBatch(ctx, input.BatchID)
	if err != nil {
		return sohaapi.DeliveryBatchAssessment{}, err
	}
	if err := s.authorizeDeliveryTargets(ctx, principal, batch.Definition.Targets, domainaccess.ActionView); err != nil {
		return sohaapi.DeliveryBatchAssessment{}, err
	}
	if _, err := s.deliverySnapshotScopes(ctx, principal, batch.Targets); err != nil {
		return sohaapi.DeliveryBatchAssessment{}, err
	}
	for _, target := range batch.Targets {
		if target.Target.ID == input.TargetID {
			return s.deliveryRuntime.AssessDeliveryTarget(ctx, principal, run, target, input)
		}
	}
	return sohaapi.DeliveryBatchAssessment{}, apperrors.ErrNotFound
}

func (s *Service) CancelDeliveryBatch(ctx context.Context, principal domainidentity.Principal, id, reason string) (domainworkflow.DeliveryBatch, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowsTrigger); err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	repo, err := s.deliveryRepository()
	if err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	batch, run, err := repo.GetDeliveryBatch(ctx, id)
	if err != nil {
		return batch, err
	}
	if err := s.authorizeDeliveryTargets(ctx, principal, batch.Definition.Targets, domainaccess.ActionTrigger); err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	if _, err := s.deliverySnapshotScopes(ctx, principal, batch.Targets); err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	run, err = repo.StopManagedRun(ctx, run.ID, "user", strings.TrimSpace(reason))
	if err != nil {
		return domainworkflow.DeliveryBatch{}, err
	}
	return s.scopedDeliveryBatch(ctx, principal, batch, run)
}

func projectDeliveryBatch(batch domainworkflow.DeliveryBatch, run domainworkflow.Run) domainworkflow.DeliveryBatch {
	batch.Targets = slices.Clone(batch.Targets)
	for i := range batch.Targets {
		batch.Targets[i].HealthTimeoutSeconds = 0
		batch.Targets[i].FrozenBuild = nil
		batch.Targets[i].FrozenManifest = nil
		batch.Targets[i].FrozenHelm = nil
		batch.Targets[i].FrozenScope = nil
		batch.Targets[i].FrozenDocker = nil
		batch.Targets[i].FrozenHelmCiphertext = ""
		batch.Targets[i].FrozenReleaseTarget = nil
	}
	batch.Status, batch.StopReason, batch.StopSummary, batch.Nodes = run.Status, run.StopReason, run.StopSummary, run.NodeRuns
	batch.UpdatedAt, _ = time.Parse(time.RFC3339, run.UpdatedAt)
	services, builds := map[string]bool{}, map[string]bool{}
	for _, target := range batch.Targets {
		services[target.Target.ApplicationID+":"+target.Target.ServiceID] = true
	}
	for _, node := range batch.Nodes {
		if node.Stage == "build" && node.BuildRecordID != "" {
			builds[node.BuildRecordID] = true
		}
	}
	batch.ServiceCount, batch.TargetCount, batch.BuildCount = len(services), len(batch.Targets), len(builds)
	return batch
}

func deliveryRunTerminal(status string) bool {
	switch status {
	case "completed", "partially_completed", "failed", "canceled":
		return true
	default:
		return false
	}
}

func validateDeliveryResourceTargets(targets []domainworkflow.DeliveryTargetSnapshot) error {
	owners := map[string]string{}
	for _, target := range targets {
		for _, key := range target.ResourceKeys() {
			if previous, exists := owners[key]; exists {
				return fmt.Errorf("%w: targets %s and %s address the same Kubernetes resource", apperrors.ErrInvalidArgument, previous, target.Target.ID)
			}
			owners[key] = target.Target.ID
		}
	}
	return nil
}

func (s *Service) validateDeliveryBatchActor(ctx context.Context, principal domainidentity.Principal, input domainworkflow.DeliveryBatchInput) error {
	if strings.HasPrefix(principal.UserID, "service_account:") && principal.AccessTokenID == "" {
		return fmt.Errorf("%w: service account execution requires a token identity", apperrors.ErrUnauthorized)
	}
	if err := s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowsTrigger); err != nil {
		return err
	}
	if s.deliveryRuntime == nil || s.deliveryPrincipals == nil {
		return fmt.Errorf("%w: delivery runtime is not configured", apperrors.ErrInvalidArgument)
	}
	if principal.UserID == "" || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 128 {
		return fmt.Errorf("%w: actor and an 8–128 character idempotency key are required", apperrors.ErrInvalidArgument)
	}
	return nil
}
