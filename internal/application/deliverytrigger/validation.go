package deliverytrigger

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/deliverytrigger"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/cron"
)

func validateInput(id string, input domain.Input) error {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 200 || input.TargetID == "" || len(input.TargetID) > 200 || id == "" && input.ExpectedRevision != 0 || id != "" && input.ExpectedRevision < 1 || len(input.ServiceAccountToken) > 8192 {
		return apperrors.ErrInvalidArgument
	}
	if input.TargetKind != "workflow" && input.TargetKind != "template_source" || input.TargetKind == "workflow" && input.WorkflowVersion < 1 || input.TargetKind == "template_source" && input.WorkflowVersion != 0 {
		return apperrors.ErrInvalidArgument
	}
	switch input.Type {
	case "webhook":
		return validateWebhookInput(input)
	case "schedule", "poll":
		return validateTimedInput(input)
	default:
		return apperrors.ErrInvalidArgument
	}
}

func validateWebhookInput(input domain.Input) error {
	w := input.Webhook
	if w == nil || input.Schedule != nil {
		return apperrors.ErrInvalidArgument
	}
	if w.Provider != "gitlab_standard" || w.RepositoryID == "" || len(w.RepositoryID) > 200 || w.RefType != "branch" && w.RefType != "tag" {
		return apperrors.ErrInvalidArgument
	}
	if w.RefValue == "" || len(w.RefValue) > 512 || strings.ContainsAny(w.RefValue, "\x00\r\n") || strings.HasPrefix(w.RefValue, "-") {
		return apperrors.ErrInvalidArgument
	}
	return nil
}

func validateTimedInput(input domain.Input) error {
	if input.Webhook != nil || input.Schedule == nil || input.WebhookSigningSecret != "" {
		return apperrors.ErrInvalidArgument
	}
	if input.Type == "poll" && input.TargetKind != "template_source" || input.Type == "schedule" && input.TargetKind != "workflow" {
		return apperrors.ErrInvalidArgument
	}
	if input.Type == "poll" && (input.Schedule.Cron == "" || len(input.Schedule.RunAt) > 0) {
		return fmt.Errorf("%w: polling requires cron", apperrors.ErrInvalidArgument)
	}
	return validateSchedule(*input.Schedule)
}

func validateSchedule(schedule domain.Schedule) error {
	if schedule.TimeZone == "" || len(schedule.TimeZone) > 100 || len(schedule.Cron) > 200 || len(schedule.RunAt) > 100 || len(schedule.ExcludedDates) > 366 || schedule.Cron == "" && len(schedule.RunAt) == 0 {
		return apperrors.ErrInvalidArgument
	}
	if _, err := time.LoadLocation(schedule.TimeZone); err != nil {
		return fmt.Errorf("%w: select an IANA time zone", apperrors.ErrInvalidArgument)
	}
	if schedule.Cron != "" {
		if err := cron.Validate(schedule.Cron); err != nil {
			return fmt.Errorf("%w: %s", apperrors.ErrInvalidArgument, err)
		}
	}
	for _, at := range schedule.RunAt {
		if at.IsZero() || at.Second() != 0 || at.Nanosecond() != 0 {
			return fmt.Errorf("%w: calendar times must use minute precision", apperrors.ErrInvalidArgument)
		}
	}
	for _, date := range schedule.ExcludedDates {
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return fmt.Errorf("%w: invalid excluded date", apperrors.ErrInvalidArgument)
		}
	}
	return nil
}

func scheduleSlot(schedule domain.Schedule, now time.Time) (string, bool) {
	location, err := time.LoadLocation(schedule.TimeZone)
	if err != nil {
		return "", false
	}
	at := now.In(location)
	if slices.Contains(schedule.ExcludedDates, at.Format("2006-01-02")) {
		return "", false
	}
	match := schedule.Cron != "" && cron.Matches(schedule.Cron, at)
	for _, planned := range schedule.RunAt {
		if planned.Equal(now.Truncate(time.Minute)) {
			match = true
		}
	}
	return at.Format("2006-01-02T15:04"), match
}

