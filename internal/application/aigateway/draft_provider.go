package aigateway

import (
	"context"
	"errors"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"reflect"
)

type DraftCapabilityService interface {
	GetDeliveryDraftApplication(context.Context, domainidentity.Principal, string) (domainapp.App, error)
	PrepareDeliveryDraft(context.Context, domainidentity.Principal, domaindelivery.DeliveryDraftInput) (domaindelivery.DeliveryDraftInput, error)
	CreateDeliveryDraft(context.Context, domainidentity.Principal, domaindelivery.DeliveryDraftInput) (domaindelivery.DeliveryDraft, error)
	ConfirmDeliveryDraft(context.Context, domainidentity.Principal, string) (domaindelivery.DeliveryDraftConfirmResult, error)
	GetDeliveryDraft(context.Context, domainidentity.Principal, string) (domaindelivery.DeliveryDraft, error)
	FindDeliveryDraftCreation(context.Context, domainidentity.Principal, domaindelivery.DeliveryDraftInput) (domaindelivery.DeliveryDraft, error)
	GetDeliveryDraftConfirmation(context.Context, domainidentity.Principal, string) (domaindelivery.DeliveryDraftConfirmResult, error)
}
type draftCapabilityProvider struct {
	service DraftCapabilityService
	tools   []domainaigateway.ToolCapability
}

func NewDraftCapabilityProvider(service DraftCapabilityService) (CapabilityProvider, error) {
	schema, err := capabilityOpenAPISchema("DeliveryDraftInput")
	if err != nil {
		return nil, err
	}
	schema["required"] = []string{"applicationDraft", "idempotencyKey"}
	p := &draftCapabilityProvider{service: service}
	for _, tool := range defaultTools() {
		if tool.Name != "delivery.drafts.create" && tool.Name != "delivery.drafts.confirm" {
			continue
		}
		tool.Version = "1"
		tool.Execution = &domainaigateway.ToolExecutionContract{Mode: "sync", Idempotent: true, RecoveryMode: "original_call"}
		if tool.Name == "delivery.drafts.create" {
			tool.InputSchema = schema
			tool.Execution.IdempotencyKeyField = "idempotencyKey"
			tool.OutputSemantics = []domainaigateway.CapabilityValueSemantic{{Path: "/id", Kind: "delivery.draft"}}
			tool.Effects = []string{"delivery.draft.created"}
			tool.InputSemantics = []domainaigateway.CapabilityValueSemantic{
				{Path: "/environmentBindings/*/targets/*/docker/hostId", Kind: "docker.host"},
				{Path: "/environmentBindings/*/targets/*/docker/projectId", Kind: "docker.project"},
			}
		} else {
			tool.InputSemantics = []domainaigateway.CapabilityValueSemantic{{Path: "/draftId", Kind: "delivery.draft"}}
			tool.OutputSemantics = []domainaigateway.CapabilityValueSemantic{{Path: "/application/id", Kind: "delivery.application"}, {Path: "/services/*/id", Kind: "delivery.service", ScopePaths: map[string]string{"applicationId": "/services/*/applicationId"}}}
			tool.OutputSemantics = append(tool.OutputSemantics,
				domainaigateway.CapabilityValueSemantic{Path: "/environmentBindings/*/id", Kind: "delivery.application_environment", ScopePaths: map[string]string{"applicationId": "/environmentBindings/*/applicationId"}},
				domainaigateway.CapabilityValueSemantic{Path: "/environmentBindings/*/targets/*/id", Kind: "delivery.release_target", ScopePaths: map[string]string{"applicationId": "/environmentBindings/*/applicationId", "applicationEnvironmentId": "/environmentBindings/*/id", "serviceId": "/environmentBindings/*/targets/*/metadata/serviceId"}},
			)
			tool.Effects = []string{"delivery.application.configured", "delivery.services.configured", "delivery.environments.configured"}
		}
		p.tools = append(p.tools, tool)
	}
	return p, nil
}
func (p *draftCapabilityProvider) Tools() []domainaigateway.ToolCapability       { return p.tools }
func (*draftCapabilityProvider) Resources() []domainaigateway.ResourceCapability { return nil }
func (*draftCapabilityProvider) Prompts() []domainaigateway.PromptCapability     { return nil }
func (*draftCapabilityProvider) Skills() []domainaigateway.SkillCapability       { return nil }
func (p *draftCapabilityProvider) InvokeTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, map[string]any, error) {
	ctx = p.withScopeCheck(ctx, principal, tool, input)
	if tool.Name == "delivery.drafts.confirm" {
		result, err := p.service.ConfirmDeliveryDraft(ctx, principal, firstMapString(input, "draftId"))
		return result, nil, err
	}
	var request domaindelivery.DeliveryDraftInput
	if err := mapInput(input, &request); err != nil {
		return nil, nil, err
	}
	result, err := p.service.CreateDeliveryDraft(ctx, principal, request)
	return result, nil, err
}
func (p *draftCapabilityProvider) RecoverTool(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) (any, bool, error) {
	ctx = p.withScopeCheck(ctx, principal, tool, input)
	var result any
	var err error
	if tool.Name == "delivery.drafts.confirm" {
		result, err = p.service.GetDeliveryDraftConfirmation(ctx, principal, firstMapString(input, "draftId"))
	} else {
		var request domaindelivery.DeliveryDraftInput
		if err = mapInput(input, &request); err == nil {
			result, err = p.service.FindDeliveryDraftCreation(ctx, principal, request)
		}
	}
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil, false, nil
	}
	return result, err == nil, err
}
func (p *draftCapabilityProvider) ToolInvocationScopes(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) ([]map[string]string, error) {
	var draft domaindelivery.DeliveryDraft
	if tool.Name == "delivery.drafts.confirm" {
		var err error
		draft, err = p.service.GetDeliveryDraft(ctx, principal, firstMapString(input, "draftId"))
		if err != nil {
			return nil, err
		}
	} else {
		var request domaindelivery.DeliveryDraftInput
		if err := mapInput(input, &request); err != nil {
			return nil, err
		}
		existing, err := p.service.FindDeliveryDraftCreation(ctx, principal, request)
		if err == nil {
			draft = existing
		} else {
			if !errors.Is(err, apperrors.ErrNotFound) {
				return nil, err
			}
			prepared, err := p.service.PrepareDeliveryDraft(ctx, principal, request)
			if err != nil {
				return nil, err
			}
			draft.ApplicationDraft, draft.EnvironmentBindings = prepared.ApplicationDraft, prepared.EnvironmentBindings
		}
	}
	return p.draftScopes(ctx, principal, draft)
}

