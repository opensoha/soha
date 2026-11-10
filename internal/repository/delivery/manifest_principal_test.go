package delivery

import (
	"encoding/json"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	di "github.com/opensoha/soha/internal/domain/identity"
	"strings"
	"testing"
)

func TestManifestExecutionIdentityKeepsTokenReferencePrivate(t *testing.T) {
	task := dd.ExecutionTask{TaskKind: "manifest_apply", SecretPrincipal: di.Principal{UserID: "actor", AccessTokenID: "private-token-reference"}, Payload: map[string]any{"deploymentId": "deployment"}}
	stored, err := marshalExecutionPrincipal(task)
	if err != nil {
		t.Fatal(err)
	}
	restored := unmarshalExecutionPrincipal(stored)
	if restored.UserID != "actor" || restored.AccessTokenID != task.SecretPrincipal.AccessTokenID {
		t.Fatal("manifest claim lost durable identity")
	}
	public, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(public), "private-token-reference") || strings.Contains(string(public), "executionTokenId") {
		t.Fatal("private execution identity leaked")
	}
}
