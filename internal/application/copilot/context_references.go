package copilot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainknowledge "github.com/opensoha/soha/internal/domain/knowledge"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/redaction"
)

func (s *Service) appendChatReferences(ctx context.Context, principal domainidentity.Principal, envelope *domaincopilot.ContextEnvelope, selection *domaincopilot.ContextSelection) error {
	if selection == nil {
		return nil
	}
	if len(selection.References) > 20 || len(selection.Attachments) > 8 {
		return fmt.Errorf("%w: too many context references or attachments", apperrors.ErrInvalidArgument)
	}
	if err := s.appendChatMemory(ctx, principal, envelope, selection.MemoryIDs); err != nil {
		return err
	}
	for _, attachment := range selection.Attachments {
		if err := validateChatAttachment(attachment); err != nil {
			return err
		}
	}
	for _, ref := range selection.References {
		content, title, uri, err := s.readChatReference(ctx, principal, ref)
		if err != nil {
			return err
		}
		appendChatEvidence(envelope, title, uri, content)
	}
	for _, attachment := range selection.Attachments {
		appendChatEvidence(envelope, attachment.Name, "attachment:"+url.PathEscape(attachment.ID), attachment.Content)
	}
	envelope.ContentHash = ""
	payload, _ := json.Marshal(envelope)
	hash := sha256.Sum256(payload)
	envelope.ContentHash = hex.EncodeToString(hash[:])
	return nil
}

func validateChatAttachment(attachment domaincopilot.TextAttachment) error {
	allowed := []string{".txt", ".log", ".md", ".json", ".yaml", ".yml", ".toml", ".ini", ".conf", ".cfg", ".csv", ".xml"}
	if strings.TrimSpace(attachment.ID) == "" || len(attachment.ID) > 128 || strings.TrimSpace(attachment.Name) == "" || len(attachment.Name) > 256 || len(attachment.Content) > 65536 || !utf8.ValidString(attachment.Content) || strings.ContainsRune(attachment.Content, 0) || !slices.Contains(allowed, strings.ToLower(filepath.Ext(attachment.Name))) {
		return fmt.Errorf("%w: attachment must be a supported UTF-8 text file up to 64 KiB", apperrors.ErrInvalidArgument)
	}
	return nil
}

func appendChatEvidence(envelope *domaincopilot.ContextEnvelope, title, uri, content string) {
	content = redaction.Text(content)
	identity := sha256.Sum256([]byte(uri + "\n" + content))
	id := fmt.Sprintf("ref:%x", identity[:12])
	remaining := max(0, envelope.Budgets.MaxEvidenceTokens-envelope.BudgetUsage.EvidenceTokens) * 4
	runes := []rune(content)
	if len(runes) > remaining {
		envelope.Truncations = append(envelope.Truncations, uri+":maxEvidenceTokens")
		runes = runes[:remaining]
	}
	if len(runes) == 0 {
		return
	}
	tokens := (len(runes) + 3) / 4
	hash := sha256.Sum256([]byte(string(runes)))
	envelope.Evidence = append(envelope.Evidence, domaincopilot.ContextEvidence{CitationID: id, Content: string(runes), TokenCount: tokens})
	envelope.Citations = append(envelope.Citations, domainknowledge.Citation{ID: id, DocumentTitle: title, URI: uri, ContentHash: hex.EncodeToString(hash[:])})
	envelope.BudgetUsage.EvidenceTokens += tokens
	envelope.BudgetUsage.EvidenceItems++
}

