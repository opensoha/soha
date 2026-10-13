package copilot

import (
	"context"
	"github.com/google/uuid"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	virtualization "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"time"
)

type PersonalGuestProvisioningStore interface {
	PersonalGuestPreparationStore
	ReservePersonalGuestData(context.Context, domain.PersonalEnvironmentRecord, string) error
	GuestDataCheckpoint(context.Context, guest.DataPlan, string, guest.DataResult) error
	PersonalGuestDataLease(context.Context, guest.Binding) (guest.DataPlan, error)
}

type PersonalGuestProvisioner interface {
	QueuePersonalGuest(context.Context, identity.Principal, guest.Provisioning) (virtualization.Task, error)
	AuthorizePersonalGuestReconciliation(context.Context, identity.Principal, string, string) error
	ConfigurePersonalGuest(func(context.Context, guest.Binding) error, func(context.Context, guest.DataPlan, string, guest.DataResult) error, func(context.Context, func(context.Context) error) error)
}

func (s *PersonalGuestPreparationService) ConfigureProvisioning(p PersonalGuestProvisioner, artifactSHA string) {
	s.provisioner, s.artifactSHA = p, artifactSHA
}

// Reserve, certificate issuance, encrypted queue input and audit share one transaction.
func (s *PersonalGuestPreparationService) Provision(ctx context.Context, principal identity.Principal, input sohaapi.PersonalGuestPreparationInput) (virtualization.Task, error) {
	return s.provision(ctx, principal, input, "")
}

func (s *PersonalGuestPreparationService) Reconcile(ctx context.Context, principal identity.Principal, operationID string, input sohaapi.PersonalGuestPreparationInput) (virtualization.Task, error) {
	return s.provision(ctx, principal, input, operationID)
}

func (s *PersonalGuestPreparationService) provision(ctx context.Context, principal identity.Principal, input sohaapi.PersonalGuestPreparationInput, originalOperation string) (virtualization.Task, error) {
	store, ok := s.store.(PersonalGuestProvisioningStore)
	if !ok || s.provisioner == nil || s.artifactSHA == "" {
		return virtualization.Task{}, apperrors.ErrServiceUnavailable
	}
	var task virtualization.Task
	err := store.WithinPersonalGuestTransaction(ctx, func(txCtx context.Context) error {
		env, _, _, err := store.LockPersonalGuestPreparation(txCtx, principal.UserID)
		if err != nil {
			return err
		}
		if env.OperationID != originalOperation || env.Version != input.EnvironmentVersion {
			return apperrors.ErrConflict
		}
		if originalOperation != "" {
			if err := s.provisioner.AuthorizePersonalGuestReconciliation(txCtx, principal, originalOperation, env.ComputeRef); err != nil {
				return err
			}
		}
		prepared, err := s.prepare(txCtx, principal, input, originalOperation)
		if err != nil {
			return err
		}
		record, err := s.guest.store.GetPersonalGuest(txCtx, prepared.BindingID.String())
		if err != nil {
			return err
		}
		quota := env.Lease.Quota
		operationID := uuid.NewString()
		if originalOperation != "" {
			operationID = originalOperation
		}
		bundle := guest.Provisioning{Plan: guest.DataPlan{SchemaVersion: guest.DataPlanVersion, OperationID: operationID, Binding: record.Binding, DiskQuotaBytes: quota.DiskBytes},
			Endpoint: prepared.Endpoint, EnrollmentID: prepared.EnrollmentID.String(), Challenge: prepared.Challenge, CertificatePEM: prepared.CertificatePEM, ClientCAPEM: prepared.ClientCAPEM, ArtifactSHA256: s.artifactSHA,
			CPUQuotaMilli: quota.CPUUnits, MemoryBytes: quota.MemoryBytes, PidsLimit: int64(quota.MaxProcesses), Network: quota.NetworkEgress}
		if bundle.Plan.Validate(time.Now()) != nil || quota.ValidatePersonalDocker() != nil {
			return apperrors.ErrConflict
		}
		if err := store.ReservePersonalGuestData(txCtx, env, operationID); err != nil {
			return err
		}
		task, err = s.provisioner.QueuePersonalGuest(txCtx, principal, bundle)
		return err
	})
	return task, err
}
