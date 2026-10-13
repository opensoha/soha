package copilot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) DeletePersonalEnvironmentData(ctx context.Context, principal domainidentity.Principal, input sohaapi.PersonalEnvironmentDataDeletionRequest, locale string) (domain.PersonalEnvironment, error) {
	if input.AssistantID == "" || input.EnvironmentVersion < 1 || input.ConfirmationName == "" || !utf8.ValidString(input.ConfirmationName) || utf8.RuneCountInString(input.ConfirmationName) > 60 {
		return domain.PersonalEnvironment{}, apperrors.ErrInvalidArgument
	}
	assistant, err := s.EnsurePersonalAssistant(ctx, principal, locale)
	if err != nil {
		return domain.PersonalEnvironment{}, err
	}
	if assistant.ID != input.AssistantID || assistant.Name != input.ConfirmationName {
		return domain.PersonalEnvironment{}, apperrors.ErrConflict
	}
	item, err := s.personalEnvironmentForChange(ctx, principal, "reconcile", "", locale)
	if err != nil {
		return domain.PersonalEnvironment{}, err
	}
	if item.Version != input.EnvironmentVersion {
		return domain.PersonalEnvironment{}, apperrors.ErrConflict
	}
	reserved, err := s.personalEnvironment.Store.ReservePersonalDataDeletion(ctx, item, input.ConfirmationName, uuid.NewString())
	if err != nil {
		return domain.PersonalEnvironment{}, err
	}
	request := contractenv.Request{SchemaVersion: contractenv.RequestVersion, Action: "delete_data", Lease: reserved.Lease}
	view, _, err := s.invokeReservedPersonalEnvironment(ctx, principal, "delete_data", item, reserved, request)
	return view, err
}

func (s *Service) ExportPersonalEnvironmentData(ctx context.Context, principal domainidentity.Principal, input sohaapi.PersonalEnvironmentDataExportRequest, locale string, target io.Writer) (contractenv.ExportResult, error) {
	if input.AssistantID == "" || input.EnvironmentVersion < 1 {
		return contractenv.ExportResult{}, apperrors.ErrInvalidArgument
	}
	item, err := s.personalEnvironmentForChange(ctx, principal, "reconcile", "", locale)
	if err != nil {
		return contractenv.ExportResult{}, err
	}
	if item.AssistantID != input.AssistantID || item.Version != input.EnvironmentVersion || item.State != "sleeping" || !item.DataInitialized || item.ActiveTaskID != "" {
		return contractenv.ExportResult{}, apperrors.ErrConflict
	}
	if s.personalEnvironment.Export == nil {
		return contractenv.ExportResult{}, apperrors.ErrServiceUnavailable
	}
	reserved, err := s.personalEnvironment.Store.ReservePersonalEnvironment(ctx, item, item.Lease, uuid.NewString(), false)
	if err != nil {
		return contractenv.ExportResult{}, err
	}
	s.auditPersonalEnvironment(ctx, principal, reserved, "export_data", "reserved")
	request := contractenv.Request{SchemaVersion: contractenv.RequestVersion, Action: "export_data", Lease: item.Lease}
	started := time.Now().UTC()
	result, invokeErr := s.personalEnvironment.Export(ctx, principal, item.AgentClusterID, request, target)
	if errors.Is(invokeErr, domain.ErrPersonalEnvironmentNotDispatched) {
		_, err := s.abortPersonalEnvironment(ctx, principal, "export_data", reserved, item, invokeErr)
		return contractenv.ExportResult{}, err
	}
	if invokeErr == nil {
		invokeErr = result.ValidateFor(request)
		if result.ObservedAt.Before(started.Add(-time.Minute)) || result.ObservedAt.After(time.Now().UTC().Add(time.Minute)) {
			invokeErr = errors.New("personal data export receipt is stale")
		}
	}
	reserved.State, reserved.ObservedAt = "unknown", nil
	if invokeErr == nil {
		reserved.State, reserved.ObservedAt = "sleeping", &result.ObservedAt
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	completed, err := s.personalEnvironment.Store.CompletePersonalEnvironment(commitCtx, reserved)
	if err != nil {
		return contractenv.ExportResult{}, err
	}
	s.auditPersonalEnvironment(commitCtx, principal, completed, "export_data", completed.State)
	if invokeErr != nil {
		return contractenv.ExportResult{}, fmt.Errorf("%w: personal data export was not verified; reconcile the original environment", apperrors.ErrServiceUnavailable)
	}
	return result, nil
}
