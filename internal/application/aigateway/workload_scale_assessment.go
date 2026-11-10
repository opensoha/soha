package aigateway

import (
	"context"

	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const workloadScaleAssessmentTool = "k8s.workloads.scale.assess"
const conditionalWorkloadScaleTool = "k8s.workloads.scale"

type workloadScaleAssessmentService interface {
	AssessOriginalWorkloadScale(context.Context, domainidentity.Principal, domainai.ToolInvocationRequest) (domainai.CapabilityAssessment, error)
}

func appendWorkloadScaleAssessment(tools []domainai.ToolCapability, service ConditionalResourceCapabilityService, target, preconditions, output map[string]any) []domainai.ToolCapability {
	if _, ok := service.(workloadScaleAssessmentService); !ok {
		return tools
	}
	target["kind"] = map[string]any{"type": "string", "enum": []string{"Deployment", "StatefulSet"}}
	target["mutationRequestId"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 128}
	target["preconditions"] = preconditions
	target["replicas"] = map[string]any{"type": "integer", "minimum": 0, "maximum": 2147483647}
	tools = append(tools, domainai.ToolCapability{Name: "k8s.workloads.scale.assess", Title: "Verify Original Scale and Current Desired Replicas",
		Description: "Read only the original actor/credential-bound scale receipt and current original UID. Satisfied requires a known applied scale, the exact approved replica count and preconditions matching its frozen input hash, and current desired replicas on that UID. Later status revisions are allowed. Unknown writes, replacements and coincidental state without an acknowledgment are never success. Does not assess rollout readiness, health or uninterrupted state; exact acknowledged-revision verification remains a separate capability.",
		Action:      "assess", RiskLevel: domainai.RiskLevelRead, ProducesAssessment: true,
		InputSchema: gatewayObjectSchema([]string{"clusterId", "namespace", "kind", "name", "mutationRequestId", "preconditions", "replicas"}, target), OutputSchema: output})
	if _, ok := service.(conditionalOriginalMutationService); !ok {
		return tools
	}
	scale := make(map[string]any, len(target)-1)
	for name, property := range target {
		if name != "mutationRequestId" {
			scale[name] = property
		}
	}
	return append(tools, domainai.ToolCapability{Name: "k8s.workloads.scale", Title: "Request Reviewed Workload Scale",
		Description: "Request an independently approved scale of the original reviewed Deployment or StatefulSet UID/version. Claims the original actor/credential-bound request once; unknown outcomes never replay. Its declared verification proves the known original scale and current approved desired replicas, separately from acknowledged-revision and rollout readiness checks.",
		Action:      "execute", RiskLevel: domainai.RiskLevelHigh, RequiresApproval: true,
		InputSchema: gatewayObjectSchema([]string{"clusterId", "namespace", "kind", "name", "preconditions", "replicas"}, scale)})
}

func configureConditionalAssessmentSemantics(tool *domainai.ToolCapability) {
	if tool.Name == "k8s.resources.conditional.assess" || tool.Name == workloadScaleAssessmentTool {
		tool.InputSemantics = []domainai.CapabilityValueSemantic{conditionalMutationSemantic()}
	}
}

func (p *conditionalResourceCapabilityProvider) invokeWorkloadScaleAssessment(ctx context.Context, principal domainidentity.Principal, tool domainai.ToolCapability, input map[string]any) (any, map[string]any, error) {
	service, ok := p.service.(workloadScaleAssessmentService)
	if !ok {
		return nil, nil, apperrors.ErrUnsupportedOperation
	}
	authorization, accepted := domainai.ExecutionAuthorizationFrom(ctx)
	if !accepted || authorization.Call.ToolName != tool.Name || !executionJSONEqual(authorization.Call.Input, input) {
		return nil, nil, apperrors.ErrAccessDenied
	}
	output, err := service.AssessOriginalWorkloadScale(ctx, principal, authorization.Call)
	return output, map[string]any{"clusterId": firstMapString(input, "clusterId"), "namespace": firstMapString(input, "namespace"), "resourceKind": firstMapString(input, "kind"), "resourceName": firstMapString(input, "name")}, err
}
