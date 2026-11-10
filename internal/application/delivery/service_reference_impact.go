package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	dc "github.com/opensoha/soha/internal/domain/catalog"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	di "github.com/opensoha/soha/internal/domain/identity"
	dm "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type serviceReferenceImpactRepository interface {
	CreateDeliveryPlanWithServiceReferenceImpact(context.Context, dd.DeliveryPlanInput, string, dd.ServiceReferencePlanImpact) (dd.DeliveryPlan, error)
	GetDeliveryPlanServiceReferenceSnapshot(context.Context, string) (dd.ServiceReferencePlanImpact, error)
}

type ServiceReferenceManifestConfigurationReader interface {
	ServiceReferenceManifestConfigurationRevision(context.Context, string, []string) (string, error)
}

var errReferenceImpactTargetMissing = errors.New("current release target is missing")

func (s *Service) SetServiceReferenceManifestConfigurationReader(reader ServiceReferenceManifestConfigurationReader) {
	s.referenceManifests = reader
}

func referenceImpactDigest(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (s *Service) referenceImpactDatabase(ctx context.Context, p di.Principal, plan dd.DeliveryPlan) (dd.ServiceReferencePlanImpact, error) {
	applicationID, sourceID := plan.ApplicationID, plan.ApplicationEnvironmentID
	snapshot := dd.ServiceReferencePlanImpact{Version: dd.ServiceReferencePlanImpactVersion, ApplicationID: applicationID, SourceBindingID: sourceID, Consumers: []dd.ServiceReferenceRecord{}, SourceRevisions: map[string]string{}}
	repo, ok := s.repository.(ServiceReferenceScopeRepository)
	if !ok {
		return snapshot, apperrors.ErrUnsupportedOperation
	}
	binding, err := s.catalog.GetApplicationEnvironment(ctx, p, sourceID)
	if err != nil {
		return snapshot, err
	}
	if binding.ApplicationID != applicationID {
		return snapshot, apperrors.ErrConflict
	}
	records, err := repo.ListSourceServiceReferences(ctx, sourceID, "")
	if err != nil {
		return snapshot, err
	}
	snapshot.Consumers = append(snapshot.Consumers, records...)
	slices.SortFunc(snapshot.Consumers, func(a, b dd.ServiceReferenceRecord) int { return strings.Compare(a.Reference.ID, b.Reference.ID) })
	type consumerRevision struct {
		ID                                                          string
		Revision                                                    int64
		Consumer, Service, Source, Control, Configuration, Identity string
		Targets                                                     []string
	}
	consumers := make([]consumerRevision, 0, len(records))
	for _, record := range snapshot.Consumers {
		if record.ApplicationID != applicationID || record.SourceBindingID != sourceID {
			return snapshot, apperrors.ErrConflict
		}
		ids := slices.Clone(record.SourceTargetIDs)
		slices.Sort(ids)
		consumers = append(consumers, consumerRevision{record.Reference.ID, record.Reference.Revision, record.ConsumerBindingID, record.ServiceID, record.SourceBindingID, record.ControlRevision, record.ConsumerConfigurationRevision, record.Reference.SourceIdentityRevision, ids})
	}
	environment, err := s.manifestContextEnvironment(ctx, p, binding.EnvironmentID)
	if err != nil {
		return snapshot, err
	}
	if err := s.validateImpactConsumerClassifications(ctx, p, binding.EnvironmentID, records); err != nil {
		return snapshot, err
	}
	// Status and observation timestamps are deliberately absent from configuration evidence.
	binding.CreatedAt, binding.UpdatedAt = time.Time{}, time.Time{}
	binding.Alias, binding.WorkflowTemplate = "", nil
	environment.Name, environment.SortOrder = "", 0
	environment.CreatedAt, environment.UpdatedAt = time.Time{}, time.Time{}
	targets, selectionErr := referenceImpactTargets(binding.Targets, plan, records)
	if selectionErr != nil && !errors.Is(selectionErr, errReferenceImpactTargetMissing) {
		return snapshot, selectionErr
	}
	binding.Targets = nil
	sourceConfiguration, err := s.referenceManifestConfiguration(ctx, sourceID, targets)
	if err != nil {
		return snapshot, err
	}
	serviceVersions, err := s.referenceImpactServices(ctx, p, applicationID)
	if err != nil {
		return snapshot, err
	}
	snapshot.DatabaseRevision, err = referenceImpactDigest(struct {
		Version             int
		Binding             dc.ApplicationEnvironment
		Environment         dc.Environment
		Targets             []dc.ReleaseTargetInput
		Consumers           []consumerRevision
		Services            []referenceImpactServiceRevision
		SourceConfiguration string
	}{snapshot.Version, binding, environment, targets, consumers, serviceVersions, sourceConfiguration})
	if err != nil {
		return snapshot, err
	}
	return snapshot, selectionErr
}

func referenceImpactTargets(targets []dc.ReleaseTarget, plan dd.DeliveryPlan, records []dd.ServiceReferenceRecord) ([]dc.ReleaseTargetInput, error) {
	requested := make(map[string]bool)
	for _, id := range plan.TargetIDs {
		requested[id] = true
	}
	if plan.TargetID != "" {
		requested[plan.TargetID] = true
	}
	for _, record := range records {
		for _, id := range record.SourceTargetIDs {
			requested[id] = true
		}
	}
	projected := make([]dc.ReleaseTargetInput, 0, len(requested))
	for _, target := range targets {
		if !requested[target.ID] {
			continue
		}
		input, err := target.Input().WithCanonicalServiceID()
		if err != nil {
			return nil, apperrors.ErrConflict
		}
		projected = append(projected, input)
	}
	slices.SortFunc(projected, func(a, b dc.ReleaseTargetInput) int { return strings.Compare(a.ID, b.ID) })
	if len(projected) != len(requested) {
		return projected, fmt.Errorf("%w: %w", staleServiceReferencePlan(), errReferenceImpactTargetMissing)
	}
	return projected, nil
}

func (s *Service) referenceManifestConfiguration(ctx context.Context, sourceID string, targets []dc.ReleaseTargetInput) (string, error) {
	ids := make([]string, 0)
	for _, target := range targets {
		if target.ExecutorKind == "manifest_ssa" && target.ConfigRef != "" {
			ids = append(ids, target.ConfigRef)
		}
	}
	if len(ids) == 0 {
		return "", nil // Empty execution dependency set, with active consumers still hashed separately.
	}
	if s.referenceManifests == nil {
		return "", apperrors.ErrUnsupportedOperation
	}
	slices.Sort(ids)
	return s.referenceManifests.ServiceReferenceManifestConfigurationRevision(ctx, sourceID, slices.Compact(ids))
}

type referenceImpactServiceRevision struct {
	ID      string
	Version int64
	Enabled bool
}

func (s *Service) referenceImpactServices(ctx context.Context, p di.Principal, applicationID string) ([]referenceImpactServiceRevision, error) {
	services, err := s.applications.ListServices(ctx, p, applicationID)
	if err != nil {
		return nil, err
	}
	serviceVersions := make([]referenceImpactServiceRevision, 0, len(services))
	for _, service := range services {
		serviceVersions = append(serviceVersions, referenceImpactServiceRevision{service.ID, service.Version, service.Enabled})
	}
	slices.SortFunc(serviceVersions, func(a, b referenceImpactServiceRevision) int { return strings.Compare(a.ID, b.ID) })
	return serviceVersions, nil
}

func (s *Service) captureServiceReferenceImpact(ctx context.Context, p di.Principal, plan dd.DeliveryPlan) (dd.ServiceReferencePlanImpact, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	snapshot, selectionErr := s.referenceImpactDatabase(ctx, p, plan)
	if selectionErr != nil && !errors.Is(selectionErr, errReferenceImpactTargetMissing) {
		return snapshot, selectionErr
	}
	snapshot.PlanID = plan.ID
	for _, record := range snapshot.Consumers {
		if _, seen := snapshot.SourceRevisions[record.ServiceID]; seen {
			continue
		}
		preview, err := s.observeSourceReferenceGroup(ctx, p, plan.ApplicationEnvironmentID, record.ServiceID)
		if err != nil {
			return snapshot, err
		}
		if err := s.validateImpactReferenceIdentities(ctx, p, snapshot.Consumers, preview); err != nil {
			return snapshot, err
		}
		// Consumers share source identity and control, not an immutable runtime version.
		// The existing Manifest/Helm operation owns its pre/post publication guards.
		identity, err := dd.ServiceReferenceIdentityRevision(preview)
		if err != nil {
			return snapshot, err
		}
		binding, err := s.catalog.GetApplicationEnvironment(ctx, p, plan.ApplicationEnvironmentID)
		if err != nil {
			return snapshot, err
		}
		control, err := dd.ServiceReferenceControlRevision(binding, record.ServiceID)
		if err != nil {
			return snapshot, err
		}
		observedRevision, err := referenceImpactDigest(struct{ Identity, Control string }{identity, control})
		if err != nil {
			return snapshot, err
		}
		snapshot.SourceRevisions[record.ServiceID] = observedRevision
	}
	revision, err := referenceImpactDigest(struct {
		Database string
		Sources  map[string]string
	}{snapshot.DatabaseRevision, snapshot.SourceRevisions})
	if err != nil {
		return snapshot, err
	}
	snapshot.Revision = revision
	return snapshot, selectionErr
}

func (s *Service) createPlanWithServiceReferenceImpact(ctx context.Context, p di.Principal, input dd.DeliveryPlanInput) (dd.DeliveryPlan, error) {
	// Unassociated standalone compatibility is intentional; known application Plans require evidence even for an empty set.
	if input.ApplicationID == "" && input.ApplicationEnvironmentID == "" {
		return s.repository.CreateDeliveryPlan(ctx, input, p.UserID)
	}
	if input.ApplicationID == "" || input.ApplicationEnvironmentID == "" {
		allowed, err := s.applicationScopedConfigurationPlan(ctx, p, dd.DeliveryPlan{TemplateCandidateID: input.TemplateCandidateID, Source: input.Source, Action: input.Action, ApplicationID: input.ApplicationID, ApplicationEnvironmentID: input.ApplicationEnvironmentID})
		if err != nil {
			return dd.DeliveryPlan{}, err
		}
		if allowed {
			return s.repository.CreateDeliveryPlan(ctx, input, p.UserID)
		}
		return dd.DeliveryPlan{}, staleServiceReferencePlan()
	}
	repo, ok := s.repository.(serviceReferenceImpactRepository)
	if !ok {
		return dd.DeliveryPlan{}, apperrors.ErrUnsupportedOperation
	}
	scope, ok := s.repository.(ServiceReferenceScopeRepository)
	if !ok {
		return dd.DeliveryPlan{}, apperrors.ErrUnsupportedOperation
	}
	selection := dd.DeliveryPlan{ID: input.ID, ApplicationID: input.ApplicationID, ApplicationEnvironmentID: input.ApplicationEnvironmentID, TargetID: input.TargetID, TargetIDs: input.TargetIDs}
	snapshot, err := s.captureServiceReferenceImpact(ctx, p, selection)
	if err != nil {
		return dd.DeliveryPlan{}, err
	}
	if input.Impact == nil {
		input.Impact = map[string]any{}
	}
	input.Impact["serviceReferenceImpactRevision"] = snapshot.Revision
	input.Impact["hasServiceReferenceConsumers"] = len(snapshot.Consumers) > 0
	var plan dd.DeliveryPlan
	err = scope.WithServiceReferenceScope(ctx, input.ApplicationID, func(lockctx context.Context) error {
		current, err := s.referenceImpactDatabase(lockctx, p, selection)
		if err != nil {
			return err
		}
		if current.DatabaseRevision != snapshot.DatabaseRevision {
			return staleServiceReferencePlan()
		}
		plan, err = repo.CreateDeliveryPlanWithServiceReferenceImpact(lockctx, input, p.UserID, snapshot)
		if err != nil {
			return err
		}
		return dm.FlushPreparedOperations(lockctx)
	})
	return plan, err
}

func staleServiceReferencePlan() error {
	return fmt.Errorf("%w: service reference impact changed, is missing, or was captured by an older version; create a new Plan preflight and obtain any required approval again", apperrors.ErrConflict)
}

func (s *Service) withValidatedServiceReferenceImpact(ctx context.Context, p di.Principal, plan dd.DeliveryPlan, apply func(context.Context) error) error {
	if plan.ApplicationID == "" && plan.ApplicationEnvironmentID == "" {
		return apply(ctx)
	}
	if plan.ApplicationID == "" || plan.ApplicationEnvironmentID == "" {
		allowed, err := s.applicationScopedConfigurationPlan(ctx, p, plan)
		if err != nil {
			return err
		}
		if allowed {
			return apply(ctx)
		}
		return staleServiceReferencePlan()
	}
	repo, ok := s.repository.(serviceReferenceImpactRepository)
	if !ok {
		return apperrors.ErrUnsupportedOperation
	}
	frozen, err := repo.GetDeliveryPlanServiceReferenceSnapshot(ctx, plan.ID)
	if errors.Is(err, apperrors.ErrNotFound) {
		return staleServiceReferencePlan()
	}
	if err != nil {
		return err
	}
	if frozen.Version != dd.ServiceReferencePlanImpactVersion || frozen.Consumers == nil || frozen.SourceRevisions == nil || frozen.ApplicationID != plan.ApplicationID || frozen.SourceBindingID != plan.ApplicationEnvironmentID {
		return staleServiceReferencePlan()
	}
	current, err := s.captureServiceReferenceImpact(ctx, p, plan)
	if err != nil {
		return err
	}
	if current.Revision != frozen.Revision {
		return staleServiceReferencePlan()
	}
	scope, ok := s.repository.(ServiceReferenceScopeRepository)
	if !ok {
		return apperrors.ErrUnsupportedOperation
	}
	return scope.WithServiceReferenceScope(ctx, plan.ApplicationID, func(lockctx context.Context) error {
		latest, err := s.referenceImpactDatabase(lockctx, p, plan)
		if err != nil {
			return err
		}
		if latest.DatabaseRevision != frozen.DatabaseRevision {
			return staleServiceReferencePlan()
		}
		return apply(lockctx)
	})
}

func (s *Service) validateServiceReferenceImpact(ctx context.Context, p di.Principal, plan dd.DeliveryPlan) error {
	return s.withValidatedServiceReferenceImpact(ctx, p, plan, func(context.Context) error { return nil })
}

// ExecutionPrincipalReader reloads identity rather than trusting persisted role claims.
type ExecutionPrincipalReader interface {
	CurrentExecutionPrincipal(context.Context, string, string) (di.Principal, error)
}

func (s *Service) SetExecutionPrincipalReader(reader ExecutionPrincipalReader) {
	s.executionPrincipals = reader
}
func (s *Service) currentImpactExecutionPrincipal(ctx context.Context, task dd.ExecutionTask) (di.Principal, error) {
	if s.executionPrincipals == nil {
		return di.Principal{}, apperrors.ErrUnsupportedOperation
	}
	p, err := s.executionPrincipals.CurrentExecutionPrincipal(ctx, task.SecretPrincipal.UserID, task.SecretPrincipal.AccessTokenID)
	if err != nil {
		return p, err
	}
	if p.UserID == "" || p.UserID != task.SecretPrincipal.UserID {
		return di.Principal{}, apperrors.ErrUnauthorized
	}
	return p, nil
}

type referenceExecutionPlanKey struct{}

func (s *Service) validateReferenceTrigger(ctx context.Context, p di.Principal, applicationID, sourceID string) error {
	if plan, ok := ctx.Value(referenceExecutionPlanKey{}).(dd.DeliveryPlan); ok {
		if plan.ApplicationID != applicationID || plan.ApplicationEnvironmentID != sourceID {
			return apperrors.ErrConflict
		}
		return s.validateServiceReferenceImpact(ctx, p, plan)
	}
	repo, ok := s.repository.(ServiceReferenceScopeRepository)
	if !ok {
		return apperrors.ErrUnsupportedOperation
	}
	records, err := repo.ListSourceServiceReferences(ctx, sourceID, "")
	if err != nil {
		return err
	}
	if len(records) > 0 {
		return fmt.Errorf("%w: referenced source changes require a reviewed Delivery Plan", apperrors.ErrConflict)
	}
	return nil
}

// ValidateExecutionTaskRetry runs before retry changes task state, stops jobs or emits logs.
func (s *Service) ValidateExecutionTaskRetry(ctx context.Context, task dd.ExecutionTask) error {
	if strings.HasPrefix(task.TaskKind, "manifest_") || dd.RequiresManifestAdmission(task) {
		_, err := s.hydrateManifestExecutionTask(ctx, task)
		return err
	}
	if !strings.HasPrefix(task.TaskKind, "helm_") {
		return nil
	}
	payload, err := decodeHelmPayload(task.Payload)
	if err != nil {
		return err
	}
	_, _, err = s.validateHelmTaskAdmission(ctx, task, payload)
	return err
}

func (s *Service) referenceConsumerSetRevision(ctx context.Context, sourceID string) (string, error) {
	repo, ok := s.repository.(ServiceReferenceScopeRepository)
	if !ok {
		return "", apperrors.ErrUnsupportedOperation
	}
	records, err := repo.ListSourceServiceReferences(ctx, sourceID, "")
	if err != nil {
		return "", err
	}
	type entry struct {
		ID, Consumer, Service, Source, Control, Configuration string
		Revision                                              int64
		Targets                                               []string
	}
	values := make([]entry, 0, len(records))
	for _, r := range records {
		ids := slices.Clone(r.SourceTargetIDs)
		slices.Sort(ids)
		values = append(values, entry{r.Reference.ID, r.ConsumerBindingID, r.ServiceID, r.SourceBindingID, r.ControlRevision, r.ConsumerConfigurationRevision, r.Reference.Revision, ids})
	}
	slices.SortFunc(values, func(a, b entry) int { return strings.Compare(a.ID, b.ID) })
	return referenceImpactDigest(values)
}

// ValidateDeliveryPlanServiceReferenceImpact also admits preflight tasks while a Plan is still draft.
func (s *Service) ValidateDeliveryPlanServiceReferenceImpact(ctx context.Context, p di.Principal, planID string) error {
	plan, err := s.GetDeliveryPlan(ctx, p, planID)
	if err != nil {
		return err
	}
	return s.validateServiceReferenceImpact(ctx, p, plan)
}

func (s *Service) validateImpactReferenceIdentities(ctx context.Context, p di.Principal, records []dd.ServiceReferenceRecord, preview dd.ServiceReferencePreview) error {
	identity, err := dd.ServiceReferenceIdentityRevision(preview)
	if err != nil {
		return err
	}
	binding, err := s.catalog.GetApplicationEnvironment(ctx, p, preview.Source.ApplicationEnvironmentID)
	if err != nil {
		return err
	}
	control, err := dd.ServiceReferenceControlRevision(binding, preview.ServiceID)
	if err != nil {
		return err
	}
	targets, err := referenceServiceTargets(binding, preview.ServiceID)
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.ServiceID != preview.ServiceID {
			continue
		}
		if record.Reference.SourceIdentityRevision != identity || record.ControlRevision != control {
			return fmt.Errorf("%w: source reference identity or control requires review", apperrors.ErrConflict)
		}
		if err := validateReferenceTargetSelection(targets, record.SourceTargetIDs); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) applicationScopedConfigurationPlan(ctx context.Context, p di.Principal, plan dd.DeliveryPlan) (bool, error) {
	if s.templateUpdates == nil || plan.TemplateCandidateID == "" || plan.Source != "delivery_batch" || plan.Action != dd.ApplicationDeliveryActionDeploy || plan.ApplicationID == "" || plan.ApplicationEnvironmentID != "" {
		return false, nil
	}
	frozen, err := s.templateUpdates.ReadCandidate(ctx, p, plan.TemplateCandidateID)
	if err != nil {
		return false, err
	}
	target := frozen.Candidate.Target
	return target.Scope == "application" && (target.Kind == "build" || target.Kind == "workflow") && target.ApplicationID == plan.ApplicationID && target.ApplicationEnvironmentID == "" && frozen.Candidate.ID == plan.TemplateCandidateID, nil
}

func (s *Service) validateImpactConsumerClassifications(ctx context.Context, p di.Principal, environmentID string, records []dd.ServiceReferenceRecord) error {
	if len(records) == 0 {
		return nil
	}
	environments, err := s.catalog.ListEnvironments(ctx, p)
	if err != nil {
		return err
	}
	var source dc.Environment
	for _, item := range environments {
		if item.ID == environmentID {
			source = item
			break
		}
	}
	if !validReferenceEnvironment(source) {
		return fmt.Errorf("%w: source classification requires review", apperrors.ErrConflict)
	}
	for _, record := range records {
		consumer := record.ConsumerEnvironment
		if !validReferenceEnvironment(consumer) || consumer.Tier != source.Tier || consumer.IsProduction != source.IsProduction || consumer.StageLevel != source.StageLevel {
			return fmt.Errorf("%w: active consumer classification requires review", apperrors.ErrConflict)
		}
	}
	return nil
}
