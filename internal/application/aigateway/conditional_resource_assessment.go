package aigateway

import (
	"context"

	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func conditionalMutationSemantic() domainai.CapabilityValueSemantic {
	return domainai.CapabilityValueSemantic{Path: "/mutationRequestId", Kind: "k8s.conditional_mutation", ScopePaths: map[string]string{"clusterId": "/clusterId", "namespace": "/namespace", "resourceKind": "/kind", "resourceName": "/name"}}
}

func configureConditionalMutationChecks(tool *domainai.ToolCapability) {
	tool.OutputSemantics = []domainai.CapabilityValueSemantic{conditionalMutationSemantic()}
	verification := "k8s.resources.conditional.assess"
	if tool.Name == conditionalWorkloadScaleTool {
		verification = workloadScaleAssessmentTool
	}
	tool.Execution.Checks = []domainai.CapabilityCheckReference{{Purpose: "precondition", ToolName: "k8s.resources.identity.read", CapabilityVersion: "1"}, {Purpose: "verification", ToolName: verification, CapabilityVersion: "1"}}
}

func (p *conditionalResourceCapabilityProvider) invokeConditionalAssessment(ctx context.Context, principal domainidentity.Principal, tool domainai.ToolCapability, input map[string]any) (any, map[string]any, error) {
	service, ok := p.service.(conditionalMutationAssessmentService)
	if !ok {
		return nil, nil, apperrors.ErrUnsupportedOperation
	}
	authorization, accepted := domainai.ExecutionAuthorizationFrom(ctx)
	if !accepted || authorization.Call.ToolName != tool.Name || !executionJSONEqual(authorization.Call.Input, input) {
		return nil, nil, apperrors.ErrAccessDenied
	}
	output, err := service.AssessOriginalConditionalMutation(ctx, principal, authorization.Call)
	return output, map[string]any{"clusterId": firstMapString(input, "clusterId"), "namespace": firstMapString(input, "namespace"), "resourceKind": firstMapString(input, "kind"), "resourceName": firstMapString(input, "name")}, err
}
