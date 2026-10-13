package copilot

import (
	"context"
	"maps"
	"strings"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/redaction"
)

func (s *Service) currentChatSummary(ctx context.Context, principal domainidentity.Principal, sessionID string, metadata domaincopilot.SessionMetadata) (string, []domainknowledge.Citation, string) {
	if strings.TrimSpace(metadata.Summary) == "" {
		return "", nil, ""
	}
	ids := agentPrincipalStringList(metadata.PinnedContext["summarySourceMessageIds"])
	if len(ids) == 0 || len(ids) > 20 || s.sessions == nil || s.messages == nil {
		return "", nil, "session.summary:source_lineage_unavailable"
	}
	lineage := []domainknowledge.Citation{}
	if _, _, _, err := s.readChatSessionReference(ctx, principal, domaincopilot.ContextReference{Kind: "session", SessionID: sessionID, MessageIDs: ids}, &lineage); err != nil {
		return "", nil, "session.summary:source_unavailable"
	}
	text := []rune(redaction.Text(metadata.Summary))
	if len(text) > 4000 {
		return string(text[:4000]), lineage, "session.summary:maxInputTokens"
	}
	return string(text), lineage, ""
}

func bindChatSummarySource(metadata domaincopilot.SessionMetadata, patch map[string]any, messageID string) domaincopilot.SessionMetadata {
	if summary, ok := patch["summary"].(string); ok && strings.TrimSpace(summary) != "" {
		metadata.Summary = strings.TrimSpace(summary)
		metadata.PinnedContext = maps.Clone(metadata.PinnedContext)
		if metadata.PinnedContext == nil {
			metadata.PinnedContext = map[string]any{}
		}
		metadata.PinnedContext["summarySourceMessageIds"] = []string{messageID}
		patch["pinnedContext"] = metadata.PinnedContext
	}
	return metadata
}

func (s *Service) visibleSessionSummary(ctx context.Context, principal domainidentity.Principal, session domaincopilot.Session) domaincopilot.Session {
	metadata := parseSessionMetadata(session.Metadata)
	if metadata.Summary == "" {
		return session
	}
	summary, _, _ := s.currentChatSummary(ctx, principal, session.ID, metadata)
	session.Metadata = maps.Clone(session.Metadata)
	delete(session.Metadata, "summary")
	if summary != "" {
		session.Metadata["summary"] = summary
	}
	return session
}

func (s *Service) visibleSessionPatch(ctx context.Context, principal domainidentity.Principal, sessionID string, patch map[string]any) map[string]any {
	if stringValue(patch["summary"]) == "" {
		return patch
	}
	visible := maps.Clone(patch)
	delete(visible, "summary")
	session, err := s.ownedSession(ctx, principal, sessionID)
	if err == nil && stringValue(session.Metadata["summary"]) == stringValue(patch["summary"]) {
		if summary := stringValue(s.visibleSessionSummary(ctx, principal, session).Metadata["summary"]); summary != "" {
			visible["summary"] = summary
		}
	}
	return visible
}
