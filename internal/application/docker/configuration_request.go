package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/idempotency"
)

type ConfigurationMutationStore interface {
	GetConfigurationRequest(context.Context, string, string) (domaindocker.ConfigurationRequest, error)
	ApplyConfigurationRequest(context.Context, domaindocker.ConfigurationRequest, func(context.Context) (sohaapi.DockerResourceIdentity, error)) (domaindocker.ConfigurationRequest, error)
	PatchHostConfiguration(context.Context, string, domaindocker.HostInput, []string) (domaindocker.Host, error)
	PatchProjectConfiguration(context.Context, string, domaindocker.ProjectInput, []string, []string) (domaindocker.Project, error)
}

var configurationRequestKey = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

func configurationRequest(principal domainidentity.Principal, kind, action, id, key string, input any) (domaindocker.ConfigurationRequest, error) {
	request := domaindocker.ConfigurationRequest{ActorID: principal.UserID, Key: key, Kind: kind, Action: action, ResourceID: id}
	if principal.UserID == "" || !configurationRequestKey.MatchString(key) || configurationPermission(kind, action) == "" {
		return request, apperrors.ErrInvalidArgument
	}
	claim, digest, err := idempotency.Derive("docker.configuration-request/v1", principal.UserID, key, struct {
		Kind, Action, ID string
		Input            any
	}{kind, action, id, input})
	if err != nil {
		return request, apperrors.ErrInvalidArgument
	}
	request.ID, request.InputDigest = claim, digest
	if action == "register" {
		request.ResourceID = "docker-host:" + claim
	}
	return request, nil
}

func configurationPermission(kind, action string) string {
	permission := ""
	switch kind {
	case "docker.host":
		permission = appaccess.PermDockerHostsManage
	case "docker.project":
		permission = appaccess.PermDockerProjectsManage
	default:
		return ""
	}
	if action == "register" {
		if kind != "docker.host" {
			return ""
		}
		action = "create"
	}
	if action != "create" && action != "update" && action != "delete" {
		return ""
	}
	return appaccess.ManagedActionPermission(permission, action)
}

func (s *Service) RegisterHostConfiguration(ctx context.Context, principal domainidentity.Principal, input sohaapi.DockerHostRegistrationInput) (_ sohaapi.DockerConfigurationReceipt, retErr error) {
	request, err := configurationRequest(principal, "docker.host", "register", "", input.IdempotencyKey, input)
	if err != nil {
		return sohaapi.DockerConfigurationReceipt{}, err
	}
	defer func() {
		s.recordMutationFailure(ctx, principal, "docker.configuration.register", request.ResourceID, request.ResourceID, retErr, nil)
	}()
	return s.applyConfigurationRequest(ctx, principal, request, func(txctx context.Context, store ConfigurationMutationStore) (sohaapi.DockerResourceIdentity, error) {
		var hostInput domaindocker.HostInput
		if err := mergeConfigurationInput(input.Host, nil, &hostInput); err != nil {
			return sohaapi.DockerResourceIdentity{}, err
		}
		hostInput.ID = request.ResourceID
		if !validConfigurationMap(hostInput.Config) || !validConfigurationMap(hostInput.Labels) {
			return sohaapi.DockerResourceIdentity{}, apperrors.ErrInvalidArgument
		}
		if err := validateConditionalHostInput(&hostInput); err != nil {
			return sohaapi.DockerResourceIdentity{}, err
		}
		host, err := s.CreateHost(txctx, principal, hostInput)
		if err != nil {
			return sohaapi.DockerResourceIdentity{}, err
		}
		return resourceIdentity("docker.host", host.ID, host.ResourceUID, host.ConfigurationRevision, "")
	})
}

