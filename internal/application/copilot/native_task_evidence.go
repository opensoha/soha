package copilot

import (
	"context"
	"encoding/json"
	"slices"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) validateMessageTaskEvidence(ctx context.Context, principal domainidentity.Principal, message domaincopilot.Message) error {
	value := message.Metadata["capabilityTaskIds"]
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil || len(data) > 2048 {
		return apperrors.ErrAccessDenied
	}
	var ids []string
	if json.Unmarshal(data, &ids) != nil || len(ids) > 8 {
		return apperrors.ErrAccessDenied
	}
	seen := []string{}
	for _, id := range ids {
		if id == "" || s.nativeTasks == nil {
			return apperrors.ErrAccessDenied
		}
		if slices.Contains(seen, id) {
			continue
		}
		seen = append(seen, id)
		task, err := s.nativeTasks.GetCapabilityTask(ctx, principal, id)
		if err != nil || task.ID != id || task.CreatedBy != principal.UserID {
			return apperrors.ErrAccessDenied
		}
	}
	return nil
}
