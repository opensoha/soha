package assistantmesh

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	domain "github.com/opensoha/soha/internal/domain/assistantmesh"
	domainevent "github.com/opensoha/soha/internal/domain/event"
)

const (
	maxFeishuIDLength      = 128
	maxFeishuContentBytes  = 64 << 10
	maxFeishuQuestionRunes = 4096
)

type feishuMention struct {
	key            string
	openID         string
	valid          bool
	tenantMismatch bool
}

// parseFeishuMessage converts a verified connector event into private job
// state. Credentials, scope, expiry and DLP are deliberately handled by the
// caller.
func parseFeishuMessage(event domainevent.ConnectorEvent, appID, tenantKey, botOpenID string) (domain.FeishuJob, bool) {
	if event.Source != "feishu" || event.Type != "im.message.receive_v1" || appID == "" || tenantKey == "" {
		return domain.FeishuJob{}, false
	}
	if event.Payload["appId"] != appID || event.Payload["tenantKey"] != tenantKey {
		return domain.FeishuJob{}, false
	}

	body, ok := feishuObject(event.Payload["event"])
	if !ok {
		return domain.FeishuJob{}, false
	}
	senderID := feishuUserSender(body["sender"], tenantKey, botOpenID)
	if senderID == "" {
		return domain.FeishuJob{}, false
	}
	message, ok := feishuObject(body["message"])
	if !ok || feishuString(message["message_type"]) != "text" {
		return domain.FeishuJob{}, false
	}
	job := domain.FeishuJob{ID: feishuString(message["message_id"]), SenderID: senderID}
	if !validFeishuID(job.ID, "om_") {
		return domain.FeishuJob{}, false
	}
	text, textOK := feishuMessageText(message)
	mentions, mentionsOK := feishuMessageMentions(message, tenantKey)
	switch feishuString(message["chat_type"]) {
	case "group":
		job.GroupID = feishuString(message["chat_id"])
		if !textOK || !validFeishuGroupID(job.GroupID) {
			return domain.FeishuJob{}, false
		}
		return feishuGroupQuestion(job, text, mentions, mentionsOK, botOpenID)
	case "p2p":
		if !textOK || !mentionsOK {
			return domain.FeishuJob{}, false
		}
		return feishuDirectQuestion(job, text, mentions, botOpenID)
	default:
		return domain.FeishuJob{}, false
	}
}

func feishuUserSender(value any, tenantKey, botOpenID string) string {
	sender, ok := feishuObject(value)
	if !ok || feishuString(sender["sender_type"]) != "user" {
		return ""
	}
	if value, present := sender["tenant_key"]; present && feishuString(value) != tenantKey {
		return ""
	}
	id, ok := feishuObject(sender["sender_id"])
	if !ok {
		return ""
	}
	openID := feishuString(id["open_id"])
	if !validFeishuID(openID, "ou_") || openID == botOpenID {
		return ""
	}
	return openID
}

func feishuGroupQuestion(job domain.FeishuJob, text string, mentions []feishuMention, mentionsOK bool, botOpenID string) (domain.FeishuJob, bool) {
	botMentioned := false
	employeeIDs := make(map[string]struct{})
	for _, mention := range mentions {
		if !mention.valid || mention.tenantMismatch {
			mentionsOK = false
			continue
		}
		if mention.openID == botOpenID {
			botMentioned = botMentioned || feishuPlaceholderIndex(text, mention.key) >= 0
			continue
		}
		employeeIDs[mention.openID] = struct{}{}
	}
	if !botMentioned {
		return domain.FeishuJob{}, false
	}
	job.Stage = domain.FeishuReceipt
	if !mentionsOK || len(employeeIDs) != 1 {
		return job, true
	}
	question, ok := stripFeishuMentions(text, mentions)
	if !ok || !validFeishuQuestion(question) {
		return job, true
	}
	job.Question = question
	for openID := range employeeIDs {
		job.TargetID = openID
	}
	return job, true
}

