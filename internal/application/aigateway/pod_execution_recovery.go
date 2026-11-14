package aigateway

import (
	"context"
	"errors"

	contractresource "github.com/opensoha/soha/contracts/resource"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (p *podExecutionCapabilityProvider) RecoverTool(ctx context.Context, principal domainidentity.Principal, tool domainai.ToolCapability, input map[string]any) (any, bool, error) {
	if tool.Name != protectedPodExecTool {
		return nil, false, nil
	}
	var original contractresource.PodArgvToolInput
	if mapInput(input, &original) != nil {
		return nil, false, apperrors.ErrInvalidArgument
	}
	original, err := original.Normalized()
	if err != nil {
		return nil, false, apperrors.ErrInvalidArgument
	}
	receipt, err := p.service.FindOriginalPodExecution(ctx, principal, original)
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return podGatewayOutput(receipt), true, nil
}