func (s *Service) PatchConfiguration(ctx context.Context, principal domainidentity.Principal, kind, id string, input sohaapi.DockerConfigurationPatchInput) (_ sohaapi.DockerConfigurationReceipt, retErr error) {
	request, err := configurationRequest(principal, kind, "update", id, input.IdempotencyKey, input)
	if err != nil {
		return sohaapi.DockerConfigurationReceipt{}, err
	}
	defer func() { s.recordMutationFailure(ctx, principal, "docker.configuration.update", id, id, retErr, nil) }()
	if err := configurationConditions(&request, id, input.Preconditions); err != nil {
		return sohaapi.DockerConfigurationReceipt{}, err
	}
	changes, fields, err := configurationChanges(kind, input.Changes)
	if err != nil {
		return sohaapi.DockerConfigurationReceipt{}, err
	}
	if host, changing := changes["hostId"]; changing {
		request.TargetHostID, _ = host.(string)
		if input.TargetHostUID == nil || *input.TargetHostUID == uuid.Nil || strings.TrimSpace(request.TargetHostID) == "" {
			return sohaapi.DockerConfigurationReceipt{}, apperrors.ErrInvalidArgument
		}
		request.TargetHostUID = input.TargetHostUID.String()
	} else if input.TargetHostUID != nil {
		return sohaapi.DockerConfigurationReceipt{}, apperrors.ErrInvalidArgument
	}
	return s.applyConfigurationRequest(ctx, principal, request, func(txctx context.Context, store ConfigurationMutationStore) (sohaapi.DockerResourceIdentity, error) {
		return s.patchConfigurationTarget(txctx, principal, store, request, changes, fields)
	})
}

func (s *Service) DeleteConfiguration(ctx context.Context, principal domainidentity.Principal, kind, id string, input sohaapi.DockerConfigurationDeleteInput) (_ sohaapi.DockerConfigurationReceipt, retErr error) {
	request, err := configurationRequest(principal, kind, "delete", id, input.IdempotencyKey, input)
	if err != nil {
		return sohaapi.DockerConfigurationReceipt{}, err
	}
	defer func() { s.recordMutationFailure(ctx, principal, "docker.configuration.delete", id, id, retErr, nil) }()
	if err := configurationConditions(&request, id, input.Preconditions); err != nil {
		return sohaapi.DockerConfigurationReceipt{}, err
	}
	return s.applyConfigurationRequest(ctx, principal, request, func(txctx context.Context, store ConfigurationMutationStore) (sohaapi.DockerResourceIdentity, error) {
		return s.deleteConfigurationTarget(txctx, kind, id)
	})
}

func (s *Service) GetConfigurationRequest(ctx context.Context, principal domainidentity.Principal, key string) (sohaapi.DockerConfigurationReceipt, error) {
	if principal.UserID == "" || !configurationRequestKey.MatchString(key) {
		return sohaapi.DockerConfigurationReceipt{}, apperrors.ErrInvalidArgument
	}
	store, ok := s.repo.(ConfigurationMutationStore)
	if !ok {
		return sohaapi.DockerConfigurationReceipt{}, apperrors.ErrUnsupportedOperation
	}
	request, err := store.GetConfigurationRequest(ctx, principal.UserID, key)
	if err != nil {
		return sohaapi.DockerConfigurationReceipt{}, err
	}
	if err := s.authorizeConfigurationReceipt(ctx, principal, request); err != nil {
		return sohaapi.DockerConfigurationReceipt{}, err
	}
	return request.Receipt, nil
}

func configurationConditions(request *domaindocker.ConfigurationRequest, id string, input sohaapi.DockerConfigurationPreconditions) error {
	if strings.TrimSpace(id) == "" || id != strings.TrimSpace(id) || strings.ContainsAny(id, "\x00\r\n\t") || input.ExpectedUID == uuid.Nil || input.ExpectedConfigurationRevision == uuid.Nil {
		return apperrors.ErrInvalidArgument
	}
	request.ExpectedUID, request.ExpectedRevision = input.ExpectedUID.String(), input.ExpectedConfigurationRevision.String()
	return nil
}

