package copilot

import (
	"context"
	"fmt"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type InspectionTriggerStore interface {
	QueueDueInspectionRuns(context.Context, time.Time, int) error
	PendingInspectionRunIDs(context.Context, int) ([]string, error)
	QueueManualInspectionRun(context.Context, string, string, string, int64) (domain.InspectionRun, error)
	WithInspectionRun(context.Context, string, func(context.Context, domain.InspectionTask, domain.InspectionRun) (domain.InspectionRun, error)) (domain.InspectionRun, error)
}

type InspectionCapabilityPlanner interface {
	ValidateCapabilityPlan(context.Context, domainidentity.Principal, domainai.CapabilityTaskInput) (domainai.CapabilityPlanValidation, error)
	MaterializeRegisteredCapabilityPlan(context.Context, domainidentity.Principal, domainai.CapabilityTaskInput) (domainai.CapabilityTaskInput, error)
}

type InspectionCapabilityExecutor interface {
	CreateCapabilityTask(context.Context, domainidentity.Principal, domainai.CapabilityTaskInput) (domainai.CapabilityTask, error)
}

func (s *Service) SetInspectionCapabilityRuntime(planner InspectionCapabilityPlanner, executor InspectionCapabilityExecutor) {
	s.inspectionRuntimeMu.Lock()
	defer s.inspectionRuntimeMu.Unlock()
	s.inspectionPlanner, s.inspectionExecutor = planner, executor
}

func (s *Service) inspectionCapabilityRuntime() (InspectionCapabilityPlanner, InspectionCapabilityExecutor, error) {
	s.inspectionRuntimeMu.RLock()
	defer s.inspectionRuntimeMu.RUnlock()
	if s.inspectionPlanner == nil || s.inspectionExecutor == nil {
		return nil, nil, fmt.Errorf("%w: inspection capability runtime unavailable", apperrors.ErrServiceUnavailable)
	}
	return s.inspectionPlanner, s.inspectionExecutor, nil
}

func (s *Service) validateInspectionRegistration(ctx context.Context, principal domainidentity.Principal, input *domain.InspectionTaskInput) error {
	if input.CapabilityPlan == nil {
		if input.Trigger != nil || input.AIClientID != "" || input.SkillID != "" {
			return fmt.Errorf("%w: capability trigger requires a plan", apperrors.ErrInvalidArgument)
		}
		return nil
	}
	if input.Trigger == nil || input.IntervalMinutes < 1 || input.IntervalMinutes > 525600 || len(input.Checks) > 0 {
		return fmt.Errorf("%w: specify one trigger, an interval between 1 and 525600 minutes, and a capability plan without legacy checks", apperrors.ErrInvalidArgument)
	}
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIInspectionRun); err != nil {
		return err
	}
	if err := s.validateInspectionTrigger(ctx, principal, input); err != nil {
		return err
	}
	planner, _, err := s.inspectionCapabilityRuntime()
	if err != nil {
		return err
	}
	validation, err := planner.ValidateCapabilityPlan(ctx, principal, domainai.CapabilityTaskInput{IdempotencyKey: "inspection-registration", Plan: *input.CapabilityPlan, AIClientID: input.AIClientID, SkillID: input.SkillID})
	if err != nil {
		return err
	}
	if !validation.Valid {
		return fmt.Errorf("%w: validate and fix the capability plan before registration", apperrors.ErrInvalidArgument)
	}
	return nil
}

func (s *Service) validateInspectionTrigger(ctx context.Context, principal domainidentity.Principal, input *domain.InspectionTaskInput) error {
	trigger := input.Trigger
	switch trigger.Kind {
	case "schedule":
		if trigger.AlertRuleID != "" || trigger.MaxEventAgeSeconds != 0 {
			return fmt.Errorf("%w: schedule cannot declare an alert selector", apperrors.ErrInvalidArgument)
		}
	case "alert":
		if trigger.MaxEventAgeSeconds == 0 {
			trigger.MaxEventAgeSeconds = 3600
		}
		if strings.TrimSpace(trigger.AlertRuleID) == "" || trigger.MaxEventAgeSeconds < 60 || trigger.MaxEventAgeSeconds > 86400 {
			return fmt.Errorf("%w: alert rule and bounded event age required", apperrors.ErrInvalidArgument)
		}
		if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAlertsView); err != nil {
			return err
		}
		reader, ok := s.alerts.(interface {
			GetRule(context.Context, domainidentity.Principal, string) (domainalert.AlertRule, error)
		})
		if !ok {
			return apperrors.ErrServiceUnavailable
		}
		if _, err := reader.GetRule(ctx, principal, trigger.AlertRuleID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: unsupported inspection trigger", apperrors.ErrInvalidArgument)
	}
	return nil
}

