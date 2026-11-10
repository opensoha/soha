package workflow

import (
	"context"
	"errors"
	"sync"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type requestWorkflowRepository struct {
	*stubWorkflowRepository
	requestMu sync.Mutex
	requests  map[string]domainworkflow.TriggerRequest
	createErr error
}

func (r *requestWorkflowRepository) ClaimTriggerRequest(_ context.Context, item domainworkflow.TriggerRequest) (domainworkflow.TriggerRequest, bool, error) {
	r.requestMu.Lock()
	defer r.requestMu.Unlock()
	key := item.ActorID + "\x00" + item.Key
	if old, ok := r.requests[key]; ok {
		if old.InputDigest != item.InputDigest {
			return old, false, apperrors.ErrConflict
		}
		return old, false, nil
	}
	item.Status = "unknown"
	r.requests[key] = item
	return item, true, nil
}
func (r *requestWorkflowRepository) GetTriggerRequest(_ context.Context, actor, key string) (domainworkflow.TriggerRequest, error) {
	r.requestMu.Lock()
	defer r.requestMu.Unlock()
	if old, ok := r.requests[actor+"\x00"+key]; ok {
		return old, nil
	}
	return domainworkflow.TriggerRequest{}, apperrors.ErrNotFound
}
func (r *requestWorkflowRepository) FinishTriggerRequest(_ context.Context, id, status, code string) error {
	r.requestMu.Lock()
	defer r.requestMu.Unlock()
	for key, item := range r.requests {
		if item.ID == id && item.Status == "unknown" {
			item.Status, item.ErrorCode = status, code
			r.requests[key] = item
			return nil
		}
	}
	return apperrors.ErrNotFound
}
func (r *requestWorkflowRepository) Create(_ context.Context, item domainworkflow.Run) (domainworkflow.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createCalls++
	if r.createErr != nil {
		return domainworkflow.Run{}, r.createErr
	}
	r.items = append(r.items, cloneWorkflowRun(item))
	return cloneWorkflowRun(item), nil
}

func newWorkflowRequestTest(t *testing.T) (*Service, *requestWorkflowRepository, domainidentity.Principal, domainworkflow.Input) {
	t.Helper()
	repo := &requestWorkflowRepository{stubWorkflowRepository: &stubWorkflowRepository{}, requests: map[string]domainworkflow.TriggerRequest{}}
	service := &Service{repo: repo, apps: &stubWorkflowApps{}, permissions: appaccess.NewPermissionResolver(stubWorkflowRolePermissionReader{matrix: map[string][]string{"developer": {appaccess.PermDeliveryWorkflowsTrigger, appaccess.PermDeliveryWorkflowsView}, "reader": {appaccess.PermDeliveryWorkflowsView}, "trigger-only": {appaccess.PermDeliveryWorkflowsTrigger}}})}
	return service, repo, domainidentity.Principal{UserID: "request-actor", Roles: []string{"developer"}}, domainworkflow.Input{ApplicationID: "app-1", WorkflowName: "reviewed-workflow", TriggerBuild: true}
}

func TestWorkflowRequestConcurrentSubmissionUsesOneOriginalRun(t *testing.T) {
	service, repo, principal, input := newWorkflowRequestTest(t)
	var wg sync.WaitGroup
	results := make(chan domainworkflow.TriggerReceipt, 16)
	failures := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			receipt, err := service.SubmitWorkflowRequest(t.Context(), principal, "original-request-1", input)
			results <- receipt
			failures <- err
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	runID := ""
	for receipt := range results {
		if runID == "" {
			runID = receipt.RunID
		}
		if receipt.RunID != runID || receipt.ID == "" || receipt.RequestKey != "original-request-1" {
			t.Fatal("original receipt changed")
		}
	}
	if repo.createCallCount() != 1 {
		t.Fatal("duplicate execution submitted")
	}
	receipt, err := service.GetWorkflowRequest(t.Context(), principal, "original-request-1")
	if err != nil || receipt.SubmissionStatus != "accepted" || receipt.Run == nil || receipt.Run.ID != runID {
		t.Fatalf("original receipt unavailable: %+v %v", receipt, err)
	}
}

func TestWorkflowRequestRejectsChangedInputAndKeepsTombstone(t *testing.T) {
	service, repo, principal, input := newWorkflowRequestTest(t)
	original, err := service.SubmitWorkflowRequest(t.Context(), principal, "original-request-1", input)
	if err != nil {
		t.Fatal(err)
	}
	changed := input
	changed.TriggerBuild = false
	if _, err := service.SubmitWorkflowRequest(t.Context(), principal, "original-request-1", changed); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("changed input reused original key", err)
	}
	repo.mu.Lock()
	repo.items = nil
	repo.mu.Unlock()
	receipt, err := service.SubmitWorkflowRequest(t.Context(), principal, "original-request-1", input)
	if err != nil || receipt.RunID != original.RunID || receipt.Run != nil || repo.createCallCount() != 1 {
		t.Fatal("run deletion allowed a new submission", err)
	}
}

