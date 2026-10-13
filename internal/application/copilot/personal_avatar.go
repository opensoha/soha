package copilot

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type personalAvatarStore interface {
	UpdatePersonalAssistantAvatar(context.Context, string, domain.PersonalAssistantAvatar) (domain.PersonalAssistant, error)
}

func (s *Service) UpdatePersonalAssistantAvatar(ctx context.Context, principal domainidentity.Principal, input domain.PersonalAssistantAvatar, locale string) (domain.PersonalAssistant, error) {
	return s.savePersonalAssistantAvatar(ctx, principal, input, locale, nil, nil)
}

type conditionalPersonalAvatarStore interface {
	CommitPersonalAssistantAvatar(context.Context, string, domain.PersonalAssistantAvatar, domain.PersonalAssistant, domain.Message) (domain.PersonalAssistant, error)
}

func (s *Service) savePersonalAssistantAvatar(ctx context.Context, principal domainidentity.Principal, input domain.PersonalAssistantAvatar, locale string, expected *domain.PersonalAssistant, receipt *domain.Message) (domain.PersonalAssistant, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domain.PersonalAssistant{}, err
	}
	if input.AvatarMotion == "" {
		input.AvatarMotion = "auto"
	}
	if input.AvatarMotion != "auto" && input.AvatarMotion != "off" {
		return domain.PersonalAssistant{}, apperrors.ErrInvalidArgument
	}
	kind, err := validatePersonalAvatar(input.AvatarURL, 700000, false)
	if err != nil {
		return domain.PersonalAssistant{}, err
	}
	if _, err := validatePersonalAvatar(input.AvatarPosterURL, 150000, true); err != nil {
		return domain.PersonalAssistant{}, err
	}
	if (kind == "image/gif" || kind == "image/webp") && input.AvatarPosterURL == "" {
		return domain.PersonalAssistant{}, apperrors.ErrInvalidArgument
	}
	if input.AvatarURL == "" {
		input.AvatarPosterURL = ""
	}
	store, ok := s.sessions.(personalAvatarStore)
	if !ok {
		return domain.PersonalAssistant{}, apperrors.ErrUnsupportedOperation
	}
	if _, err := s.EnsurePersonalAssistant(ctx, principal, locale); err != nil {
		return domain.PersonalAssistant{}, err
	}
	var item domain.PersonalAssistant
	if expected != nil {
		conditional, ok := store.(conditionalPersonalAvatarStore)
		if !ok {
			return item, apperrors.ErrUnsupportedOperation
		}
		item, err = conditional.CommitPersonalAssistantAvatar(ctx, principal.UserID, input, *expected, *receipt)
	} else {
		item, err = store.UpdatePersonalAssistantAvatar(ctx, principal.UserID, input)
	}
	if err == nil && s.audits != nil {
		meta := requestctx.FromContext(ctx)
		_ = s.audits.Record(ctx, domainaudit.Entry{ActorID: principal.UserID, ActorName: principal.UserName, ResourceKind: "PersonalAssistant", ResourceName: item.ID, Action: "personal_assistant.avatar", Result: "success", Summary: "Updated personal assistant image and motion preference", RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP})
	}
	return item, err
}

func validatePersonalAvatar(value string, limit int, poster bool) (string, error) {
	if value == "" {
		return "", nil
	}
	if len(value) > limit {
		return "", apperrors.ErrInvalidArgument
	}
	header, encoded, ok := strings.Cut(value, ";base64,")
	kind := strings.TrimPrefix(header, "data:")
	if !ok || header != "data:"+kind || (kind != "image/png" && kind != "image/jpeg" && (poster || (kind != "image/gif" && kind != "image/webp"))) {
		return "", apperrors.ErrInvalidArgument
	}
	if strings.ContainsAny(encoded, "\r\n") {
		return "", apperrors.ErrInvalidArgument
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) == 0 || http.DetectContentType(data) != kind {
		return "", apperrors.ErrInvalidArgument
	}
	var width, height int
	if kind == "image/webp" {
		width, height, err = personalWebPDimensions(data)
	} else {
		var config image.Config
		config, _, err = image.DecodeConfig(bytes.NewReader(data))
		width, height = config.Width, config.Height
	}
	if err != nil || width < 1 || height < 1 || width > 1024 || height > 1024 {
		return "", apperrors.ErrInvalidArgument
	}
	return kind, nil
}

// Read only the WebP canvas header; the size guard runs before any client decode.
// https://developers.google.com/speed/webp/docs/riff_container
func personalWebPDimensions(data []byte) (int, int, error) {
	if len(data) < 25 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" || uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
		return 0, 0, apperrors.ErrInvalidArgument
	}
	size := uint64(binary.LittleEndian.Uint32(data[16:20]))
	if size+20 > uint64(len(data)) {
		return 0, 0, apperrors.ErrInvalidArgument
	}
	payload := data[20 : 20+size]
	switch string(data[12:16]) {
	case "VP8X":
		if len(payload) != 10 || len(data) < 42 {
			break
		}
		return 1 + personalUint24(payload[4:7]), 1 + personalUint24(payload[7:10]), nil
	case "VP8L":
		if len(payload) < 5 || payload[0] != 0x2f {
			break
		}
		bits := binary.LittleEndian.Uint32(payload[1:5])
		return 1 + int(bits&0x3fff), 1 + int((bits>>14)&0x3fff), nil
	case "VP8 ":
		if len(payload) < 10 || !bytes.Equal(payload[3:6], []byte{0x9d, 0x01, 0x2a}) {
			break
		}
		return int(binary.LittleEndian.Uint16(payload[6:8]) & 0x3fff), int(binary.LittleEndian.Uint16(payload[8:10]) & 0x3fff), nil
	}
	return 0, 0, fmt.Errorf("%w: invalid WebP canvas", apperrors.ErrInvalidArgument)
}

func personalUint24(data []byte) int { return int(data[0]) | int(data[1])<<8 | int(data[2])<<16 }
