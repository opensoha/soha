package assistantmesh

import (
	"encoding/json"
	"strings"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	domainevent "github.com/opensoha/soha/internal/domain/event"
)

const (
	feishuTestAppID    = "cli_test"
	feishuTestTenant   = "tenant_test"
	feishuTestBot      = "ou_bot"
	feishuTestEmployee = "ou_employee"
	feishuTestChatID   = "oc_chat"
)

func TestParseFeishuMessage(t *testing.T) {
	runFeishuMessageCases(t, feishuGroupMessageCases())
	runFeishuMessageCases(t, feishuRejectedEnvelopeCases(t))
	runFeishuMessageCases(t, feishuDirectMessageCases(t))
}

type feishuMessageCase struct {
	name       string
	event      domainevent.ConnectorEvent
	wantOK     bool
	wantStage  string
	wantTarget string
	wantGroup  string
	wantText   string
}

func feishuGroupMessageCases() []feishuMessageCase {
	return []feishuMessageCase{
		{
			name:       "group bot and employee asks a question",
			event:      feishuTestEvent("group", "@_user_1 @_user_2 how do I deploy?", feishuTestMention("@_user_1", feishuTestBot), feishuTestMention("@_user_2", feishuTestEmployee)),
			wantOK:     true,
			wantStage:  domain.FeishuReceipt,
			wantTarget: feishuTestEmployee,
			wantGroup:  feishuTestChatID,
			wantText:   "how do I deploy?",
		},
		{
			name:   "group mention metadata without a bot placeholder is ignored",
			event:  feishuTestEvent("group", "question", feishuTestMention("@_user_1", feishuTestBot)),
			wantOK: false,
		},
		{
			name:      "group bot without owner gets a bare receipt",
			event:     feishuTestEvent("group", "@_user_1 question", feishuTestMention("@_user_1", feishuTestBot)),
			wantOK:    true,
			wantStage: domain.FeishuReceipt,
			wantGroup: feishuTestChatID,
		},
		{
			name:      "group with multiple employee targets gets a bare receipt",
			event:     feishuTestEvent("group", "@_user_1 @_user_2 @_user_3 question", feishuTestMention("@_user_1", feishuTestBot), feishuTestMention("@_user_2", "ou_one"), feishuTestMention("@_user_3", "ou_two")),
			wantOK:    true,
			wantStage: domain.FeishuReceipt,
			wantGroup: feishuTestChatID,
		},
		{
			name:   "group without the bot is ignored",
			event:  feishuTestEvent("group", "@_user_1 question", feishuTestMention("@_user_1", feishuTestEmployee)),
			wantOK: false,
		},
		{
			name: "non text message is ignored",
			event: feishuTestEventWithMessage("group", map[string]any{
				"message_id": "om_message", "chat_id": feishuTestChatID, "chat_type": "group", "message_type": "image", "content": `{"image_key":"img"}`,
				"mentions": []any{feishuTestMention("@_user_1", feishuTestBot)},
			}),
			wantOK: false,
		},
		{
			name: "tenant mismatch on the mention fails the question",
			event: feishuTestEvent("group", "@_user_1 @_user_2 question",
				feishuTestMention("@_user_1", feishuTestBot),
				map[string]any{"key": "@_user_2", "id": map[string]any{"open_id": feishuTestEmployee}, "tenant_key": "tenant_other"},
			),
			wantOK:    true,
			wantStage: domain.FeishuReceipt,
			wantGroup: feishuTestChatID,
		},
	}
}

func feishuRejectedEnvelopeCases(t *testing.T) []feishuMessageCase {
	t.Helper()
	selfSend := feishuTestEvent("group", "@_user_1 question", feishuTestMention("@_user_1", feishuTestBot))
	body := feishuTestObject(t, selfSend.Payload, "event")
	sender := feishuTestObject(t, body, "sender")
	feishuTestObject(t, sender, "sender_id")["open_id"] = feishuTestBot
	return []feishuMessageCase{
		{
			name: "wrong app id is ignored",
			event: func() domainevent.ConnectorEvent {
				event := feishuTestEvent("group", "@_user_1 question", feishuTestMention("@_user_1", feishuTestBot))
				event.Payload["appId"] = "cli_other"
				return event
			}(),
			wantOK: false,
		},
		{
			name: "wrong tenant is ignored",
			event: func() domainevent.ConnectorEvent {
				event := feishuTestEvent("group", "@_user_1 question", feishuTestMention("@_user_1", feishuTestBot))
				event.Payload["tenantKey"] = "tenant_other"
				return event
			}(),
			wantOK: false,
		},
		{
			name:   "bot self send is ignored",
			event:  selfSend,
			wantOK: false,
		},
	}
}

