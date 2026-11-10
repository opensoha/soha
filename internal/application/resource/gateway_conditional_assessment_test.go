package resource

import (
	"testing"
	"time"

	contractresource "github.com/opensoha/soha-contracts/resource"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func mutationAssessmentCall(original domainai.ToolInvocationRequest) domainai.ToolInvocationRequest {
	return domainai.ToolInvocationRequest{ToolName: conditionalMutationAssessmentTool, CapabilityVersion: "1", RequestID: "assessment", SessionID: original.SessionID, Input: map[string]any{"clusterId": "cluster-a", "namespace": "team-a", "kind": "ConfigMap", "name": "app", "mutationRequestId": original.RequestID}}
}

func TestConditionalMutationAssessmentRequiresCurrentOriginalUIDAndRevision(t *testing.T) {
	for _, scenario := range []string{"current", "replaced", "changed-revision", "deleting", "ambiguous-404", "changed-connection", "unknown", "wrong-target", "changed-credential", "revoked-permission"} {
		t.Run(scenario, func(t *testing.T) {
			conditionalMutationAssessmentRequiresCurrentOriginalUIDAndRevisionPhase1(t, &scenario)
		})
	}
}

func TestConditionalDeleteAssessmentNeverTreatsGeneric404AsCompletion(t *testing.T) {
	service, _, direct, principal, call, _ := mutationFixture(t)
	call.ToolName = "k8s.resources.conditional.delete"
	delete(call.Input, "content")
	ctx := domainai.WithExecutionAuthorization(t.Context(), domainai.ExecutionAuthorization{ActorID: principal.UserID, ActorTokenID: principal.AccessTokenID, Call: call, ApprovalID: "approved"})
	if _, err := service.ExecuteOriginalConditionalMutation(ctx, principal, call); err != nil {
		t.Fatal(err)
	}
	direct.identityError = apperrors.ErrNotFound
	assessment, err := service.AssessOriginalConditionalMutation(t.Context(), principal, mutationAssessmentCall(call))
	if err != nil || assessment.Verdict != "inconclusive" || !assessment.Evidence[0].Incomplete {
		t.Fatal("deletion acceptance became completion", assessment, err)
	}
}

func TestConditionalDeleteAssessmentRequiresOriginalUIDAndVersionedPresence(t *testing.T) {
	for _, scenario := range []string{"absent", "replacement", "present", "deleting", "http404", "old-protocol", "missing-state", "wrong-gvr", "wrong-namespace", "missing-collection-version", "unknown-outcome", "changed-connection", "revoked"} {
		t.Run(scenario, func(t *testing.T) {
			conditionalDeleteAssessmentRequiresOriginalUIDAndVersionedPresencePhase1(t, &scenario)
		})
	}
}

func TestOriginalTypedWorkloadMutationHasCurrentRevisionAssessment(t *testing.T) {
	for _, action := range []string{"scale", "restart", "rollback"} {
		t.Run(action, func(t *testing.T) {
			service, _, direct, principal, call, _ := mutationFixture(t)
			service.authorizer = exactPermissionAuthorizer{resourcePermissionKey("workloads", "Deployment", domainaccess.Action(action)): true, resourcePermissionKey("workloads", "Deployment", domainaccess.ActionView): true}
			call.ToolName = "k8s.workloads.conditional.action"
			call.Input = map[string]any{"clusterId": "cluster-a", "namespace": "team-a", "kind": "Deployment", "name": "app", "action": action, "preconditions": map[string]any{"expectedUid": "uid", "expectedResourceVersion": "rv"}}
			if action == "scale" {
				call.Input["replicas"] = float64(0)
			}
			if action == "rollback" {
				call.Input["revision"] = "2"
			}
			ctx := domainai.WithExecutionAuthorization(t.Context(), domainai.ExecutionAuthorization{ActorID: principal.UserID, ActorTokenID: principal.AccessTokenID, Call: call, ApprovalID: "approved"})
			output, err := service.ExecuteOriginalConditionalMutation(ctx, principal, call)
			if err != nil || output["applied"] != true || direct.writes != 1 {
				t.Fatal("typed original mutation failed", output, err)
			}
			lookup := mutationAssessmentCall(call)
			lookup.Input["kind"] = "Deployment"
			assessment, err := service.AssessOriginalConditionalMutation(t.Context(), principal, lookup)
			if err != nil || assessment.Verdict != "satisfied" || direct.writes != 1 {
				t.Fatal("typed mutation did not verify its original revision", assessment, err)
			}
		})
	}
}

func conditionalMutationAssessmentRequiresCurrentOriginalUIDAndRevisionPhase1(t *testing.T, sharedScenario *string) {

	service, store, direct, principal, call, ctx := mutationFixture(t)
	output, err := service.ExecuteOriginalConditionalMutation(ctx, principal, call)
	if err != nil {
		t.Fatal(err)
	}
	lookup := mutationAssessmentCall(call)
	identity := contractresource.ResourceIdentity{Kind: "ConfigMap", Name: "app", Namespace: "team-a", UID: "uid", ResourceVersion: "next"}
	switch *sharedScenario {
	case "replaced":
		identity.UID = "replacement"
	case "changed-revision":
		identity.ResourceVersion = "later"
	case "deleting":
		now := time.Now()
		identity.DeletionTimestamp = &now
	case "ambiguous-404":
		direct.identityError = apperrors.ErrNotFound
	case "changed-connection":
		service.resolver = stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}, Metadata: map[string]any{"endpoint": "other"}}}
	case "unknown":
		for key, record := range store.rows {
			record.Status = "unknown"
			store.rows[key] = record
		}
	case "wrong-target":
		lookup.Input["name"] = "other"
	case "changed-credential":
		principal.AccessTokenID = "other-token"
	case "revoked-permission":
		service.authorizer = exactPermissionAuthorizer{resourcePermissionKey("configuration", "ConfigMap", domainaccess.ActionView): true}
	}
	direct.identity = identity
	assessment, err := service.AssessOriginalConditionalMutation(t.Context(), principal, lookup)
	denied := (*sharedScenario) == "wrong-target" || (*sharedScenario) == "changed-credential" || (*sharedScenario) == "revoked-permission"
	if denied {
		if err == nil {
			t.Fatal("assessment crossed original authorization/target boundary")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	assertCurrentMutationAssessment(t, assessment, *sharedScenario == "current", output["mutationReceiptId"], direct.writes)
}

func conditionalDeleteAssessmentRequiresOriginalUIDAndVersionedPresencePhase1(t *testing.T, sharedScenario *string) {

	service, store, direct, principal, call, _ := mutationFixture(t)
	call.ToolName = "k8s.resources.conditional.delete"
	delete(call.Input, "content")
	ctx := domainai.WithExecutionAuthorization(t.Context(), domainai.ExecutionAuthorization{ActorID: principal.UserID, ActorTokenID: principal.AccessTokenID, Call: call, ApprovalID: "approved"})
	if _, err := service.ExecuteOriginalConditionalMutation(ctx, principal, call); err != nil {
		t.Fatal(err)
	}
	direct.presence = contractresource.ResourcePresence{Protocol: contractresource.ResourcePresenceProtocol, APIVersion: "v1", Resource: "configmaps", Kind: "ConfigMap", Namespace: "team-a", Name: "app", CollectionResourceVersion: "opaque-current", State: "absent"}
	switch *sharedScenario {
	case "replacement", "present", "deleting":
		direct.presence.State, direct.presence.UID, direct.presence.ResourceVersion = "present", "uid", "opaque-object"
		if (*sharedScenario) == "replacement" {
			direct.presence.UID = "replacement-uid"
		}
		if (*sharedScenario) == "deleting" {
			now := time.Now()
			direct.presence.DeletionTimestamp = &now
		}
	case "http404":
		direct.presenceError = apperrors.ErrNotFound
	case "old-protocol":
		direct.presence.Protocol = "preconditions-v1"
	case "missing-state":
		direct.presence.State = ""
	case "wrong-gvr":
		direct.presence.Resource = "secrets"
	case "wrong-namespace":
		direct.presence.Namespace = "foreign"
	case "missing-collection-version":
		direct.presence.CollectionResourceVersion = ""
	case "unknown-outcome":
		for key, record := range store.rows {
			record.Status = "unknown"
			store.rows[key] = record
		}
	case "changed-connection":
		service.resolver = stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}, Metadata: map[string]any{"endpoint": "other"}}}
	case "revoked":
		service.authorizer = exactPermissionAuthorizer{resourcePermissionKey("configuration", "ConfigMap", domainaccess.ActionView): true}
	}
	assessment, err := service.AssessOriginalConditionalMutation(t.Context(), principal, mutationAssessmentCall(call))
	if (*sharedScenario) == "revoked" {
		if err == nil || direct.presenceReads != 0 {
			t.Fatal("revoked deletion observed protected metadata")
		}
		return
	}
	verified := (*sharedScenario) == "absent" || (*sharedScenario) == "replacement"
	assertOriginalDeletionAssessment(t, assessment, err, verified, direct.writes)
}

func assertCurrentMutationAssessment(t *testing.T, assessment domainai.CapabilityAssessment, current bool, receiptID any, writes int) {
	t.Helper()
	if (assessment.Verdict == "satisfied") != current || len(assessment.Evidence) != 1 || assessment.Evidence[0].Resource.ID != receiptID || writes != 1 {
		t.Fatal("assessment did not bind the original live revision", assessment, writes)
	}
}

func assertOriginalDeletionAssessment(t *testing.T, assessment domainai.CapabilityAssessment, err error, verified bool, writes int) {
	t.Helper()
	if err != nil || (assessment.Verdict == "satisfied") != verified || writes != 1 || len(assessment.Evidence) != 1 || assessment.Evidence[0].Incomplete == verified {
		t.Fatal("unconfirmed or wrong original UID became deletion success", assessment, err)
	}
}
