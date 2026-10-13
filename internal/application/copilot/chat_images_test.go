package copilot

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestChatImageAndAppearanceIntent(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 400, 200))); err != nil {
		t.Fatal(err)
	}
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(data.Bytes())
	selection := &domain.ContextSelection{Images: []sohaapi.WorkbenchImageAttachment{{ID: "image", Name: "diagram.png", DataURL: url}}}
	if err := validateChatImages(selection); err != nil {
		t.Fatal(err)
	}
	service := &Service{}
	input := domain.WorkbenchSendMessageInput{ContextSelection: selection}
	metadata := domain.SessionMetadata{AgentProviderID: "hermes"}
	if err := service.validateChatImageInput(input, metadata); !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatalf("external provider silently accepted image: %v", err)
	}
	input.AgentProviderID = agentProviderInternal
	if err := service.validateChatImageInput(input, metadata); err != nil {
		t.Fatalf("internal provider override rejected image: %v", err)
	}
	current := domain.Message{ID: "user", Role: "user", Content: "What is in this diagram?", Metadata: chatUserMetadata(map[string]any{}, selection)}
	messages, _ := buildProviderChatMessages(nil, current, "en-US")
	if len(messages[len(messages)-1].Images) != 1 || messages[len(messages)-1].Images[0] != url {
		t.Fatal("chat image did not reach model context")
	}
	if _, requested := assistantAppearanceTask(domain.WorkbenchSendMessageInput{Content: current.Content, ContextSelection: selection}, "general"); requested {
		t.Fatal("ordinary image became an avatar task")
	}
	if _, requested := assistantAppearanceTask(domain.WorkbenchSendMessageInput{Content: "用这张图片作为你的头像", ContextSelection: selection}, "general"); !requested {
		t.Fatal("explicit appearance intent ignored")
	}
	avatar, err := chatImageForAvatar(url)
	if err != nil || avatar.AvatarURL == url {
		t.Fatalf("explicit avatar conversion failed: %v", err)
	}
	for _, bad := range []string{"https://example.com/image.png", "data:image/png;base64,aGVsbG8=", "data:image/svg+xml;base64,aGVsbG8=", strings.Repeat("x", 700001)} {
		selection.Images[0].DataURL = bad
		if validateChatImages(selection) == nil {
			t.Fatal("invalid image accepted")
		}
	}
	selection.Images = []sohaapi.WorkbenchImageAttachment{{ID: "a", Name: "a", DataURL: url}, {ID: "b", Name: "b", DataURL: url}}
	if validateChatImages(selection) == nil {
		t.Fatal("image count limit lost")
	}
}

func TestModelReactionStreamingAndDurableReply(t *testing.T) {
	for _, content := range []string{"[[reaction:👋]]\nHello", "ordinary reply", "[[reaction:no]] literal"} {
		var visible strings.Builder
		filtered := reactionStream{send: func(delta string) bool { visible.WriteString(delta); return true }}
		for _, r := range content {
			if !filtered.push(string(r)) {
				t.Fatal("unexpected stop")
			}
		}
		filtered.flush()
		body, emoji := splitMessageReaction(content)
		if visible.String() != body {
			t.Fatalf("stream marker leaked: %q", visible.String())
		}
		if strings.HasPrefix(content, "[[reaction:👋]]") && emoji != "👋" {
			t.Fatal("AI emoji lost")
		}
	}
	if (&reactionStream{send: func(string) bool { return false }}).push("plain") {
		t.Fatal("stream cancellation swallowed")
	}
	service, store, _, _ := nativeLoopFixture(t)
	store.lease.Run.Input["requestMessageId"] = "owned-user"
	if err := service.commitNativeResult(t.Context(), store, store.lease, nativeCheckpoint{Reaction: "👍"}, "completed", "answer_accepted", "Done"); err != nil {
		t.Fatal(err)
	}
	if len(store.final) != 1 || store.final[0].Metadata["assistantReaction"] != "👍" || store.final[0].Metadata["requestMessageId"] != "owned-user" {
		t.Fatal("durable reply lost reaction binding")
	}
	store.final = nil
	if err := service.commitNativeResult(t.Context(), store, store.lease, nativeCheckpoint{Reaction: "👍"}, "canceled", "user_cancelled", "Canceled"); err != nil {
		t.Fatal(err)
	}
	if store.final[0].Metadata["assistantReaction"] != nil {
		t.Fatal("canceled run saved a reaction")
	}
}
