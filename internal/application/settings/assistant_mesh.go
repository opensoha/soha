package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type AssistantMeshSettingsWriter interface {
	UpdateAssistantMesh(context.Context, string, map[string]any, func(context.Context, map[string]any, bool) error) error
}

type AssistantMeshUsers interface {
	GetByID(context.Context, string) (domainidentity.User, error)
}

func (s *Service) SetAssistantMeshSettingsDependencies(writer AssistantMeshSettingsWriter, users AssistantMeshUsers) {
	s.assistantMeshWriter = writer
	s.assistantMeshUsers = users
}

func (s *Service) GetAssistantMeshSettings(ctx context.Context, principal domainidentity.Principal) (domainsettings.AssistantMeshSettings, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermSettingsAIManage, "update")); err != nil {
		return domainsettings.AssistantMeshSettings{}, err
	}
	return s.ResolveAssistantMeshSettings(ctx)
}

func (s *Service) ResolveAssistantMeshSettings(ctx context.Context) (domainsettings.AssistantMeshSettings, error) {
	value, found, err := s.store.Get(ctx, domainsettings.AssistantMeshSettingKey)
	if err != nil {
		return domainsettings.AssistantMeshSettings{}, err
	}
	return decodeAssistantMeshSettings(value, found)
}

func (s *Service) UpdateAssistantMeshSettings(ctx context.Context, principal domainidentity.Principal, input domainsettings.AssistantMeshSettings) (domainsettings.AssistantMeshSettings, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermSettingsAIManage, "update")); err != nil {
		return domainsettings.AssistantMeshSettings{}, err
	}
	if err := validateAssistantMeshSettings(input); err != nil {
		return domainsettings.AssistantMeshSettings{}, err
	}
	if s.assistantMeshWriter == nil || s.assistantMeshUsers == nil || s.audit == nil {
		return domainsettings.AssistantMeshSettings{}, apperrors.ErrServiceUnavailable
	}
	if err := s.validateAssistantMeshApprovers(ctx, input.OrganizationApproverUserIDs); err != nil {
		return domainsettings.AssistantMeshSettings{}, err
	}
	meta := requestctx.FromContext(ctx)
	after := assistantMeshValues(input)
	err := s.assistantMeshWriter.UpdateAssistantMesh(ctx, principal.UserID, after, func(tx context.Context, raw map[string]any, found bool) error {
		before, err := decodeAssistantMeshSettings(raw, found)
		if err != nil {
			return err
		}
		if input.DelegationLifetimeHours == 0 {
			input.DelegationLifetimeHours = before.DelegationLifetimeHours
			after["delegationLifetimeHours"] = input.DelegationLifetimeHours
		}
		retainInquirySettings(&input, before)
		for _, field := range assistantMeshInquiryFields(&input) {
			after[field.key] = *field.value
		}
		return s.audit.Record(tx, domainaudit.Entry{
			ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams,
			ResourceKind: "AssistantMeshSettings", ResourceName: domainsettings.AssistantMeshSettingKey,
			Action: "settings.ai.assistant_mesh.update", Result: "success", Summary: "updated Heya Assistant Mesh settings",
			RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP,
			Metadata: map[string]any{"before": assistantMeshValues(before), "after": after},
		})
	})
	if err != nil {
		return domainsettings.AssistantMeshSettings{}, err
	}
	return input, nil
}

