package copilot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"time"

	"github.com/google/uuid"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type assistantImageInvoker interface {
	InvokeWorkbenchImage(context.Context, domainidentity.Principal, appaigateway.WorkbenchImageRequest) ([]byte, error)
}

func (s *Service) assistantProfileFromChat(ctx context.Context, principal domainidentity.Principal, session domain.Session, input domain.WorkbenchSendMessageInput, mode, locale string) (domain.WorkbenchStreamResult, bool, error) {
	if !isPersonalSession(session) {
		return domain.WorkbenchStreamResult{}, false, nil
	}
	if task, requested := assistantAppearanceTask(input, mode); requested {
		if task.Image == nil && input.ContextSelection != nil && len(input.ContextSelection.Images) > 0 {
			image, err := chatImageForAvatar(input.ContextSelection.Images[0].DataURL)
			if err != nil {
				return domain.WorkbenchStreamResult{}, true, err
			}
			task.Image = &image
		}
		result, err := s.assistantAppearanceFromChat(ctx, principal, session, input, task, locale)
		return result, true, err
	}
	if name, requested := personalAssistantNameForTurn(input.Content, mode, input.TaskIntent); requested {
		result, err := s.renameAssistantFromChatStream(ctx, principal, session, input, name, locale)
		return result, true, err
	}
	return domain.WorkbenchStreamResult{}, false, nil
}

func assistantAppearanceTask(input domain.WorkbenchSendMessageInput, mode string) (domain.WorkbenchAssistantTask, bool) {
	if input.AssistantTask != nil {
		return *input.AssistantTask, true
	}
	if mode != "general" || (input.TaskIntent != nil && len(input.TaskIntent.Capabilities) > 0) {
		return domain.WorkbenchAssistantTask{}, false
	}
	text := strings.TrimSpace(input.Content)
	if strings.ContainsAny(text, "\n\r?？") {
		return domain.WorkbenchAssistantTask{}, false
	}
	if text == "恢复默认形象" || text == "关闭头像动态效果" || text == "开启头像动态效果" {
		return domain.WorkbenchAssistantTask{Kind: "avatar"}, true
	}
	if directAssistantImageInstruction(text) && input.ContextSelection != nil && len(input.ContextSelection.Images) > 0 {
		return domain.WorkbenchAssistantTask{Kind: "avatar"}, true
	}
	for _, prefix := range []string{"把虚拟形象改成", "把你的头像改成", "调整你的头像：", "/avatar "} {
		if strings.HasPrefix(text, prefix) {
			return domain.WorkbenchAssistantTask{Kind: "avatar"}, true
		}
	}
	return domain.WorkbenchAssistantTask{}, false
}

