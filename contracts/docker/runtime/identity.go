// Package runtime owns the versioned Docker execution identity protocol.
package runtime

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"

	"github.com/google/uuid"
)

const (
	IdentityProtocol = "docker-project-identity-v1"
	HostUIDLabel     = "io.opensoha.host-uid"
	ProjectUIDLabel  = "io.opensoha.project-uid"
	ProtocolLabel    = "io.opensoha.runtime-protocol"
)

// ProjectBinding is supplied by the control plane, never by Compose input.
// Labels corroborate this trusted binding; labels alone do not authorize access.
type ProjectBinding struct {
	Protocol   string `json:"protocol"`
	HostUID    string `json:"hostUid"`
	ProjectUID string `json:"projectUid"`
	Namespace  string `json:"namespace"`
}

type ContainerBinding struct {
	ProjectBinding
	ServiceName string `json:"serviceName"`
	ContainerID string `json:"containerId"`
}

var containerIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var serviceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var protocolNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func NewProjectBinding(hostUID, projectUID string) (ProjectBinding, error) {
	b := ProjectBinding{Protocol: IdentityProtocol, HostUID: hostUID, ProjectUID: projectUID}
	if !canonicalUID(hostUID) || !canonicalUID(projectUID) {
		return ProjectBinding{}, errors.New("docker resource UID is invalid")
	}
	digest := sha256.Sum256([]byte(hostUID + ":" + projectUID))
	b.Namespace = "soha-" + hex.EncodeToString(digest[:26])
	return b, nil
}

func canonicalUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func (b ProjectBinding) Validate() error {
	expected, err := NewProjectBinding(b.HostUID, b.ProjectUID)
	if err != nil || b != expected {
		return errors.New("docker project runtime binding is invalid")
	}
	return nil
}

func (b ContainerBinding) Validate() error {
	if err := b.ProjectBinding.Validate(); err != nil {
		return err
	}
	if !serviceNamePattern.MatchString(b.ServiceName) || !containerIDPattern.MatchString(b.ContainerID) {
		return errors.New("docker container runtime binding is invalid")
	}
	return nil
}

func (b ProjectBinding) Labels() map[string]string {
	return map[string]string{HostUIDLabel: b.HostUID, ProjectUIDLabel: b.ProjectUID, ProtocolLabel: b.Protocol}
}

func ProjectBindingFromValue(value any) (ProjectBinding, error) {
	var binding ProjectBinding
	if decodeBinding(value, &binding) != nil || binding.Validate() != nil {
		return ProjectBinding{}, errors.New("docker runtime binding is invalid")
	}
	return binding, nil
}

func ContainerBindingFromValue(value any) (ContainerBinding, error) {
	var binding ContainerBinding
	if decodeBinding(value, &binding) != nil || binding.Validate() != nil {
		return ContainerBinding{}, errors.New("docker container binding is invalid")
	}
	return binding, nil
}

func decodeBinding(value any, binding any) error {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 2048 {
		return errors.New("docker runtime binding is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(binding)
}

func ValidateRuntimeProtocols(protocols []string) error {
	if len(protocols) > 4 {
		return errors.New("too many Docker runtime protocols")
	}
	for index, value := range protocols {
		if !protocolNamePattern.MatchString(value) || slices.Contains(protocols[:index], value) {
			return errors.New("docker runtime protocols are invalid or duplicated")
		}
	}
	return nil
}

// MatchesContainer requires the full immutable Engine ID and both Compose
// coordinates in addition to the control-plane identity labels. Never select a
// different container after this check fails.
func (b ContainerBinding) MatchesContainer(id string, labels map[string]string) bool {
	if b.Validate() != nil || id != b.ContainerID || labels["com.docker.compose.project"] != b.Namespace || labels["com.docker.compose.service"] != b.ServiceName {
		return false
	}
	for key, value := range b.Labels() {
		if labels[key] != value {
			return false
		}
	}
	return true
}