func (s *Service) inspectionExecutionPrincipal(ctx context.Context, task domain.InspectionTask) (domainidentity.Principal, error) {
	reader, ok := s.agentPrincipals.(interface {
		CurrentExecutionPrincipal(context.Context, string, string) (domainidentity.Principal, error)
	})
	if !ok {
		return domainidentity.Principal{}, apperrors.ErrServiceUnavailable
	}
	principal, err := reader.CurrentExecutionPrincipal(ctx, task.CreatedBy, task.ExecutionTokenID)
	if err != nil {
		return principal, err
	}
	if principal.UserID == "" || principal.UserID != task.CreatedBy {
		return principal, apperrors.ErrAccessDenied
	}
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIInspectionRun); err != nil {
		return principal, err
	}
	if task.Trigger != nil && task.Trigger.Kind == "alert" {
		if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAlertsView); err != nil {
			return principal, err
		}
		if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAlertRulesView); err != nil {
			return principal, err
		}
	}
	return principal, nil
}

func (s *Service) handoffInspectionRun(ctx context.Context, task domain.InspectionTask, run domain.InspectionRun) (domain.InspectionRun, error) {
	principal, err := s.inspectionExecutionPrincipal(ctx, task)
	if err != nil {
		return run, err
	}
	if task.CapabilityPlan == nil {
		result := s.buildInspectionRun(ctx, principal, task, run.TriggeredBy, localeFromInspectionMetadata(task.Metadata, ""))
		result.ID, result.StartedAt, result.CreatedAt = run.ID, run.StartedAt, run.CreatedAt
		result.Report["registrationRevision"] = task.Revision
		return result, nil
	}
	planner, executor, err := s.inspectionCapabilityRuntime()
	if err != nil {
		return run, err
	}
	input, err := planner.MaterializeRegisteredCapabilityPlan(ctx, principal, domainai.CapabilityTaskInput{IdempotencyKey: run.ID, Plan: *task.CapabilityPlan, AIClientID: task.AIClientID, SkillID: task.SkillID})
	if err != nil {
		return run, err
	}
	capability, err := executor.CreateCapabilityTask(ctx, principal, input)
	if err != nil {
		return run, err
	}
	run.Status, run.Summary = "handed_off", "Capability task created; inspect its evidence to determine the outcome"
	run.Report["capabilityTaskId"] = capability.ID
	return run, nil
}

// Explicit manual execution uses the same receipt and authorization as triggers.
func (s *Service) ExecuteRegisteredInspection(ctx context.Context, principal domainidentity.Principal, taskID, key string, revision int64) (domain.InspectionRun, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIInspectionRun); err != nil {
		return domain.InspectionRun{}, err
	}
	if strings.TrimSpace(key) == "" || len(key) > 128 || revision < 1 {
		return domain.InspectionRun{}, apperrors.ErrInvalidArgument
	}
	store, ok := s.inspectionTasks.(InspectionTriggerStore)
	if !ok {
		return domain.InspectionRun{}, apperrors.ErrServiceUnavailable
	}
	// Re-read the owned registration before returning even an earlier receipt.
	if _, err := s.inspectionTasks.GetInspectionTask(ctx, principal.UserID, taskID); err != nil {
		return domain.InspectionRun{}, err
	}
	run, err := store.QueueManualInspectionRun(ctx, principal.UserID, taskID, key, revision)
	if err != nil {
		return run, err
	}
	// Only persist the handoff here; the existing worker owns actual effects.
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return store.WithInspectionRun(runCtx, run.ID, s.handoffInspectionRun)
}
