package resource

import (
	"context"
	"fmt"
	"strings"

	contractresource "github.com/opensoha/soha/contracts/resource"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Kept separate from legacy ports: an older transport must fail closed, rather
// than silently executing a mutation without the caller's preconditions.
type ConditionalResourceAgent interface {
	GetResourceIdentity(context.Context, string, string, string) (contractresource.ResourceIdentity, error)
	DryRunResourceYAMLConditional(context.Context, string, string, string, string, contractresource.ResourceMutationPreconditions) (domainresource.ResourceUpdateAnalysis, error)
	ApplyResourceYAMLConditional(context.Context, string, string, string, string, contractresource.ResourceMutationPreconditions) (domainresource.ResourceYAMLView, error)
	DeleteResourceConditional(context.Context, string, string, string, contractresource.ResourceMutationPreconditions) error
}

type ConditionalDirectResource interface {
	GetResourceIdentity(context.Context, string, string, string, string) (contractresource.ResourceIdentity, error)
	DryRunResourceYAMLConditional(context.Context, string, string, string, string, string, contractresource.ResourceMutationPreconditions) (domainresource.ResourceUpdateAnalysis, error)
	ApplyResourceYAMLConditional(context.Context, string, string, string, string, string, contractresource.ResourceMutationPreconditions) (domainresource.ResourceYAMLView, error)
	DeleteResourceConditional(context.Context, string, string, string, string, contractresource.ResourceMutationPreconditions) error
}

func conditionalResourceKind(namespace, kind, name string) (string, error) {
	definition, ok := contractresource.LookupKubernetesResourceKind(kind)
	if !ok || !definition.GenericYAML || strings.TrimSpace(name) == "" || strings.TrimSpace(name) != name || strings.ContainsAny(name, "/\\?#\x00\r\n") {
		return "", fmt.Errorf("%w: supported resource kind and valid name are required", apperrors.ErrInvalidArgument)
	}
	if definition.Namespaced && strings.TrimSpace(namespace) == "" || !definition.Namespaced && namespace != "" || strings.TrimSpace(namespace) != namespace {
		return "", fmt.Errorf("%w: namespace does not match resource scope", apperrors.ErrInvalidArgument)
	}
	return definition.Kind, nil
}

func (g *GenericResources) conditionalTransports(connection domaincluster.Connection) (ConditionalResourceAgent, ConditionalDirectResource, error) {
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := g.genericResourceAgentClient(connection)
		if err != nil {
			return nil, nil, err
		}
		adapter, ok := client.(ConditionalResourceAgent)
		if !ok {
			return nil, nil, unsupportedAgentOperation("Agent does not support conditional resource mutations")
		}
		return adapter, nil, nil
	}
	adapter, ok := g.direct.(ConditionalDirectResource)
	if !ok {
		return nil, nil, fmt.Errorf("%w: direct adapter does not support conditional resource mutations", apperrors.ErrUnsupportedOperation)
	}
	return nil, adapter, nil
}

func (g *GenericResources) GetResourceIdentity(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name string) (contractresource.ResourceIdentity, error) {
	kind, err := conditionalResourceKind(namespace, kind, name)
	if err != nil {
		return contractresource.ResourceIdentity{}, err
	}
	connection, _, err := g.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionView)
	if err != nil {
		return contractresource.ResourceIdentity{}, err
	}
	agent, direct, err := g.conditionalTransports(connection)
	if err != nil {
		return contractresource.ResourceIdentity{}, err
	}
	var item contractresource.ResourceIdentity
	if agent != nil {
		item, err = agent.GetResourceIdentity(ctx, namespace, kind, name)
	} else {
		item, err = direct.GetResourceIdentity(ctx, clusterID, namespace, kind, name)
	}
	if err != nil {
		return contractresource.ResourceIdentity{}, wrapAgentResourceError(err)
	}
	_ = g.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, name, string(domainaccess.ActionView), "success", "viewed resource identity metadata")
	return item, nil
}