func (s *Service) target(ctx context.Context, p domainidentity.Principal, item domain.StoredTrigger, execute bool) (string, error) {
	var definition any
	var err error
	if item.TargetKind == "template_source" {
		definition, err = s.sourceTarget(ctx, p, item, execute)
	} else {
		definition, err = s.workflowTarget(ctx, p, item, execute)
	}
	if err != nil {
		return "", err
	}

	if item.Webhook != nil {
		repository, err := s.repositories.GetRepository(ctx, p, item.Webhook.RepositoryID)
		if err != nil {
			return "", err
		}
		if execute && (repository.Provider != "gitlab" || repository.SourceConnectionID == "" || repository.ProviderRepositoryID == "") {
			return "", fmt.Errorf("%w: use a registered GitLab repository", apperrors.ErrInvalidArgument)
		}
		definition = map[string]any{"target": definition, "repositoryId": repository.ID, "providerRepositoryId": repository.ProviderRepositoryID, "connectionId": repository.SourceConnectionID, "url": repository.URL}
	}
	return digest(definition)
}

func workflowUsesWebhookRef(definition domainworkflow.DeliveryWorkflowDefinition, w domain.Webhook) bool {
	for _, target := range definition.Targets {
		if target.Action != "build" && target.Action != "build_deploy" {
			continue
		}
		for _, ref := range target.RepositoryRefs {
			if ref.RepositoryID == w.RepositoryID && ref.RefType == string(w.RefType) && ref.RefName == w.RefValue {
				return true
			}
		}
	}
	return false
}

func (s *Service) sourceTarget(ctx context.Context, p domainidentity.Principal, item domain.StoredTrigger, execute bool) (any, error) {
	source, err := s.sources.Get(ctx, p, item.TargetID)
	if err != nil {
		return "", err
	}
	if execute {
		if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, p, "delivery.template-sources.sync"); err != nil {
			return "", err
		}
		if !source.Enabled {
			return "", fmt.Errorf("%w: source is disabled", apperrors.ErrConflict)
		}
		if item.Webhook != nil && (item.Webhook.RepositoryID != source.RepositoryID || string(item.Webhook.RefType) != string(source.RefType) || item.Webhook.RefValue != source.RefValue) {
			return "", fmt.Errorf("%w: webhook ref must match the source", apperrors.ErrInvalidArgument)
		}
	}
	// Sync progress and generation do not change the configured source intent.
	return map[string]any{"repositoryId": source.RepositoryID, "refType": source.RefType, "refValue": source.RefValue, "path": source.Path, "kinds": source.Kinds, "include": source.IncludePatterns, "exclude": source.ExcludePatterns}, nil
}

func (s *Service) workflowTarget(ctx context.Context, p domainidentity.Principal, item domain.StoredTrigger, execute bool) (any, error) {
	workflow, err := s.workflows.GetDeliveryWorkflow(ctx, p, item.TargetID)
	if err != nil {
		return "", err
	}
	if execute {
		if workflow.Version != int64(item.WorkflowVersion) {
			return "", fmt.Errorf("%w: workflow version changed; update its trigger", apperrors.ErrConflict)
		}
		if _, err := s.workflows.PrepareDeliveryWorkflow(ctx, p, item.TargetID, domainworkflow.DeliveryWorkflowInput{ExpectedVersion: &workflow.Version, Definition: workflow.Definition}); err != nil {
			return "", err
		}
		if item.Webhook != nil && !workflowUsesWebhookRef(workflow.Definition, *item.Webhook) {
			return "", fmt.Errorf("%w: workflow must explicitly select the webhook repository ref", apperrors.ErrInvalidArgument)
		}
	}
	return workflow.Definition, nil
}
