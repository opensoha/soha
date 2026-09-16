package docker

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/idempotency"
)

func operationScope(item domaindocker.Operation) map[string]string {
	scope := map[string]string{}
	for key, value := range map[string]string{"operationId": item.ID, "hostId": item.HostID, "projectId": item.ProjectID, "serviceId": item.ServiceID, "virtualizationConnectionId": stringValue(item.Payload, "virtualizationConnectionId")} {
		if value != "" {
			scope[key] = value
		}
	}
	return scope
}

// ToolInvocationScopes implements the shared capability resolver with local reads only.
// The tool descriptor is supplied by the server registry, never by a client.
func (s *Service) ToolInvocationScopes(ctx context.Context, principal domainidentity.Principal, tool domainaigateway.ToolCapability, input map[string]any) ([]map[string]string, error) {
	for _, permission := range tool.PermissionKeys {
		if err := s.authorize(ctx, principal, permission); err != nil {
			return nil, err
		}
	}
	scope, claim, err := s.capabilityScope(ctx, principal, tool.Name, input)
	if err != nil {
		return nil, err
	}
	scopes := []map[string]string{scope}
	if claim.id != "" {
		// Read the immutable receipt without reconciliation or control-plane mutations.
		item, found, err := s.findClaimedOperation(ctx, claim)
		if err != nil {
			return nil, err
		}
		if found {
			prior := operationScope(item)
			for key := range prior {
				if _, ok := scope[key]; !ok {
					delete(prior, key)
				}
			}
			if !reflect.DeepEqual(prior, scope) {
				scopes = append(scopes, prior)
			}
		}
	}
	return scopes, nil
}

func (s *Service) capabilityScope(ctx context.Context, principal domainidentity.Principal, name string, input map[string]any) (map[string]string, operationClaim, error) {
	var claim operationClaim
	var scope map[string]string
	var err error
	switch name {
	case "docker.hosts.quick_create.plan", "docker.hosts.quick_create.trigger":
		return s.quickCreateCapabilityScope(principal, name, input)
	case "docker.projects.deploy.plan", "docker.projects.deploy.trigger", "docker.projects.runtime.assess":
		project, getErr := s.repo.GetProject(ctx, stringValue(input, "projectId"))
		if getErr != nil {
			return nil, claim, getErr
		}
		scope = map[string]string{"projectId": project.ID, "hostId": project.HostID}
		if name == "docker.projects.deploy.trigger" {
			request := domaindocker.ProjectDeployInput{Action: stringValue(input, "action"), IdempotencyKey: stringValue(input, "idempotencyKey")}
			claim, err = operationClaimFor("docker.project.deploy:"+project.ID, principal, request.IdempotencyKey, request)
		}
		if name == "docker.projects.runtime.assess" {
			operation, getErr := s.repo.GetOperation(ctx, stringValue(input, "afterOperationId"))
			if getErr != nil {
				return nil, claim, getErr
			}
			if operation.ProjectID != project.ID || operation.HostID != project.HostID {
				return nil, claim, apperrors.ErrAccessDenied
			}
			scope["operationId"] = operation.ID
		}
	case "docker.services.action.trigger":
		service, getErr := s.repo.GetService(ctx, stringValue(input, "serviceId"))
		if getErr != nil {
			return nil, claim, getErr
		}
		scope = map[string]string{"serviceId": service.ID, "projectId": service.ProjectID, "hostId": service.HostID}
		request := domaindocker.ServiceActionInput{Action: stringValue(input, "action"), IdempotencyKey: stringValue(input, "idempotencyKey")}
		claim, err = operationClaimFor("docker.service.action:"+service.ID, principal, request.IdempotencyKey, request)
	case "docker.operations.get", "docker.operations.cancel":
		item, getErr := s.repo.GetOperation(ctx, stringValue(input, "operationId"))
		if getErr != nil {
			return nil, claim, getErr
		}
		scope = operationScope(item)
	default:
		return nil, claim, apperrors.ErrUnsupportedOperation
	}
	if err != nil {
		return nil, claim, err
	}
	return scope, claim, nil
}

func (s *Service) quickCreateCapabilityScope(principal domainidentity.Principal, name string, input map[string]any) (map[string]string, operationClaim, error) {
	var claim operationClaim
	var scope map[string]string
	var err error
	var request domaindocker.QuickCreateHostInput
	encoded, encodeErr := json.Marshal(input)
	if encodeErr != nil {
		return nil, claim, encodeErr
	}
	if err = json.Unmarshal(encoded, &request); err != nil {
		return nil, claim, err
	}
	request, _, err = s.prepareQuickCreateHost(request)
	if err != nil {
		return nil, claim, err
	}
	scope = map[string]string{"resourceKind": "docker.host"}
	if request.VirtualizationConnectionID != "" {
		scope["virtualizationConnectionId"] = request.VirtualizationConnectionID
	}
	if strings.HasSuffix(name, ".trigger") {
		claim, err = operationClaimFor("docker.host.quick_create", principal, request.IdempotencyKey, request)
		if err == nil {
			scope["hostId"], _, err = idempotency.Derive("docker.host.quick_create.host", firstNonEmpty(principal.UserID, principal.UserName), request.IdempotencyKey, request)
		}
	}
	if err != nil {
		return nil, claim, err
	}
	return scope, claim, nil
}

func checkProjectScope(ctx context.Context, project domaindocker.Project) error {
	return domaindocker.CheckScope(ctx, map[string]string{"hostId": project.HostID, "projectId": project.ID})
}

func checkServiceProject(ctx context.Context, service domaindocker.Service, project domaindocker.Project) error {
	if service.ProjectID != project.ID || service.HostID != project.HostID {
		return apperrors.ErrConflict
	}
	return domaindocker.CheckScope(ctx, map[string]string{"hostId": service.HostID, "projectId": service.ProjectID, "serviceId": service.ID})
}
