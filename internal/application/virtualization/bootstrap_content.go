package virtualization

import (
	"fmt"
	"strings"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Reuse the configured credential keyring; bootstrap tokens must survive worker
// restart without being persisted as cleartext task input.
func (s *Service) sealVMBootstrap(payload map[string]any, content string) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	if len(content) > 1<<20 {
		return fmt.Errorf("%w: VM bootstrap content exceeds 1 MiB", apperrors.ErrInvalidArgument)
	}
	if s.credentialKeys.Active().ID() == "" && strings.TrimSpace(s.credentialKey) == "" {
		return fmt.Errorf("%w: credential encryption is required for VM bootstrap content", apperrors.ErrInvalidArgument)
	}
	sealed, err := s.encryptCredentialJSON(map[string]any{"cloudInit": content})
	if err != nil {
		return fmt.Errorf("%w: cannot encrypt VM bootstrap content", apperrors.ErrInvalidArgument)
	}
	payload["cloudInitCredential"], payload["cloudInitConfigured"] = sealed, true
	return nil
}

func (s *Service) vmBootstrapContent(payload map[string]any) (string, error) {
	sealed := payloadString(payload, "cloudInitCredential")
	if sealed == "" {
		return payloadString(payload, "cloudInit"), nil
	} // Existing queued records remain readable.
	values, err := s.decryptCredentialJSON(sealed)
	if err != nil {
		return "", fmt.Errorf("%w: VM bootstrap key is unavailable or content is invalid", apperrors.ErrConflict)
	}
	content, ok := values["cloudInit"].(string)
	if !ok || len(content) > 1<<20 {
		return "", fmt.Errorf("%w: VM bootstrap content is invalid", apperrors.ErrConflict)
	}
	return content, nil
}
