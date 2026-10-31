package resource

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	contractresource "github.com/opensoha/soha-contracts/resource"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainai "github.com/opensoha/soha/internal/domain/aigateway"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type mutationMemoryStore struct {
	mu          sync.Mutex
	rows        map[string]domainresource.ConditionalMutationRecord
	finishError bool
}

func (m *mutationMemoryStore) Claim(_ context.Context, record domainresource.ConditionalMutationRecord) (domainresource.ConditionalMutationRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := record.ActorID + ":" + record.KeyHash
	if old, ok := m.rows[key]; ok {
		if old.InputHash != record.InputHash || old.CredentialID != record.CredentialID {
			return old, false, apperrors.ErrConflict
		}
		return old, false, nil
	}
	m.rows[key] = record
	return record, true, nil
}
func (m *mutationMemoryStore) Get(_ context.Context, actor, key string) (domainresource.ConditionalMutationRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if record, ok := m.rows[actor+":"+key]; ok {
		return record, nil
	}
	return domainresource.ConditionalMutationRecord{}, apperrors.ErrNotFound
}
func (m *mutationMemoryStore) Finish(_ context.Context, record domainresource.ConditionalMutationRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.finishError {
		return errors.New("database unavailable")
	}
	m.rows[record.ActorID+":"+record.KeyHash] = record
	return nil
}

type mutationDirectStub struct {
	conditionalDirectStub
	failure       bool
	identity      contractresource.ResourceIdentity
	identityError error
	presence      contractresource.ResourcePresence
	presenceError error
	presenceReads int
}

func (d *mutationDirectStub) ObserveResourcePresence(context.Context, string, string, string, string) (contractresource.ResourcePresence, error) {
	d.presenceReads++
	return d.presence, d.presenceError
}

func (d *mutationDirectStub) GetResourceIdentity(_ context.Context, _, namespace, kind, name string) (contractresource.ResourceIdentity, error) {
	if d.identityError != nil {
		return contractresource.ResourceIdentity{}, d.identityError
	}
	if d.identity.UID != "" {
		return d.identity, nil
	}
	return contractresource.ResourceIdentity{Kind: kind, Name: name, Namespace: namespace, UID: "uid", ResourceVersion: "next"}, nil
}

func (d *mutationDirectStub) ExecuteWorkloadActionConditional(_ context.Context, _, namespace, kind, name, action string, input contractresource.ConditionalWorkloadActionInput) (contractresource.ConditionalWorkloadActionReceipt, error) {
	d.writes++
	return contractresource.ConditionalWorkloadActionReceipt{Applied: true, Kind: kind, Name: name, Namespace: namespace, Action: action, UID: input.Preconditions.ExpectedUID, ResourceVersion: "next", Preconditions: input.Preconditions}, nil
}

func (d *mutationDirectStub) ApplyResourceYAMLConditional(ctx context.Context, cluster, namespace, kind, name, content string, expected contractresource.ResourceMutationPreconditions) (domainresource.ResourceYAMLView, error) {
	item, err := d.conditionalDirectStub.ApplyResourceYAMLConditional(ctx, cluster, namespace, kind, name, content, expected)
	item.Content = "kind: " + kind + "\nmetadata:\n  name: " + name + "\n  namespace: " + namespace + "\n  uid: " + expected.ExpectedUID + "\n  resourceVersion: next\n"
	if d.failure {
		return item, errors.New("provider echoed private-yaml-value")
	}
	return item, err
}

