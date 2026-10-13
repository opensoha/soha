package copilot

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func personalExportReceipt(request contractenv.Request, target io.Writer) (contractenv.ExportResult, error) {
	data := []byte("complete fixture archive")
	_, err := target.Write(data)
	l := request.Lease
	return contractenv.ExportResult{SchemaVersion: contractenv.ExportResultVersion, LeaseID: l.ID, RunID: l.RunID, OwnerID: l.OwnerID, ScopeHash: l.ScopeHash, SizeBytes: int64(len(data)), SHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(data)), ObservedAt: time.Now().UTC()}, err
}

func personalExportInput(t *testing.T, f *personalEnvironmentFixture, owner string) sohaapi.PersonalEnvironmentDataExportRequest {
	t.Helper()
	f.wake(t, owner)
	view, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor(owner), "sleep", "", "en-US")
	if err != nil {
		t.Fatal(err)
	}
	return sohaapi.PersonalEnvironmentDataExportRequest{AssistantID: view.AssistantID, EnvironmentVersion: view.Version}
}

func testPersonalDataExportOwnerFence(t *testing.T, f *personalEnvironmentFixture) {
	owner := "export-owner"
	input := personalExportInput(t, f, owner)
	before := f.exportCalls
	for _, bad := range []sohaapi.PersonalEnvironmentDataExportRequest{{AssistantID: "other", EnvironmentVersion: input.EnvironmentVersion}, {AssistantID: input.AssistantID, EnvironmentVersion: input.EnvironmentVersion - 1}} {
		if _, err := f.service.ExportPersonalEnvironmentData(f.ctx, f.actor(owner), bad, "en-US", &bytes.Buffer{}); !errors.Is(err, apperrors.ErrConflict) || f.exportCalls != before {
			t.Fatal("stale or foreign export reached Agent")
		}
	}
	f.scopeError = apperrors.ErrAccessDenied
	_, err := f.service.ExportPersonalEnvironmentData(f.ctx, f.actor(owner), input, "en-US", &bytes.Buffer{})
	f.scopeError = nil
	if !errors.Is(err, apperrors.ErrAccessDenied) || f.exportCalls != before {
		t.Fatal("revoked node permission reached Agent")
	}
	assertPersonalDataExportBusyFence(t, f, owner, input, before)
	f.exporter = func(r contractenv.Request, target io.Writer) (contractenv.ExportResult, error) {
		if _, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor(owner), "wake", "", "en-US"); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatal("export reservation allowed a concurrent wake")
		}
		return personalExportReceipt(r, target)
	}
	var archive bytes.Buffer
	if _, err := f.service.ExportPersonalEnvironmentData(f.ctx, f.actor(owner), input, "en-US", &archive); err != nil || archive.String() != "complete fixture archive" {
		t.Fatalf("confirmed export failed: %v", err)
	}
	record, err := f.repo.GetPersonalEnvironment(f.ctx, owner, input.AssistantID)
	if err != nil || record.State != "sleeping" || !record.DataInitialized || record.OperationID != "" || record.Version != input.EnvironmentVersion+2 {
		t.Fatal("export did not retain sleeping data and clear its reservation")
	}
}

func assertPersonalDataExportBusyFence(t *testing.T, f *personalEnvironmentFixture, owner string, input sohaapi.PersonalEnvironmentDataExportRequest, before int) {
	t.Helper()
	for _, state := range []string{"ready", "deletion_unknown", "sleeping"} {
		task := ""
		if state == "sleeping" {
			task = "00000000-0000-4000-8000-000000000048"
		}
		if err := f.db.Exec("UPDATE ai_personal_environments SET state=?,active_task_id=? WHERE assistant_id=?", state, task, input.AssistantID).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := f.service.ExportPersonalEnvironmentData(f.ctx, f.actor(owner), input, "en-US", &bytes.Buffer{}); !errors.Is(err, apperrors.ErrConflict) || f.exportCalls != before {
			t.Fatal("active or unconfirmed data export reached provider")
		}
	}
	if err := f.db.Exec("UPDATE ai_personal_environments SET state='sleeping',active_task_id='' WHERE assistant_id=?", input.AssistantID).Error; err != nil {
		t.Fatal(err)
	}
}

func testPersonalDataExportUnknownReceipt(t *testing.T, f *personalEnvironmentFixture) {
	owner := "export-unknown"
	input := personalExportInput(t, f, owner)
	f.exporter = func(r contractenv.Request, target io.Writer) (contractenv.ExportResult, error) {
		result, err := personalExportReceipt(r, target)
		result.OwnerID = "foreign-owner"
		return result, err
	}
	if _, err := f.service.ExportPersonalEnvironmentData(f.ctx, f.actor(owner), input, "en-US", &bytes.Buffer{}); !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatal("foreign stream receipt was reported as an export")
	}
	before := f.exportCalls
	if _, err := f.service.ExportPersonalEnvironmentData(f.ctx, f.actor(owner), input, "en-US", &bytes.Buffer{}); !errors.Is(err, apperrors.ErrConflict) || f.exportCalls != before {
		t.Fatal("lost export was automatically replayed")
	}
	record, err := f.repo.GetPersonalEnvironment(f.ctx, owner, input.AssistantID)
	if err != nil || record.State != "unknown" || record.OperationUntil == nil || record.ObservedAt != nil || !record.DataInitialized {
		t.Fatal("unconfirmed export fabricated a fresh receipt or cleared its window")
	}
}

func testPersonalDataExportLocalAbort(t *testing.T, f *personalEnvironmentFixture) {
	owner := "export-not-sent"
	input := personalExportInput(t, f, owner)
	f.exporter = func(contractenv.Request, io.Writer) (contractenv.ExportResult, error) {
		return contractenv.ExportResult{}, domain.ErrPersonalEnvironmentNotDispatched
	}
	if _, err := f.service.ExportPersonalEnvironmentData(f.ctx, f.actor(owner), input, "en-US", &bytes.Buffer{}); !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatal("local export failure became success")
	}
	record, err := f.repo.GetPersonalEnvironment(f.ctx, owner, input.AssistantID)
	if err != nil || record.State != "sleeping" || record.OperationID != "" || !record.DataInitialized {
		t.Fatal("local unissued export did not restore retained data")
	}
	f.exporter = personalExportReceipt
	input.EnvironmentVersion = record.Version
	if _, err := f.service.ExportPersonalEnvironmentData(f.ctx, f.actor(owner), input, "en-US", &bytes.Buffer{}); err != nil {
		t.Fatal("explicit fresh-version export failed")
	}
}
