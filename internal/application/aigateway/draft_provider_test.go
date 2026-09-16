package aigateway

import (
	"context"
	"errors"
	"testing"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type draftProviderFixture struct {
	currentApp        *domainapp.App
	preparedApp       *domaindelivery.BlueprintApplicationDraft
	draft             domaindelivery.DeliveryDraft
	receipt           *domaindelivery.DeliveryDraftConfirmResult
	creates, confirms int
}

func (f *draftProviderFixture) GetDeliveryDraftApplication(_ context.Context, _ domainidentity.Principal, id string) (domainapp.App, error) {
	if f.currentApp != nil {
		return *f.currentApp, nil
	}
	return domainapp.App{ID: id, Key: f.draft.ApplicationDraft.Key, BusinessLineID: f.draft.ApplicationDraft.BusinessLineID}, nil
}

func (f *draftProviderFixture) PrepareDeliveryDraft(_ context.Context, _ domainidentity.Principal, input domaindelivery.DeliveryDraftInput) (domaindelivery.DeliveryDraftInput, error) {
	if f.preparedApp != nil {
		input.ApplicationDraft = *f.preparedApp
	}
	return input, nil
}
func (f *draftProviderFixture) CreateDeliveryDraft(ctx context.Context, principal domainidentity.Principal, input domaindelivery.DeliveryDraftInput) (domaindelivery.DeliveryDraft, error) {
	input, _ = f.PrepareDeliveryDraft(ctx, principal, input)
	if err := domaindelivery.CheckDraftScope(ctx, domaindelivery.DeliveryDraft{ApplicationDraft: input.ApplicationDraft, EnvironmentBindings: input.EnvironmentBindings}); err != nil {
		return domaindelivery.DeliveryDraft{}, err
	}
	f.creates++
	f.draft = domaindelivery.DeliveryDraft{ID: "draft-1", Status: "draft", ApplicationDraft: input.ApplicationDraft}
	return f.draft, nil
}
func (f *draftProviderFixture) GetDeliveryDraft(context.Context, domainidentity.Principal, string) (domaindelivery.DeliveryDraft, error) {
	return f.draft, nil
}
func (f *draftProviderFixture) FindDeliveryDraftCreation(context.Context, domainidentity.Principal, domaindelivery.DeliveryDraftInput) (domaindelivery.DeliveryDraft, error) {
	if f.creates == 0 {
		return f.draft, apperrors.ErrNotFound
	}
	return f.draft, nil
}
func (f *draftProviderFixture) ConfirmDeliveryDraft(ctx context.Context, _ domainidentity.Principal, _ string) (domaindelivery.DeliveryDraftConfirmResult, error) {
	if err := domaindelivery.CheckDraftScope(ctx, f.draft); err != nil {
		return domaindelivery.DeliveryDraftConfirmResult{}, err
	}
	f.confirms++
	f.draft.Status = "confirmed"
	f.draft.ApplicationDraft.ID = "app-1"
	result := domaindelivery.DeliveryDraftConfirmResult{Draft: f.draft, Application: domainapp.App{ID: "app-1", Key: "api"}, Services: []domainapp.Service{{ID: "service-1", ApplicationID: "app-1"}}}
	f.receipt = &result
	return result, nil
}

func TestDraftProviderChecksActualPreparedTargetAndOriginalScope(t *testing.T) {
	fixture := &draftProviderFixture{}
	owner, err := NewDraftCapabilityProvider(fixture)
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := owner.(*draftCapabilityProvider)
	if !ok {
		t.Fatalf("provider type = %T, want *draftCapabilityProvider", owner)
	}
	var tool domainaigateway.ToolCapability
	for _, item := range provider.Tools() {
		if item.Name == "delivery.drafts.create" {
			tool = item
		}
	}
	input := map[string]any{"applicationDraft": map[string]any{"name": "API", "key": "api", "group": "", "language": "go"}, "idempotencyKey": "draft-scope-check"}
	scopes, err := provider.ToolInvocationScopes(context.Background(), domainidentity.Principal{}, tool, input)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), capabilityScopeContextKey{}, capabilityScopeContext{tool: tool.Name, scopes: scopes})
	fixture.preparedApp = &domaindelivery.BlueprintApplicationDraft{ID: "appeared-app", Key: "api", Name: "API"}
	fixture.currentApp = &domainapp.App{ID: "appeared-app", Key: "api"}
	if _, _, err := provider.InvokeTool(ctx, domainidentity.Principal{}, tool, input); !errors.Is(err, apperrors.ErrAccessDenied) || fixture.creates != 0 {
		t.Fatalf("scope changed before persistence: %v, writes=%d", err, fixture.creates)
	}
	fixture.currentApp = &domainapp.App{ID: "appeared-app", Key: "old-key", BusinessLineID: "private"}
	fixture.preparedApp.BusinessLineID = "public"
	scopes, err = provider.ToolInvocationScopes(context.Background(), domainidentity.Principal{}, tool, input)
	if err != nil || len(scopes) != 2 || scopes[1]["businessLineId"] != "private" || scopes[1]["applicationKey"] != "old-key" {
		t.Fatalf("original scope missing: %v %v", scopes, err)
	}
}
func (f *draftProviderFixture) GetDeliveryDraftConfirmation(context.Context, domainidentity.Principal, string) (domaindelivery.DeliveryDraftConfirmResult, error) {
	if f.receipt == nil {
		return domaindelivery.DeliveryDraftConfirmResult{}, apperrors.ErrNotFound
	}
	return *f.receipt, nil
}

