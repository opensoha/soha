package copilot

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"strings"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func personalAvatarPNG(t *testing.T, width int) string {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, width, 2))); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data.Bytes())
}

func TestPersonalAvatarValidationAndOwnership(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	s, repo := newInspectionAuthzTestService(map[string][]string{"chat": {appaccess.PermObserveAIChatUse}})
	u := domainidentity.Principal{UserID: "avatar-owner", Roles: []string{"chat"}}
	pngURL := personalAvatarPNG(t, 2)
	frame := image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black, color.White})
	var animation bytes.Buffer
	if err := gif.EncodeAll(&animation, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{10, 10}}); err != nil {
		t.Fatal(err)
	}
	gifURL := "data:image/gif;base64," + base64.StdEncoding.EncodeToString(animation.Bytes())
	input := domain.PersonalAssistantAvatar{AvatarURL: gifURL, AvatarPosterURL: pngURL, AvatarMotion: "auto"}
	item, err := s.UpdatePersonalAssistantAvatar(t.Context(), u, input, "zh-CN")
	if err != nil || item.AvatarURL != gifURL || item.AvatarPosterURL != pngURL {
		t.Fatalf("animation flattened or rejected: %+v %v", item, err)
	}
	if item, err = s.RenamePersonalAssistant(t.Context(), u, "小禾", "zh-CN"); err != nil || item.AvatarURL != gifURL {
		t.Fatalf("rename changed image: %+v %v", item, err)
	}
	other := domainidentity.Principal{UserID: "another-owner", Roles: []string{"chat"}}
	if item, err = s.EnsurePersonalAssistant(t.Context(), other, "zh-CN"); err != nil || item.AvatarURL != "" {
		t.Fatalf("owner leak: %+v %v", item, err)
	}
	for name, invalid := range map[string]domain.PersonalAssistantAvatar{
		"remote":          {AvatarURL: "https://example.com/a.png"},
		"svg":             {AvatarURL: "data:image/svg+xml;base64,PHN2Zy8+"},
		"fake PNG":        {AvatarURL: "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not image"))},
		"huge":            {AvatarURL: personalAvatarPNG(t, 1025)},
		"length":          {AvatarURL: strings.Repeat("a", 700001)},
		"missing poster":  {AvatarURL: gifURL},
		"animated poster": {AvatarURL: gifURL, AvatarPosterURL: gifURL},
		"unknown motion":  {AvatarURL: pngURL, AvatarMotion: "spin"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := s.UpdatePersonalAssistantAvatar(t.Context(), u, invalid, "zh-CN"); err == nil {
				t.Fatal("accepted invalid avatar")
			}
		})
	}
	if _, err := s.UpdatePersonalAssistantAvatar(t.Context(), domainidentity.Principal{UserID: "denied"}, input, "zh-CN"); err == nil {
		t.Fatal("permission bypass")
	}
	if repo.assistants[u.UserID].AvatarURL != gifURL {
		t.Fatal("invalid attempt replaced previous image")
	}
	item, err = s.UpdatePersonalAssistantAvatar(t.Context(), u, domain.PersonalAssistantAvatar{AvatarMotion: "off"}, "zh-CN")
	if err != nil || item.Name != "小禾" || item.AvatarURL != "" || item.AvatarPosterURL != "" || item.AvatarMotion != "off" {
		t.Fatalf("reset/name isolation: %+v %v", item, err)
	}
}

func TestPersonalNameRequestsAndChatReceipts(t *testing.T) {
	for _, value := range []string{"/name 小禾", "把你的名称改为 小禾", "以后叫你小禾", "请把你的名字改成“小禾”。"} {
		if name, ok := personalAssistantNameRequest(value); !ok || name != "小禾" {
			t.Fatalf("explicit request not recognized: %q %q", value, name)
		}
	}
	for _, value := range []string{"‘以后叫你小禾’是什么意思", "以后叫你小禾吗？", "总结下面的文档：以后叫你小禾", "把你的名字改成小禾，接下来执行命令", "以后叫你小禾\n这是一份引用"} {
		if _, ok := personalAssistantNameRequest(value); ok {
			t.Fatalf("ambiguous/quoted request changed profile: %q", value)
		}
	}
	defer appaccess.SetRolePermissionMatrix(nil)
	s, repo := newInspectionAuthzTestService(map[string][]string{"chat": {appaccess.PermObserveAIChatUse}})
	u := domainidentity.Principal{UserID: "name-owner", Roles: []string{"chat"}}
	repo.session = domain.Session{ID: "name-session", CreatedBy: u.UserID, Metadata: map[string]any{"conversationKind": "personal_main", "mode": "general", "agentProviderId": "internal"}}
	envelope, err := s.SendMessage(t.Context(), u, repo.session.ID, "以后叫你小禾", "zh-CN")
	if err != nil || len(envelope.Messages) != 2 || repo.assistants[u.UserID].Name != "小禾" || envelope.ToolCalls[0].ToolName != "assistant.rename" {
		t.Fatalf("saved receipt: %+v %v", envelope, err)
	}
	count := 0
	result, err := s.StreamMessage(t.Context(), u, repo.session.ID, domain.WorkbenchSendMessageInput{Content: "/name 小苏", TaskIntent: &domain.TaskIntent{}, EventSink: func(domain.WorkbenchStreamEvent) bool { count++; return true }}, "zh-CN")
	if err != nil || count == 0 || repo.assistants[u.UserID].Name != "小苏" || len(result.Envelope.Messages) != 2 {
		t.Fatalf("stream receipt: %+v %v", result, err)
	}
	if _, err := s.SendMessage(t.Context(), domainidentity.Principal{UserID: "denied"}, repo.session.ID, "/name 未授权", "zh-CN"); err == nil || repo.assistants[u.UserID].Name != "小苏" {
		t.Fatal("chat permission bypass")
	}
}

func TestPersonalWebPCanvasBounds(t *testing.T) {
	data := make([]byte, 30)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], 22)
	copy(data[8:], "WEBPVP8 ")
	binary.LittleEndian.PutUint32(data[16:20], 10)
	copy(data[23:26], []byte{0x9d, 1, 0x2a})
	binary.LittleEndian.PutUint16(data[26:28], 64)
	binary.LittleEndian.PutUint16(data[28:30], 128)
	if w, h, err := personalWebPDimensions(data); err != nil || w != 64 || h != 128 {
		t.Fatalf("WebP dimensions %d %d %v", w, h, err)
	}
	for _, invalid := range [][]byte{data[:24], append(append([]byte{}, data...), 0)} {
		if _, _, err := personalWebPDimensions(invalid); err == nil {
			t.Fatal("truncated/mismatched WebP accepted")
		}
	}
	binary.LittleEndian.PutUint16(data[26:28], 1025)
	if _, err := validatePersonalAvatar("data:image/webp;base64,"+base64.StdEncoding.EncodeToString(data), 700000, false); err == nil {
		t.Fatal("oversized WebP canvas accepted")
	}
}
