package delivery

import (
	"context"
	"errors"
	"fmt"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func validateActiveReferenceInput(input domaindelivery.ServiceReferenceInput) error {
	if !validCandidateID(input.ExpectedCandidateID) || input.ExpectedCandidateRevision < 1 || input.ExpectedConsumerUpdatedAt.IsZero() || input.ExpectedSourceUpdatedAt.IsZero() || !validReferenceDigest(input.ExpectedSourceRevision) || input.ExpectedRevision == nil || *input.ExpectedRevision < 0 {
		return fmt.Errorf("%w: candidate identity/revision, both binding revisions and source revision required", apperrors.ErrInvalidArgument)
	}
	if (*input.ExpectedRevision == 0 && input.ExpectedReferenceID != "") || (*input.ExpectedRevision > 0 && !validCandidateID(input.ExpectedReferenceID)) {
		return fmt.Errorf("%w: existing reference identity required only for updates", apperrors.ErrInvalidArgument)
	}
	return nil
}

func (s *Service) PutEnvironmentServiceReference(ctx context.Context, principal domainidentity.Principal, consumerID, serviceID string, input domaindelivery.ServiceReferenceInput) (domaindelivery.ServiceReference, error) {
	var empty domaindelivery.ServiceReference
	if !validCandidateID(consumerID) || !validCandidateID(serviceID) {
		return empty, fmt.Errorf("%w: explicit consumer and service required", apperrors.ErrInvalidArgument)
	}
	if err := validateActiveReferenceInput(input); err != nil {
		return empty, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	candidate, err := s.loadActiveReferenceCandidate(ctx, principal, consumerID, serviceID, input)
	if err != nil {
		return empty, err
	}
	previewInput := domaindelivery.ServiceReferencePreviewInput{ServiceID: serviceID, SourceApplicationEnvironmentID: candidate.Preview.Source.ApplicationEnvironmentID, ExpectedConsumerUpdatedAt: input.ExpectedConsumerUpdatedAt, ExpectedSourceUpdatedAt: input.ExpectedSourceUpdatedAt, SourceTargetIDs: candidateTargetIDs(candidate), ExpectedSourceRevision: input.ExpectedSourceRevision}
	preview, state, err := s.previewReferenceWithAccess(ctx, principal, consumerID, previewInput, true)
	if err != nil {
		return empty, err
	}
	write, err := prepareActiveReferenceWrite(candidate, preview, state, input)
	if err != nil {
		return empty, err
	}
	store, err := s.referenceActiveStore()
	if err != nil {
		return empty, err
	}
	if _, err := store.GetServiceReference(ctx, state.source.ID, serviceID); err == nil {
		return empty, fmt.Errorf("%w: a reference cannot be used as a source", apperrors.ErrConflict)
	} else if !errors.Is(err, apperrors.ErrNotFound) {
		return empty, err
	}
	saved, err := store.SaveServiceReference(ctx, write, func(txctx context.Context) (domaindelivery.ServiceReferenceConfiguration, error) {
		current, err := s.loadReferenceContextWithAccess(txctx, principal, consumerID, previewInput, true)
		return referenceConfiguration(current), err
	})
	if err != nil {
		return empty, err
	}
	s.recordActiveReference(ctx, principal, "save", consumerID, serviceID, saved.Reference.ID, saved.Reference.Revision)
	return saved.Reference, nil
}

func (s *Service) loadActiveReferenceCandidate(ctx context.Context, principal domainidentity.Principal, consumerID, serviceID string, input domaindelivery.ServiceReferenceInput) (domaindelivery.ServiceReferenceCandidate, error) {
	var empty domaindelivery.ServiceReferenceCandidate
	consumer, err := s.authorizeReferenceCandidateConsumer(ctx, principal, consumerID)
	if err != nil {
		return empty, err
	}
	candidates, err := s.referenceCandidateStore()
	if err != nil {
		return empty, err
	}
	candidate, err := candidates.GetServiceReferenceCandidate(ctx, consumerID, serviceID)
	if err != nil {
		return empty, err
	}
	if candidate.ID != input.ExpectedCandidateID || candidate.Revision != input.ExpectedCandidateRevision || candidate.Preview.ApplicationID != consumer.ApplicationID || candidate.Preview.ServiceID != serviceID {
		return empty, fmt.Errorf("%w: saved candidate changed", apperrors.ErrConflict)
	}
	return candidate, nil
}

func prepareActiveReferenceWrite(candidate domaindelivery.ServiceReferenceCandidate, preview domaindelivery.ServiceReferencePreview, state referenceContext, input domaindelivery.ServiceReferenceInput) (domaindelivery.ServiceReferenceWrite, error) {
	var empty domaindelivery.ServiceReferenceWrite
	if len(preview.CurrentLocalTargetIDs) != 0 {
		return empty, fmt.Errorf("%w: remove the consumer's local service targets explicitly before activation", apperrors.ErrConflict)
	}
	identity, err := domaindelivery.ServiceReferenceIdentityRevision(preview)
	if err != nil {
		return empty, err
	}
	revision, err := referenceConfiguration(state).Revision()
	if err != nil {
		return empty, err
	}
	if candidate.ContextRevision != revision || candidate.SourceIdentityRevision != identity || candidate.Preview.SourceRevision != preview.SourceRevision {
		return empty, fmt.Errorf("%w: candidate is outdated; save a fresh candidate", apperrors.ErrConflict)
	}
	control, err := domaindelivery.ServiceReferenceControlRevision(state.source, preview.ServiceID)
	if err != nil {
		return empty, err
	}
	consumerRevision, err := (domaindelivery.ServiceReferenceConfiguration{Consumer: state.consumer}).Revision()
	if err != nil {
		return empty, err
	}
	record := domaindelivery.ServiceReferenceRecord{ApplicationID: state.consumer.ApplicationID, ConsumerBindingID: state.consumer.ID, ServiceID: preview.ServiceID, SourceBindingID: state.source.ID, SourceTargetIDs: candidateTargetIDs(candidate), ControlRevision: control, ConsumerConfigurationRevision: consumerRevision, Reference: domaindelivery.ServiceReference{SourceIdentityRevision: identity, CurrentSourceRevision: preview.SourceRevision, Preview: preview}}
	return domaindelivery.ServiceReferenceWrite{Record: record, ExpectedCandidateID: input.ExpectedCandidateID, ExpectedCandidateRevision: input.ExpectedCandidateRevision, ExpectedReferenceID: input.ExpectedReferenceID, ExpectedRevision: *input.ExpectedRevision, ConfigurationRevision: revision}, nil
}

func (s *Service) GetEnvironmentServiceReference(ctx context.Context, principal domainidentity.Principal, consumerID, serviceID string) (domaindelivery.ServiceReference, error) {
	var empty domaindelivery.ServiceReference
	if !validCandidateID(consumerID) || !validCandidateID(serviceID) {
		return empty, fmt.Errorf("%w: explicit consumer and service required", apperrors.ErrInvalidArgument)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	// Reading an active reference requires view permission, never consumer update.
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationEnvView); err != nil {
		return empty, err
	}
	consumer, err := s.catalog.GetApplicationEnvironment(ctx, principal, consumerID)
	if err != nil {
		return empty, err
	}
	store, err := s.referenceActiveStore()
	if err != nil {
		return empty, err
	}
	item, err := store.GetServiceReference(ctx, consumerID, serviceID)
	if err != nil {
		return empty, err
	}
	if item.ApplicationID != consumer.ApplicationID {
		return empty, fmt.Errorf("%w: reference application changed", apperrors.ErrConflict)
	}
	source, err := s.catalog.GetApplicationEnvironment(ctx, principal, item.SourceBindingID)
	if err != nil {
		return empty, err
	}
	input := domaindelivery.ServiceReferencePreviewInput{ServiceID: serviceID, SourceApplicationEnvironmentID: source.ID, ExpectedConsumerUpdatedAt: consumer.UpdatedAt, ExpectedSourceUpdatedAt: source.UpdatedAt, SourceTargetIDs: item.SourceTargetIDs}
	current, state, err := s.previewReferenceWithAccess(ctx, principal, consumerID, input, false)
	if err != nil {
		return empty, err
	}
	if err := validateActiveReferenceObservation(item, current, state); err != nil {
		return empty, err
	}
	latest, err := store.GetServiceReference(ctx, consumerID, serviceID)
	if err != nil {
		return empty, err
	}
	if latest.Reference.ID != item.Reference.ID || latest.Reference.Revision != item.Reference.Revision {
		return empty, fmt.Errorf("%w: reference changed during source validation", apperrors.ErrConflict)
	}
	item.Reference.Preview = current
	item.Reference.CurrentSourceRevision = current.SourceRevision
	return item.Reference, nil
}

func (s *Service) DeleteEnvironmentServiceReference(ctx context.Context, principal domainidentity.Principal, consumerID, serviceID string, input domaindelivery.ServiceReferenceDeleteInput) error {
	if !validCandidateID(consumerID) || !validCandidateID(serviceID) || !validCandidateID(input.ExpectedReferenceID) || input.ExpectedRevision < 1 || input.ExpectedConsumerUpdatedAt.IsZero() {
		return fmt.Errorf("%w: consumer, reference identity and revisions required", apperrors.ErrInvalidArgument)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	authorize := func(ctx context.Context) error {
		consumer, err := s.authorizeReferenceCandidateConsumer(ctx, principal, consumerID)
		if err != nil {
			return err
		}
		if !consumer.UpdatedAt.Equal(input.ExpectedConsumerUpdatedAt) {
			return fmt.Errorf("%w: consumer configuration changed", apperrors.ErrConflict)
		}
		return nil
	}
	if err := authorize(ctx); err != nil {
		return err
	}
	store, err := s.referenceActiveStore()
	if err != nil {
		return err
	}
	if err := store.DeleteServiceReference(ctx, consumerID, serviceID, input, authorize); err != nil {
		return err
	}
	s.recordActiveReference(ctx, principal, "delete", consumerID, serviceID, input.ExpectedReferenceID, input.ExpectedRevision)
	return nil
}

func (s *Service) ListEnvironmentServiceReferences(ctx context.Context, principal domainidentity.Principal, consumerID, cursor string, limit int) (domaindelivery.ServiceReferencePage, error) {
	var empty domaindelivery.ServiceReferencePage
	if !validCandidateID(consumerID) || (cursor != "" && !validCandidateID(cursor)) || limit < 1 || limit > 100 {
		return empty, fmt.Errorf("%w: explicit consumer and valid page required", apperrors.ErrInvalidArgument)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	consumer, err := s.authorizeReferenceCandidateConsumer(ctx, principal, consumerID)
	if err != nil {
		return empty, err
	}
	store, err := s.referenceActiveStore()
	if err != nil {
		return empty, err
	}
	page, err := store.ListServiceReferences(ctx, consumerID, consumer.ApplicationID, cursor, limit)
	if err != nil {
		return empty, err
	}
	current, err := s.authorizeReferenceCandidateConsumer(ctx, principal, consumerID)
	if err != nil {
		return empty, err
	}
	if current.ApplicationID != consumer.ApplicationID || !current.UpdatedAt.Equal(consumer.UpdatedAt) {
		return empty, fmt.Errorf("%w: consumer configuration changed", apperrors.ErrConflict)
	}
	return page, nil
}

func (s *Service) ResolveEnvironmentServiceReferenceTarget(ctx context.Context, principal domainidentity.Principal, consumerID, serviceID, targetID, referenceID string, revision int64) (domaindelivery.ServiceReferenceResolvedTarget, error) {
	var empty domaindelivery.ServiceReferenceResolvedTarget
	if !validCandidateID(targetID) || !validCandidateID(referenceID) || revision < 1 {
		return empty, fmt.Errorf("%w: target and reference identity/revision required", apperrors.ErrInvalidArgument)
	}
	item, err := s.GetEnvironmentServiceReference(ctx, principal, consumerID, serviceID)
	if err != nil {
		return empty, err
	}
	if item.ID != referenceID || item.Revision != revision {
		return empty, fmt.Errorf("%w: reference changed", apperrors.ErrConflict)
	}
	for _, target := range item.Preview.Targets {
		if target.TargetID == targetID {
			return domaindelivery.ServiceReferenceResolvedTarget{ReferenceID: item.ID, ReferenceRevision: item.Revision, ApplicationID: item.Preview.ApplicationID, ServiceID: serviceID, View: item.Preview.Consumer, Origin: item.Preview.Source, Target: target, ObservedAt: item.Preview.ObservedAt}, nil
		}
	}
	return empty, fmt.Errorf("%w: target is outside the reference source group", apperrors.ErrNotFound)
}

func validateActiveReferenceObservation(item domaindelivery.ServiceReferenceRecord, current domaindelivery.ServiceReferencePreview, state referenceContext) error {
	identity, err := domaindelivery.ServiceReferenceIdentityRevision(current)
	if err != nil {
		return err
	}
	control, err := domaindelivery.ServiceReferenceControlRevision(state.source, item.ServiceID)
	if err != nil {
		return err
	}
	if identity != item.Reference.SourceIdentityRevision || control != item.ControlRevision || len(current.CurrentLocalTargetIDs) != 0 {
		return fmt.Errorf("%w: source identity or publication control changed; review a new candidate", apperrors.ErrConflict)
	}
	return nil
}
