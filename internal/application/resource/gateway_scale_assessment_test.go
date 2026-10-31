package resource

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type scaleObservationDirect struct {
	*mutationDirectStub
	object    map[string]any
	readError error
	afterRead func()
	reads     int
	pinned    bool
}

func (d *scaleObservationDirect) GetResourceYAML(ctx context.Context, _, _, _, _ string) (domainresource.ResourceYAMLView, error) {
	d.reads++
	d.pinned = domaincluster.VerifyExecutionConnection(ctx, domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}, Metadata: map[string]any{"endpoint": "different"}}) != nil
	if d.afterRead != nil {
		d.afterRead()
	}
	raw, _ := json.Marshal(d.object)
	return domainresource.ResourceYAMLView{Content: string(raw)}, d.readError
}

func TestOriginalScaleAssessmentBindsAcknowledgmentInputAndCurrentUID(t *testing.T) {
	for _, scenario := range []string{"later-status-revision", "statefulset-zero", "dedicated-scale", "dedicated-scale-zero", "replacement", "different-desired", "missing-replicas", "missing-version", "deleting", "read-404", "wrong-kind", "unknown", "running", "unfinished", "future-finish", "not-applied", "dry-run", "different-approved-replicas", "different-reviewed-version", "changed-input-hash", "missing-approval", "wrong-original-tool", "wrong-target", "changed-credential", "revoked-view", "revoked-scale", "changed-connection", "connection-changes-during-read", "permission-revoked-during-read"} {
		t.Run(scenario, func(t *testing.T) { originalScaleAssessmentBindsAcknowledgmentInputAndCurrentUIDPhase1(t, &scenario) })
	}
}

func TestOriginalScaleAssessmentRejectsUnsupportedInputBeforeReads(t *testing.T) {
	service, _, direct, principal, call, _ := mutationFixture(t)
	call.ToolName = conditionalScaleAssessmentTool
	call.Input = map[string]any{"clusterId": "cluster-a", "namespace": "team-a", "kind": "Secret", "name": "app", "mutationRequestId": "original", "replicas": 2, "preconditions": map[string]any{"expectedUid": "uid", "expectedResourceVersion": "rv"}}
	if _, err := service.AssessOriginalWorkloadScale(t.Context(), principal, call); !errors.Is(err, apperrors.ErrInvalidArgument) || direct.writes != 0 {
		t.Fatal("unsupported resource accepted", err)
	}
}

func originalScaleAssessmentBindsAcknowledgmentInputAndCurrentUIDPhase1(t *testing.T, sharedScenario *string) {

	service, store, direct, principal, original, _ := mutationFixture(t)
	kind, replicas := "Deployment", json.Number("2")
	if (*sharedScenario) == "statefulset-zero" || (*sharedScenario) == "dedicated-scale-zero" {
		kind, replicas = "StatefulSet", json.Number("0")
	}
	viewKey := resourcePermissionKey("workloads", kind, domainaccess.ActionView)
	scaleKey := resourcePermissionKey("workloads", kind, domainaccess.Action("scale"))
	service.authorizer = exactPermissionAuthorizer{viewKey: true, scaleKey: true}
	original.ToolName = "k8s.workloads.conditional.action"
	delete(original.Input, "content")
	original.Input["kind"], original.Input["action"], original.Input["replicas"] = kind, "scale", replicas
	if strings.HasPrefix((*sharedScenario), "dedicated-scale") {
		original.ToolName = "k8s.workloads.scale"
		delete(original.Input, "action")
	}
	ctx := domainai.WithExecutionAuthorization(t.Context(), domainai.ExecutionAuthorization{ActorID: principal.UserID, ActorTokenID: principal.AccessTokenID, Call: original, ApprovalID: "approved-original"})
	output, err := service.ExecuteOriginalConditionalMutation(ctx, principal, original)
	if err != nil {
		t.Fatal(err)
	}
	lookup := mutationAssessmentCall(original)
	lookup.ToolName, lookup.Input["kind"] = conditionalScaleAssessmentTool, kind
	lookup.Input["replicas"], lookup.Input["preconditions"] = replicas, map[string]any{"expectedUid": "uid", "expectedResourceVersion": "rv"}
	metadata := map[string]any{"name": "app", "namespace": "team-a", "uid": "uid", "resourceVersion": "later-controller-status"}
	spec := map[string]any{"replicas": replicas, "template": map[string]any{"private": "SYNTHETIC_PRIVATE_WORKLOAD_VALUE"}}
	observer := &scaleObservationDirect{mutationDirectStub: direct, object: map[string]any{"kind": kind, "metadata": metadata, "spec": spec}}
	service.direct = observer
	readExpected, denied := true, false
	for key, record := range store.rows {
		tamperScaleObservation(*sharedScenario, metadata, spec, observer)
		if tamperOriginalScaleReceipt(*sharedScenario, &record, &lookup, spec) {
			readExpected = false
		}
		switch *sharedScenario {
		case "wrong-target":
			lookup.Input["name"], denied, readExpected = "other", true, false
		case "changed-credential":
			principal.AccessTokenID, denied, readExpected = "other", true, false
		case "revoked-view":
			service.authorizer, denied, readExpected = exactPermissionAuthorizer{scaleKey: true}, true, false
		case "revoked-scale":
			service.authorizer, denied, readExpected = exactPermissionAuthorizer{viewKey: true}, true, false
		case "changed-connection", "connection-changes-during-read":
			change := func() {
				service.resolver = stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}, Metadata: map[string]any{"endpoint": "different"}}}
			}
			if (*sharedScenario) == "changed-connection" {
				change()
				readExpected = false
			} else {
				observer.afterRead = change
			}
		case "permission-revoked-during-read":
			observer.afterRead = func() { service.authorizer = exactPermissionAuthorizer{} }
			denied = true
		}
		store.rows[key] = record
	}
	assessment, err := service.AssessOriginalWorkloadScale(t.Context(), principal, lookup)
	assertOriginalScaleOutcome(t, *sharedScenario, assessment, err, denied, readExpected, output, direct.writes, observer)
}

