package docker

import (
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

// ConfigurationRequest is private transaction state. Input bodies are never
// duplicated in the retained receipt. No foreign key removes this tombstone.
type ConfigurationRequest struct {
	ID, ActorID, Key, InputDigest string
	Kind, ResourceID, Action      string
	ExpectedUID, ExpectedRevision string
	TargetHostID, TargetHostUID   string
	Receipt                       sohaapi.DockerConfigurationReceipt
}
