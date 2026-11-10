package copilot

import (
	"context"
	"errors"
	"reflect"
	"testing"

	appaigateway "github.com/opensoha/soha/internal/application/aigateway"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type runtimeModelCatalogFixture struct{}

func (*runtimeModelCatalogFixture) ListWorkbenchModels(context.Context, domainidentity.Principal, string) ([]appaigateway.WorkbenchModelOption, error) {
	return nil, nil
}

func (*runtimeModelCatalogFixture) ResolveWorkbenchModelOption(context.Context, domainidentity.Principal, appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchModelOption, error) {
	return appaigateway.WorkbenchModelOption{}, nil
}

func runtimeAssemblyFixture() (Dependencies, RuntimeDependencies) {
	gateway := &nativeLoopGateway{}
	runs := &nativeLoopStore{}
	deps := newTestDependencies(&inspectionAuthzTestRepository{})
	deps.Settings = &inspectionAuthzSettingsResolver{}
	deps.AgentPrincipals = &chatToolPrincipalResolver{}
	deps.NativeAuthorization = &nativeAuthorizationFixture{}
	deps.NativeRuns = runs
	deps.NativeTaskParents = &nativeTaskStoreFixture{nativeQueueCapture: &nativeQueueCapture{nativeLoopStore: runs}}
	deps.NativeTasks = &nativeTaskOwnerFixture{store: runs}
	return deps, RuntimeDependencies{
		Model: &fakeWorkbenchModelInvoker{}, Stream: gateway, Tools: gateway,
		ApprovalRequests: &fakeChatChanges{}, Approvals: &activityApprovals{},
		ModelCatalog: &runtimeModelCatalogFixture{}, CapabilityCatalog: gateway,
		GatewayAuthorization: gateway, PlanValidator: gateway, Image: &appearanceImageTestInvoker{},
	}
}

func configuredRuntimeTestService(t *testing.T) *Service {
	t.Helper()
	deps, runtime := runtimeAssemblyFixture()
	runtime.Image = nil
	service := MustNew(deps)
	if err := service.ConfigureRuntime(runtime); err != nil {
		t.Fatalf("configure runtime with optional image disabled: %v", err)
	}
	return service
}

func TestRuntimeDependenciesValidateEveryGatewayPort(t *testing.T) {
	for _, typedNil := range []bool{false, true} {
		deps, complete := runtimeAssemblyFixture()
		fields := reflect.ValueOf(complete).Type()
		for i := 0; i < fields.NumField(); i++ {
			name := fields.Field(i).Name
			if !typedNil && name == "Image" {
				continue
			}
			if typedNil {
				name += "/typed_nil"
			}
			t.Run(name, func(t *testing.T) {
				runtime := complete
				field := reflect.ValueOf(&runtime).Elem().Field(i)
				removeRuntimePort(field, typedNil)
				service := MustNew(deps)
				if err := service.ConfigureRuntime(runtime); err == nil {
					t.Fatal("incomplete runtime was configured")
				}
				if service.runtimeConfigured || service.runtime.Model != nil {
					t.Fatal("configuration failure published a partial runtime")
				}
				service.Start(t.Context())
				if service.Running() {
					t.Fatal("worker started with incomplete runtime")
				}
				if err := service.ConfigureRuntime(complete); err != nil {
					t.Fatalf("complete assembly after rejected binding: %v", err)
				}
				if err := service.ValidateRuntimeDependencies(); err != nil {
					t.Fatalf("validate complete assembly: %v", err)
				}
			})
		}
	}
}

func TestRuntimeDependenciesValidateNativeAndSettingsPorts(t *testing.T) {
	for _, name := range []string{"Settings", "AgentPrincipals", "NativeAuthorization", "NativeRuns", "NativeTaskParents", "NativeTasks"} {
		t.Run(name, func(t *testing.T) {
			for _, typedNil := range []bool{false, true} {
				deps, runtime := runtimeAssemblyFixture()
				removeRuntimePort(reflect.ValueOf(&deps).Elem().FieldByName(name), typedNil)
				service, err := New(deps)
				if typedNil {
					if err == nil {
						t.Fatal("typed nil accepted during construction")
					}
					continue
				}
				if err != nil {
					t.Fatalf("construction before runtime assembly: %v", err)
				}
				if err := service.ConfigureRuntime(runtime); err == nil {
					t.Fatal("required runtime dependency missing from validation")
				}
			}
		})
	}
}

func removeRuntimePort(field reflect.Value, typedNil bool) {
	if typedNil {
		field.Set(reflect.Zero(field.Elem().Type()))
		return
	}
	field.SetZero()
}

func TestRuntimeDependenciesOptionalImageIsUnsupported(t *testing.T) {
	service := configuredRuntimeTestService(t)
	_, err := service.generateAssistantAppearance(t.Context(), domainidentity.Principal{}, domaincopilot.Session{}, domaincopilot.WorkbenchAssistantTask{}, "", domaincopilot.PersonalAssistant{})
	if !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatalf("missing optional image generator returned %v", err)
	}
}