func TestDraftProviderUsesAtomicDomainAndRecoversReceipt(t *testing.T) {
	fixture := &draftProviderFixture{}
	provider, err := NewDraftCapabilityProvider(fixture)
	if err != nil {
		t.Fatal(err)
	}
	repo := &memoryGatewayRepository{}
	service := newDeliveryGatewayTestService(repo)
	service.SetCapabilityProviders(provider, BuiltinCapabilityProvider{})
	count := 0
	for _, tool := range service.gatewayRegistry().Tools() {
		if tool.Name == "delivery.drafts.create" {
			count++
			if tool.Version != "1" {
				t.Fatal("legacy entry shadowed the domain provider")
			}
		}
	}
	if count != 1 {
		t.Fatal("duplicate catalog entry")
	}
	create := domainaigateway.ToolInvocationRequest{ToolName: "delivery.drafts.create", CapabilityVersion: "1", Input: map[string]any{"idempotencyKey": "draft-create-1", "applicationDraft": map[string]any{"name": "API", "key": "api", "group": "test", "language": "go", "enabled": true}}}
	invoke := func(call domainaigateway.ToolInvocationRequest) domainaigateway.ToolInvocationResult {
		result, err := service.InvokeTool(context.Background(), testPrincipal("admin"), call)
		if err != nil {
			t.Fatal(err)
		}
		if result.RequiresApproval && result.Result != "success" {
			decision, err := service.ApproveApprovalRequest(context.Background(), testPrincipal("admin"), repo.approvalRequests[len(repo.approvalRequests)-1].ID, domainaigateway.ApprovalDecisionInput{})
			if err != nil || decision.Invocation == nil {
				t.Fatalf("approval: %+v %v", decision, err)
			}
			return *decision.Invocation
		}
		return result
	}
	created := invoke(create)
	step := domainaigateway.CapabilityPlanStep{ID: "confirm", DependsOn: []string{"draft"}, Call: sohaapi.CapabilityCall{ToolName: "delivery.drafts.confirm", CapabilityVersion: "1", Input: map[string]any{}}, Bindings: []domainaigateway.CapabilityInputBinding{{InputPath: "/draftId", StepID: "draft", OutputPath: "/id"}}}
	plan := domainaigateway.CapabilityPlan{Goal: "configure app", Steps: []domainaigateway.CapabilityPlanStep{{ID: "draft", Call: sohaapi.CapabilityCall{ToolName: create.ToolName, CapabilityVersion: "1", Input: create.Input}}, step}}
	call, err := service.ResolveCapabilityStep(step, plan, map[string]domainaigateway.ToolInvocationResult{"draft": created})
	if err != nil {
		t.Fatal(err)
	}
	confirmed := invoke(call)
	if confirmed.Result != "success" || fixture.confirms != 1 || fixture.creates != 1 {
		t.Fatal("domain confirmation was not used")
	}
	recreated := newDeliveryGatewayTestService(repo)
	recreated.SetCapabilityProviders(provider, BuiltinCapabilityProvider{})
	recovered, err := recreated.recoverCapabilityCall(context.Background(), testPrincipal("admin"), call)
	if err != nil || recovered == nil || fixture.confirms != 1 {
		t.Fatalf("recovery repeated confirmation: %+v %v", recovered, err)
	}
}

