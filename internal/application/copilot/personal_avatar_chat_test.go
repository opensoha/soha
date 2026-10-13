package copilot

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (r *inspectionAuthzTestRepository) CommitPersonalAssistantAvatar(ctx context.Context, owner string, input domain.PersonalAssistantAvatar, expected domain.PersonalAssistant, receipt domain.Message) (domain.PersonalAssistant, error) {
	before := r.assistants[owner]
	if assistantAvatar(before) != assistantAvatar(expected) || !before.UpdatedAt.Equal(expected.UpdatedAt) {
		return before, apperrors.ErrConflict
	}
	item, err := r.UpdatePersonalAssistantAvatar(ctx, owner, input)
	if err != nil {
		return item, err
	}
	item.UpdatedAt = time.Now().UTC()
	r.assistants[owner] = item
	receipt.Metadata["assistantProfileAppliedUpdatedAt"] = item.UpdatedAt.Format(time.RFC3339Nano)
	_, err = r.CreateMessage(ctx, receipt)
	return item, err
}

type appearanceImageTestInvoker struct {
	image []byte
	err   error
	input appaigateway.WorkbenchImageRequest
}

func (i *appearanceImageTestInvoker) InvokeWorkbenchModel(context.Context, identity.Principal, appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
	return appaigateway.WorkbenchRelayResponse{}, errors.New("unexpected chat model call")
}
func (i *appearanceImageTestInvoker) InvokeWorkbenchImage(ctx context.Context, _ identity.Principal, input appaigateway.WorkbenchImageRequest) ([]byte, error) {
	i.input = input
	return i.image, i.err
}

func TestAssistantAppearanceChatLifecycle(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	s, repo := newInspectionAuthzTestService(map[string][]string{"chat": {appaccess.PermObserveAIChatUse}})
	user := identity.Principal{UserID: "appearance-owner", Roles: []string{"chat"}}
	repo.session = domain.Session{ID: "appearance-session", CreatedBy: user.UserID, Metadata: map[string]any{"conversationKind": "personal_main", "mode": "general"}}
	source := personalAvatarPNG(t, 2)
	raw, _ := base64.StdEncoding.DecodeString(strings.Split(source, ",")[1])
	invoker := &appearanceImageTestInvoker{image: raw}
	s.workbenchInvoker = invoker
	input := domain.WorkbenchSendMessageInput{Content: "把虚拟形象改成白色小机器人", RequestID: "appearance-once", AssistantTask: &domain.WorkbenchAssistantTask{Kind: "avatar"}}
	result, err := s.StreamMessage(t.Context(), user, repo.session.ID, input, "zh-CN")
	if err != nil || len(result.Envelope.Messages) != 2 || result.Envelope.Messages[1].Metadata["assistantProfileUpdated"] != true || repo.assistants[user.UserID].AvatarURL == "" {
		t.Fatalf("missing profile receipt: %+v %v", result, err)
	}
	count := len(repo.messages)
	replay, err := s.StreamMessage(t.Context(), user, repo.session.ID, input, "zh-CN")
	if err != nil || replay.Envelope.Messages[1].ID != result.Envelope.Messages[1].ID || len(repo.messages) != count {
		t.Fatal("repeated appearance request reran")
	}
	changed := input
	changed.Content = "different instruction"
	if _, err := s.StreamMessage(t.Context(), user, repo.session.ID, changed, "zh-CN"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("changed key accepted")
	}
	input.RequestID = ""
	updated := repo.assistants[user.UserID]
	input.AssistantTask.UseCurrentAvatar = true
	input.Content = "头发改成蓝色，其他保持"
	edited, err := s.StreamMessage(t.Context(), user, repo.session.ID, input, "zh-CN")
	if err != nil || invoker.input.Image != updated.AvatarPosterURL {
		t.Fatalf("reference edit: %+v %v", invoker.input, err)
	}
	invoker.err = apperrors.ErrNotFound
	failed, err := s.StreamMessage(t.Context(), user, repo.session.ID, input, "zh-CN")
	if err != nil || failed.Envelope.Messages[1].Metadata["assistantProfileUpdated"] == true || repo.assistants[user.UserID].AvatarURL != updated.AvatarURL {
		t.Fatal("failed generation changed profile")
	}
	undo := domain.WorkbenchSendMessageInput{Content: "撤销这次形象更改", AssistantTask: &domain.WorkbenchAssistantTask{Kind: "undo", MessageID: edited.Envelope.Messages[1].ID}}
	if undone, err := s.StreamMessage(t.Context(), user, repo.session.ID, undo, "zh-CN"); err != nil || undone.Envelope.Messages[1].Metadata["assistantProfileUpdated"] != true || repo.assistants[user.UserID].AvatarURL != updated.AvatarURL {
		t.Fatalf("undo failed: %v", err)
	}
	// Same image bytes after another update must not make an older receipt valid again.
	undo.AssistantTask.MessageID = result.Envelope.Messages[1].ID
	assertAppearanceRejected(t, s, repo, user, undo, updated.AvatarURL)
	old := repo.assistants[user.UserID]
	old.AvatarURL = source
	repo.assistants[user.UserID] = old
	assertAppearanceRejected(t, s, repo, user, undo, source)
	if _, err = s.StreamMessage(t.Context(), identity.Principal{UserID: "denied"}, repo.session.ID, input, "zh-CN"); err == nil {
		t.Fatal("permission bypass")
	}
}

func assertAppearanceRejected(t *testing.T, s *Service, repo *inspectionAuthzTestRepository, user identity.Principal, input domain.WorkbenchSendMessageInput, expectedURL string) {
	t.Helper()
	result, err := s.StreamMessage(t.Context(), user, repo.session.ID, input, "zh-CN")
	if err != nil || result.Envelope.Messages[1].Metadata["assistantProfileUpdated"] == true || repo.assistants[user.UserID].AvatarURL != expectedURL {
		t.Fatal("stale undo overwrote a newer profile")
	}
}