func (s *Service) validateAssistantMeshApprovers(ctx context.Context, ids []string) error {
	for _, id := range ids {
		user, err := s.assistantMeshUsers.GetByID(ctx, id)
		if errors.Is(err, apperrors.ErrNotFound) || err == nil && (user.ID != id || user.Status != "active") {
			return fmt.Errorf("%w: publication reviewers must be active users", apperrors.ErrInvalidArgument)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func validateAssistantMeshSettings(input domainsettings.AssistantMeshSettings) error {
	switch input.DefaultVisibility {
	case "private", "teams", "projects":
	default:
		return fmt.Errorf("%w: invalid default visibility", apperrors.ErrInvalidArgument)
	}
	if input.ExperienceCardLifetimeDays < 7 || input.ExperienceCardLifetimeDays > 365 {
		return fmt.Errorf("%w: experience card lifetime must be between 7 and 365 days", apperrors.ErrInvalidArgument)
	}
	if input.DelegationLifetimeHours < 0 || input.DelegationLifetimeHours > 72 {
		return fmt.Errorf("%w: delegation lifetime must be between 1 and 72 hours", apperrors.ErrInvalidArgument)
	}
	if input.OrganizationApproverUserIDs == nil || len(input.OrganizationApproverUserIDs) > 100 {
		return fmt.Errorf("%w: invalid publication reviewer list", apperrors.ErrInvalidArgument)
	}
	for _, field := range assistantMeshInquiryFields(&input) {
		if *field.value != 0 && (*field.value < field.minimum || *field.value > field.maximum) {
			return fmt.Errorf("%w: %s must be between %d and %d", apperrors.ErrInvalidArgument, field.key, field.minimum, field.maximum)
		}
	}
	seen := make(map[string]struct{}, len(input.OrganizationApproverUserIDs))
	for _, id := range input.OrganizationApproverUserIDs {
		parsed, err := uuid.Parse(id)
		if err != nil || parsed.String() != id {
			return fmt.Errorf("%w: invalid publication reviewer ID", apperrors.ErrInvalidArgument)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("%w: duplicate publication reviewer", apperrors.ErrInvalidArgument)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func decodeAssistantMeshSettings(raw map[string]any, found bool) (domainsettings.AssistantMeshSettings, error) {
	if !found {
		return domainsettings.DefaultAssistantMeshSettings(), nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return domainsettings.AssistantMeshSettings{}, err
	}
	item := domainsettings.DefaultAssistantMeshSettings()
	if err := json.Unmarshal(data, &item); err != nil {
		return domainsettings.AssistantMeshSettings{}, err
	}
	retainInquirySettings(&item, domainsettings.DefaultAssistantMeshSettings())
	return item, validateAssistantMeshSettings(item)
}

func assistantMeshValues(item domainsettings.AssistantMeshSettings) map[string]any {
	values := map[string]any{
		"delegationLifetimeHours":       item.DelegationLifetimeHours,
		"enabled":                       item.Enabled,
		"organizationApproverUserIds":   item.OrganizationApproverUserIDs,
		"defaultVisibility":             item.DefaultVisibility,
		"experienceCardLifetimeDays":    item.ExperienceCardLifetimeDays,
		"allowPermanentExperienceCards": item.AllowPermanentExperienceCards,
	}
	for _, field := range assistantMeshInquiryFields(&item) {
		values[field.key] = *field.value
	}
	return values
}

type inquirySettingField struct {
	key              string
	value            *int
	minimum, maximum int
}

func assistantMeshInquiryFields(item *domainsettings.AssistantMeshSettings) []inquirySettingField {
	return []inquirySettingField{
		{"inquiryPairLimitPerHour", &item.InquiryPairLimitPerHour, 1, 60},
		{"inquiryCallerLimitPerDay", &item.InquiryCallerLimitPerDay, 1, 500},
		{"inquiryMergeWindowHours", &item.InquiryMergeWindowHours, 1, 168},
		{"inquiryPendingLimit", &item.InquiryPendingLimit, 5, 200},
		{"inquiryTimeoutHours", &item.InquiryTimeoutHours, 1, 720},
		{"inquiryRetentionDays", &item.InquiryRetentionDays, 1, 365},
	}
}

func retainInquirySettings(input *domainsettings.AssistantMeshSettings, old domainsettings.AssistantMeshSettings) {
	before := assistantMeshInquiryFields(&old)
	for i, field := range assistantMeshInquiryFields(input) {
		if *field.value == 0 {
			*field.value = *before[i].value
		}
	}
}