func (s *Service) assistantAppearanceFromChat(ctx context.Context, principal domainidentity.Principal, session domain.Session, input domain.WorkbenchSendMessageInput, task domain.WorkbenchAssistantTask, locale string) (domain.WorkbenchStreamResult, error) {
	if err := validateAssistantAppearanceRequest(input, task); err != nil {
		return domain.WorkbenchStreamResult{}, err
	}

	if replay, found, err := s.replayAssistantAppearance(ctx, session, input, task); found || err != nil {
		return replay, err
	}
	before, err := s.EnsurePersonalAssistant(ctx, principal, locale)
	if err != nil {
		return domain.WorkbenchStreamResult{}, err
	}
	metadata := map[string]any{"userId": principal.UserID, "mode": "general", "locale": locale, "assistantTaskRequestId": input.RequestID, "assistantTaskHash": assistantAppearanceHash(input.Content, task)}
	if task.Image != nil {
		metadata["assistantImage"] = task.Image
	}
	user, err := s.messages.CreateMessage(ctx, domain.Message{ID: uuid.NewString(), SessionID: session.ID, Role: "user", Content: strings.TrimSpace(input.Content), Metadata: metadata, CreatedAt: time.Now().UTC()})
	if err != nil {
		return domain.WorkbenchStreamResult{}, err
	}
	tool := domain.ToolExecution{ID: uuid.NewString(), AdapterID: "conversation", ToolName: "assistant.avatar", Summary: "正在调整助手形象", Status: "running", StartedAt: time.Now().UTC()}
	call := streamToolCallFromExecution(tool)
	if input.EventSink != nil && !input.EventSink(domain.WorkbenchStreamEvent{Type: "tool.started", MessageID: user.ID, ToolCall: &call}) {
		return domain.WorkbenchStreamResult{}, context.Canceled
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	next, err := s.assistantAppearanceResult(ctx, principal, session, task, input.Content, before)
	if next.AvatarMotion == "" {
		next.AvatarMotion = "auto"
	}
	if ctx.Err() != nil {
		return domain.WorkbenchStreamResult{}, ctx.Err()
	}
	reply := map[string]string{
		"avatar": "形象已更新。可以继续描述你想调整的地方。 ✅",
		"undo":   "已撤销这次形象更改，恢复为之前的头像。",
	}[string(task.Kind)]
	responseMetadata := map[string]any{"replySource": "assistant-profile", "assistantTaskRequestId": input.RequestID}
	tool.Status = "success"
	tool.Summary = "已更新助手形象"
	if err != nil {
		tool.Status = "failed"
		tool.Summary = "形象未更新"
		reply = assistantImageFailure(err)
	} else {
		responseMetadata["assistantProfileUpdated"] = true
		responseMetadata["assistantImage"] = next
		responseMetadata["assistantProfilePrevious"] = assistantAvatar(before)
		responseMetadata["assistantProfileApplied"] = next
	}
	completed := time.Now().UTC()
	tool.CompletedAt = &completed
	tool.Output = map[string]any{"updated": err == nil}
	responseMetadata["toolExecutions"] = []domain.ToolExecution{tool}
	assistant := domain.Message{ID: uuid.NewString(), SessionID: session.ID, Role: "assistant", Content: reply, Metadata: responseMetadata, CreatedAt: completed}
	if err == nil {
		_, err = s.savePersonalAssistantAvatar(ctx, principal, next, locale, &before, &assistant)
		if err != nil {
			for _, key := range []string{"assistantProfileUpdated", "assistantImage", "assistantProfilePrevious", "assistantProfileApplied", "assistantProfileAppliedUpdatedAt"} {
				delete(responseMetadata, key)
			}
			tool.Status = "failed"
			tool.Summary = "形象未更新"
			tool.Output = map[string]any{"updated": false}
			responseMetadata["toolExecutions"] = []domain.ToolExecution{tool}
			assistant.Content = assistantImageFailure(err)
		}
	}
	if err != nil {
		if _, saveErr := s.messages.CreateMessage(ctx, assistant); saveErr != nil {
			return domain.WorkbenchStreamResult{}, saveErr
		}
	}

	envelope := domain.SessionMessageEnvelope{Messages: []domain.Message{user, assistant}, ToolCalls: []domain.ToolExecution{tool}}
	events := streamEventsFromEnvelope(session.ID, envelope, completed, "succeeded")
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

func (s *Service) assistantAppearanceResult(ctx context.Context, principal domainidentity.Principal, session domain.Session, task domain.WorkbenchAssistantTask, instruction string, before domain.PersonalAssistant) (domain.PersonalAssistantAvatar, error) {
	if task.Kind == "undo" {
		return s.undoAssistantAppearance(ctx, principal, session, task.MessageID, before)
	}
	next := assistantAvatar(before)
	if task.Image == nil && directAssistantImageInstruction(instruction) {
		return next, apperrors.ErrInvalidArgument
	}
	switch strings.TrimSpace(instruction) {
	case "恢复默认形象", "把虚拟形象恢复默认":
		next.AvatarURL = ""
		next.AvatarPosterURL = ""
	case "关闭头像动态效果":
		next.AvatarMotion = "off"
	case "开启头像动态效果":
		next.AvatarMotion = "auto"
	default:
		if task.Image != nil && directAssistantImageInstruction(instruction) {
			next = domain.PersonalAssistantAvatar{AvatarURL: task.Image.AvatarURL, AvatarPosterURL: task.Image.AvatarPosterURL, AvatarMotion: string(task.Image.AvatarMotion)}
		} else {
			var err error
			next, err = s.generateAssistantAppearance(ctx, principal, session, task, instruction, before)
			if err != nil {
				return next, err
			}
		}
	}
	if ctx.Err() != nil {
		return next, ctx.Err()
	}
	return next, nil
}

func directAssistantImageInstruction(instruction string) bool {
	switch strings.TrimSpace(instruction) {
	case "用这张图片作为你的头像", "用这张图片作为头像", "把虚拟形象改成这张图片", "把你的头像改成这张图片":
		return true
	}
	return false
}
func assistantAvatar(item domain.PersonalAssistant) domain.PersonalAssistantAvatar {
	return domain.PersonalAssistantAvatar{AvatarURL: item.AvatarURL, AvatarPosterURL: item.AvatarPosterURL, AvatarMotion: firstNonEmpty(item.AvatarMotion, "auto")}
}

func (s *Service) generateAssistantAppearance(ctx context.Context, principal domainidentity.Principal, session domain.Session, task domain.WorkbenchAssistantTask, instruction string, before domain.PersonalAssistant) (domain.PersonalAssistantAvatar, error) {
	invoker, ok := s.workbenchInvoker.(assistantImageInvoker)
	if !ok {
		return domain.PersonalAssistantAvatar{}, apperrors.ErrUnsupportedOperation
	}
	reference := ""
	if task.UseCurrentAvatar {
		reference = firstNonEmpty(before.AvatarPosterURL, before.AvatarURL)
	}
	if task.Image != nil {
		reference = firstNonEmpty(task.Image.AvatarPosterURL, task.Image.AvatarURL)
	}
	for _, prefix := range []string{"把虚拟形象改成", "把你的头像改成", "调整你的头像：", "/avatar "} {
		instruction = strings.TrimSpace(strings.TrimPrefix(instruction, prefix))
	}
	if instruction == "" {
		return domain.PersonalAssistantAvatar{}, apperrors.ErrInvalidArgument
	}
	model := task.PublicModel
	if rest, ok := strings.CutPrefix(instruction, "使用图片模型 "); ok {
		if selected, prompt, found := strings.Cut(rest, "："); found {
			model = strings.TrimSpace(selected)
			instruction = strings.TrimSpace(prompt)
		}
	}
	data, err := invoker.InvokeWorkbenchImage(ctx, principal, appaigateway.WorkbenchImageRequest{Prompt: instruction, Image: reference, PublicModel: model, SessionID: session.ID})
	if err != nil {
		return domain.PersonalAssistantAvatar{}, err
	}
	return generatedAssistantAvatar(data, before.AvatarMotion)
}

func generatedAssistantAvatar(data []byte, motion string) (domain.PersonalAssistantAvatar, error) {
	if len(data) > 5<<20 {
		return domain.PersonalAssistantAvatar{}, apperrors.ErrInvalidArgument
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 {
		return domain.PersonalAssistantAvatar{}, apperrors.ErrInvalidArgument
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return domain.PersonalAssistantAvatar{}, err
	}
	bounds := source.Bounds()
	edge := min(bounds.Dx(), bounds.Dy())
	crop := image.Rect(bounds.Min.X+(bounds.Dx()-edge)/2, bounds.Min.Y+(bounds.Dy()-edge)/2, bounds.Min.X+(bounds.Dx()+edge)/2, bounds.Min.Y+(bounds.Dy()+edge)/2)
	encode := func(size int) (string, error) {
		target := image.NewRGBA(image.Rect(0, 0, size, size))
		// ponytail: nearest-neighbor crop; replace only if avatar quality needs a resampler.
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				target.Set(x, y, source.At(crop.Min.X+x*crop.Dx()/size, crop.Min.Y+y*crop.Dy()/size))
			}
		}
		var buffer bytes.Buffer
		if err := png.Encode(&buffer, target); err != nil {
			return "", err
		}
		kind := "image/png"
		limit := 700000
		if size == 256 {
			limit = 150000
		}
		if base64.StdEncoding.EncodedLen(buffer.Len())+22 > limit {
			buffer.Reset()
			if err := jpeg.Encode(&buffer, target, &jpeg.Options{Quality: 85}); err != nil {
				return "", err
			}
			kind = "image/jpeg"
		}
		return "data:" + kind + ";base64," + base64.StdEncoding.EncodeToString(buffer.Bytes()), nil
	}
	avatar, err := encode(512)
	if err != nil {
		return domain.PersonalAssistantAvatar{}, err
	}
	poster, err := encode(256)
	if err != nil {
		return domain.PersonalAssistantAvatar{}, err
	}
	return domain.PersonalAssistantAvatar{AvatarURL: avatar, AvatarPosterURL: poster, AvatarMotion: motion}, nil
}

func (s *Service) undoAssistantAppearance(ctx context.Context, principal domainidentity.Principal, session domain.Session, messageID string, current domain.PersonalAssistant) (domain.PersonalAssistantAvatar, error) {
	messages, err := s.messages.ListMessages(ctx, session.ID, 100)
	if err != nil {
		return domain.PersonalAssistantAvatar{}, err
	}
	for _, message := range messages {
		if message.ID != messageID || message.Role != "assistant" || message.Metadata["assistantProfileUpdated"] != true {
			continue
		}
		var applied, previous domain.PersonalAssistantAvatar
		raw, _ := json.Marshal(message.Metadata["assistantProfileApplied"])
		if json.Unmarshal(raw, &applied) != nil {
			return previous, apperrors.ErrInvalidArgument
		}
		raw, _ = json.Marshal(message.Metadata["assistantProfilePrevious"])
		if json.Unmarshal(raw, &previous) != nil {
			return previous, apperrors.ErrInvalidArgument
		}
		if applied != assistantAvatar(current) || message.Metadata["assistantProfileAppliedUpdatedAt"] != current.UpdatedAt.Format(time.RFC3339Nano) {
			return previous, apperrors.ErrConflict
		}
		return previous, nil
	}
	return domain.PersonalAssistantAvatar{}, apperrors.ErrNotFound
}
func assistantImageFailure(err error) string {
	switch {
	case errors.Is(err, apperrors.ErrAccessDenied):
		return "当前账号没有图片生成权限，形象未更改。"
	case errors.Is(err, apperrors.ErrNotFound):
		return "没有可用的图片模型。请先在模型管理中配置图片生成或编辑模型，再重试；原头像已保留。"
	case errors.Is(err, apperrors.ErrConflict):
		return "形象已有后续更改，这次撤销没有覆盖新的形象。"
	case errors.Is(err, apperrors.ErrInvalidArgument):
		return "图片或描述不符合要求，形象未更改。可以选择 PNG、JPEG、GIF 或 WebP 图片，或者补充形象描述。"
	default:
		return "图片服务暂时无法完成这次调整，原头像已保留。可以稍后重试。"
	}
}

func validateAssistantAppearanceRequest(input domain.WorkbenchSendMessageInput, task domain.WorkbenchAssistantTask) error {
	if len(input.RequestID) > 128 {
		return apperrors.ErrInvalidArgument
	}
	if task.Kind != "avatar" && task.Kind != "undo" {
		return apperrors.ErrInvalidArgument
	}
	if len([]rune(input.Content)) > 2000 || strings.TrimSpace(input.Content) == "" || len(task.PublicModel) > 128 || (input.TaskIntent != nil && len(input.TaskIntent.Capabilities) > 0) {
		return apperrors.ErrInvalidArgument
	}
	if task.Kind == "undo" && (task.Image != nil || task.PublicModel != "" || task.UseCurrentAvatar || task.MessageID == "") {
		return apperrors.ErrInvalidArgument
	}
	if task.Kind == "avatar" && task.MessageID != "" {
		return apperrors.ErrInvalidArgument
	}
	if task.Image != nil {
		if _, err := validatePersonalAvatar(task.Image.AvatarURL, 700000, false); err != nil {
			return err
		}
		if _, err := validatePersonalAvatar(task.Image.AvatarPosterURL, 150000, true); err != nil {
			return err
		}
	}
	return nil
}

func assistantAppearanceHash(content string, task domain.WorkbenchAssistantTask) string {
	raw, _ := json.Marshal(struct {
		Content string
		Task    domain.WorkbenchAssistantTask
	}{content, task})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func (s *Service) replayAssistantAppearance(ctx context.Context, session domain.Session, input domain.WorkbenchSendMessageInput, task domain.WorkbenchAssistantTask) (domain.WorkbenchStreamResult, bool, error) {
	if input.RequestID == "" {
		return domain.WorkbenchStreamResult{}, false, nil
	}
	// ponytail: replay covers the owned session's latest 100 messages; add a durable key index for older retries.
	messages, err := s.messages.ListMessages(ctx, session.ID, 100)
	if err != nil {
		return domain.WorkbenchStreamResult{}, false, err
	}
	var user, assistant domain.Message
	for _, message := range messages {
		if message.Metadata["assistantTaskRequestId"] != input.RequestID {
			continue
		}
		if message.Role == "user" {
			user = message
		}
		if message.Role == "assistant" {
			assistant = message
		}
	}
	if user.ID == "" {
		return domain.WorkbenchStreamResult{}, false, nil
	}
	if user.Metadata["assistantTaskHash"] != assistantAppearanceHash(input.Content, task) || assistant.ID == "" {
		return domain.WorkbenchStreamResult{}, true, apperrors.ErrConflict
	}
	var tools []domain.ToolExecution
	raw, _ := json.Marshal(assistant.Metadata["toolExecutions"])
	if err := json.Unmarshal(raw, &tools); err != nil {
		return domain.WorkbenchStreamResult{}, true, err
	}
	envelope := domain.SessionMessageEnvelope{Messages: []domain.Message{user, assistant}, ToolCalls: tools}
	events := streamEventsFromEnvelope(session.ID, envelope, assistant.CreatedAt, "succeeded")
	if input.EventSink != nil {
		for _, event := range events {
			if !input.EventSink(event) {
				return domain.WorkbenchStreamResult{}, true, context.Canceled
			}
		}
		events = nil
	}
	return domain.WorkbenchStreamResult{Envelope: envelope, Events: events}, true, nil
}
