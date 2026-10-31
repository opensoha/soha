package contenttransfer

import (
	"encoding/json"
	"testing"
)

func TestMissingEnabledCannotBecomeDestinationClosure(t *testing.T) {
	for _, raw := range []string{`{"name":"target","kind":"s3"}`, `{"name":"target","kind":"s3","enabled":null}`, `{"name":"target","kind":"s3","enabled":false,"clean":true}`} {
		var config Config
		if json.Unmarshal([]byte(raw), &config) == nil {
			t.Fatal("incomplete or untrusted config accepted", raw)
		}
	}
}
