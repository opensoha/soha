package copilot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

// Only an independent, explicit user instruction is a profile mutation.
func personalAssistantNameForTurn(content, mode string, intent *domain.TaskIntent) (string, bool) {
	if mode != "general" || (intent != nil && len(intent.Capabilities) > 0) {
		return "", false
	}
	return personalAssistantNameRequest(content)
}

func personalAssistantNameRequest(content string) (string, bool) {
	content = strings.TrimSpace(content)
	if content == "/name" {
		return "", true
	}
	if name, ok := strings.CutPrefix(content, "/name "); ok {
		return strings.TrimSpace(name), true
	}
	if strings.ContainsAny(content, "\n\r?？!！,，;；") {
		return "", false
	}
	for _, prefix := range []string{"以后叫你", "以后你叫", "把你的名字改成", "把你的昵称改成", "把助手昵称改成", "把你的名字改为", "把你的名称改为", "把你的昵称改为", "把你改名为"} {
		if name, ok := strings.CutPrefix(strings.TrimPrefix(content, "请"), prefix); ok {
			name = strings.TrimSpace(strings.TrimSuffix(name, "。"))
			for _, quote := range [][2]string{{"“", "”"}, {"「", "」"}, {"\"", "\""}} {
				if strings.HasPrefix(name, quote[0]) && strings.HasSuffix(name, quote[1]) {
					name = strings.TrimSuffix(strings.TrimPrefix(name, quote[0]), quote[1])
				}
			}
			return name, true
		}
	}
	return "", false
}

func (s *Service) renameAssistantFromChat(ctx context.Context, principal domainidentity.Principal, session domain.Session, content, name, locale string) (domain.SessionMessageEnvelope, error) {
	user, err := s.messages.CreateMessage(ctx, domain.Message{ID: uuid.NewString(), SessionID: session.ID, Role: "user", Content: strings.TrimSpace(content), Metadata: map[string]any{"userId": principal.UserID, "locale": locale, "mode": "general"}, CreatedAt: time.Now().UTC()})
	if err != nil {
		return domain.SessionMessageEnvelope{}, err
	}
	item, err := s.RenamePersonalAssistant(ctx, principal, name, locale)
	if err != nil {
		return domain.SessionMessageEnvelope{}, err
	}
	now := time.Now().UTC()
	tool := domain.ToolExecution{ID: uuid.NewString(), AdapterID: "conversation", ToolName: "assistant.rename", Status: "success", Summary: "已更新助手昵称", StartedAt: now, CompletedAt: &now, Input: map[string]any{"name": item.Name}, Output: map[string]any{"assistantId": item.ID, "name": item.Name}}
	escaped := strings.NewReplacer("\\", "\\\\", "*", "\\*", "_", "\\_", "[", "\\[", "]", "\\]", "`", "\\`", "<", "\\<", ">", "\\>").Replace(item.Name)
	reply := fmt.Sprintf("好，以后叫我「%s」。昵称已保存，主聊天和旁聊都会使用这个名字。 ✅", escaped)
	if strings.HasPrefix(strings.ToLower(locale), "en") {
		reply = fmt.Sprintf("I'll go by %s. The saved name applies to your main and side chats. ✅", escaped)
	}
	assistant, err := s.messages.CreateMessage(ctx, domain.Message{ID: uuid.NewString(), SessionID: session.ID, Role: "assistant", Content: reply, Metadata: map[string]any{"replySource": "assistant-profile", "assistantProfileUpdated": true, "toolExecutions": []domain.ToolExecution{tool}}, CreatedAt: now})
	if err != nil {
		return domain.SessionMessageEnvelope{}, err
	}
	return domain.SessionMessageEnvelope{Messages: []domain.Message{user, assistant}, ToolCalls: []domain.ToolExecution{tool}}, nil
}

func (s *Service) renameAssistantFromChatStream(ctx context.Context, principal domainidentity.Principal, session domain.Session, input domain.WorkbenchSendMessageInput, name, locale string) (domain.WorkbenchStreamResult, error) {
	envelope, err := s.renameAssistantFromChat(ctx, principal, session, input.Content, name, locale)
	if err != nil {
		return domain.WorkbenchStreamResult{}, err
	}
	events := streamEventsFromEnvelope(session.ID, envelope, time.Now().UTC(), "succeeded")
	if input.EventSink != nil {
		for _, event := range events {
			if !input.EventSink(event) {
				return domain.WorkbenchStreamResult{Envelope: envelope}, context.Canceled
			}
		}
		return domain.WorkbenchStreamResult{Envelope: envelope}, nil
	}
	return domain.WorkbenchStreamResult{Envelope: envelope, Events: events}, nil
}
