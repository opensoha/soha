package manifest

import (
	"testing"
)

func TestSensitiveConfigurationSurvivesContainersAndInvalidDrafts(t *testing.T) {
	for _, content := range []string{`{"apiVersion":"unknown.example/v1","kind":"Database","spec":{"password":"synthetic"}}`, `{"apiVersion":"apps/v1","kind":"UnknownCustomKind","spec":{}}`, `{"apiVersion":"v1","kind":"List","items":[{"apiVersion":"unknown.example/v1","kind":"Database"}]}`, "kind: [broken", `{"apiVersion":"v1","kind":"Secret"}`} {
		if !SensitiveFiles([]File{{Content: content}}) {
			t.Fatalf("protected configuration classified public: %s", content)
		}
	}
	if SensitiveFiles([]File{{Content: `{"apiVersion":"apps/v1","kind":"Deployment","spec":{}}`}}) {
		t.Fatal("native workload classification changed")
	}
}
