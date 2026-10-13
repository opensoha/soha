package copilot

import (
	"context"
	"fmt"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// FindNativeRequest reads the original run without rebuilding today's model or context policy.
// Absence is not a dispatch receipt: an in-flight creation can still commit after this read.
func (s *Service) FindNativeRequest(ctx context.Context, principal domainidentity.Principal, sessionID, requestID string) (domaincopilot.AgentRun, error) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(requestID) == "" || len(requestID) > 128 {
		return domaincopilot.AgentRun{}, fmt.Errorf("%w: session and bounded request ID are required", apperrors.ErrInvalidArgument)
	}
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincopilot.AgentRun{}, err
	}
	if _, err := s.ownedSession(ctx, principal, sessionID); err != nil {
		return domaincopilot.AgentRun{}, err
	}
	items, err := s.agentRuns.ListAgentRuns(ctx, domaincopilot.AgentRunFilter{CreatedBy: principal.UserID, SessionID: sessionID, NativeRequestID: requestID, Limit: 1})
	if err != nil {
		return domaincopilot.AgentRun{}, err
	}
	if len(items) == 0 {
		return domaincopilot.AgentRun{}, fmt.Errorf("%w: Native request is not visible", apperrors.ErrNotFound)
	}
	run := items[0]
	run.CallbackToken = ""
	return domaincopilot.WithOperationState(s.visibleKnowledgeAgentRun(ctx, principal, run), time.Now().UTC()), nil
}
