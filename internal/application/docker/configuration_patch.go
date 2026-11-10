package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"unicode/utf8"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func configurationFieldClass(kind, field string) string {
	common := map[string]string{"name": "string", "environment": "string", "owner": "string", "team": "string", "labels": "object", "config": "object"}
	if value := common[field]; value != "" {
		return value
	}
	switch kind {
	case "docker.host":
		return map[string]string{"endpoint": "string", "virtualizationConnectionId": "string", "vmId": "string", "vmName": "string", "availablePortStart": "integer", "availablePortEnd": "integer"}[field]
	case "docker.project":
		return map[string]string{"hostId": "string", "slug": "string", "description": "string", "sourceKind": "string", "sourceRef": "string", "composeContent": "string", "envContent": "string", "templateId": "string", "ttlSeconds": "integer"}[field]
	}
	return ""
}

func configurationChanges(kind string, input []sohaapi.DockerConfigurationChange) (map[string]any, []string, error) {
	if len(input) == 0 || len(input) > 32 {
		return nil, nil, apperrors.ErrInvalidArgument
	}
	changes := map[string]any{}
	fields := make([]string, 0, len(input))
	for _, change := range input {
		field := string(change.Field)
		if _, exists := changes[field]; exists {
			return nil, nil, apperrors.ErrInvalidArgument
		}
		value, err := configurationChangeValue(configurationFieldClass(kind, field), change)
		if err != nil {
			return nil, nil, err
		}
		changes[field] = value
		fields = append(fields, field)
	}
	return changes, fields, nil
}

func configurationChangeValue(class string, input sohaapi.DockerConfigurationChange) (any, error) {
	count := 0
	for _, present := range []bool{input.StringValue != nil, input.IntegerValue != nil, input.ObjectValue != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return nil, apperrors.ErrInvalidArgument
	}
	switch class {
	case "string":
		if input.StringValue != nil && utf8.RuneCountInString(*input.StringValue) <= 1048576 {
			return *input.StringValue, nil
		}
	case "integer":
		if input.IntegerValue != nil && *input.IntegerValue >= 0 && int64(*input.IntegerValue) <= math.MaxInt32 {
			return *input.IntegerValue, nil
		}
	case "object":
		if input.ObjectValue == nil {
			return nil, apperrors.ErrInvalidArgument
		}
		raw, err := json.Marshal(input.ObjectValue)
		if err != nil {
			return nil, apperrors.ErrInvalidArgument
		}
		var value map[string]any
		if configurationDecode(raw, &value) == nil && value != nil && validConfigurationMap(value) {
			return value, nil
		}
	}
	return nil, apperrors.ErrInvalidArgument
}

func validConfigurationMap(input map[string]any) bool {
	for _, value := range input {
		if configurationScalar(value) {
			continue
		}
		switch nested := value.(type) {
		case []any:
			for _, item := range nested {
				if !configurationScalar(item) {
					return false
				}
			}
		case map[string]any:
			for _, item := range nested {
				if !configurationScalar(item) {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}
func configurationScalar(input any) bool {
	switch input.(type) {
	case string, bool, json.Number:
		return true
	}
	return false
}

func configurationDecode(raw []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(out) != nil || decoder.Decode(new(any)) != io.EOF {
		return apperrors.ErrInvalidArgument
	}
	return nil
}

func validateConditionalHostInput(input *domaindocker.HostInput) error {
	if input.AvailablePortStart == 0 {
		input.AvailablePortStart = 20000
	}
	if input.AvailablePortEnd == 0 {
		input.AvailablePortEnd = 39999
	}
	if input.AvailablePortStart < 1 || input.AvailablePortEnd > 65535 || input.AvailablePortStart > input.AvailablePortEnd {
		return apperrors.ErrInvalidArgument
	}
	return validateHostInput(*input)
}

func (s *Service) patchConfigurationTarget(ctx context.Context, principal domainidentity.Principal, store ConfigurationMutationStore, request domaindocker.ConfigurationRequest, changes map[string]any, fields []string) (sohaapi.DockerResourceIdentity, error) {
	if request.Kind == "docker.host" {
		current, err := s.repo.GetHost(ctx, request.ResourceID)
		if err != nil {
			return sohaapi.DockerResourceIdentity{}, err
		}
		var input domaindocker.HostInput
		if mergeConfigurationInput(current, changes, &input) != nil {
			return sohaapi.DockerResourceIdentity{}, apperrors.ErrInvalidArgument
		}
		if err := validateConditionalHostInput(&input); err != nil {
			return sohaapi.DockerResourceIdentity{}, err
		}
		item, err := store.PatchHostConfiguration(ctx, request.ResourceID, input, fields)
		if err != nil {
			return sohaapi.DockerResourceIdentity{}, err
		}
		return resourceIdentity(request.Kind, item.ID, item.ResourceUID, item.ConfigurationRevision, "")
	}
	current, err := s.repo.GetProject(ctx, request.ResourceID)
	if err != nil {
		return sohaapi.DockerResourceIdentity{}, err
	}
	var input domaindocker.ProjectInput
	if mergeConfigurationInput(current, changes, &input) != nil {
		return sohaapi.DockerResourceIdentity{}, apperrors.ErrInvalidArgument
	}
	if err := validateProjectInput(input); err != nil {
		return sohaapi.DockerResourceIdentity{}, err
	}
	item, err := store.PatchProjectConfiguration(ctx, request.ResourceID, input, fields, composeServiceNames(input.ComposeContent))
	if err != nil {
		return sohaapi.DockerResourceIdentity{}, err
	}
	return resourceIdentity(request.Kind, item.ID, item.ResourceUID, item.ConfigurationRevision, item.HostID)
}

func (s *Service) deleteConfigurationTarget(ctx context.Context, kind, id string) (sohaapi.DockerResourceIdentity, error) {
	if kind == "docker.host" {
		item, err := s.repo.GetHost(ctx, id)
		if err != nil {
			return sohaapi.DockerResourceIdentity{}, err
		}
		identity, err := resourceIdentity(kind, item.ID, item.ResourceUID, item.ConfigurationRevision, "")
		if err != nil {
			return identity, err
		}
		return identity, s.repo.DeleteHost(ctx, id)
	}
	item, err := s.repo.GetProject(ctx, id)
	if err != nil {
		return sohaapi.DockerResourceIdentity{}, err
	}
	identity, err := resourceIdentity(kind, item.ID, item.ResourceUID, item.ConfigurationRevision, item.HostID)
	if err != nil {
		return identity, err
	}
	return identity, s.repo.DeleteProject(ctx, id)
}
