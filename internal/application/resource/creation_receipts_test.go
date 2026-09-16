package resource

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type creationObservationStub struct {
	resourceUpdatePlanDirectStub
	content string
	err     error
}

func (s *creationObservationStub) GetResourceYAML(context.Context, string, string, string, string) (domainresource.ResourceYAMLView, error) {
	return domainresource.ResourceYAMLView{Content: s.content}, s.err
}

func TestCreationReceiptRecoveryAndOriginalIdentity(t *testing.T) {
	ctx := context.Background()
	principal := domainidentity.Principal{UserID: "user-1"}
	content := "apiVersion: v1\nkind: Service\nmetadata:\n  name: service\n  namespace: minio\n  uid: original-uid\n"
	direct := &creationDirectStub{manifests: []domainresource.ResolvedCreateManifest{testCreateManifest("Service", "minio", true)}, createdContent: content + "spec:\n  neverReturn: hidden-body-value\n"}
	authorizer := &recordingCreateAuthorizer{allow: true}
	store := &resourceCreationBatchStub{}
	creation := testResourceCreation(direct, authorizer, allowRuntimePermission{}, domaincluster.ConnectionModeDirectKubeconfig)
	creation.batches = store
	request := domainresource.ResourceCreateRequest{Source: domainresource.ResourceCreateSourceGlobal, Content: "manifest", RequestID: "original-key"}
	created, err := creation.ExecuteCreate(ctx, principal, "cluster-a", request)
	if err != nil || created.Documents[0].Resource.UID != "original-uid" {
		t.Fatalf("create: %+v %v", created, err)
	}
	raw, _ := json.Marshal(created)
	if strings.Contains(string(raw), "hidden-body-value") {
		t.Fatal("manifest leaked into receipt")
	}
	// A fresh service recovers the original receipt, without a second create or dry-run.
	recreated := testResourceCreation(direct, authorizer, allowRuntimePermission{}, domaincluster.ConnectionModeDirectKubeconfig)
	recreated.batches = store
	reader := &creationObservationStub{content: content}
	recreated.yaml = &GenericResources{resourceAccess: recreated.resourceAccess, direct: reader}
	got, err := recreated.FindCreate(ctx, principal, "cluster-a", request)
	if err != nil || got.OperationID != created.OperationID || got.Documents[0].Resource.UID != "original-uid" || direct.createCalls != 1 || direct.dryRunCalls != 1 {
		t.Fatalf("recovery: %+v %v", got, err)
	}
	for _, tc := range []struct {
		name, content string
		readErr       error
		verdict       string
	}{
		{"original", content, nil, "satisfied"},
		{"replacement", strings.ReplaceAll(content, "original-uid", "different-uid"), nil, "unsatisfied"},
		{"deleting", content + "  deletionTimestamp: '2026-09-16T00:00:00Z'\n", nil, "unsatisfied"},
		{"missing", "", apperrors.ErrNotFound, "inconclusive"},
		{"read revoked", "", apperrors.ErrAccessDenied, "inconclusive"},
		{"malformed", "not-an-object", nil, "inconclusive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader.content, reader.err = tc.content, tc.readErr
			assessment, err := recreated.AssessCreate(ctx, principal, "cluster-a", created.OperationID)
			if err != nil || string(assessment.Verdict) != tc.verdict {
				t.Fatalf("assessment: %+v %v", assessment, err)
			}
		})
	}
	store.batch.Documents[0].Resource.UID = ""
	reader.content, reader.err = content, nil
	assessment, err := recreated.AssessCreate(ctx, principal, "cluster-a", created.OperationID)
	if err != nil || assessment.Verdict != "inconclusive" {
		t.Fatalf("legacy UID was inferred from a later object: %+v %v", assessment, err)
	}
	if _, err := recreated.GetCreate(ctx, domainidentity.Principal{UserID: "other"}, "cluster-a", created.OperationID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("cross actor: %v", err)
	}
	if _, err := recreated.GetCreate(ctx, principal, "other-cluster", created.OperationID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("cross cluster: %v", err)
	}
	changed := request
	changed.Content = "different manifest"
	if _, err := recreated.FindCreate(ctx, principal, "cluster-a", changed); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("changed content: %v", err)
	}
	authorizer.denyNamespace = "minio"
	if _, err := recreated.FindCreate(ctx, principal, "cluster-a", request); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("revoked recovery: %v", err)
	}
	if _, err := recreated.ExecuteCreate(ctx, principal, "cluster-a", request); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("revoked direct replay: %v", err)
	}
	if direct.createCalls != 1 {
		t.Fatal("recovery or verification repeated a create")
	}
}

func TestResolveCreateTargetsIncludesEveryActualScope(t *testing.T) {
	for _, tc := range []struct{ apiVersion, group, plural, expected string }{
		{"apps/v1", "apps", "deployments", "workloads"},
		{"example.io/v1", "example.io", "deployments", "extensions"},
	} {
		t.Run(tc.apiVersion, func(t *testing.T) {
			manifest := testCreateManifest("Deployment", "second", true)
			manifest.Ref.APIVersion, manifest.Group, manifest.Resource = tc.apiVersion, tc.group, tc.plural
			direct := &creationDirectStub{manifests: []domainresource.ResolvedCreateManifest{testCreateManifest("Service", "first", true), manifest}}
			authorizer := &recordingCreateAuthorizer{allow: true}
			creation := testResourceCreation(direct, authorizer, allowRuntimePermission{}, domaincluster.ConnectionModeDirectKubeconfig)
			refs, err := creation.ResolveCreateTargets(context.Background(), domainidentity.Principal{UserID: "actor"}, "cluster-a", domainresource.ResourceCreateRequest{Source: domainresource.ResourceCreateSourceGlobal, Content: "manifest"})
			if err != nil || len(refs) != 2 || refs[0].Namespace != "first" || refs[1].Namespace != "second" {
				t.Fatalf("refs: %+v %v", refs, err)
			}
			if authorizer.requests[1].Resource.Group != tc.expected {
				t.Fatalf("wrong permission family: %+v", authorizer.requests[1])
			}
			if direct.dryRunCalls != 0 || direct.createCalls != 0 {
				t.Fatal("scope resolution invoked Kubernetes dry-run or create")
			}
		})
	}
}
