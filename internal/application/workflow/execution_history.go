package workflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type executionHistoryRepository interface {
	ListExecutionHistoryCandidates(context.Context, domainworkflow.ExecutionHistoryFilter, *domainworkflow.ExecutionHistoryPosition, int) ([]domainworkflow.ExecutionHistoryPosition, error)
}

type executionBuildReader interface {
	Get(context.Context, domainidentity.Principal, string) (domainbuild.Record, error)
}

func (s *Service) ListExecutionHistory(ctx context.Context, principal domainidentity.Principal, f domainworkflow.ExecutionHistoryFilter) (domainworkflow.ExecutionHistoryPage, error) {
	page := domainworkflow.ExecutionHistoryPage{Items: []domainworkflow.ExecutionHistoryEntry{}}
	after, err := normalizeExecutionHistoryFilter(&f)
	if err != nil {
		return page, err
	}
	f.IncludeWorkflows = s.authorizePermission(ctx, principal, appaccess.PermDeliveryWorkflowsView) == nil
	f.IncludeBuilds = s.authorizePermission(ctx, principal, appaccess.PermDeliveryApplicationsView) == nil
	if !f.IncludeWorkflows && !f.IncludeBuilds {
		return page, apperrors.ErrAccessDenied
	}
	repo, ok := s.repo.(executionHistoryRepository)
	if !ok {
		return page, fmt.Errorf("%w: execution history unavailable", apperrors.ErrUnsupportedOperation)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// Read bounded chunks until a complete authorized page is available. Never
	// return a cursor or total based on an inaccessible candidate.
	for {
		candidates, err := repo.ListExecutionHistoryCandidates(ctx, f, after, 100)
		if err != nil {
			return page, err
		}
		for _, candidate := range candidates {
			after = &candidate
			entry, err := s.executionHistoryEntry(ctx, principal, candidate, f)
			if errors.Is(err, apperrors.ErrAccessDenied) || errors.Is(err, apperrors.ErrNotFound) {
				continue
			}
			if err != nil {
				return page, err
			}
			if !executionHistoryMatches(entry, f) {
				continue
			}
			if len(page.Items) == f.Limit {
				page.NextCursor = encodeExecutionHistoryCursor(page.Items[len(page.Items)-1].ExecutionHistoryPosition)
				return page, nil
			}
			page.Items = append(page.Items, entry)
		}
		if len(candidates) < 100 {
			return page, nil
		}
	}
}

func (s *Service) executionHistoryEntry(ctx context.Context, principal domainidentity.Principal, position domainworkflow.ExecutionHistoryPosition, f domainworkflow.ExecutionHistoryFilter) (domainworkflow.ExecutionHistoryEntry, error) {
	entry := domainworkflow.ExecutionHistoryEntry{ExecutionHistoryPosition: position}
	switch position.Kind {
	case "batch":
		batch, err := s.GetDeliveryBatch(ctx, principal, position.ID)
		if err != nil {
			return entry, err
		}
		visible := map[string]bool{}
		for _, snapshot := range batch.Targets {
			if executionHistoryTargetMatches(snapshot, f) {
				visible[snapshot.Target.ID] = true
			}
		}
		if len(visible) == 0 {
			return entry, apperrors.ErrNotFound
		}
		batch = filterDeliveryBatch(batch, visible)
		entry.Batch = &batch
	case "application":
		run, err := s.Get(ctx, principal, position.ID)
		if err != nil {
			return entry, err
		}
		entry.Application = &run
	case "build":
		reader, ok := s.builds.(executionBuildReader)
		if !ok {
			return entry, fmt.Errorf("%w: build history unavailable", apperrors.ErrUnsupportedOperation)
		}
		build, err := reader.Get(ctx, principal, position.ID)
		if err != nil {
			return entry, err
		}
		entry.Build = &build
	default:
		return entry, apperrors.ErrInvalidArgument
	}
	return entry, nil
}

func executionHistoryTargetMatches(snapshot domainworkflow.DeliveryTargetSnapshot, f domainworkflow.ExecutionHistoryFilter) bool {
	return (f.ApplicationID == "" || snapshot.Target.ApplicationID == f.ApplicationID) &&
		(f.ServiceID == "" || snapshot.Target.ServiceID == f.ServiceID) &&
		(f.ApplicationEnvironmentID == "" || snapshot.Target.ApplicationEnvironmentID == f.ApplicationEnvironmentID) &&
		(f.BuildSourceID == "" || snapshot.BuildSourceID == f.BuildSourceID)
}

func normalizeExecutionHistoryFilter(f *domainworkflow.ExecutionHistoryFilter) (*domainworkflow.ExecutionHistoryPosition, error) {
	for _, value := range []*string{&f.ApplicationID, &f.ServiceID, &f.ApplicationEnvironmentID, &f.WorkflowID, &f.BuildSourceID, &f.Status, &f.Search, &f.Cursor} {
		*value = strings.TrimSpace(*value)
	}
	if f.Limit == 0 {
		f.Limit = 12
	}
	if f.Limit < 1 || f.Limit > 100 || utf8.RuneCountInString(f.Search) > 200 {
		return nil, fmt.Errorf("%w: invalid history limit or search", apperrors.ErrInvalidArgument)
	}
	switch f.Status {
	case "", "all", "running", "approval", "succeeded", "failed", "canceled":
	default:
		return nil, fmt.Errorf("%w: invalid history status", apperrors.ErrInvalidArgument)
	}
	if f.Cursor == "" {
		return nil, nil
	}
	if len(f.Cursor) > 2048 {
		return nil, fmt.Errorf("%w: invalid history cursor", apperrors.ErrInvalidArgument)
	}
	data, err := base64.RawURLEncoding.DecodeString(f.Cursor)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid history cursor", apperrors.ErrInvalidArgument)
	}
	var position domainworkflow.ExecutionHistoryPosition
	if json.Unmarshal(data, &position) != nil || position.CreatedAt.IsZero() || position.ID == "" {
		return nil, fmt.Errorf("%w: invalid history cursor", apperrors.ErrInvalidArgument)
	}
	if position.Kind != "batch" && position.Kind != "application" && position.Kind != "build" {
		return nil, fmt.Errorf("%w: invalid history cursor", apperrors.ErrInvalidArgument)
	}
	return &position, nil
}

func encodeExecutionHistoryCursor(position domainworkflow.ExecutionHistoryPosition) string {
	data, _ := json.Marshal(position)
	return base64.RawURLEncoding.EncodeToString(data)
}
