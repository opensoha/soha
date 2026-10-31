package resource

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	contractresource "github.com/opensoha/soha-contracts/resource"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const gatewayPodExecTool = "k8s.pods.exec"
const gatewayPodExecReceiptTool = "k8s.pods.exec.receipt"
const gatewayPodExecAssessmentTool = "k8s.pods.exec.assess"

// GatewayPodExecutions binds accepted Gateway provenance to the existing native
// one-attempt executor. It has no repository, provider or execution queue.
type GatewayPodExecutions struct {
	workloads *Workloads
	resolve   func(context.Context, domainidentity.Principal) (domainidentity.Principal, error)
	authorize func(context.Context, domainidentity.Principal, domainai.ExecutionAuthorization) error
}

func NewGatewayPodExecutions(workloads *Workloads, resolve func(context.Context, domainidentity.Principal) (domainidentity.Principal, error), authorize func(context.Context, domainidentity.Principal, domainai.ExecutionAuthorization) error) (*GatewayPodExecutions, error) {
	if workloads == nil || resolve == nil || authorize == nil {
		return nil, fmt.Errorf("%w: Gateway Pod execution dependencies are required", apperrors.ErrInvalidArgument)
	}
	return &GatewayPodExecutions{workloads: workloads, resolve: resolve, authorize: authorize}, nil
}

type gatewayPodExecutionContextKey struct{}

type gatewayPodExecutionBinding struct {
	gateway       *GatewayPodExecutions
	authorization domainai.ExecutionAuthorization
	input         contractresource.PodArgvToolInput
}

func (g *GatewayPodExecutions) ExecutePodArgv(ctx context.Context, principal domainidentity.Principal, input contractresource.PodArgvToolInput) (contractresource.PodExecutionReceipt, error) {
	var empty contractresource.PodExecutionReceipt
	input, err := input.Normalized()
	if err != nil {
		return empty, apperrors.ErrInvalidArgument
	}
	authorization, err := g.acceptedAuthorization(ctx, principal, gatewayPodExecTool)
	if err != nil {
		return empty, err
	}
	var accepted contractresource.PodArgvToolInput
	if !decodeGatewayPodInput(authorization.Call.Input, &accepted) {
		return empty, apperrors.ErrAccessDenied
	}
	accepted, err = accepted.Normalized()
	if err != nil || !reflect.DeepEqual(accepted, input) {
		return empty, apperrors.ErrAccessDenied
	}
	binding := gatewayPodExecutionBinding{gateway: g, authorization: authorization, input: accepted}
	ctx = context.WithValue(ctx, gatewayPodExecutionContextKey{}, binding)
	return g.workloads.ExecutePodArgv(ctx, principal, domainidentity.AccessContext{}, input.ClusterID, input.Namespace, input.PodName, input.IdempotencyKey, input.Execution)
}

func (g *GatewayPodExecutions) GetPodExecution(ctx context.Context, principal domainidentity.Principal, input contractresource.PodExecutionLookupInput) (contractresource.PodExecutionReceipt, error) {
	return g.readPodExecution(ctx, principal, input, nil)
}

func (g *GatewayPodExecutions) FindOriginalPodExecution(ctx context.Context, principal domainidentity.Principal, original contractresource.PodArgvToolInput) (contractresource.PodExecutionReceipt, error) {
	normalized, err := original.Normalized()
	if err != nil {
		return contractresource.PodExecutionReceipt{}, apperrors.ErrInvalidArgument
	}
	lookup := contractresource.PodExecutionLookupInput{ClusterID: normalized.ClusterID, Namespace: normalized.Namespace, PodName: normalized.PodName, IdempotencyKey: normalized.IdempotencyKey}
	return g.readPodExecution(ctx, principal, lookup, &normalized)
}

func (g *GatewayPodExecutions) readPodExecution(ctx context.Context, principal domainidentity.Principal, input contractresource.PodExecutionLookupInput, original *contractresource.PodArgvToolInput) (contractresource.PodExecutionReceipt, error) {
	var empty contractresource.PodExecutionReceipt
	if input.Validate() != nil {
		return empty, apperrors.ErrInvalidArgument
	}
	tool := gatewayPodExecReceiptTool
	if original, ok := domainai.ExecutionAuthorizationFrom(ctx); ok && original.Call.ToolName == gatewayPodExecAssessmentTool {
		tool = gatewayPodExecAssessmentTool
	}
	authorization, err := g.acceptedAuthorization(ctx, principal, tool)
	if err != nil {
		return empty, err
	}
	var accepted contractresource.PodExecutionLookupInput
	if !decodeGatewayPodInput(authorization.Call.Input, &accepted) || accepted.Validate() != nil || accepted != input {
		return empty, apperrors.ErrAccessDenied
	}
	current, err := g.currentPrincipal(ctx, principal, authorization)
	if err != nil {
		return empty, err
	}
	var item contractresource.PodExecutionReceipt
	if original != nil {
		item, err = g.workloads.FindOriginalPodExecution(ctx, current, input.ClusterID, input.Namespace, input.PodName, input.IdempotencyKey, original.Execution)
	} else {
		item, err = g.workloads.GetPodExecution(ctx, current, input.ClusterID, input.Namespace, input.PodName, input.ExecutionID, input.IdempotencyKey)
	}
	if err != nil {
		return empty, err
	}
	if _, err := g.currentPrincipal(ctx, principal, authorization); err != nil {
		return empty, err
	}
	return item, nil
}