func (g *GenericResources) PlanResourceYAMLConditional(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name string, input contractresource.ConditionalResourceYAMLInput) (domainoperation.Plan, error) {
	kind, err := validateConditionalResourceInput(namespace, kind, name, input)
	if err != nil {
		return domainoperation.Plan{}, err
	}
	connection, _, err := g.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionUpdate)
	if err != nil {
		return domainoperation.Plan{}, err
	}
	agent, direct, err := g.conditionalTransports(connection)
	if err != nil {
		return domainoperation.Plan{}, err
	}
	var analysis domainresource.ResourceUpdateAnalysis
	if agent != nil {
		analysis, err = agent.DryRunResourceYAMLConditional(ctx, namespace, kind, name, input.Content, input.Preconditions)
	} else {
		analysis, err = direct.DryRunResourceYAMLConditional(ctx, clusterID, namespace, kind, name, input.Content, input.Preconditions)
	}
	if err != nil {
		return domainoperation.Plan{}, wrapAgentResourceError(err)
	}
	request := domainresource.ResourceUpdatePlanRequest{Namespace: namespace, Kind: kind, Name: name, Content: input.Content}
	return resourceUpdatePlan(connection.Summary.ID, request, analysis, struct {
		Target        domainresource.ResourceUpdatePlanRequest       `json:"target"`
		Preconditions contractresource.ResourceMutationPreconditions `json:"preconditions"`
	}{request, input.Preconditions})
}

func validateConditionalResourceInput(namespace, kind, name string, input contractresource.ConditionalResourceYAMLInput) (string, error) {
	canonical, err := conditionalResourceKind(namespace, kind, name)
	if err != nil {
		return "", err
	}
	if input.Preconditions.Validate() != nil || strings.TrimSpace(input.Content) == "" || len(input.Content) > 4<<20 {
		return "", fmt.Errorf("%w: bounded YAML and valid UID/resourceVersion preconditions are required", apperrors.ErrInvalidArgument)
	}
	return canonical, nil
}

func (g *GenericResources) ApplyResourceYAMLConditional(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name string, input contractresource.ConditionalResourceYAMLInput) (domainresource.ResourceYAMLView, error) {
	kind, err := validateConditionalResourceInput(namespace, kind, name, input)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	connection, _, err := g.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionUpdate)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if kind == "Secret" {
		if err := g.authorizeRuntimePermission(ctx, principal, appaccess.PermPlatformConfigurationSecretDataView); err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
	}
	agent, direct, err := g.conditionalTransports(connection)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	var item domainresource.ResourceYAMLView
	if agent != nil {
		item, err = agent.ApplyResourceYAMLConditional(ctx, namespace, kind, name, input.Content, input.Preconditions)
	} else {
		item, err = direct.ApplyResourceYAMLConditional(ctx, clusterID, namespace, kind, name, input.Content, input.Preconditions)
	}
	if err != nil {
		_ = g.recordAudit(ctx, principal, clusterID, namespace, kind, name, "update", "failure", "conditional resource update failed")
		return domainresource.ResourceYAMLView{}, wrapAgentResourceError(err)
	}
	_ = g.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, name, "update", "success", "conditionally applied resource YAML")
	g.recordOperation(ctx, principal, "platform.resource.apply", connection.Summary.ID, namespace, kind, name, "conditionally applied resource YAML", nil)
	return item, nil
}

func (g *GenericResources) DeleteResourceConditional(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name string, expected contractresource.ResourceMutationPreconditions) (contractresource.ResourceDeletionReceipt, error) {
	kind, err := conditionalResourceKind(namespace, kind, name)
	if err != nil {
		return contractresource.ResourceDeletionReceipt{}, err
	}
	if err := expected.Validate(); err != nil {
		return contractresource.ResourceDeletionReceipt{}, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	connection, _, err := g.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionDelete)
	if err != nil {
		return contractresource.ResourceDeletionReceipt{}, err
	}
	agent, direct, err := g.conditionalTransports(connection)
	if err != nil {
		return contractresource.ResourceDeletionReceipt{}, err
	}
	if agent != nil {
		err = agent.DeleteResourceConditional(ctx, namespace, kind, name, expected)
	} else {
		err = direct.DeleteResourceConditional(ctx, clusterID, namespace, kind, name, expected)
	}
	if err != nil {
		_ = g.recordAudit(ctx, principal, clusterID, namespace, kind, name, "delete", "failure", "conditional resource deletion request failed")
		return contractresource.ResourceDeletionReceipt{}, wrapAgentResourceError(err)
	}
	_ = g.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, name, "delete", "success", "resource deletion requested with preconditions")
	g.recordOperation(ctx, principal, "platform.resource.delete", connection.Summary.ID, namespace, kind, name, "resource deletion requested; completion not observed", nil)
	return contractresource.ResourceDeletionReceipt{Accepted: true, State: "deletion_requested", Kind: kind, Name: name, Namespace: namespace, Preconditions: expected}, nil
}