func (s *Service) readChatReference(ctx context.Context, principal domainidentity.Principal, ref domaincopilot.ContextReference) (content, title, uri string, err error) {
	if ref.Kind == "session" {
		return s.readChatSessionReference(ctx, principal, ref)
	}
	if strings.TrimSpace(ref.Name) == "" || len(ref.Name) > 256 || strings.TrimSpace(ref.ClusterID) == "" || (ref.Kind != "node" && strings.TrimSpace(ref.Namespace) == "") {
		return "", "", "", fmt.Errorf("%w: resource reference needs a cluster, namespace and name", apperrors.ErrInvalidArgument)
	}
	if s.resources == nil {
		return "", "", "", fmt.Errorf("%w: resource reader unavailable", apperrors.ErrUnsupportedOperation)
	}
	var items []map[string]any
	switch ref.Kind {
	case "pod":
		values, readErr := s.resources.ListPods(ctx, principal, ref.ClusterID, ref.Namespace)
		items, err = agentPodSummaries(values), readErr
	case "deployment":
		values, readErr := s.resources.ListDeployments(ctx, principal, ref.ClusterID, ref.Namespace)
		items, err = agentDeploymentSummaries(values), readErr
	case "service":
		values, readErr := s.resources.ListServices(ctx, principal, ref.ClusterID, ref.Namespace)
		items, err = agentServiceSummaries(values), readErr
	case "node":
		values, readErr := s.resources.ListNodes(ctx, principal, ref.ClusterID)
		items, err = agentNodeSummaries(values), readErr
	default:
		return "", "", "", fmt.Errorf("%w: unsupported resource reference", apperrors.ErrInvalidArgument)
	}
	if err != nil {
		return "", "", "", err
	}
	for _, item := range items {
		if stringValue(item["name"]) != ref.Name || (ref.Kind != "node" && stringValue(item["namespace"]) != ref.Namespace) {
			continue
		}
		data, marshalErr := json.Marshal(item)
		uri = "resource:" + url.PathEscape(ref.ClusterID) + "/" + url.PathEscape(ref.Namespace) + "/" + string(ref.Kind) + "/" + url.PathEscape(ref.Name)
		return string(data), string(ref.Kind) + "/" + ref.Name, uri, marshalErr
	}
	return "", "", "", fmt.Errorf("%w: referenced resource is unavailable", apperrors.ErrNotFound)
}

func (s *Service) readChatSessionReference(ctx context.Context, principal domainidentity.Principal, ref domaincopilot.ContextReference) (content, title, uri string, err error) {
	if strings.TrimSpace(ref.SessionID) == "" || len(ref.MessageIDs) == 0 || len(ref.MessageIDs) > 20 {
		return "", "", "", fmt.Errorf("%w: session reference needs 1 to 20 message IDs", apperrors.ErrInvalidArgument)
	}
	session, err := s.sessions.GetSession(ctx, principal.UserID, ref.SessionID)
	if err != nil {
		return "", "", "", err
	}
	if parseSessionMetadata(session.Metadata).ArchivedAt != "" {
		return "", "", "", fmt.Errorf("%w: referenced session is archived", apperrors.ErrNotFound)
	}
	var messages []domaincopilot.Message
	if reader, ok := s.messages.(interface {
		GetMessage(context.Context, string, string) (domaincopilot.Message, error)
	}); ok {
		for _, id := range ref.MessageIDs {
			message, err := reader.GetMessage(ctx, session.ID, id)
			if err != nil {
				return "", "", "", err
			}
			messages = append(messages, message)
		}
	} else {
		messages, err = s.listRecentMessages(ctx, session.ID, 100)
		if err != nil {
			return "", "", "", err
		}
	}
	var selected []map[string]string
	if len(normalizeStringList(ref.MessageIDs)) != len(ref.MessageIDs) {
		return "", "", "", fmt.Errorf("%w: message IDs must be unique and nonempty", apperrors.ErrInvalidArgument)
	}
	for _, id := range normalizeStringList(ref.MessageIDs) {
		index := slices.IndexFunc(messages, func(message domaincopilot.Message) bool {
			return message.ID == id && (message.Role == "user" || message.Role == "assistant")
		})
		if index < 0 {
			return "", "", "", fmt.Errorf("%w: referenced message is unavailable; refresh the reference", apperrors.ErrNotFound)
		}
		message := messages[index]
		selected = append(selected, map[string]string{"id": message.ID, "role": message.Role, "content": message.Content})
	}
	data, err := json.Marshal(selected)
	return string(data), session.Title, "session:" + url.PathEscape(session.ID), err
}
