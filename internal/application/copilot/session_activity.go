package copilot

import (
	"context"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func (s *Service) sessionActivities(ctx context.Context, principal domainidentity.Principal, sessions []domaincopilot.Session) []domaincopilot.Session {
	activity := map[string]string{}
	legacySessions := map[string]bool{}
	if s.agentRuns != nil {
		for _, status := range []string{"queued", "running"} {
			runs, err := s.agentRuns.ListAgentRuns(ctx, domaincopilot.AgentRunFilter{CreatedBy: principal.UserID, Status: status, Limit: 100})
			if err != nil {
				continue
			}
			for _, run := range runs {
				if run.CreatedBy == principal.UserID && run.ParentRunID == "" && run.Status == status {
					if legacyExecutionReadOnly(run) {
						legacySessions[run.SessionID] = true
						continue
					}
					activity[run.SessionID] = status
				}
			}
		}
	}
	if reader := s.runtime.Approvals; reader != nil {
		requests, err := reader.ListApprovalRequests(ctx, principal, domainaigateway.ApprovalRequestFilter{ActorType: "user", ActorID: principal.UserID, Status: "pending", Limit: 100})
		if err == nil {
			for _, request := range requests {
				if request.ActorID == principal.UserID && request.Status == "pending" && !(legacySessions[request.ActorSessionID] && activity[request.ActorSessionID] == "") {
					activity[request.ActorSessionID] = "waiting_approval"
				}
			}
		}
	}
	for index := range sessions {
		sessions[index].Activity = activity[sessions[index].ID]
	}
	return sessions
}

// Frozen execution records remain available for history and reconciliation.
func legacyExecutionReadOnly(run domaincopilot.AgentRun) bool {
	return run.ProviderKind == "native" || run.Output["executionReadOnly"] == true || run.Output["stopReason"] == "legacy_reconciliation_required"
}
