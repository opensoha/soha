package sohaapi

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestDockerConfigurationPatchPreservesExplicitTypedValues(t *testing.T) {
	raw := []byte(`{"idempotencyKey":"original-request-1","preconditions":{"expectedUid":"11111111-1111-4111-8111-111111111111","expectedConfigurationRevision":"22222222-2222-4222-8222-222222222222"},"changes":[{"field":"ttlSeconds","integerValue":0},{"field":"ttlSeconds","integerValue":2147483647},{"field":"description","stringValue":""},{"field":"config","objectValue":{}},{"field":"labels","objectValue":{"counter":9007199254740993}}]}`)
	var input DockerConfigurationPatchInput
	if err := json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	decode := func(data []byte) any {
		var value any
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	if !reflect.DeepEqual(decode(raw), decode(encoded)) {
		t.Fatalf("patch lost presence, precision or injected values: %s", encoded)
	}
}