func TestCapabilityArrayBindingsPreserveEachResourceParent(t *testing.T) {
	draftProvider, err := NewDraftCapabilityProvider(&draftProviderFixture{})
	if err != nil {
		t.Fatal(err)
	}
	deliveryProvider, err := NewDeliveryCapabilityProvider(&deliveryProviderFixture{})
	if err != nil {
		t.Fatal(err)
	}
	service := newDeliveryGatewayTestService(&memoryGatewayRepository{})
	service.SetCapabilityProviders(draftProvider, deliveryProvider)
	step := domainaigateway.CapabilityPlanStep{ID: "workflow", DependsOn: []string{"confirm"}, Call: sohaapi.CapabilityCall{ToolName: "delivery.workflows.create", CapabilityVersion: "1", Input: map[string]any{"idempotencyKey": "workflow-create-1", "definition": map[string]any{"name": "Build API", "targets": []any{map[string]any{"id": "api", "action": "build"}}}}}, Bindings: []domainaigateway.CapabilityInputBinding{{InputPath: "/definition/targets/0/serviceId", StepID: "confirm", OutputPath: "/services/0/id"}, {InputPath: "/definition/targets/0/applicationId", StepID: "confirm", OutputPath: "/application/id"}}}
	plan := domainaigateway.CapabilityPlan{Goal: "configure and build", Steps: []domainaigateway.CapabilityPlanStep{{ID: "confirm", Call: sohaapi.CapabilityCall{ToolName: "delivery.drafts.confirm", CapabilityVersion: "1", Input: map[string]any{"draftId": "draft-1"}}}, step}}
	output := map[string]any{"application": map[string]any{"id": "app-1"}, "services": []any{map[string]any{"id": "service-1", "applicationId": "app-1"}}}
	completed := map[string]domainaigateway.ToolInvocationResult{"confirm": {Result: "success", Output: output}}
	tool, _ := service.toolByName(step.Call.ToolName)
	if !validPlannedCapabilityInput(tool, step) {
		t.Fatal("array binding did not defer only its missing fields")
	}
	if _, err := service.ResolveCapabilityStep(step, plan, completed); err != nil {
		t.Fatalf("valid binding: %v", err)
	}
	services, ok := output["services"].([]any)
	if !ok || len(services) == 0 {
		t.Fatalf("services type/value = %T %#v", output["services"], output["services"])
	}
	serviceOutput, ok := services[0].(map[string]any)
	if !ok {
		t.Fatalf("service output type = %T, want map[string]any", services[0])
	}
	serviceOutput["applicationId"] = "another-app"
	if _, err := service.ResolveCapabilityStep(step, plan, completed); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("service crossed application scope: %v", err)
	}
	for _, path := range []string{"/services/-1/id", "/services/01/id", "/services/name/id"} {
		source, _ := service.toolByName("delivery.drafts.confirm")
		if _, ok := capabilitySemantic(source.OutputSemantics, path); ok {
			t.Fatalf("non-canonical array index matched %s", path)
		}
	}
}

func TestCapabilityReleaseTargetBindingsKeepNestedScopes(t *testing.T) {
	draft, err := NewDraftCapabilityProvider(&draftProviderFixture{})
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := NewDeliveryCapabilityProvider(&deliveryProviderFixture{})
	if err != nil {
		t.Fatal(err)
	}
	service := newDeliveryGatewayTestService(&memoryGatewayRepository{})
	service.SetCapabilityProviders(draft, delivery)
	target := map[string]any{"id": "api", "action": "deploy", "releaseBundleId": "bundle-1"}
	step := domainaigateway.CapabilityPlanStep{ID: "workflow", DependsOn: []string{"confirm"}, Call: sohaapi.CapabilityCall{ToolName: "delivery.workflows.create", CapabilityVersion: "1", Input: map[string]any{"idempotencyKey": "workflow-docker-1", "definition": map[string]any{"name": "Deploy API", "targets": []any{target}}}}}
	for field, output := range map[string]string{"applicationId": "/application/id", "serviceId": "/services/0/id", "applicationEnvironmentId": "/environmentBindings/1/id", "releaseTargetId": "/environmentBindings/1/targets/1/id"} {
		step.Bindings = append(step.Bindings, domainaigateway.CapabilityInputBinding{InputPath: "/definition/targets/0/" + field, StepID: "confirm", OutputPath: output})
	}
	plan := domainaigateway.CapabilityPlan{Goal: "deploy", Steps: []domainaigateway.CapabilityPlanStep{{ID: "confirm", Call: sohaapi.CapabilityCall{ToolName: "delivery.drafts.confirm", CapabilityVersion: "1", Input: map[string]any{"draftId": "draft-1"}}}, step}}
	selected := map[string]any{"id": "target-1", "metadata": map[string]any{"serviceId": "service-1"}}
	environment := map[string]any{"id": "env-1", "applicationId": "app-1", "targets": []any{map[string]any{"id": "decoy-target", "metadata": map[string]any{"serviceId": "other-service"}}, selected}}
	completed := map[string]domainaigateway.ToolInvocationResult{"confirm": {Result: "success", Output: map[string]any{"application": map[string]any{"id": "app-1"}, "services": []any{map[string]any{"id": "service-1", "applicationId": "app-1"}}, "environmentBindings": []any{map[string]any{"id": "decoy-env", "applicationId": "other-app"}, environment}}}}
	if _, err := service.ResolveCapabilityStep(step, plan, completed); err != nil {
		t.Fatalf("nested binding: %v", err)
	}
	selectedMetadata, ok := selected["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("selected metadata type = %T, want map[string]any", selected["metadata"])
	}
	selectedMetadata["serviceId"] = "other-service"
	if _, err := service.ResolveCapabilityStep(step, plan, completed); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("release target crossed service scope: %v", err)
	}
	selectedMetadata["serviceId"] = "service-1"
	environment["applicationId"] = "other-app"
	if _, err := service.ResolveCapabilityStep(step, plan, completed); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("environment crossed application scope: %v", err)
	}
}
