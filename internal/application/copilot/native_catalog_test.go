package copilot

import (
	"errors"
	"testing"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestNativeCatalogFreezesActualDefaultProviderDefinitions(t *testing.T) {
	service, loop, gateway, _ := nativeLoopFixture(t)
	sessions, ok := service.sessions.(*inspectionAuthzTestRepository)
	if !ok {
		t.Fatal("fixture has no inspection session repository")
	}
	sessions.session = domaincopilot.Session{ID: loop.lease.Run.SessionID, CreatedBy: "reader", Metadata: map[string]any{"conversationKind": "personal_main"}}
	professional, err := appaigateway.NewProfessionalCapabilityProvider(service.RunProfessionalCapability)
	if err != nil {
		t.Fatal(err)
	}
	gateway.catalog = append(appaigateway.BuiltinCapabilityProvider{}.Tools(), appaigateway.NewKnowledgeCapabilityProvider(nil).Tools()...)
	gateway.catalog = append(gateway.catalog, professional.Tools()...)
	gateway.catalog = append(gateway.catalog, appaigateway.NewPersonalEnvironmentCapabilityProvider(service).Tools()...)
	bindings := append(chatKubernetesToolBindings(), nativeProfessionalBindings()...)
	bindings = append(bindings, loop.lease.Run.ToolBindings...)
	bindings = append(bindings, nativePersonalEnvironmentBindings()...)
	frozen, snapshot, err := service.freezeNativeCatalog(t.Context(), domainidentity.Principal{UserID: "reader"}, "session", bindings, nil)
	if err != nil || len(frozen) != len(bindings) || len(snapshot.Tools) != len(bindings) {
		t.Fatalf("actual Native provider catalog is not usable: bindings=%d frozen=%d definitions=%d err=%v", len(bindings), len(frozen), len(snapshot.Tools), err)
	}
	loop.lease.Run.ToolBindings = frozen
	loop.lease.Run.Input["_sohaNativeCatalog"] = snapshot
	tools, err := service.nativeModelTools(t.Context(), domainidentity.Principal{UserID: "reader"}, loop.lease.Run)
	if err != nil || len(tools) != len(bindings)+1 {
		t.Fatalf("frozen actual tools could not reach model: count=%d err=%v", len(tools), err)
	}
	if _, err := service.executeChatAgentTool(t.Context(), loop.lease.Run, frozen[len(frozen)-2], map[string]any{}); err != nil || gateway.lastCall.CapabilityVersion != "1" {
		t.Fatalf("environment tool bypassed the frozen Gateway version: %+v %v", gateway.lastCall, err)
	}
	if _, err := service.executeChatAgentTool(t.Context(), loop.lease.Run, frozen[len(frozen)-1], map[string]any{"path": "notes.txt"}); err != nil || gateway.lastCall.Input["path"] != "notes.txt" || gateway.lastCall.CapabilityVersion != "1" {
		t.Fatalf("file tool bypassed frozen Gateway input/version: %+v %v", gateway.lastCall, err)
	}
}

func TestNativeCatalogPinsDefinitionAndRejectsChangedOrNewlyGrantedTools(t *testing.T) {
	for _, scenario := range []string{"unchanged", "schema_changed", "version_changed", "revoked", "new_tool"} {
		t.Run(scenario, func(t *testing.T) {
			service, loop, gateway, _ := nativeLoopFixture(t)
			principal := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
			newTool := domainaigateway.ToolCapability{Name: "k8s.nodes.detail", Version: "1", RiskLevel: domainaigateway.RiskLevelRead, InputSchema: map[string]any{"type": "object"}}
			bindings := append(loop.lease.Run.ToolBindings, domaincopilot.AgentToolBinding{ToolName: newTool.Name})
			frozen, snapshot, err := service.freezeNativeCatalog(t.Context(), principal, "session", bindings, nil)
			if err != nil || len(frozen) != 1 || frozen[0].Config["capabilityVersion"] != "1" {
				t.Fatalf("initial catalog did not freeze authorization: %+v %v", frozen, err)
			}
			loop.lease.Run.ToolBindings = frozen
			loop.lease.Run.Input["_sohaNativeCatalog"] = snapshot
			catalog, _ := gateway.WorkbenchCapabilities(t.Context(), principal, domainaigateway.ManifestRequest{})
			gateway.catalog = catalog.Tools
			switch scenario {
			case "schema_changed":
				gateway.catalog[0].InputSchema = map[string]any{"type": "array"}
			case "version_changed":
				gateway.catalog[0].Version = "2"
			case "revoked":
				gateway.catalog = []domainaigateway.ToolCapability{}
			case "new_tool":
				gateway.catalog = append(gateway.catalog, newTool)
			}
			tools, err := service.nativeModelTools(t.Context(), principal, loop.lease.Run)
			want := 1
			if scenario == "unchanged" || scenario == "new_tool" {
				want = 2
			}
			if err != nil || len(tools) != want {
				t.Fatalf("model received a changed catalog: %+v %v", tools, err)
			}
			err = service.authorizeNativeCatalogTool(t.Context(), principal, loop.lease.Run, frozen[0])
			if (err == nil) != (want == 2) {
				t.Fatalf("changed definition was dispatched: %v", err)
			}
			if scenario == "new_tool" {
				if err := service.authorizeNativeCatalogTool(t.Context(), principal, loop.lease.Run, domaincopilot.AgentToolBinding{ToolName: newTool.Name}); !errors.Is(err, apperrors.ErrAccessDenied) {
					t.Fatalf("newly granted tool exceeded the original catalog: %v", err)
				}
			}
			if scenario == "unchanged" {
				if _, err := service.executeChatAgentTool(t.Context(), loop.lease.Run, frozen[0], map[string]any{"query": "lease"}); err != nil || gateway.lastCall.CapabilityVersion != "1" {
					t.Fatalf("frozen version did not reach Gateway invocation: %+v %v", gateway.lastCall, err)
				}
			}
		})
	}
}

func TestSelectedNativeCapabilityCannotFallBackToChatWhenCatalogIsUnavailable(t *testing.T) {
	service, loop, gateway, _ := nativeLoopFixture(t)
	principal := domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}
	intent := &domaincopilot.TaskIntent{Capabilities: []sohaapi.WorkbenchTaskIntentCapabilities{"trace"}}
	bindings := append(loop.lease.Run.ToolBindings, nativeProfessionalBindings()...)
	if _, _, err := service.freezeNativeCatalog(t.Context(), principal, "session", bindings, intent); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("unavailable selected task was accepted: %v", err)
	}
	gateway.catalog = []domainaigateway.ToolCapability{{Name: "copilot.trace.analyze", Version: "1", RiskLevel: domainaigateway.RiskLevelAnalyze, InputSchema: map[string]any{"type": "object"}}}
	frozen, snapshot, err := service.freezeNativeCatalog(t.Context(), principal, "session", bindings, intent)
	if err != nil {
		t.Fatal(err)
	}
	loop.lease.Run.ToolBindings = frozen
	loop.lease.Run.Input["taskIntent"], loop.lease.Run.Input["_sohaNativeCatalog"] = intent, snapshot
	gateway.catalog[0].Version = "2"
	if _, err := service.nativeModelTools(t.Context(), principal, loop.lease.Run); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("changed selected capability was replaced by ordinary chat: %v", err)
	}
	gateway.catalog = []domainaigateway.ToolCapability{}
	if _, err := service.nativeModelTools(t.Context(), principal, loop.lease.Run); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("revoked selected capability was replaced by ordinary chat: %v", err)
	}
}
