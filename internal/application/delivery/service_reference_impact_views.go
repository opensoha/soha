package delivery

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	dd "github.com/opensoha/soha/internal/domain/delivery"
	di "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) ListEnvironmentServiceReferenceConsumers(ctx context.Context, p di.Principal, sourceID, serviceID, cursor string, limit int) (dd.ServiceReferenceConsumerPage, error) {
	var empty dd.ServiceReferenceConsumerPage
	if !validCandidateID(sourceID) || !validCandidateID(serviceID) {
		return empty, apperrors.ErrInvalidArgument
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if _, err := s.loadSourceReferenceContext(ctx, p, sourceID, serviceID); err != nil {
		return empty, err
	}
	repo, ok := s.repository.(ServiceReferenceScopeRepository)
	if !ok {
		return empty, apperrors.ErrUnsupportedOperation
	}
	records, err := repo.ListSourceServiceReferences(ctx, sourceID, serviceID)
	if err != nil {
		return empty, err
	}
	page, err := s.projectServiceReferenceConsumers(ctx, p, records, cursor, limit)
	if err != nil {
		return empty, err
	}
	if _, err := s.loadSourceReferenceContext(ctx, p, sourceID, serviceID); err != nil {
		return empty, err
	}
	return page, nil
}

func (s *Service) projectServiceReferenceConsumers(ctx context.Context, p di.Principal, records []dd.ServiceReferenceRecord, cursor string, limit int) (dd.ServiceReferenceConsumerPage, error) {
	page := dd.ServiceReferenceConsumerPage{Items: []dd.ServiceReferenceConsumer{}}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 || len(cursor) > 200 || strings.TrimSpace(cursor) != cursor {
		return page, apperrors.ErrInvalidArgument
	}
	visible := make([]dd.ServiceReferenceConsumer, 0)
	for _, record := range records {
		binding, err := s.catalog.GetApplicationEnvironment(ctx, p, record.ConsumerBindingID)
		if errors.Is(err, apperrors.ErrAccessDenied) || errors.Is(err, apperrors.ErrNotFound) {
			page.HasRestrictedConsumers = true
			continue
		}
		if err != nil {
			return page, err
		}
		if binding.ApplicationID != record.ApplicationID {
			return page, apperrors.ErrConflict
		}
		visible = append(visible, dd.ServiceReferenceConsumer{ApplicationEnvironmentID: binding.ID, EnvironmentID: binding.EnvironmentID, EnvironmentKey: binding.EnvironmentKey, ReferenceID: record.Reference.ID, ReferenceRevision: record.Reference.Revision, ServiceID: record.ServiceID, SourceApplicationEnvironmentID: record.SourceBindingID, SourceTargetIDs: slices.Clone(record.SourceTargetIDs)})
	}
	slices.SortFunc(visible, func(a, b dd.ServiceReferenceConsumer) int { return strings.Compare(a.ReferenceID, b.ReferenceID) })
	start := 0
	if cursor != "" {
		index := slices.IndexFunc(visible, func(item dd.ServiceReferenceConsumer) bool { return item.ReferenceID == cursor })
		// Never accept a hidden ID as a cursor or leak whether that ID exists.
		if index < 0 {
			return page, fmt.Errorf("%w: invalid consumer cursor", apperrors.ErrInvalidArgument)
		}
		start = index + 1
	}
	end := min(start+limit, len(visible))
	page.Items = append(page.Items, visible[start:end]...)
	if end < len(visible) {
		page.NextCursor = visible[end-1].ReferenceID
	}
	return page, nil
}

func (s *Service) GetDeliveryPlanServiceReferenceImpact(ctx context.Context, p di.Principal, planID, cursor string, limit int) (dd.DeliveryPlanServiceReferenceImpact, error) {
	var empty dd.DeliveryPlanServiceReferenceImpact
	if !validCandidateID(planID) {
		return empty, apperrors.ErrInvalidArgument
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	plan, err := s.GetDeliveryPlan(ctx, p, planID)
	if err != nil {
		return empty, err
	}
	if err := s.authorizeImpactPlanSource(ctx, p, plan); err != nil {
		return empty, err
	}
	repo, ok := s.repository.(serviceReferenceImpactRepository)
	if !ok {
		return empty, apperrors.ErrUnsupportedOperation
	}
	frozen, err := repo.GetDeliveryPlanServiceReferenceSnapshot(ctx, plan.ID)
	if errors.Is(err, apperrors.ErrNotFound) {
		return empty, staleServiceReferencePlan()
	}
	if err != nil {
		return empty, err
	}
	current, err := s.captureServiceReferenceImpact(ctx, p, plan)
	if err != nil {
		return empty, err
	}
	page, err := s.projectServiceReferenceConsumers(ctx, p, current.Consumers, cursor, limit)
	if err != nil {
		return empty, err
	}
	if _, err := s.GetDeliveryPlan(ctx, p, planID); err != nil {
		return empty, err
	}
	if err := s.authorizeImpactPlanSource(ctx, p, plan); err != nil {
		return empty, err
	}
	return dd.DeliveryPlanServiceReferenceImpact{PlanID: plan.ID, SourceApplicationEnvironmentID: plan.ApplicationEnvironmentID, Revision: frozen.Revision, CurrentRevision: current.Revision, IsCurrent: frozen.Revision == current.Revision, Items: page.Items, NextCursor: page.NextCursor, HasRestrictedConsumers: page.HasRestrictedConsumers}, nil
}

func (s *Service) authorizeImpactPlanSource(ctx context.Context, p di.Principal, plan dd.DeliveryPlan) error {
	binding, err := s.catalog.GetApplicationEnvironment(ctx, p, plan.ApplicationEnvironmentID)
	if err != nil {
		return err
	}
	if binding.ApplicationID != plan.ApplicationID {
		return apperrors.ErrConflict
	}
	services := map[string]bool{}
	for _, target := range binding.Targets {
		if target.ID != plan.TargetID && !slices.Contains(plan.TargetIDs, target.ID) {
			continue
		}
		id := target.ResolvedServiceID()
		if id == "" || services[id] {
			continue
		}
		services[id] = true
		if _, err := s.authorizeReferenceServiceWithAccess(ctx, p, plan.ApplicationID, id, false); err != nil {
			return err
		}
	}
	return nil
}
