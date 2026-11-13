package resource

import (
	"context"
	"encoding/json"

	contractresource "github.com/opensoha/soha/contracts/resource"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"sigs.k8s.io/yaml"
)

const conditionalScaleAssessmentTool = "k8s.workloads.scale.assess"

type conditionalScaleAssessmentInput struct {
	conditionalAssessmentInput
	Preconditions contractresource.ResourceMutationPreconditions `json:"preconditions"`
	Replicas      *int32                                         `json:"replicas"`
}

// This separate assessment proves a known original scale acknowledgment and
// current desired replicas on that UID. It does not redefine exact-revision
// assessment or claim rollout readiness, health or uninterrupted state.
func (g *GatewayConditionalMutations) AssessOriginalWorkloadScale(ctx context.Context, principal domainidentity.Principal, call domainai.ToolInvocationRequest) (domainai.CapabilityAssessment, error) {
	var input conditionalScaleAssessmentInput
	raw, err := json.Marshal(call.Input)
	if err != nil || len(raw) > 4096 || json.Unmarshal(raw, &input) != nil || input.Preconditions.Validate() != nil || input.Replicas == nil || *input.Replicas < 0 || (input.Kind != "Deployment" && input.Kind != "StatefulSet") {
		return domainai.CapabilityAssessment{}, apperrors.ErrInvalidArgument
	}
	observe := func(ctx context.Context, actor domainidentity.Principal, target conditionalAssessmentInput, record domainresource.ConditionalMutationRecord) (bool, string) {
		if !approvedScaleInputMatches(actor, input, record) {
			return false, "Requested scale assessment does not match the original approved input and acknowledgment"
		}
		return g.observeApprovedScale(ctx, actor, input)
	}
	return g.assessOriginalConditionalEffect(ctx, principal, call, conditionalScaleAssessmentTool, observe)
}

func approvedScaleInputMatches(principal domainidentity.Principal, input conditionalScaleAssessmentInput, record domainresource.ConditionalMutationRecord) bool {
	original := record.Authorization.Call
	if record.Authorization.ApprovalID == "" || original.CapabilityVersion != "1" || original.RequestID != input.MutationRequestID {
		return false
	}
	// Reconstruct the small typed scale input and compare the original hash.
	// The receipt deliberately stores no YAML or other original tool input.
	original.Input = map[string]any{"clusterId": input.ClusterID, "namespace": input.Namespace, "kind": input.Kind, "name": input.Name,
		"replicas": *input.Replicas, "preconditions": map[string]any{
			"expectedUid": input.Preconditions.ExpectedUID, "expectedResourceVersion": input.Preconditions.ExpectedResourceVersion}}
	switch original.ToolName {
	case "k8s.workloads.conditional.action":
		original.Input["action"] = "scale"
	case "k8s.workloads.scale":
	default:
		return false
	}
	keyHash, inputHash, err := conditionalMutationHashes(principal, original)
	if err != nil || keyHash != record.KeyHash || inputHash != record.InputHash {
		return false
	}
	raw, err := json.Marshal(record.Output)
	var receipt contractresource.ConditionalWorkloadActionReceipt
	if err != nil || json.Unmarshal(raw, &receipt) != nil {
		return false
	}
	return conditionalWorkloadReceiptMatches(receipt, conditionalMutationTarget{Kind: input.Kind, Namespace: input.Namespace, Name: input.Name,
		Action: "scale", Workload: contractresource.ConditionalWorkloadActionInput{Preconditions: input.Preconditions, Replicas: input.Replicas}})
}

func (g *GatewayConditionalMutations) observeApprovedScale(ctx context.Context, principal domainidentity.Principal, input conditionalScaleAssessmentInput) (bool, string) {
	view, err := g.GetResourceYAML(ctx, principal, input.ClusterID, input.Namespace, input.Kind, input.Name)
	if err != nil || len(view.Content) > 8<<20 {
		return false, "Current original workload desired replicas could not be read"
	}
	var object struct {
		Kind     string `json:"kind"`
		Metadata struct {
			Name, Namespace, UID, ResourceVersion string
			DeletionTimestamp                     *string `json:"deletionTimestamp"`
		} `json:"metadata"`
		Spec struct {
			Replicas *int32 `json:"replicas"`
		} `json:"spec"`
	}
	if yaml.Unmarshal([]byte(view.Content), &object) != nil || object.Kind != input.Kind || object.Metadata.Name != input.Name || object.Metadata.Namespace != input.Namespace || object.Metadata.UID != input.Preconditions.ExpectedUID || object.Metadata.ResourceVersion == "" || object.Metadata.DeletionTimestamp != nil || object.Spec.Replicas == nil || *object.Spec.Replicas != *input.Replicas {
		return false, "The original UID and approved desired replicas are not currently confirmed"
	}
	return true, "The original acknowledged scale is known applied and its original UID currently has the approved desired replicas; rollout readiness, health and uninterrupted state were not assessed"
}