func feishuDirectQuestion(job domain.FeishuJob, text string, mentions []feishuMention, botOpenID string) (domain.FeishuJob, bool) {
	employeeIDs := make(map[string]struct{})
	for _, mention := range mentions {
		if !mention.valid || mention.tenantMismatch || mention.openID == botOpenID {
			return domain.FeishuJob{}, false
		}
		employeeIDs[mention.openID] = struct{}{}
	}
	if len(employeeIDs) != 1 {
		return domain.FeishuJob{}, false
	}
	question, ok := stripFeishuMentions(text, mentions)
	if !ok || !validFeishuQuestion(question) {
		return domain.FeishuJob{}, false
	}
	job.Stage, job.Question = domain.FeishuQuestion, question
	for openID := range employeeIDs {
		job.TargetID = openID
	}
	return job, true
}

func feishuMessageText(message map[string]any) (string, bool) {
	var text string
	switch content := message["content"].(type) {
	case string:
		if content == "" || len(content) > maxFeishuContentBytes {
			return "", false
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(content), &body); err != nil {
			return "", false
		}
		value, ok := body["text"].(string)
		if !ok {
			return "", false
		}
		text = value
	case map[string]any:
		value, ok := content["text"].(string)
		if !ok {
			return "", false
		}
		text = value
	default:
		return "", false
	}
	return strings.TrimSpace(text), utf8.ValidString(text)
}

func feishuMessageMentions(message map[string]any, tenantKey string) ([]feishuMention, bool) {
	value, present := message["mentions"]
	if !present || value == nil {
		return nil, true
	}
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	mentions := make([]feishuMention, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		mention := feishuMention{}
		object, ok := feishuObject(item)
		if !ok {
			mentions = append(mentions, mention)
			continue
		}
		mention.key = feishuString(object["key"])
		_, duplicate := seen[mention.key]
		if validFeishuMentionKey(mention.key) && !duplicate {
			seen[mention.key] = struct{}{}
			mention.valid = true
		}
		if id, ok := feishuObject(object["id"]); ok {
			mention.openID = feishuString(id["open_id"])
			if value, present := id["tenant_key"]; present && feishuString(value) != tenantKey {
				mention.tenantMismatch = true
			}
		}
		if value, present := object["tenant_key"]; present && feishuString(value) != tenantKey {
			mention.tenantMismatch = true
		}
		if !validFeishuID(mention.openID, "ou_") {
			mention.valid = false
		}
		mentions = append(mentions, mention)
	}
	return mentions, true
}

func stripFeishuMentions(text string, mentions []feishuMention) (string, bool) {
	for _, mention := range mentions {
		if !mention.valid || mention.tenantMismatch {
			return "", false
		}
		index := feishuPlaceholderIndex(text, mention.key)
		if index < 0 {
			return "", false
		}
		text = text[:index] + text[index+len(mention.key):]
	}
	text = strings.TrimSpace(text)
	if strings.Contains(text, "@_user_") {
		return "", false
	}
	return text, true
}

func feishuPlaceholderIndex(text, key string) int {
	found := -1
	for offset := 0; ; {
		index := strings.Index(text[offset:], key)
		if index < 0 {
			return found
		}
		index += offset
		end := index + len(key)
		if end == len(text) || !isFeishuKeyChar(text[end]) {
			if found >= 0 {
				return -1
			}
			found = index
		}
		offset = end
	}
}

func validFeishuQuestion(text string) bool {
	return text != "" && utf8.ValidString(text) && utf8.RuneCountInString(text) <= maxFeishuQuestionRunes
}

func validFeishuMentionKey(key string) bool {
	if !strings.HasPrefix(key, "@_user_") || len(key) == len("@_user_") {
		return false
	}
	for _, char := range key[len("@_user_"):] {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func validFeishuID(value, prefix string) bool {
	if len(value) > maxFeishuIDLength || !strings.HasPrefix(value, prefix) || len(value) == len(prefix) {
		return false
	}
	for index := 0; index < len(value); index++ {
		char := value[index]
		if !isFeishuKeyChar(char) && char != '-' {
			return false
		}
	}
	return true
}

func validFeishuGroupID(value string) bool {
	if len(value) > maxFeishuIDLength || !strings.HasPrefix(value, "oc_") || len(value) == len("oc_") {
		return false
	}
	for index := 0; index < len(value); index++ {
		char := value[index]
		if !isFeishuKeyChar(char) || (char >= 'A' && char <= 'Z') {
			return false
		}
	}
	return true
}

func isFeishuKeyChar(char byte) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_'
}

func feishuObject(value any) (map[string]any, bool) {
	object, ok := value.(map[string]any)
	return object, ok
}

func feishuString(value any) string {
	text, _ := value.(string)
	return text
}
