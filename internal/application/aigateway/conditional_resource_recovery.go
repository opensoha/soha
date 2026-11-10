package aigateway

import (
	"context"
	"errors"

	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (p *conditionalResourceCapabilityProvider) RecoverOriginalTool(ctx context.Context, principal domainidentity.Principal, tool domainai.ToolCapability, call domainai.ToolInvocationRequest) (any, bool, error) {
	service, ok := p.service.(conditionalOriginalMutationService)
	if !ok || !conditionalMutationTool(tool.Name) {
		return nil, false, nil
	}
	if tool.Name != call.ToolName || call.RequestID == "" || validateCapabilityInput(tool, call.Input) != nil {
		return nil, false, apperrors.ErrInvalidArgument
	}
	output, err := service.FindOriginalConditionalMutation(ctx, principal, call)
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil, false, nil
	}
	return output, err == nil, err
}
