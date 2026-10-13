package copilot

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func personalDataDeletionInput(t *testing.T, f *personalEnvironmentFixture, owner string, view domain.PersonalEnvironment) sohaapi.PersonalEnvironmentDataDeletionRequest {
	t.Helper()
	assistant, err := f.service.EnsurePersonalAssistant(f.ctx, f.actor(owner), "en-US")
	if err != nil {
		t.Fatal(err)
	}
	return sohaapi.PersonalEnvironmentDataDeletionRequest{AssistantID: assistant.ID, EnvironmentVersion: view.Version, ConfirmationName: assistant.Name}
}

func testPersonalDataDeletionConfirmation(t *testing.T, f *personalEnvironmentFixture) {
	owner := "delete-confirm"
	view := f.wake(t, owner)
	input := personalDataDeletionInput(t, f, owner, view)
	before := len(f.calls)
	if _, err := f.service.DeletePersonalEnvironmentData(f.ctx, f.actor(owner), input, "en-US"); !errors.Is(err, apperrors.ErrConflict) || len(f.calls) != before {
		t.Fatal("running environment was deleted")
	}
	view, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor(owner), "sleep", "", "en-US")
	if err != nil {
		t.Fatal(err)
	}
	input = personalDataDeletionInput(t, f, owner, view)
	before = len(f.calls)
	for _, mutate := range []func(*sohaapi.PersonalEnvironmentDataDeletionRequest){
		func(i *sohaapi.PersonalEnvironmentDataDeletionRequest) { i.AssistantID = "foreign" },
		func(i *sohaapi.PersonalEnvironmentDataDeletionRequest) { i.ConfirmationName += " " },
		func(i *sohaapi.PersonalEnvironmentDataDeletionRequest) { i.EnvironmentVersion-- },
	} {
		bad := input
		mutate(&bad)
		if _, err := f.service.DeletePersonalEnvironmentData(f.ctx, f.actor(owner), bad, "en-US"); !errors.Is(err, apperrors.ErrConflict) || len(f.calls) != before {
			t.Fatal("stale or incorrect confirmation dispatched deletion")
		}
	}
	assertPersonalDataDeletionScope(t, f, owner, input, before)
	deleted, err := f.service.DeletePersonalEnvironmentData(f.ctx, f.actor(owner), input, "en-US")
	if err != nil || deleted.State != "deleted" || deleted.ObservedAt == nil || len(f.calls) != before+1 || f.calls[before].Action != "delete_data" {
		t.Fatalf("confirmed deletion receipt: %+v %v", deleted, err)
	}
	before = len(f.calls)
	for _, action := range []string{"wake", "sleep"} {
		if _, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor(owner), action, "", "en-US"); !errors.Is(err, apperrors.ErrConflict) || len(f.calls) != before {
			t.Fatal("lifecycle operation silently undid data deletion")
		}
	}
}

func assertPersonalDataDeletionScope(t *testing.T, f *personalEnvironmentFixture, owner string, input sohaapi.PersonalEnvironmentDataDeletionRequest, before int) {
	t.Helper()
	f.scopeError = apperrors.ErrAccessDenied
	_, err := f.service.DeletePersonalEnvironmentData(f.ctx, f.actor(owner), input, "en-US")
	f.scopeError = nil
	if !errors.Is(err, apperrors.ErrAccessDenied) || len(f.calls) != before {
		t.Fatal("revoked node scope deleted data")
	}
	if err := f.db.Exec(`UPDATE ai_personal_environments SET active_task_id='other-task' WHERE created_by=?`, owner).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.DeletePersonalEnvironmentData(f.ctx, f.actor(owner), input, "en-US"); !errors.Is(err, apperrors.ErrConflict) || len(f.calls) != before {
		t.Fatal("deletion ignored another task")
	}
	if err := f.db.Exec(`UPDATE ai_personal_environments SET active_task_id='' WHERE created_by=?`, owner).Error; err != nil {
		t.Fatal(err)
	}
	current, err := f.repo.GetPersonalEnvironment(f.ctx, owner, input.AssistantID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec(`UPDATE ai_personal_assistants SET name=? WHERE id=? AND created_by=?`, input.ConfirmationName+" renamed", input.AssistantID, owner).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.ReservePersonalDataDeletion(f.ctx, current, input.ConfirmationName, uuid.NewString()); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("name change between read and reservation accepted the old confirmation")
	}
	if err := f.db.Exec(`UPDATE ai_personal_assistants SET name=? WHERE id=? AND created_by=?`, input.ConfirmationName, input.AssistantID, owner).Error; err != nil {
		t.Fatal(err)
	}

}