func mutationFixture(t *testing.T) (*GatewayConditionalMutations, *mutationMemoryStore, *mutationDirectStub, domainidentity.Principal, domainai.ToolInvocationRequest, context.Context) {
	t.Helper()
	store := &mutationMemoryStore{rows: map[string]domainresource.ConditionalMutationRecord{}}
	direct := &mutationDirectStub{}
	permissions := exactPermissionAuthorizer{resourcePermissionKey("configuration", "ConfigMap", domainaccess.ActionUpdate): true, resourcePermissionKey("configuration", "ConfigMap", domainaccess.ActionDelete): true, resourcePermissionKey("configuration", "ConfigMap", domainaccess.ActionView): true}
	service, err := NewGatewayConditionalMutations(conditionalService(direct, permissions), store,
		func(_ context.Context, p domainidentity.Principal, _ string) (domainidentity.Principal, error) {
			return p, nil
		},
		func(context.Context, domainidentity.Principal, domainai.ExecutionAuthorization) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	principal := domainidentity.Principal{UserID: "actor", AccessTokenID: "original-token"}
	call := domainai.ToolInvocationRequest{ToolName: "k8s.resources.conditional.update", CapabilityVersion: "1", RequestID: "original-请求", Input: map[string]any{
		"clusterId": "cluster-a", "namespace": "team-a", "kind": "ConfigMap", "name": "app", "content": "apiVersion: v1\nkind: ConfigMap\ndata:\n  value: private-yaml-value\n",
		"preconditions": map[string]any{"expectedUid": "uid", "expectedResourceVersion": "rv"}}}
	authorization := domainai.ExecutionAuthorization{ActorID: principal.UserID, ActorTokenID: principal.AccessTokenID, Call: call, ApprovalID: "approved-original"}
	return service, store, direct, principal, call, domainai.WithExecutionAuthorization(t.Context(), authorization)
}

func TestOriginalConditionalMutationExecutesOnceAndRecoversSafeReceipt(t *testing.T) {
	service, store, direct, principal, call, ctx := mutationFixture(t)
	for attempt := 0; attempt < 2; attempt++ {
		output, err := service.ExecuteOriginalConditionalMutation(ctx, principal, call)
		if err != nil || output["applied"] != true || direct.writes != 1 {
			t.Fatalf("attempt=%d output=%v error=%v writes=%d", attempt, output, err, direct.writes)
		}
	}
	output, err := service.FindOriginalConditionalMutation(t.Context(), principal, call)
	raw, _ := json.Marshal(output)
	if err != nil || strings.Contains(string(raw), "private-yaml-value") || direct.writes != 1 || len(store.rows) != 1 {
		t.Fatal("original read replayed or leaked YAML", err)
	}
	for _, record := range store.rows {
		if record.Authorization.Call.Input != nil || record.InputHash == "" || record.Status != "succeeded" {
			t.Fatal("receipt stored YAML or lost its original binding")
		}
	}
}

func TestOriginalConditionalMutationFencesTheConnectionChosenForDispatch(t *testing.T) {
	service, store, direct, principal, call, ctx := mutationFixture(t)
	checks := 0
	service.check = func(context.Context, domainidentity.Principal, domainai.ExecutionAuthorization) error {
		checks++
		if checks == 2 {
			// Change after the wrapper's last authorize, before the owning
			// domain method resolves the transport used for the actual write.
			service.resolver = stubConnectionResolver{connection: domaincluster.Connection{
				Summary:  domaincluster.Summary{ID: "cluster-a", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig},
				Metadata: map[string]any{"kubeconfig_data": "replacement"}}}
		}
		return nil
	}
	_, err := service.ExecuteOriginalConditionalMutation(ctx, principal, call)
	if !errors.Is(err, apperrors.ErrConflict) || direct.writes != 0 || len(store.rows) != 1 {
		t.Fatalf("changed actual dispatch destination was accepted: %v writes=%d", err, direct.writes)
	}
	for _, record := range store.rows {
		if record.Status != "unknown" {
			t.Fatal("occupied failed attempt became replayable")
		}
	}
}

func TestOriginalConditionalMutationUnknownAndLostCommitNeverReplay(t *testing.T) {
	for _, scenario := range []string{"provider-response-lost", "receipt-commit-lost"} {
		t.Run(scenario, func(t *testing.T) {
			service, store, direct, principal, call, ctx := mutationFixture(t)
			direct.failure, store.finishError = scenario == "provider-response-lost", scenario == "receipt-commit-lost"
			for attempt := 0; attempt < 2; attempt++ {
				_, err := service.ExecuteOriginalConditionalMutation(ctx, principal, call)
				if !errors.Is(err, apperrors.ErrConflict) || strings.Contains(err.Error(), "private-yaml-value") || direct.writes != 1 {
					t.Fatalf("unknown replay/leak: %v writes=%d", err, direct.writes)
				}
			}
			if _, err := service.FindOriginalConditionalMutation(t.Context(), principal, call); !errors.Is(err, apperrors.ErrConflict) || direct.writes != 1 {
				t.Fatal("unknown original became success", err)
			}
			audit, ok := service.audit.(*workloadAuditRecorder)
			if !ok {
				t.Fatal("missing workload audit fixture")
			}
			raw, _ := json.Marshal(audit.entries)
			if strings.Contains(string(raw), "private-yaml-value") {
				t.Fatal("provider error leaked YAML into audit storage")
			}
		})
	}
}

func TestOriginalConditionalMutationRejectsChangedInputActorAndAuthorization(t *testing.T) {
	for _, scenario := range []string{"input", "actor", "token", "approval", "permission", "revoked"} {
		t.Run(scenario, func(t *testing.T) {
			service, _, direct, principal, call, ctx := mutationFixture(t)
			if _, err := service.ExecuteOriginalConditionalMutation(ctx, principal, call); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "input":
				copy := map[string]any{}
				for k, v := range call.Input {
					copy[k] = v
				}
				copy["content"] = "other"
				call.Input = copy
			case "actor":
				principal.UserID = "other"
			case "token":
				principal.AccessTokenID = "other"
			case "approval":
				ctx = t.Context()
			case "permission":
				service.authorizer = exactPermissionAuthorizer{}
			case "revoked":
				service.resolve = func(context.Context, domainidentity.Principal, string) (domainidentity.Principal, error) {
					return domainidentity.Principal{}, apperrors.ErrAccessDenied
				}
			}
			if _, err := service.ExecuteOriginalConditionalMutation(ctx, principal, call); err == nil || direct.writes != 1 {
				t.Fatalf("changed authorization/input accepted: %v writes=%d", err, direct.writes)
			}
			if scenario != "approval" {
				if _, err := service.FindOriginalConditionalMutation(t.Context(), principal, call); err == nil || direct.writes != 1 {
					t.Fatal("original read crossed authorization/input boundary")
				}
			}
		})
	}
}

func TestOriginalConditionalMutationDeletionReceiptIsAcceptanceOnly(t *testing.T) {
	service, _, direct, principal, call, _ := mutationFixture(t)
	call.ToolName = "k8s.resources.conditional.delete"
	delete(call.Input, "content")
	ctx := domainai.WithExecutionAuthorization(t.Context(), domainai.ExecutionAuthorization{ActorID: principal.UserID, ActorTokenID: principal.AccessTokenID, Call: call, ApprovalID: "approved"})
	output, err := service.ExecuteOriginalConditionalMutation(ctx, principal, call)
	if err != nil || output["accepted"] != true || output["state"] != "deletion_requested" || direct.writes != 1 {
		t.Fatal("deletion acceptance changed", output, err)
	}
}
