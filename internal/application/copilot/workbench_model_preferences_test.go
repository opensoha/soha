package copilot

import (
	"context"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"testing"
)

func TestWorkbenchModelPreferencesPersistAndReachInvoker(t *testing.T) {
	defer appaccess.SetRolePermissionMatrix(nil)
	service, repo := newInspectionAuthzTestService(map[string][]string{"chat": {appaccess.PermObserveAIChatUse}})
	service.settings = inspectionAuthzSettingsResolver{settings: domainsettings.AISettings{WorkbenchModel: domainsettings.AIWorkbenchModelSettings{Enabled: true, DefaultPublicModel: "default", DefaultRouteID: "default-route", DefaultEndpoint: "chat/completions"}}}
	invoker := &fakeWorkbenchModelInvoker{streamDeltas: []string{"ok"}}
	service.SetWorkbenchModelInvoker(invoker)
	repo.session = domaincopilot.Session{ID: "session-1", CreatedBy: "user-1", Metadata: sessionMetadataMap(domaincopilot.SessionMetadata{Mode: "general"})}
	principal := domainidentity.Principal{UserID: "user-1", Roles: []string{"chat"}}
	prefs := domaincopilot.WorkbenchModelPreferences{PublicModel: "selected", ReasoningEffort: "high"}
	_, err := service.StreamMessage(context.Background(), principal, "session-1", domaincopilot.WorkbenchSendMessageInput{Content: "hello", ModelPreferences: &prefs}, "en-US")
	if err != nil {
		t.Fatal(err)
	}
	if invoker.request.PublicModel != "selected" || invoker.request.RouteID != "" || invoker.request.ReasoningEffort != "high" {
		t.Fatalf("preferences lost: %+v", invoker.request)
	}
	if parseSessionMetadata(repo.session.Metadata).ModelPreferences != prefs {
		t.Fatal("preferences not persisted")
	}
	_, err = service.StreamMessage(context.Background(), principal, "session-1", domaincopilot.WorkbenchSendMessageInput{Content: "again"}, "en-US")
	if err != nil || invoker.request.PublicModel != "selected" {
		t.Fatalf("saved selection lost: %v", err)
	}
	prefs.ReasoningEffort = "invalid"
	_, err = service.StreamMessage(context.Background(), principal, "session-1", domaincopilot.WorkbenchSendMessageInput{Content: "bad", ModelPreferences: &prefs}, "en-US")
	if err == nil {
		t.Fatal("invalid effort accepted")
	}
}