func testPersonalDataDeletionUnknownRecovery(t *testing.T, f *personalEnvironmentFixture) {
	owner := "delete-unknown"
	f.wake(t, owner)
	view, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor(owner), "sleep", "", "en-US")
	if err != nil {
		t.Fatal(err)
	}
	input := personalDataDeletionInput(t, f, owner, view)
	f.providerError = errors.New("transport outcome unavailable")
	unknown, err := f.service.DeletePersonalEnvironmentData(f.ctx, f.actor(owner), input, "en-US")
	f.providerError = nil
	if !errors.Is(err, apperrors.ErrServiceUnavailable) || unknown.State != "deletion_unknown" || unknown.ObservedAt != nil {
		t.Fatalf("unknown deletion became success: %+v %v", unknown, err)
	}
	f.allowReconcile(t, owner, input.AssistantID)
	f.receiptState = "released"
	view, err = f.service.ChangePersonalEnvironment(f.ctx, f.actor(owner), "reconcile", "", "en-US")
	f.receiptState = "active"
	if err != nil || view.State != "deletion_unknown" || view.ObservedAt != nil {
		t.Fatal("stopped receipt was mistaken for file deletion")
	}
	f.allowReconcile(t, owner, input.AssistantID)
	f.receiptState = "deleted"
	view, err = f.service.ChangePersonalEnvironment(f.ctx, f.actor(owner), "reconcile", "", "en-US")
	f.receiptState = "active"
	if err != nil || view.State != "deleted" || f.calls[len(f.calls)-1].Action != "inspect" {
		t.Fatal("reconciliation replayed deletion or ignored the actual tombstone receipt")
	}
	if err := f.db.Exec(`UPDATE ai_personal_environments SET state='deleting', operation_id='00000000-0000-4000-8000-000000000049', operation_until=? WHERE created_by=?`, time.Now().Add(-time.Second), owner).Error; err != nil {
		t.Fatal(err)
	}
	view, err = f.service.GetPersonalEnvironment(f.ctx, f.actor(owner), "en-US")
	if err != nil || view.State != "deletion_unknown" {
		t.Fatalf("expired deletion was not projected through the owner API: %+v %v", view, err)
	}
}

func testPersonalDataDeletionLocalAbort(t *testing.T, f *personalEnvironmentFixture) {
	owner := "delete-local"
	f.wake(t, owner)
	view, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor(owner), "sleep", "", "en-US")
	if err != nil {
		t.Fatal(err)
	}
	input := personalDataDeletionInput(t, f, owner, view)
	f.providerError = domain.ErrPersonalEnvironmentNotDispatched
	view, err = f.service.DeletePersonalEnvironmentData(f.ctx, f.actor(owner), input, "en-US")
	f.providerError = nil
	if !errors.Is(err, apperrors.ErrServiceUnavailable) || view.State != "sleeping" || !view.DataInitialized {
		t.Fatal("unissued deletion lost the preceding data state")
	}
	input.EnvironmentVersion = view.Version
	if view, err := f.service.DeletePersonalEnvironmentData(f.ctx, f.actor(owner), input, "en-US"); err != nil || view.State != "deleted" {
		t.Fatalf("explicit retry of unissued deletion: %+v %v", view, err)
	}
}
