package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestPersonalEnvironmentChoicesFilterCurrentNodeScopeAndHideInternalPolicy(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, _ := newInspectionAuthzTestService(map[string][]string{"chat": {appaccess.PermObserveAIChatUse}})
	listed := false
	service.personalEnvironment = &PersonalEnvironmentDependencies{
		Templates: func(context.Context) ([]domain.PersonalEnvironmentPolicy, error) {
			listed = true
			return []domain.PersonalEnvironmentPolicy{{TemplateID: "mine", Name: "Personal", AgentClusterID: "allowed", Mode: "observe-only", LeaseSeconds: 600}, {TemplateID: "foreign", AgentClusterID: "denied"}}, nil
		},
		Authorize: func(_ context.Context, principal domainidentity.Principal, node string) error {
			if principal.UserID == "owner" && node == "allowed" {
				return nil
			}
			return apperrors.ErrAccessDenied
		},
	}
	if _, err := service.ListPersonalEnvironmentTemplates(t.Context(), domainidentity.Principal{UserID: "no-chat"}); !errors.Is(err, apperrors.ErrAccessDenied) || listed {
		t.Fatalf("unauthorized user listed choices: %v", err)
	}
	items, err := service.ListPersonalEnvironmentTemplates(t.Context(), domainidentity.Principal{UserID: "owner", Roles: []string{"chat"}})
	if err != nil || len(items) != 1 || items[0].ID != "mine" {
		t.Fatalf("foreign template disclosed: %+v %v", items, err)
	}
	payload, err := json.Marshal(items)
	if err != nil || strings.Contains(string(payload), "allowed") || strings.Contains(string(payload), "agentCluster") {
		t.Fatalf("private policy leaked: %s %v", payload, err)
	}
}

func TestPersonalEnvironmentCannotReplayUnknownOrRecreateInitializedData(t *testing.T) {
	now := time.Now().UTC()
	lease := contractenv.Lease{SchemaVersion: contractenv.LeaseVersion, ID: "original", RunID: "original", OwnerID: "assistant", ScopeHash: "sha256:" + strings.Repeat("a", 64), Mode: "restricted-write", Quota: contractenv.Quota{DiskBytes: 32 << 20, MemoryBytes: 64 << 20, CPUUnits: 250, MaxProcesses: 16}, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}
	unknown := domain.PersonalEnvironmentRecord{AssistantID: "assistant", LeaseSeconds: 600, Lease: lease, State: "unknown", DataCreationAttempted: true}
	if _, dispatch, err := personalEnvironmentRequest(unknown, "wake"); !errors.Is(err, apperrors.ErrConflict) || dispatch {
		t.Fatalf("unknown acquisition replay: %v", err)
	}
	request, dispatch, err := personalEnvironmentRequest(unknown, "reconcile")
	if err != nil || !dispatch || request.Action != "inspect" || request.CreateData || request.Lease.ID != lease.ID || !request.Lease.IssuedAt.Equal(lease.IssuedAt) {
		t.Fatalf("original lease changed: %+v %v", request, err)
	}
	unknown.State = "missing"
	request, dispatch, err = personalEnvironmentRequest(unknown, "wake")
	if err != nil || !dispatch || request.CreateData || request.Lease.ID == lease.ID || request.Lease.Quota != lease.Quota {
		t.Fatalf("data/quota reset: %+v %v", request, err)
	}
	unknown.State = "ready"
	unknown.Lease.ExpiresAt = now.Add(-time.Second)
	if view := personalEnvironmentView(unknown); view.State != "unknown" || view.Usage != nil {
		t.Fatalf("expired receipt claims readiness: %+v", view)
	}
}

func TestStoppedPersonalStdioReceiptFencesOriginalEnvironment(t *testing.T) {
	principal := domainidentity.Principal{UserID: "owner"}
	lease := personalEnvironmentLease("assistant", principal.UserID, domain.PersonalEnvironmentPolicy{Mode: "restricted-write", LeaseSeconds: 600,
		Quota: contractenv.Quota{DiskBytes: 32 << 20, MemoryBytes: 64 << 20, CPUUnits: 250, MaxProcesses: 16}}, time.Now().UTC())
	original := domain.PersonalEnvironmentRecord{CreatedBy: principal.UserID, AssistantID: "assistant", Backend: "container", Provider: "docker", State: "ready", Version: 5,
		DataInitialized: true, DataCreationAttempted: true, Usage: &contractenv.Usage{Processes: 1}, Lease: lease, LeaseSeconds: 600}
	store := &personalCleanupStore{item: original}
	service := &Service{personalEnvironment: &PersonalEnvironmentDependencies{Store: store}}
	if err := service.recordStoppedPersonalStdio(t.Context(), principal, original); err != nil {
		t.Fatal(err)
	}
	if got := store.item; got.State != "missing" || got.Usage != nil || got.ObservedAt == nil || !got.DataInitialized || !got.DataCreationAttempted || got.Lease.ID != original.Lease.ID || got.Version != original.Version+1 {
		t.Fatalf("stopped receipt changed data or retained readiness: %+v", got)
	}
	wake, dispatch, err := personalEnvironmentRequest(store.item, "wake")
	if err != nil || !dispatch || wake.Action != "acquire" || wake.CreateData || wake.Lease.ID == lease.ID {
		t.Fatalf("stopped receipt did not enable a fresh wake over existing data: %+v %v", wake, err)
	}
	store.item.Lease.ID, store.item.State = "replacement", "ready"
	if err := service.recordStoppedPersonalStdio(t.Context(), principal, original); !errors.Is(err, apperrors.ErrConflict) || store.item.State != "ready" || store.item.Lease.ID != "replacement" {
		t.Fatalf("late receipt changed replacement: %+v %v", store.item, err)
	}
}