func TestNativeQueueRejectsIncompleteRuntimeBeforeCreatingRun(t *testing.T) {
	service, runs, gateway, _ := nativeLoopFixture(t)
	capture := &nativeQueueCapture{nativeLoopStore: runs}
	service.nativeRuns = capture
	service.runtime.PlanValidator = nil
	gateway.model = func(context.Context, appaigateway.WorkbenchRelayRequest) (appaigateway.WorkbenchRelayResponse, error) {
		t.Fatal("incomplete runtime called the model")
		return appaigateway.WorkbenchRelayResponse{}, nil
	}
	_, err := service.queueNativeMessage(t.Context(), domainidentity.Principal{UserID: "reader", Roles: []string{"reader"}}, domaincopilot.Session{ID: "session"}, domaincopilot.SessionMetadata{}, domaincopilot.WorkbenchSendMessageInput{Content: "hello"}, "en-US")
	if !errors.Is(err, apperrors.ErrUnsupportedOperation) || len(capture.hashes) != 0 {
		t.Fatalf("incomplete runtime queued a run: calls=%d error=%v", len(capture.hashes), err)
	}
}

func TestPlainChatRuntimeStartsAndInvokesModel(t *testing.T) {
	deps, runtime := runtimeAssemblyFixture()
	deps.NativeAuthorization, deps.NativeRuns, deps.NativeTaskParents, deps.NativeTasks = nil, nil, nil, nil
	deps.Settings = &inspectionAuthzSettingsResolver{settings: domainsettings.AISettings{WorkbenchModel: domainsettings.AIWorkbenchModelSettings{Enabled: true, DefaultPublicModel: "test-model"}}}
	runtime.GatewayAuthorization, runtime.PlanValidator = nil, nil
	model := &fakeWorkbenchModelInvoker{response: appaigateway.WorkbenchRelayResponse{Content: "hello"}}
	runtime.Model = model
	service := MustNew(deps)
	if err := service.ConfigureRuntime(runtime); err != nil {
		t.Fatalf("configure plain chat: %v", err)
	}
	service.Start(t.Context())
	if !service.Running() {
		t.Fatal("plain chat did not start")
	}
	defer func() {
		if err := service.Stop(context.Background()); err != nil {
			t.Errorf("stop plain chat: %v", err)
		}
	}()
	_, err := service.sendMessageWithSessionConfig(t.Context(), domainidentity.Principal{}, domaincopilot.Session{ID: "session"}, domaincopilot.SessionMetadata{}, "hello", "en-US")
	if !errors.Is(err, apperrors.ErrUnsupportedOperation) || model.request.PublicModel != "" {
		t.Fatalf("missing dsh must not invoke direct model: %v %+v", err, model.request)
	}

}