func feishuDirectMessageCases(t *testing.T) []feishuMessageCase {
	t.Helper()
	invalidID := feishuTestEvent("p2p", "@_user_1 question", feishuTestMention("@_user_1", feishuTestEmployee))
	body := feishuTestObject(t, invalidID.Payload, "event")
	feishuTestObject(t, body, "message")["message_id"] = "om_" + strings.Repeat("a", 126)
	return []feishuMessageCase{
		{
			name:       "p2p employee mention asks a question",
			event:      feishuTestEvent("p2p", "@_user_1 summarize the incident", feishuTestMention("@_user_1", feishuTestEmployee)),
			wantOK:     true,
			wantStage:  domain.FeishuQuestion,
			wantTarget: feishuTestEmployee,
			wantText:   "summarize the incident",
		},
		{
			name:       "display name is never used as the target",
			event:      feishuTestEvent("p2p", "@_user_1 check @CFO", map[string]any{"key": "@_user_1", "id": map[string]any{"open_id": feishuTestEmployee}, "name": "CFO"}),
			wantOK:     true,
			wantStage:  domain.FeishuQuestion,
			wantTarget: feishuTestEmployee,
			wantText:   "check @CFO",
		},
		{
			name: "malformed message body is ignored",
			event: func() domainevent.ConnectorEvent {
				event := feishuTestEvent("p2p", "@_user_1 question", feishuTestMention("@_user_1", feishuTestEmployee))
				event.Payload["event"] = "not-an-object"
				return event
			}(),
			wantOK: false,
		},
		{
			name:   "invalid id bounds are ignored",
			event:  invalidID,
			wantOK: false,
		},
	}
}

func runFeishuMessageCases(t *testing.T, tests []feishuMessageCase) {
	t.Helper()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			job, ok := parseFeishuMessage(test.event, feishuTestAppID, feishuTestTenant, feishuTestBot)
			if ok != test.wantOK {
				t.Fatalf("ok = %v, want %v (job %+v)", ok, test.wantOK, job)
			}
			if !ok {
				return
			}
			if job.ID != "om_message" || job.SenderID != "ou_sender" || job.Stage != test.wantStage || job.TargetID != test.wantTarget || job.GroupID != test.wantGroup || job.Question != test.wantText {
				t.Fatalf("job = %+v, want stage %q target %q group %q text %q", job, test.wantStage, test.wantTarget, test.wantGroup, test.wantText)
			}
		})
	}
}

func feishuTestObject(t *testing.T, object map[string]any, key string) map[string]any {
	t.Helper()
	nested, ok := object[key].(map[string]any)
	if !ok || nested == nil {
		t.Fatalf("expected an object for %q, got %#v", key, object[key])
	}
	return nested
}

func feishuTestEvent(chatType, text string, mentions ...map[string]any) domainevent.ConnectorEvent {
	contentBytes, _ := json.Marshal(map[string]any{"text": text})
	message := map[string]any{
		"message_id":   "om_message",
		"chat_type":    chatType,
		"message_type": "text",
		"content":      string(contentBytes),
	}
	if chatType == "group" {
		message["chat_id"] = feishuTestChatID
	}
	if len(mentions) > 0 {
		items := make([]any, len(mentions))
		for index := range mentions {
			items[index] = mentions[index]
		}
		message["mentions"] = items
	}
	return feishuTestEventWithMessage(chatType, message)
}

func feishuTestEventWithMessage(chatType string, message map[string]any) domainevent.ConnectorEvent {
	return feishuTestEventWithPayload(map[string]any{
		"appId":     feishuTestAppID,
		"tenantKey": feishuTestTenant,
		"event": map[string]any{
			"sender": map[string]any{
				"sender_type": "user",
				"tenant_key":  feishuTestTenant,
				"sender_id":   map[string]any{"open_id": "ou_sender"},
			},
			"message": message,
		},
	})
}

func feishuTestEventWithPayload(payload map[string]any) domainevent.ConnectorEvent {
	return domainevent.ConnectorEvent{ID: "event", Source: "feishu", Type: "im.message.receive_v1", Payload: payload}
}

func feishuTestMention(key, openID string) map[string]any {
	return map[string]any{"key": key, "id": map[string]any{"open_id": openID}, "name": "display-name"}
}