func (p *draftCapabilityProvider) draftScopes(ctx context.Context, principal domainidentity.Principal, draft domaindelivery.DeliveryDraft) ([]map[string]string, error) {
	scope := map[string]string{"applicationKey": draft.ApplicationDraft.Key}
	if draft.ApplicationDraft.ID != "" {
		scope["applicationId"] = draft.ApplicationDraft.ID
	}
	if draft.ApplicationDraft.BusinessLineID != "" {
		scope["businessLineId"] = draft.ApplicationDraft.BusinessLineID
	}
	scopes := []map[string]string{}
	for _, binding := range draft.EnvironmentBindings {
		item := map[string]string{}
		for k, v := range scope {
			item[k] = v
		}
		if binding.EnvironmentID != "" {
			item["environmentId"] = binding.EnvironmentID
		}
		for _, target := range binding.Targets {
			targetScope := map[string]string{}
			for k, v := range item {
				targetScope[k] = v
			}
			if target.ClusterID != "" {
				targetScope["clusterId"] = target.ClusterID
			}
			if target.Namespace != "" {
				targetScope["namespace"] = target.Namespace
			}
			if target.Docker != nil {
				targetScope["hostId"], targetScope["projectId"] = target.Docker.HostID, target.Docker.ProjectID
			}
			scopes = append(scopes, targetScope)
		}
		if len(binding.Targets) == 0 {
			scopes = append(scopes, item)
		}
	}
	if len(scopes) == 0 {
		scopes = append(scopes, scope)
	}
	if draft.ApplicationDraft.ID != "" {
		current, err := p.service.GetDeliveryDraftApplication(ctx, principal, draft.ApplicationDraft.ID)
		if err != nil {
			return nil, err
		}
		old := map[string]string{"applicationId": current.ID, "applicationKey": current.Key}
		if current.BusinessLineID != "" {
			old["businessLineId"] = current.BusinessLineID
		}
		if !reflect.DeepEqual(scope, old) {
			scopes = append(scopes, old)
		}
	}
	return scopes, nil
}

func (p *draftCapabilityProvider) withScopeCheck(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) context.Context {
	expected := capabilityInvocationScopes(ctx, tool.Name, capabilityGatewayScope(tool, input))
	return domaindelivery.WithDraftScopeCheck(ctx, func(ctx context.Context, draft domaindelivery.DeliveryDraft) error {
		actual, err := p.draftScopes(ctx, principal, draft)
		if err != nil {
			return err
		}
		for i, scope := range actual {
			actual[i], err = mergeCapabilityScope(capabilityGatewayScope(tool, input), scope)
			if err != nil {
				return err
			}
		}
		if !reflect.DeepEqual(expected, actual) {
			return apperrors.ErrAccessDenied
		}
		return nil
	})
}