func (s *Service) applyConfigurationRequest(ctx context.Context, principal domainidentity.Principal, request domaindocker.ConfigurationRequest, apply func(context.Context, ConfigurationMutationStore) (sohaapi.DockerResourceIdentity, error)) (sohaapi.DockerConfigurationReceipt, error) {
	store, ok := s.repo.(ConfigurationMutationStore)
	if !ok {
		return sohaapi.DockerConfigurationReceipt{}, apperrors.ErrUnsupportedOperation
	}
	if err := s.authorize(ctx, principal, configurationPermission(request.Kind, request.Action)); err != nil {
		return sohaapi.DockerConfigurationReceipt{}, err
	}
	previous, err := store.GetConfigurationRequest(ctx, principal.UserID, request.Key)
	if err == nil {
		if previous.InputDigest != request.InputDigest {
			return sohaapi.DockerConfigurationReceipt{}, apperrors.ErrConflict
		}
		if err := s.authorizeConfigurationReceipt(ctx, principal, previous); err != nil {
			return sohaapi.DockerConfigurationReceipt{}, err
		}
		return previous.Receipt, nil
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		return sohaapi.DockerConfigurationReceipt{}, err
	}
	result, err := store.ApplyConfigurationRequest(ctx, request, func(txctx context.Context) (sohaapi.DockerResourceIdentity, error) {
		if err := s.authorizeConfigurationTarget(txctx, principal, request); err != nil {
			return sohaapi.DockerResourceIdentity{}, err
		}
		return apply(txctx, store)
	})
	if err != nil {
		return sohaapi.DockerConfigurationReceipt{}, err
	}
	if err := s.authorizeConfigurationReceipt(ctx, principal, result); err != nil {
		return sohaapi.DockerConfigurationReceipt{}, err
	}
	s.recordOperation(ctx, principal, "docker.configuration."+request.Action, request.ResourceID, request.ResourceID, "success", "committed conditional Docker metadata request", map[string]any{"configurationRequestId": result.ID})
	return result.Receipt, nil
}

func (s *Service) authorizeConfigurationTarget(ctx context.Context, principal domainidentity.Principal, request domaindocker.ConfigurationRequest) error {
	if err := s.authorize(ctx, principal, configurationPermission(request.Kind, request.Action)); err != nil {
		return err
	}
	if request.Kind == "docker.host" {
		return domaindocker.CheckScope(ctx, map[string]string{"hostId": request.ResourceID, "resourceKind": request.Kind})
	}
	project, err := s.repo.GetProject(ctx, request.ResourceID)
	if err != nil {
		return err
	}
	if err := checkProjectScope(ctx, project); err != nil {
		return err
	}
	if request.TargetHostID != "" {
		return domaindocker.CheckScope(ctx, map[string]string{"projectId": project.ID, "hostId": request.TargetHostID, "resourceKind": request.Kind})
	}
	return nil
}

func (s *Service) authorizeConfigurationReceipt(ctx context.Context, principal domainidentity.Principal, request domaindocker.ConfigurationRequest) error {
	permission := configurationPermission(request.Kind, request.Action)
	if permission == "" || request.ActorID != principal.UserID || !validConfigurationReceipt(request) {
		return fmt.Errorf("%w: invalid original configuration receipt", apperrors.ErrServiceUnavailable)
	}
	if err := s.authorize(ctx, principal, permission); err != nil {
		return err
	}
	if request.Kind == "docker.host" {
		return domaindocker.CheckScope(ctx, map[string]string{"hostId": request.ResourceID, "resourceKind": request.Kind})
	}
	return domaindocker.CheckScope(ctx, map[string]string{"projectId": request.ResourceID, "hostId": request.Receipt.Resource.HostID, "resourceKind": request.Kind})
}

func validConfigurationReceipt(request domaindocker.ConfigurationRequest) bool {
	receipt := request.Receipt
	return receipt.ID == request.ID && receipt.RequestKey == request.Key && receipt.Resource.ID == request.ResourceID &&
		string(receipt.Resource.ResourceKind) == request.Kind && string(receipt.Action) == request.Action &&
		receipt.Resource.UID != uuid.Nil && receipt.Resource.ConfigurationRevision != uuid.Nil &&
		receipt.Status == "committed" && receipt.Deleted == (request.Action == "delete")
}

// Restore trusted current configuration only for validation. Repositories write
// the explicit changed fields, preserving all other raw JSON/runtime/TTL data.
func mergeConfigurationInput(current any, changes map[string]any, out any) error {
	raw, err := json.Marshal(current)
	if err != nil {
		return apperrors.ErrInvalidArgument
	}
	var value map[string]any
	if configurationDecode(raw, &value) != nil {
		return apperrors.ErrInvalidArgument
	}
	for key, item := range changes {
		value[key] = item
	}
	raw, err = json.Marshal(value)
	if err != nil {
		return apperrors.ErrInvalidArgument
	}
	return configurationDecode(raw, out)
}
