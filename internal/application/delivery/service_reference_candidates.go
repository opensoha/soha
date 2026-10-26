package delivery

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/operationentry"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type ServiceReferenceCandidateRepository interface {
	ListServiceReferenceCandidates(context.Context, string, string, string, int) (domaindelivery.ServiceReferenceCandidatePage, error)
	GetServiceReferenceCandidate(context.Context, string, string) (domaindelivery.ServiceReferenceCandidate, error)
	SaveServiceReferenceCandidate(context.Context, domaindelivery.ServiceReferenceCandidateWrite, func(context.Context) (domaindelivery.ServiceReferenceConfiguration, error)) (domaindelivery.ServiceReferenceCandidate, error)
	DeleteServiceReferenceCandidate(context.Context, string, string, domaindelivery.ServiceReferenceCandidateDeleteInput, func(context.Context) error) error
}

func (s *Service) ListEnvironmentServiceReferenceCandidates(ctx context.Context, principal domainidentity.Principal, consumerID, cursor string, limit int) (domaindelivery.ServiceReferenceCandidatePage, error) {
	var empty domaindelivery.ServiceReferenceCandidatePage
	if !validCandidateID(consumerID) || (cursor != "" && !validCandidateID(cursor)) || limit < 1 || limit > 100 {
		return empty, fmt.Errorf("%w: valid consumer, cursor and a limit from 1 to 100 are required", apperrors.ErrInvalidArgument)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	consumer, err := s.authorizeReferenceCandidateConsumer(ctx, principal, consumerID)
	if err != nil {
		return empty, err
	}
	store, err := s.referenceCandidateStore()
	if err != nil {
		return empty, err
	}
	page, err := store.ListServiceReferenceCandidates(ctx, consumer.ID, consumer.ApplicationID, cursor, limit)
	if err != nil {
		return empty, err
	}
	current, err := s.authorizeReferenceCandidateConsumer(ctx, principal, consumerID)
	if err != nil {
		return empty, err
	}
	if current.ApplicationID != consumer.ApplicationID || !current.UpdatedAt.Equal(consumer.UpdatedAt) {
		return empty, fmt.Errorf("%w: consumer changed while loading candidate metadata", apperrors.ErrConflict)
	}
	return page, nil
}

func (s *Service) PutEnvironmentServiceReferenceCandidate(ctx context.Context, principal domainidentity.Principal, consumerID, serviceID string, input domaindelivery.ServiceReferenceCandidateInput) (domaindelivery.ServiceReferenceCandidate, error) {
	var empty domaindelivery.ServiceReferenceCandidate
	if !validCandidateID(consumerID) || !validCandidateID(serviceID) {
		return empty, fmt.Errorf("%w: explicit binding and service IDs are required", apperrors.ErrInvalidArgument)
	}
	if err := validateReferenceCandidateInput(input); err != nil {
		return empty, err
	}
	store, err := s.referenceCandidateStore()
	if err != nil {
		return empty, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	previewInput := input.PreviewInput(serviceID)
	preview, state, err := s.previewReferenceWithContext(ctx, principal, consumerID, previewInput)
	if err != nil {
		return empty, err
	}
	contextRevision, err := referenceConfiguration(state).Revision()
	if err != nil {
		return empty, fmt.Errorf("encode candidate context: %w", err)
	}
	identityRevision, err := domaindelivery.ServiceReferenceIdentityRevision(preview)
	if err != nil {
		return empty, fmt.Errorf("encode candidate identity: %w", err)
	}
	write := domaindelivery.ServiceReferenceCandidateWrite{ExpectedCandidateID: input.ExpectedCandidateID, ExpectedRevision: *input.ExpectedRevision,
		Candidate: domaindelivery.ServiceReferenceCandidate{Preview: preview, SourceIdentityRevision: identityRevision, ContextRevision: contextRevision}}
	saved, err := store.SaveServiceReferenceCandidate(ctx, write, func(txctx context.Context) (domaindelivery.ServiceReferenceConfiguration, error) {
		current, err := s.loadReferenceContext(txctx, principal, consumerID, previewInput)
		return referenceConfiguration(current), err
	})
	if err != nil {
		return empty, err
	}
	s.recordReferenceCandidate(ctx, principal, "save", consumerID, serviceID, saved.ID, saved.Revision)
	return saved, nil
}

func (s *Service) GetEnvironmentServiceReferenceCandidate(ctx context.Context, principal domainidentity.Principal, consumerID, serviceID string) (domaindelivery.ServiceReferenceCandidate, error) {
	var empty domaindelivery.ServiceReferenceCandidate
	if !validCandidateID(consumerID) || !validCandidateID(serviceID) {
		return empty, fmt.Errorf("%w: explicit binding and service IDs are required", apperrors.ErrInvalidArgument)
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	consumer, err := s.authorizeReferenceCandidateConsumer(ctx, principal, consumerID)
	if err != nil {
		return empty, err
	}
	store, err := s.referenceCandidateStore()
	if err != nil {
		return empty, err
	}
	item, err := store.GetServiceReferenceCandidate(ctx, consumerID, serviceID)
	if err != nil {
		return empty, err
	}
	if item.Preview.ApplicationID != consumer.ApplicationID {
		return empty, fmt.Errorf("%w: candidate application changed; review before replacing", apperrors.ErrConflict)
	}
	source, err := s.catalog.GetApplicationEnvironment(ctx, principal, item.Preview.Source.ApplicationEnvironmentID)
	if err != nil {
		return empty, err
	}
	input := domaindelivery.ServiceReferencePreviewInput{ServiceID: serviceID, SourceApplicationEnvironmentID: source.ID,
		ExpectedConsumerUpdatedAt: consumer.UpdatedAt, ExpectedSourceUpdatedAt: source.UpdatedAt, SourceTargetIDs: candidateTargetIDs(item)}
	current, state, err := s.previewReferenceWithContext(ctx, principal, consumerID, input)
	if err != nil {
		return empty, err
	}
	identity, err := domaindelivery.ServiceReferenceIdentityRevision(current)
	if err != nil {
		return empty, err
	}
	// Never disclose a cached old location after the target was reassociated.
	if identity != item.SourceIdentityRevision {
		return empty, fmt.Errorf("%w: candidate source identity changed; review a new preview", apperrors.ErrConflict)
	}
	revision, err := referenceConfiguration(state).Revision()
	if err != nil {
		return empty, err
	}
	item.Status = "current"
	if item.ContextRevision != revision || item.Preview.SourceRevision != current.SourceRevision {
		item.Status = "outdated"
	}
	item.CurrentSourceRevision = current.SourceRevision
	latest, err := store.GetServiceReferenceCandidate(ctx, consumerID, serviceID)
	if err != nil {
		return empty, err
	}
	if latest.ID != item.ID || latest.Revision != item.Revision {
		return empty, fmt.Errorf("%w: candidate changed during source validation", apperrors.ErrConflict)
	}
	return item, nil
}

func (s *Service) DeleteEnvironmentServiceReferenceCandidate(ctx context.Context, principal domainidentity.Principal, consumerID, serviceID string, input domaindelivery.ServiceReferenceCandidateDeleteInput) error {
	if !validCandidateID(consumerID) || !validCandidateID(serviceID) || !validCandidateID(input.ExpectedCandidateID) || input.ExpectedRevision < 1 || input.ExpectedConsumerUpdatedAt.IsZero() {
		return fmt.Errorf("%w: binding and candidate identity/revision are required", apperrors.ErrInvalidArgument)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	store, err := s.referenceCandidateStore()
	if err != nil {
		return err
	}
	authorize := func(ctx context.Context) error {
		binding, err := s.authorizeReferenceCandidateConsumer(ctx, principal, consumerID)
		if err != nil {
			return err
		}
		if !binding.UpdatedAt.Equal(input.ExpectedConsumerUpdatedAt) {
			return fmt.Errorf("%w: consumer configuration changed", apperrors.ErrConflict)
		}
		return nil
	}
	if err := authorize(ctx); err != nil {
		return err
	}
	if err := store.DeleteServiceReferenceCandidate(ctx, consumerID, serviceID, input, authorize); err != nil {
		return err
	}
	s.recordReferenceCandidate(ctx, principal, "delete", consumerID, serviceID, input.ExpectedCandidateID, input.ExpectedRevision)
	return nil
}

func validateReferenceCandidateInput(input domaindelivery.ServiceReferenceCandidateInput) error {
	if !validCandidateID(input.SourceApplicationEnvironmentID) || input.ExpectedRevision == nil || *input.ExpectedRevision < 0 || len(input.SourceTargetIDs) == 0 || !validReferenceDigest(input.ExpectedSourceRevision) {
		return fmt.Errorf("%w: candidate revision, complete target selection and source preview revision are required", apperrors.ErrInvalidArgument)
	}
	if (*input.ExpectedRevision == 0 && input.ExpectedCandidateID != "") || (*input.ExpectedRevision > 0 && !validCandidateID(input.ExpectedCandidateID)) {
		return fmt.Errorf("%w: updates require the existing candidate ID; creation must not reuse one", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validCandidateID(value string) bool {
	return value != "" && len(value) <= 200 && strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}

func (s *Service) referenceCandidateStore() (ServiceReferenceCandidateRepository, error) {
	store, ok := s.repository.(ServiceReferenceCandidateRepository)
	if !ok || (reflect.ValueOf(store).Kind() == reflect.Pointer && reflect.ValueOf(store).IsNil()) {
		return nil, fmt.Errorf("%w: reference candidate persistence is unavailable", apperrors.ErrUnsupportedOperation)
	}
	return store, nil
}

func (s *Service) authorizeReferenceCandidateConsumer(ctx context.Context, principal domainidentity.Principal, id string) (domaincatalog.ApplicationEnvironment, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryApplicationEnvView); err != nil {
		return domaincatalog.ApplicationEnvironment{}, err
	}
	binding, err := s.catalog.GetApplicationEnvironment(ctx, principal, id)
	if err != nil {
		return binding, err
	}
	permission := appaccess.ManagedActionPermission(appaccess.PermDeliveryApplicationEnvManage, "update")
	if _, err := s.catalog.AuthorizeApplicationEnvironmentPermission(ctx, principal, id, permission); err != nil {
		return domaincatalog.ApplicationEnvironment{}, err
	}
	_, err = s.applications.Get(ctx, principal, binding.ApplicationID)
	return binding, err
}

func referenceConfiguration(state referenceContext) domaindelivery.ServiceReferenceConfiguration {
	return domaindelivery.ServiceReferenceConfiguration{Consumer: state.consumer, Source: state.source, Service: state.service,
		ConsumerEnvironment: state.consumerEnvironment, SourceEnvironment: state.sourceEnvironment}
}

func candidateTargetIDs(item domaindelivery.ServiceReferenceCandidate) []string {
	ids := make([]string, 0, len(item.Preview.Targets))
	for _, target := range item.Preview.Targets {
		ids = append(ids, target.TargetID)
	}
	return ids
}

func (s *Service) recordReferenceCandidate(ctx context.Context, principal domainidentity.Principal, action, bindingID, serviceID, candidateID string, revision int64) {
	metadata := map[string]any{"applicationEnvironmentId": bindingID, "serviceId": serviceID, "candidateId": candidateID, "revision": revision, "state": "candidate"}
	operation := "delivery.reference_candidate." + action
	if s.audit != nil {
		meta := requestctx.FromContext(ctx)
		_ = s.audit.Record(ctx, domainaudit.Entry{ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams,
			ResourceKind: "ServiceReferenceCandidate", ResourceName: candidateID, Action: operation, Result: "success", Summary: "inactive service reference candidate " + action,
			RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP, Metadata: metadata})
	}
	if s.operations != nil {
		_ = s.operations.Record(ctx, operationentry.New(ctx, principal, operation, map[string]any{"resourceKind": "ServiceReferenceCandidate", "candidateId": candidateID}, "success", "inactive service reference candidate "+action, metadata))
	}
}
