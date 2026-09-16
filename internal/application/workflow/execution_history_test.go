package workflow

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type historyRepository struct {
	Repository
	positions []domainworkflow.ExecutionHistoryPosition
	reads     int
}

func (r *historyRepository) ListExecutionHistoryCandidates(_ context.Context, _ domainworkflow.ExecutionHistoryFilter, after *domainworkflow.ExecutionHistoryPosition, limit int) ([]domainworkflow.ExecutionHistoryPosition, error) {
	r.reads++
	start := 0
	if after != nil {
		for i, p := range r.positions {
			if p.ID == after.ID {
				start = i + 1
				break
			}
		}
	}
	return r.positions[start:min(start+limit, len(r.positions))], nil
}

func (r *historyRepository) Get(_ context.Context, id string) (domainworkflow.Run, error) {
	if strings.HasPrefix(id, "hidden") {
		return domainworkflow.Run{}, apperrors.ErrAccessDenied
	}
	return domainworkflow.Run{ID: id, ApplicationID: "app", WorkflowName: id, Status: "completed"}, nil
}

func TestExecutionHistoryFillsAuthorizedPagesPast200AndResumes(t *testing.T) {
	repo := &historyRepository{}
	for i := 0; i < 230; i++ {
		id := fmt.Sprintf("hidden-%03d", i)
		if i >= 205 {
			id = fmt.Sprintf("visible-%03d", i)
		}
		repo.positions = append(repo.positions, domainworkflow.ExecutionHistoryPosition{ID: id, Kind: "application", CreatedAt: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC).Add(-time.Duration(i) * time.Second)})
	}
	permissions := appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"reader": {appaccess.PermDeliveryWorkflowsView}}})
	service := New(repo, &stubWorkflowApps{}, nil, permissions, nil, nil, nil, nil)
	principal := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
	page, err := service.ListExecutionHistory(context.Background(), principal, domainworkflow.ExecutionHistoryFilter{Limit: 12, Status: "succeeded"})
	if err != nil || len(page.Items) != 12 || page.Items[0].ID != "visible-205" || page.NextCursor == "" || repo.reads < 3 {
		t.Fatalf("page omitted authorized history: %+v %v", page, err)
	}
	next, err := service.ListExecutionHistory(context.Background(), principal, domainworkflow.ExecutionHistoryFilter{Limit: 12, Cursor: page.NextCursor, Status: "succeeded"})
	if err != nil || len(next.Items) != 12 || next.Items[0].ID != "visible-217" {
		t.Fatalf("cursor duplicated or skipped records: %+v %v", next, err)
	}
	last, err := service.ListExecutionHistory(context.Background(), principal, domainworkflow.ExecutionHistoryFilter{Limit: 12, Cursor: next.NextCursor})
	if err != nil || len(last.Items) != 1 || last.NextCursor != "" {
		t.Fatalf("last page: %+v %v", last, err)
	}
}

func TestExecutionHistoryRejectsInvalidInput(t *testing.T) {
	for _, f := range []domainworkflow.ExecutionHistoryFilter{{Cursor: "bad cursor"}, {Status: "invented"}, {Limit: 101}, {Search: strings.Repeat("字", 201)}} {
		if _, err := normalizeExecutionHistoryFilter(&f); err == nil {
			t.Fatalf("accepted invalid filter: %+v", f)
		}
	}
}

func TestExecutionHistorySearchUsesAuthorizedProjection(t *testing.T) {
	entry := domainworkflow.ExecutionHistoryEntry{Batch: &domainworkflow.DeliveryBatch{Status: "completed", Definition: domainworkflow.DeliveryWorkflowDefinition{Name: "Visible"}, Targets: []domainworkflow.DeliveryTargetSnapshot{{ApplicationName: "public"}}}}
	if executionHistoryMatches(entry, domainworkflow.ExecutionHistoryFilter{Search: "hidden"}) || !executionHistoryMatches(entry, domainworkflow.ExecutionHistoryFilter{Search: "PUBLIC", Status: "succeeded"}) {
		t.Fatal("search did not use visible case-insensitive values")
	}
}
