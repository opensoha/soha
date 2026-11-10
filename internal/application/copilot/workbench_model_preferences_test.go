package copilot

import (
	"context"
	"testing"
	"time"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func TestWorkbenchModelPreferencesPersistAndReachInvoker(t *testing.T) {
	service, store, _, _ := executionGovernanceFixture(t)
	repo := service.sessions.(*inspectionAuthzTestRepository)
	capture := attachDSHQueueFixture(t, service, repo)
	store = capture.nativeLoopStore
	principal := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
	prefs := domaincopilot.WorkbenchModelPreferences{PublicModel: "selected", ReasoningEffort: "high"}
	_, err := service.StreamMessage(context.Background(), principal, "session", domaincopilot.WorkbenchSendMessageInput{Content: "hello", ModelPreferences: &prefs}, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	if parseSessionMetadata(mapValue(capture.run.Input["metadata"])).ModelPreferences.PublicModel != "selected" {
		t.Fatal("model selection not frozen")
	}
	invoker := &dshModelInvoker{}
	service.runtime.Model = invoker
	service.runtime.Stream = invoker
	store.lease.Run = capture.run
	store.lease.Run.Status = "running"
	until := time.Now().Add(time.Minute)
	store.lease.LeaseUntil = &until
	cp := dshCheckpoint{nativeCheckpoint: initialNativeCheckpoint(store.lease.Run)}
	call := preparedDSHCall(t, service, store, &cp)
	if _, err := service.InvokeDSHModel(t.Context(), cp.Start.ToolGrant, call); err != nil {
		t.Fatal(err)
	}
	saved, _ := readDSHCheckpoint(store.lease)
	if saved.ModelContext.RequestedPublicModel != "selected" {
		t.Fatal("selected model did not reach gateway")
	}
	if parseSessionMetadata(repo.session.Metadata).ModelPreferences != prefs {
		t.Fatal("preferences not persisted")
	}
	_, err = service.StreamMessage(context.Background(), principal, "session", domaincopilot.WorkbenchSendMessageInput{Content: "again"}, "en-US")
	if err != nil || parseSessionMetadata(mapValue(capture.run.Input["metadata"])).ModelPreferences.PublicModel != "selected" {
		t.Fatalf("saved selection lost: %v", err)
	}
	prefs.ReasoningEffort = "invalid"
	_, err = service.StreamMessage(context.Background(), principal, "session", domaincopilot.WorkbenchSendMessageInput{Content: "bad", ModelPreferences: &prefs}, "en-US")
	if err == nil {
		t.Fatal("invalid effort accepted")
	}
}