func TestWorkflowRequestUnknownFailureNeverRetriesTrigger(t *testing.T) {
	service, repo, principal, input := newWorkflowRequestTest(t)
	repo.createErr = errors.New("connection interrupted after database submission")
	first, err := service.SubmitWorkflowRequest(t.Context(), principal, "original-request-1", input)
	if err != nil || first.SubmissionStatus != "unknown" || first.Run != nil {
		t.Fatal("uncertain create became a success or rejection", err)
	}
	repo.createErr = nil
	second, err := service.SubmitWorkflowRequest(t.Context(), principal, "original-request-1", input)
	if err != nil || second.RunID != first.RunID || second.SubmissionStatus != "unknown" || repo.createCallCount() != 1 {
		t.Fatal("unknown submission was retriggered", err)
	}
}

func TestWorkflowRequestRechecksPermissionAndActorOwnership(t *testing.T) {
	service, repo, principal, input := newWorkflowRequestTest(t)
	if _, err := service.SubmitWorkflowRequest(t.Context(), principal, "original-request-1", input); err != nil {
		t.Fatal(err)
	}
	denied := principal
	denied.Roles = []string{"reader"}
	if _, err := service.GetWorkflowRequest(t.Context(), denied, "original-request-1"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("receipt bypassed current permission", err)
	}
	other := principal
	other.UserID = "other-actor"
	if _, err := service.GetWorkflowRequest(t.Context(), other, "original-request-1"); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("another actor read the receipt", err)
	}
	if _, err := service.SubmitWorkflowRequest(t.Context(), other, "original-request-1", input); err != nil {
		t.Fatal(err)
	}
	if repo.createCallCount() != 2 {
		t.Fatal("different actor reused another actor's run")
	}
	triggerOnly := principal
	triggerOnly.Roles = []string{"trigger-only"}
	receipt, err := service.GetWorkflowRequest(t.Context(), triggerOnly, "original-request-1")
	if err != nil || receipt.Run != nil {
		t.Fatal("receipt bypassed separate run view permission", err)
	}
	receipt, err = service.SubmitWorkflowRequest(t.Context(), triggerOnly, "trigger-only-first-request", input)
	if err != nil || receipt.SubmissionStatus != "accepted" || receipt.Run != nil {
		t.Fatal("first submission bypassed separate run view permission", err)
	}
}

func TestWorkflowRequestRejectsInvalidKeysBeforeClaim(t *testing.T) {
	for _, key := range []string{"short", "with/slash-key", " spaces-request ", "request?query", "request\nkey"} {
		t.Run(key, func(t *testing.T) {
			service, repo, principal, input := newWorkflowRequestTest(t)
			if _, err := service.SubmitWorkflowRequest(t.Context(), principal, key, input); !errors.Is(err, apperrors.ErrInvalidArgument) || len(repo.requests) != 0 || repo.createCallCount() != 0 {
				t.Fatal("invalid request key reached storage", err)
			}
		})
	}
}