// ResolvePodExecutionScope is read-only admission before an approval exists.
// It proves the current credential and domain scope without claiming a process.
func (g *GatewayPodExecutions) ResolvePodExecutionScope(ctx context.Context, principal domainidentity.Principal, input contractresource.PodExecutionLookupInput) (map[string]string, error) {
	if input.Validate() != nil {
		return nil, apperrors.ErrInvalidArgument
	}
	if g == nil || g.workloads == nil || g.resolve == nil {
		return nil, apperrors.ErrUnsupportedOperation
	}
	current, err := g.resolvePrincipal(ctx, principal)
	if err != nil {
		return nil, err
	}
	connection, _, err := g.workloads.authorize(ctx, current, input.ClusterID, input.Namespace, "Pod", domainaccess.ActionExec)
	if err != nil {
		return nil, err
	}
	return map[string]string{"clusterId": connection.Summary.ID, "namespace": input.Namespace, "podName": input.PodName}, nil
}

func (g *GatewayPodExecutions) acceptedAuthorization(ctx context.Context, principal domainidentity.Principal, tool string) (domainai.ExecutionAuthorization, error) {
	var empty domainai.ExecutionAuthorization
	if g == nil || g.workloads == nil || g.resolve == nil || g.authorize == nil {
		return empty, apperrors.ErrUnsupportedOperation
	}
	if principal.UserID == "" || principal.AccessTokenID == "" {
		return empty, fmt.Errorf("%w: credential-bound Gateway Pod execution is required", apperrors.ErrUnsupportedOperation)
	}
	authorization, ok := domainai.ExecutionAuthorizationFrom(ctx)
	if !ok || authorization.ActorID != principal.UserID || authorization.ActorTokenID == "" || authorization.ActorTokenID != principal.AccessTokenID || authorization.Call.ToolName != tool || authorization.Call.CapabilityVersion != "1" {
		return empty, apperrors.ErrAccessDenied
	}
	if tool == gatewayPodExecTool && authorization.ApprovalID == "" {
		return empty, apperrors.ErrAccessDenied
	}
	// Freeze the accepted maps before installing a private execution context.
	// There is no bearer secret here; argv/stdin stay in memory or in the
	// existing encrypted execution/approval storage, never in audit metadata.
	raw, err := json.Marshal(authorization)
	if err != nil || len(raw) > 2<<20 || json.Unmarshal(raw, &empty) != nil {
		return domainai.ExecutionAuthorization{}, apperrors.ErrAccessDenied
	}
	empty.Call.SessionID = authorization.Call.SessionID
	return empty, nil
}

func (g *GatewayPodExecutions) currentPrincipal(ctx context.Context, original domainidentity.Principal, authorization domainai.ExecutionAuthorization) (domainidentity.Principal, error) {
	current, err := g.resolvePrincipal(ctx, original)
	if err != nil {
		return domainidentity.Principal{}, err
	}
	if current.UserID != authorization.ActorID || current.AccessTokenID != authorization.ActorTokenID || (current.GovernedAI == nil) != (original.GovernedAI == nil) {
		return domainidentity.Principal{}, apperrors.ErrAccessDenied
	}
	if err := g.authorize(ctx, current, authorization); err != nil {
		return domainidentity.Principal{}, err
	}
	return current, nil
}

func (g *GatewayPodExecutions) resolvePrincipal(ctx context.Context, original domainidentity.Principal) (domainidentity.Principal, error) {
	if original.UserID == "" || original.AccessTokenID == "" {
		return domainidentity.Principal{}, apperrors.ErrUnsupportedOperation
	}
	current, err := g.resolve(ctx, original)
	if err != nil {
		return domainidentity.Principal{}, err
	}
	if current.UserID != original.UserID || current.AccessTokenID != original.AccessTokenID || (current.GovernedAI == nil) != (original.GovernedAI == nil) {
		return domainidentity.Principal{}, apperrors.ErrAccessDenied
	}
	return current, nil
}

func decodeGatewayPodInput(input map[string]any, output any) bool {
	raw, err := json.Marshal(input)
	return err == nil && len(raw) <= 2<<20 && json.Unmarshal(raw, output) == nil
}

func (w *Workloads) validateGatewayPodExecution(ctx context.Context, principal domainidentity.Principal, cluster, namespace, pod, key string, input contractresource.PodArgvInput) error {
	binding, ok := ctx.Value(gatewayPodExecutionContextKey{}).(gatewayPodExecutionBinding)
	if !ok {
		return nil
	}
	expected := binding.input
	if binding.gateway.workloads != w || binding.authorization.ActorID != principal.UserID || binding.authorization.ActorTokenID != principal.AccessTokenID || expected.ClusterID != cluster || expected.Namespace != namespace || expected.PodName != pod || expected.IdempotencyKey != key || !reflect.DeepEqual(expected.Execution, input) {
		return apperrors.ErrAccessDenied
	}
	return nil
}

func (w *Workloads) currentPodExecutionPrincipal(ctx context.Context, original domainidentity.Principal, access domainidentity.AccessContext) (domainidentity.Principal, error) {
	if binding, ok := ctx.Value(gatewayPodExecutionContextKey{}).(gatewayPodExecutionBinding); ok {
		if binding.gateway.workloads != w || original.UserID != binding.authorization.ActorID || original.AccessTokenID != binding.authorization.ActorTokenID {
			return domainidentity.Principal{}, apperrors.ErrAccessDenied
		}
		return binding.gateway.currentPrincipal(ctx, original, binding.authorization)
	}
	return w.executionIdentity.CurrentStreamingPrincipal(ctx, original, access)
}
