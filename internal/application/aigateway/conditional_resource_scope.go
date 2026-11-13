package aigateway

import (
	"context"
	"errors"

	contractresource "github.com/opensoha/soha/contracts/resource"

	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type conditionalResourcePresenceService interface {
	ObserveResourcePresence(context.Context, domainidentity.Principal, string, string, string, string) (contractresource.ResourcePresence, error)
}

func (p *conditionalResourceCapabilityProvider) ToolInvocationScopes(ctx context.Context, principal domainidentity.Principal, tool domainai.ToolCapability, input map[string]any) ([]map[string]string, error) {
	scope := capabilityGatewayScope(tool, input)
	if tool.Name != "k8s.resources.identity.read" {
		if firstMapString(input, "podName", "pod") != "" {
			return nil, apperrors.ErrAccessDenied
		}
		return []map[string]string{scope}, nil
	}
	identity, err := p.service.GetResourceIdentity(ctx, principal, firstMapString(input, "clusterId"), firstMapString(input, "namespace"), firstMapString(input, "kind"), firstMapString(input, "name"))
	if err != nil {
		// A completed precondition remains visible after an authorized deletion.
		// This is only scope resolution: invoking identity.read still performs
		// its normal GET and cannot manufacture a current resource identity.
		if !errors.Is(err, apperrors.ErrNotFound) {
			return nil, err
		}
		observed, failure := p.missingConditionalScopeTarget(ctx, principal, input)
		if failure != nil {
			return nil, failure
		}
		identity.Kind, identity.Name, identity.Namespace = observed.Kind, observed.Name, observed.Namespace
	}
	scope["resourceKind"], scope["resourceName"] = identity.Kind, identity.Name
	if identity.Namespace != "" {
		scope["namespace"] = identity.Namespace
	}
	// A generic resource name proves a Pod scope only after the domain has
	// authorized the read and identified the actual resource as a Pod.
	if identity.Kind == "Pod" {
		scope["podName"] = identity.Name
	} else if firstMapString(input, "podName", "pod") != "" {
		return nil, apperrors.ErrAccessDenied
	}
	return []map[string]string{scope}, nil
}

func (p *conditionalResourceCapabilityProvider) missingConditionalScopeTarget(ctx context.Context, principal domainidentity.Principal, input map[string]any) (contractresource.ResourcePresence, error) {
	reader, supported := p.service.(conditionalResourcePresenceService)
	if !supported {
		return contractresource.ResourcePresence{}, apperrors.ErrNotFound
	}
	kind, namespace, name := firstMapString(input, "kind"), firstMapString(input, "namespace"), firstMapString(input, "name")
	observation, err := reader.ObserveResourcePresence(ctx, principal, firstMapString(input, "clusterId"), namespace, kind, name)
	if err != nil {
		return contractresource.ResourcePresence{}, err
	}
	if observation.ValidateFor(kind, namespace, name) != nil {
		return contractresource.ResourcePresence{}, apperrors.ErrAccessDenied
	}
	return observation, nil
}
