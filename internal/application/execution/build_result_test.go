package execution

import (
	"context"
	"strings"
	"testing"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
)

func TestBuildCompletionRequiresDigestAndRetainsServerProvenance(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, test := range []struct{ name, ref, digest, status string }{
		{"complete", "registry:5000/api:v1", digest, "completed"},
		{"missing", "registry:5000/api:v1", "", "failed"},
		{"short", "registry:5000/api:v1", "sha256:abc", "failed"},
		{"foreign-output", "registry:5000/other:v1", digest, "failed"},
		{"mismatched-digest", "registry:5000/api@sha256:" + strings.Repeat("b", 64), digest, "failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newExecutionRepoFake()
			repo.bundles["bundle"] = domaindelivery.ReleaseBundle{ID: "bundle", ApplicationID: "app", Status: "building", Metadata: map[string]any{"serviceId": "stale"}}
			repo.tasks["task"] = domaindelivery.ExecutionTask{ID: "task", ReleaseBundleID: "bundle", ApplicationID: "app", ApplicationEnvironmentID: "dev", TaskKind: "build", Status: "running", CallbackToken: "callback",
				Payload: map[string]any{"image": "registry:5000/api:v1", "serviceId": "api", "containerName": "main", "buildSourceId": "source", "resolvedCommit": "commit"},
			}
			s := New(repo, nil, nil, nil, "", "", "", "", 0, "", nil)
			updated, err := s.RecordCallback(context.Background(), domaindelivery.ExecutionCallbackInput{CallbackToken: "callback", Status: "completed", Payload: map[string]any{
				"image": test.ref, "imageDigest": test.digest, "applicationId": "foreign", "serviceId": "other", "containerName": "sidecar", "buildSourceId": "other", "resolvedCommit": "other", "repositoryId": "forged",
			}})
			if err != nil || updated.Status != test.status {
				t.Fatalf("completion = %s, %v", updated.Status, err)
			}
			bundle := repo.bundles["bundle"]
			for key, want := range map[string]string{"applicationId": "app", "serviceId": "api", "containerName": "main", "buildSourceId": "source", "resolvedCommit": "commit", "executionTaskId": "task", "releaseBundleId": "bundle"} {
				if bundle.Metadata[key] != want || updated.Result[key] != want {
					t.Fatalf("callback replaced %s provenance", key)
				}
			}
			if bundle.Metadata["repositoryId"] != nil || (bundle.Status == "ready") != (test.status == "completed") {
				t.Fatalf("invalid bundle provenance or status: %+v", bundle)
			}
		})
	}
}
