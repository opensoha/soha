package copilot

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) validateChatImageInput(input domain.WorkbenchSendMessageInput, metadata domain.SessionMetadata) error {
	if err := validateChatImages(input.ContextSelection); err != nil {
		return err
	}
	providerID := normalizeAgentProviderID(firstNonEmpty(input.AgentProviderID, metadata.AgentProviderID, agentProviderInternal))
	if input.ContextSelection != nil && len(input.ContextSelection.Images) > 0 && s.shouldUseExternalAgent(providerID) {
		return fmt.Errorf("%w: this provider does not support chat images", apperrors.ErrUnsupportedOperation)
	}
	return nil
}

func validateChatImages(selection *domain.ContextSelection) error {
	if selection == nil {
		return nil
	}
	if len(selection.Images) > 1 {
		return fmt.Errorf("%w: at most one chat image", apperrors.ErrInvalidArgument)
	}
	for _, image := range selection.Images {
		if strings.TrimSpace(image.ID) == "" || len(image.ID) > 128 || strings.TrimSpace(image.Name) == "" || utf8.RuneCountInString(image.Name) > 256 || image.DataURL == "" {
			return fmt.Errorf("%w: invalid chat image", apperrors.ErrInvalidArgument)
		}
		if _, err := validatePersonalAvatar(image.DataURL, 700000, true); err != nil {
			return fmt.Errorf("%w: invalid chat image bytes or dimensions", err)
		}
	}
	return nil
}

func chatUserMetadata(metadata map[string]any, selection *domain.ContextSelection) map[string]any {
	if selection != nil && len(selection.Images) > 0 {
		metadata["images"] = selection.Images
	}
	return metadata
}

func messageChatImages(message domain.Message) []string {
	if message.Role != "user" {
		return nil
	}
	raw, err := json.Marshal(message.Metadata["images"])
	if err != nil {
		return nil
	}
	var images []sohaapi.WorkbenchImageAttachment
	if json.Unmarshal(raw, &images) != nil || validateChatImages(&domain.ContextSelection{Images: images}) != nil {
		return nil
	}
	urls := make([]string, 0, len(images))
	for _, image := range images {
		urls = append(urls, image.DataURL)
	}
	return urls
}

func chatImageForAvatar(url string) (sohaapi.PersonalAssistantAvatarRequest, error) {
	_, encoded, _ := strings.Cut(url, ";base64,")
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return sohaapi.PersonalAssistantAvatarRequest{}, apperrors.ErrInvalidArgument
	}
	avatar, err := generatedAssistantAvatar(data, "auto")
	if err != nil {
		return sohaapi.PersonalAssistantAvatarRequest{}, err
	}
	return sohaapi.PersonalAssistantAvatarRequest{AvatarURL: avatar.AvatarURL, AvatarPosterURL: avatar.AvatarPosterURL, AvatarMotion: sohaapi.PersonalAssistantAvatarRequestAvatarMotion(avatar.AvatarMotion)}, nil
}
