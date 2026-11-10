package aigateway

import (
	"context"
	"errors"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	contractresource "github.com/opensoha/soha-contracts/resource"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type scopedIdentityFixture struct {
	conditionalWorkloadProviderSpy
	identity contractresource.ResourceIdentity
	err      error
	presence contractresource.ResourcePresence
}

func (f *scopedIdentityFixture) GetResourceIdentity(context.Context, domainidentity.Principal, string, string, string, string) (contractresource.ResourceIdentity, error) {
	return f.identity, f.err
}

func (f *scopedIdentityFixture) ObserveResourcePresence(context.Context, domainidentity.Principal, string, string, string, string) (contractresource.ResourcePresence, error) {
	return f.presence, nil
}

func TestPodIdentityPreconditionKeepsFrozenTaskScope(t *testing.T) {
	for _, scenario := range []string{"same_pod", "absent_pod_metadata", "absent_wrong_gvr", "absent_wrong_name", "absent_old_protocol", "other_name", "same_named_secret", "spoof_pod", "spoof_podName", "other_namespace", "revoked"} {
		t.Run(scenario, func(t *testing.T) { podIdentityPreconditionKeepsFrozenTaskScopePhase1(t, &scenario) })
	}
}

func TestProfessionalPodScopeAndConflictingAliases(t *testing.T) {
	p := &professionalCapabilityProvider{}
	resolved, err := p.ToolInvocationScopes(t.Context(), domainidentity.Principal{}, domainai.ToolCapability{}, map[string]any{"sessionId": "session", "scope": map[string]any{"pod": "api"}})
	if err != nil || len(resolved) != 1 {
		t.Fatal("professional scope unavailable", err)
	}
	scope, err := mergeCapabilityScope(nil, resolved[0])
	if err != nil || scope["podName"] != "api" || scope["pod"] != "" {
		t.Fatal("professional Pod scope lost its canonical target", scope, err)
	}
	for _, pair := range []struct{ base, resolved map[string]string }{
		{map[string]string{"podName": "other"}, resolved[0]},
		{nil, map[string]string{"pod": "api", "podName": "other"}},
	} {
		if _, err := mergeCapabilityScope(pair.base, pair.resolved); !errors.Is(err, apperrors.ErrAccessDenied) {
			t.Fatal("conflicting owner scope aliases accepted", err)
		}
	}
}

func podIdentityPreconditionKeepsFrozenTaskScopePhase1(t *testing.T, sharedScenario *string) {

	repo := &memoryGatewayRepository{aiClients: map[string]domainai.AIClient{"codex": {ID: "codex", Status: "active"}}}
	s, pod, actor := newPodCapabilityGateway(t, repo, &podCapabilityFixture{})
	fixture := &scopedIdentityFixture{identity: contractresource.ResourceIdentity{Kind: "Pod", Name: "api", Namespace: "team-a", UID: "uid", ResourceVersion: "1"}}
	configurePodIdentityScopeFixture(sharedScenario, fixture)
	conditional, err := NewConditionalResourceCapabilityProvider(fixture)
	if err != nil {
		t.Fatal(err)
	}
	s.SetCapabilityProviders(pod, conditional, BuiltinCapabilityProvider{})
	input := domainai.CapabilityTaskInput{AIClientID: "codex", SkillID: "k8s-pod-executor", ScopeConstraints: []domainai.CapabilityScopeConstraint{{Dimension: "clusterId", Value: "cluster-a"}, {Dimension: "namespace", Value: "team-a"}, {Dimension: "pod", Value: "api"}}, Plan: domainai.CapabilityPlan{Goal: "Inspect the original Pod", Steps: []domainai.CapabilityPlanStep{{ID: "identity", Call: sohaapi.CapabilityCall{ToolName: "k8s.resources.identity.read", CapabilityVersion: "1", Input: map[string]any{"clusterId": "cluster-a", "namespace": "team-a", "kind": fixture.identity.Kind, "name": fixture.identity.Name}}}}}}
	if (*sharedScenario) == "spoof_pod" {
		input.Plan.Steps[0].Call.Input["pod"] = "api"
	}
	if (*sharedScenario) == "spoof_podName" {
		input.Plan.Steps[0].Call.Input["podName"] = "api"
	}
	intent := domainworkflow.CapabilityIntent{ActorID: actor.UserID, ActorTokenID: actor.AccessTokenID, Digest: "frozen", PlanVersion: 1, Input: input, Deadline: time.Now().Add(time.Hour)}
	run := domainworkflow.Run{ID: "owned", Scope: domainworkflow.ScopeCapabilityTask, Status: "queued", Metadata: map[string]any{"capabilityIntent": intent}}
	_, err = s.VisibleCapabilityTask(t.Context(), actor, run)
	valid := (*sharedScenario) == "same_pod" || (*sharedScenario) == "absent_pod_metadata"
	if valid && err != nil {
		t.Fatal("the exact Pod precondition was rejected", err)
	}
	if !valid && !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("scope or current authorization was not enforced", err)
	}
	if (*sharedScenario) == "absent_pod_metadata" {
		for _, tool := range conditional.Tools() {
			if tool.Name == "k8s.resources.identity.read" {
				_, _, readErr := mustValueAs[*conditionalResourceCapabilityProvider](t, conditional).InvokeTool(t.Context(), actor, tool, input.Plan.Steps[0].Call.Input)
				if !errors.Is(readErr, apperrors.ErrNotFound) {
					t.Fatal("scope observation fabricated a live identity", readErr)
				}
			}
		}
	}
}

func configurePodIdentityScopeFixture(sharedScenario *string, fixture *scopedIdentityFixture) {
	switch *sharedScenario {
	case "other_name":
		fixture.identity.Name = "other"
	case "same_named_secret", "spoof_pod", "spoof_podName":
		fixture.identity.Kind = "Secret"
	case "other_namespace":
		fixture.identity.Namespace = "other"
	case "revoked":
		fixture.err = apperrors.ErrAccessDenied
	case "absent_pod_metadata", "absent_wrong_gvr", "absent_wrong_name", "absent_old_protocol":
		fixture.err = apperrors.ErrNotFound
		fixture.presence = contractresource.ResourcePresence{Protocol: contractresource.ResourcePresenceProtocol, APIVersion: "v1", Resource: "pods", Kind: "Pod", Namespace: "team-a", Name: "api", State: "absent", CollectionResourceVersion: "opaque"}
		if (*sharedScenario) == "absent_wrong_gvr" {
			fixture.presence.Resource = "secrets"
		}
		if (*sharedScenario) == "absent_wrong_name" {
			fixture.presence.Name = "other"
		}
		if (*sharedScenario) == "absent_old_protocol" {
			fixture.presence.Protocol = "preconditions-v1"
		}
	}
}
