package aigateway

import (
	"context"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func (s *Service) authorizeApprovalReplayCaller(ctx context.Context, principal domainidentity.Principal, request domainaigateway.ApprovalRequest, tool domainaigateway.ToolCapability) error {
	if err := s.authorizeGovernedAICaller(ctx, principal, request.AIClientID, request.SkillID); err != nil {
		return err
	}
	if err := s.authorizeGovernedAITool(principal, tool); err != nil {
		return err
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayInvoke); err != nil {
		return err
	}
	return s.authorizeTool(ctx, principal, tool)
}
