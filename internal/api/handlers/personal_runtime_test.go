package handlers

import (
	"encoding/json"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func TestRuntimeInputRejectsAmbiguousJSON(t *testing.T) {
	for _, raw := range []string{
		`{"environmentVersion":1,"environmentVersion":2,"idempotencyKey":"abcdefgh","kind":"execute"}`,
		`{"EnvironmentVersion":1,"idempotencyKey":"abcdefgh","kind":"execute"}`,
		`{"environmentVersion":1,"idempotencyKey":"abcdefgh","kind":"browser","browser":{"action":"snapshot","x":0}}`,
		`{"environmentVersion":1,"idempotencyKey":"abcdefgh","kind":"file_read","file":{"path":"a","path":"b"}}`,
		`{"environmentVersion":1,"idempotencyKey":"abcdefgh","kind":"file_write","file":{"path":"a","contentBase64":null}}`,
	} {
		var input sohaapi.PersonalGuestRuntimeInput
		_ = json.Unmarshal([]byte(raw), &input)
		if runtimeRequiredFields([]byte(raw), input) {
			t.Fatal("ambiguous input accepted", raw)
		}
	}
}
