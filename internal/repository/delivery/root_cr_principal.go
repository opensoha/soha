package delivery

import (
	"encoding/json"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	di "github.com/opensoha/soha/internal/domain/identity"
	"strings"
)

type storedRootCRPrincipal struct {
	di.Principal
	ExecutionTokenID string `json:"executionTokenId,omitempty"`
}

// Token identifiers stay in the existing private execution identity column;
// they never enter the public task payload or the serialized Principal DTO.
func marshalExecutionPrincipal(task dd.ExecutionTask) ([]byte, error) {
	if task.Payload["rootResource"] == nil && !strings.HasPrefix(task.TaskKind, "manifest_") {
		return json.Marshal(task.SecretPrincipal)
	}
	return json.Marshal(storedRootCRPrincipal{Principal: task.SecretPrincipal, ExecutionTokenID: task.SecretPrincipal.AccessTokenID})
}
func unmarshalExecutionPrincipal(data []byte) di.Principal {
	var stored storedRootCRPrincipal
	if json.Unmarshal(data, &stored) != nil {
		return di.Principal{}
	}
	stored.AccessTokenID = stored.ExecutionTokenID
	return stored.Principal
}