func tamperScaleObservation(scenario string, metadata, spec map[string]any, observer *scaleObservationDirect) {
	switch scenario {
	case "replacement":
		metadata["uid"] = "replacement"
	case "different-desired":
		spec["replicas"] = 3
	case "missing-replicas":
		delete(spec, "replicas")
	case "missing-version":
		delete(metadata, "resourceVersion")
	case "deleting":
		metadata["deletionTimestamp"] = time.Now().UTC().Format(time.RFC3339)
	case "read-404":
		observer.readError = apperrors.ErrNotFound
	case "wrong-kind":
		observer.object["kind"] = "Secret"
	}
}

func tamperOriginalScaleReceipt(scenario string, record *domainresource.ConditionalMutationRecord, lookup *domainai.ToolInvocationRequest, spec map[string]any) bool {
	switch scenario {
	case "unknown", "running":
		record.Status = scenario
	case "unfinished":
		record.FinishedAt = nil
	case "future-finish":
		future := time.Now().Add(time.Hour)
		record.FinishedAt = &future
	case "not-applied", "dry-run":
		if scenario == "not-applied" {
			record.Output["applied"] = false
		} else {
			record.Output["dryRun"] = true
		}
	case "different-approved-replicas":
		lookup.Input["replicas"], spec["replicas"] = 3, 3
	case "different-reviewed-version":
		lookup.Input["preconditions"] = map[string]any{"expectedUid": "uid", "expectedResourceVersion": "other"}
	case "changed-input-hash":
		record.InputHash = "tampered"
	case "missing-approval":
		record.Authorization.ApprovalID = ""
	case "wrong-original-tool":
		record.Authorization.Call.ToolName = "k8s.resources.conditional.update"
	default:
		return false
	}
	return true
}

func assertOriginalScaleOutcome(t *testing.T, scenario string, assessment domainai.CapabilityAssessment, err error, denied, readExpected bool, output map[string]any, writes int, observer *scaleObservationDirect) {
	t.Helper()
	if denied != (err != nil) {
		t.Fatalf("authorization result: %v", err)
	}
	if !denied {
		satisfied := scenario == "later-status-revision" || scenario == "statefulset-zero" || strings.HasPrefix(scenario, "dedicated-scale")
		if (assessment.Verdict == "satisfied") != satisfied || len(assessment.Evidence) != 1 || assessment.Evidence[0].Incomplete == satisfied || assessment.Evidence[0].Resource.ID != output["mutationReceiptId"] || assessment.Evidence[0].Reference.ToolName != conditionalScaleAssessmentTool {
			t.Fatal("assessment lost the original scale or accepted unconfirmed state", assessment)
		}
	}
	if (observer.reads == 1) != readExpected || observer.reads > 1 || observer.reads == 1 && !observer.pinned || writes != 1 {
		t.Fatal("unauthorized, unpinned or repeated read/write", observer.reads, observer.pinned, writes)
	}
	raw, _ := json.Marshal(assessment)
	if strings.Contains(string(raw), "SYNTHETIC_PRIVATE_WORKLOAD_VALUE") {
		t.Fatal("assessment leaked unrelated workload content")
	}
}
