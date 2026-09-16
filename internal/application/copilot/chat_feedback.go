package copilot

import (
	"context"
	"fmt"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) ChatFeedbackReference(ctx context.Context, principal domainidentity.Principal, sessionID, messageID string) (string, error) {
	session, err := s.GetSession(ctx, principal, sessionID)
	if err != nil {
		return "", err
	}
	reader, ok := s.messages.(interface {
		GetMessage(context.Context, string, string) (domaincopilot.Message, error)
	})
	if !ok {
		return "", fmt.Errorf("%w: message feedback unavailable", apperrors.ErrUnsupportedOperation)
	}
	message, err := reader.GetMessage(ctx, session.ID, messageID)
	if err != nil {
		return "", err
	}
	if message.Role != "assistant" && message.Role != "system" {
		return "", fmt.Errorf("%w: feedback requires an assistant result", apperrors.ErrInvalidArgument)
	}
	runID := stringValue(message.Metadata["agentRunId"])
	if runID == "" {
		return "", fmt.Errorf("%w: result has no safe run reference", apperrors.ErrInvalidArgument)
	}
	run, err := s.agentRuns.GetAgentRun(ctx, principal.UserID, runID)
	if err != nil {
		return "", err
	}
	if run.SessionID != session.ID || run.CreatedBy != principal.UserID {
		return "", apperrors.ErrAccessDenied
	}
	return run.ID, nil
}
