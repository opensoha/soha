package resource

import (
	"context"
	"encoding/json"

	contractresource "github.com/opensoha/soha/contracts/resource"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"sigs.k8s.io/yaml"
)

type conditionalMutationTarget struct {
	ClusterID, Namespace, Kind, Name string
	Action                           domainaccess.Action
	Tool                             string
	YAML                             contractresource.ConditionalResourceYAMLInput
	Workload                         contractresource.ConditionalWorkloadActionInput
	ConnectionHash                   string
}

func (g *GatewayConditionalMutations) authorizeConditionalMutation(ctx context.Context, principal domainidentity.Principal, call domainai.ToolInvocationRequest) (domainidentity.Principal, conditionalMutationTarget, error) {
	var empty conditionalMutationTarget
	if principal.UserID == "" || conditionalCredentialID(principal, call) == "" {
		return principal, empty, apperrors.ErrUnsupportedOperation
	}
	current, err := g.resolve(ctx, principal, call.SessionID)
	if err != nil {
		return current, empty, err
	}
	if current.UserID != principal.UserID || current.AccessTokenID != principal.AccessTokenID || (current.GovernedAI == nil) != (principal.GovernedAI == nil) {
		return current, empty, apperrors.ErrAccessDenied
	}
	target, err := prepareConditionalMutation(call)
	if err != nil {
		return current, empty, err
	}
	connection, _, err := g.authorize(ctx, current, target.ClusterID, target.Namespace, target.Kind, target.Action)
	if err != nil {
		return current, empty, err
	}
	if connection.Summary.ID != target.ClusterID {
		return current, empty, apperrors.ErrConflict
	}
	target.ConnectionHash, err = conditionalConnectionHash(connection)
	if err != nil {
		return current, empty, err
	}
	if target.Kind == "Secret" && target.Action == domainaccess.ActionUpdate {
		if err := g.authorizeRuntimePermission(ctx, current, appaccess.PermPlatformConfigurationSecretDataView); err != nil {
			return current, empty, err
		}
	}
	return current, target, nil
}

func prepareConditionalMutation(call domainai.ToolInvocationRequest) (conditionalMutationTarget, error) {
	var fields struct {
		ClusterID string `json:"clusterId"`
		Namespace string `json:"namespace"`
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Action    string `json:"action"`
	}
	raw, err := json.Marshal(call.Input)
	if err != nil || len(raw) > 5<<20 || json.Unmarshal(raw, &fields) != nil || call.CapabilityVersion != "1" {
		return conditionalMutationTarget{}, apperrors.ErrInvalidArgument
	}
	kind, err := conditionalResourceKind(fields.Namespace, fields.Kind, fields.Name)
	if err != nil {
		return conditionalMutationTarget{}, err
	}
	target := conditionalMutationTarget{ClusterID: fields.ClusterID, Namespace: fields.Namespace, Kind: kind, Name: fields.Name, Tool: call.ToolName}
	switch call.ToolName {
	case "k8s.resources.conditional.update":
		target.Action = domainaccess.ActionUpdate
		if json.Unmarshal(raw, &target.YAML) != nil {
			return target, apperrors.ErrInvalidArgument
		}
		_, err = validateConditionalResourceInput(target.Namespace, target.Kind, target.Name, target.YAML)
	case "k8s.resources.conditional.delete":
		target.Action = domainaccess.ActionDelete
		if json.Unmarshal(raw, &target.YAML) != nil {
			return target, apperrors.ErrInvalidArgument
		}
		err = target.YAML.Preconditions.Validate()
	case "k8s.workloads.conditional.action", "k8s.workloads.scale":
		target.Action = domainaccess.Action(fields.Action)
		if call.ToolName == "k8s.workloads.scale" {
			target.Action = domainaccess.Action("scale")
		}
		target.Workload, err = prepareConditionalWorkloadInput(raw, kind, string(target.Action))
	default:
		err = apperrors.ErrUnsupportedOperation
	}
	return target, err
}

func prepareConditionalWorkloadInput(raw []byte, kind, action string) (contractresource.ConditionalWorkloadActionInput, error) {
	var input contractresource.ConditionalWorkloadActionInput
	if json.Unmarshal(raw, &input) != nil || input.DryRun {
		return input, apperrors.ErrInvalidArgument
	}
	return input, input.Validate(kind, action)
}

func (g *GatewayConditionalMutations) executeConditionalMutation(ctx context.Context, principal domainidentity.Principal, target conditionalMutationTarget) (map[string]any, error) {
	var result any
	var err error
	switch target.Tool {
	case "k8s.resources.conditional.update":
		var view domainresource.ResourceYAMLView
		view, err = g.ApplyResourceYAMLConditional(ctx, principal, target.ClusterID, target.Namespace, target.Kind, target.Name, target.YAML)
		if err == nil {
			result, err = conditionalAppliedMetadata(view, target)
		}
	case "k8s.resources.conditional.delete":
		result, err = g.DeleteResourceConditional(ctx, principal, target.ClusterID, target.Namespace, target.Kind, target.Name, target.YAML.Preconditions)
	case "k8s.workloads.conditional.action", "k8s.workloads.scale":
		var receipt contractresource.ConditionalWorkloadActionReceipt
		receipt, err = g.ExecuteWorkloadActionConditional(ctx, principal, target.ClusterID, target.Namespace, target.Kind, target.Name, string(target.Action), target.Workload)
		if err == nil && !conditionalWorkloadReceiptMatches(receipt, target) {
			err = apperrors.ErrConflict
		}
		result = receipt
	default:
		err = apperrors.ErrUnsupportedOperation
	}
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	var safe map[string]any
	if err != nil || len(raw) > 32<<10 || json.Unmarshal(raw, &safe) != nil {
		return nil, apperrors.ErrConflict
	}
	return safe, nil
}

func conditionalWorkloadReceiptMatches(receipt contractresource.ConditionalWorkloadActionReceipt, target conditionalMutationTarget) bool {
	return receipt.Applied && !receipt.DryRun && receipt.Kind == target.Kind && receipt.Name == target.Name && receipt.Namespace == target.Namespace && receipt.Action == string(target.Action) && receipt.Preconditions == target.Workload.Preconditions && receipt.UID == target.Workload.Preconditions.ExpectedUID && receipt.ResourceVersion != ""
}

func conditionalConnectionHash(connection domaincluster.Connection) (string, error) {
	fingerprint, err := domaincluster.RuntimeConnectionFingerprint(connection)
	if err != nil {
		return "", apperrors.ErrConflict
	}
	return fingerprint, nil
}

func conditionalAppliedMetadata(view domainresource.ResourceYAMLView, target conditionalMutationTarget) (map[string]any, error) {
	var object struct {
		Kind     string                                                 `json:"kind"`
		Metadata struct{ Name, Namespace, UID, ResourceVersion string } `json:"metadata"`
	}
	if len(view.Content) > 8<<20 || yaml.Unmarshal([]byte(view.Content), &object) != nil || object.Kind != target.Kind || object.Metadata.Name != target.Name || object.Metadata.Namespace != target.Namespace || object.Metadata.UID != target.YAML.Preconditions.ExpectedUID || object.Metadata.ResourceVersion == "" {
		return nil, apperrors.ErrConflict
	}
	return map[string]any{"applied": true, "kind": target.Kind, "name": target.Name, "namespace": target.Namespace, "preconditions": target.YAML.Preconditions, "uid": object.Metadata.UID, "resourceVersion": object.Metadata.ResourceVersion}, nil
}
