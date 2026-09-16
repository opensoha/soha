package execution

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
)

func TestHelmCallbacksPreserveTheirInputBuildBundle(t *testing.T) {
	for _, action := range []sohaapi.HelmExecutionTaskPayloadAction{sohaapi.Preflight, sohaapi.Apply, sohaapi.Observe} {
		for _, status := range []string{"completed", "failed", "canceled"} {
			t.Run(string(action)+"/"+status, func(t *testing.T) {
				repo := newExecutionRepoFake()
				bundle := domaindelivery.ReleaseBundle{ID: "bundle", ApplicationID: "app", Status: "ready", ArtifactRef: "registry/app:v1", ArtifactDigest: "sha256:" + strings.Repeat("a", 64), Metadata: map[string]any{"sourceBuildTaskId": "build"}, UpdatedAt: time.Unix(100, 0)}
				repo.bundles[bundle.ID] = bundle
				snapshot := sohaapi.HelmDeliverySnapshot{RenderedDigest: "sha256:" + strings.Repeat("b", 64), ExpectedRevision: 0}
				task := domaindelivery.ExecutionTask{ID: "helm", TaskKind: "helm_" + string(action), Status: "running", CallbackToken: "callback", ReleaseBundleID: bundle.ID, ApplicationID: bundle.ApplicationID, Payload: map[string]any{"helm": sohaapi.HelmExecutionTaskPayload{Action: action, Snapshot: snapshot}}}
				repo.tasks[task.ID] = task
				result := sohaapi.HelmExecutionTaskResult{Stopped: true, Ready: true, Revision: 1, Status: "deployed", RenderedDigest: snapshot.RenderedDigest}
				if action == sohaapi.Preflight {
					result.Revision, result.Status = 0, "preflighted"
				}
				service := New(repo, nil, nil, nil, "", "", "", "", 0, "", nil)
				updated, err := service.RecordCallback(context.Background(), domaindelivery.ExecutionCallbackInput{CallbackToken: task.CallbackToken, Status: status, Payload: map[string]any{"helm": result}})
				if err != nil || updated.Status != status {
					t.Fatalf("callback = %s, %v", updated.Status, err)
				}
				if !reflect.DeepEqual(repo.bundles[bundle.ID], bundle) {
					t.Fatal("Helm outcome changed the input bundle or its build provenance")
				}
			})
		}
	}
}

func TestHelmCallbackRequiresFrozenCompletionAndRedactsRunnerContents(t *testing.T) {
	snapshot := sohaapi.HelmDeliverySnapshot{RenderedDigest: "sha256:" + strings.Repeat("a", 64), ExpectedRevision: 2}
	for _, action := range []sohaapi.HelmExecutionTaskPayloadAction{sohaapi.Preflight, sohaapi.Apply, sohaapi.Observe} {
		task := domaindelivery.ExecutionTask{TaskKind: "helm_" + string(action), Payload: map[string]any{"helm": sohaapi.HelmExecutionTaskPayload{Action: action, Snapshot: snapshot}}}
		result := sohaapi.HelmExecutionTaskResult{Stopped: true, Ready: true, Status: "deployed", Revision: 3, RenderedDigest: snapshot.RenderedDigest, Diagnostics: []string{"secret-value"}}
		if action == sohaapi.Preflight {
			result.Status, result.Revision = "preflighted", 2
		}
		input := domaindelivery.ExecutionCallbackInput{Status: "completed", Payload: map[string]any{"helm": result, "values": "secret-value", "error": "secret-value"}}
		output, err := normalizeHelmCallback(task, input)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(output)
		if strings.Contains(string(encoded), "secret-value") {
			t.Fatal("runner diagnostics or arbitrary payload reached public callback")
		}
		for _, mutation := range []func(*sohaapi.HelmExecutionTaskResult){
			func(r *sohaapi.HelmExecutionTaskResult) { r.Stopped = false },
			func(r *sohaapi.HelmExecutionTaskResult) { r.Revision++ },
			func(r *sohaapi.HelmExecutionTaskResult) { r.RenderedDigest = "other" },
			func(r *sohaapi.HelmExecutionTaskResult) { r.Status = "pending-upgrade" },
		} {
			changed := result
			mutation(&changed)
			input.Payload["helm"] = changed
			if _, err := normalizeHelmCallback(task, input); err == nil {
				t.Fatalf("accepted invalid %s completion", action)
			}
		}
		input.Status, input.Payload["helm"] = "canceled", sohaapi.HelmExecutionTaskResult{Stopped: false}
		if _, err := normalizeHelmCallback(task, input); err == nil {
			t.Fatal("accepted cancellation without stop acknowledgement")
		}
		input.Payload["helm"] = sohaapi.HelmExecutionTaskResult{Stopped: true, Status: "failed", Revision: 3}
		if _, err := normalizeHelmCallback(task, input); err != nil {
			t.Fatal("cancellation lost the failed native revision", err)
		}
	}
}
