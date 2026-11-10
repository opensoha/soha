package resource

import (
	"context"
	"errors"
	"testing"

	contractresource "github.com/opensoha/soha-contracts/resource"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type conditionalDirectStub struct {
	resourceUpdatePlanDirectStub
	reads, writes int
	expected      contractresource.ResourceMutationPreconditions
}

func (s *conditionalDirectStub) GetResourceIdentity(context.Context, string, string, string, string) (contractresource.ResourceIdentity, error) {
	s.reads++
	return contractresource.ResourceIdentity{UID: "uid", ResourceVersion: "rv"}, nil
}
func (s *conditionalDirectStub) ApplyResourceYAMLConditional(_ context.Context, _, namespace, kind, name, _ string, expected contractresource.ResourceMutationPreconditions) (domainresource.ResourceYAMLView, error) {
	s.writes++
	s.expected = expected
	return domainresource.ResourceYAMLView{Kind: kind, Name: name, Namespace: namespace}, nil
}
func (s *conditionalDirectStub) DryRunResourceYAMLConditional(_ context.Context, _, _, _, _, _ string, expected contractresource.ResourceMutationPreconditions) (domainresource.ResourceUpdateAnalysis, error) {
	s.reads++
	s.expected = expected
	return domainresource.ResourceUpdateAnalysis{}, nil
}
func (s *conditionalDirectStub) DeleteResourceConditional(_ context.Context, _, _, _, _ string, expected contractresource.ResourceMutationPreconditions) error {
	s.writes++
	s.expected = expected
	return nil
}
func conditionalService(direct DirectGenericResource, permissions exactPermissionAuthorizer) *GenericResources {
	return &GenericResources{resourceAccess: &resourceAccess{resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}}}, authorizer: permissions, audit: &workloadAuditRecorder{}, operations: &workloadOperationRecorder{}}, direct: direct}
}

func TestConditionalResourcesAuthorizeBeforeTransportAndDoNotFallback(t *testing.T) {
	guard := contractresource.ResourceMutationPreconditions{ExpectedUID: "uid", ExpectedResourceVersion: "rv"}
	stub := &conditionalDirectStub{}
	service := conditionalService(stub, exactPermissionAuthorizer{})
	if _, err := service.DeleteResourceConditional(t.Context(), domainidentity.Principal{}, "cluster-a", "team-a", "ConfigMap", "app", guard); !errors.Is(err, apperrors.ErrAccessDenied) || stub.writes != 0 {
		t.Fatalf("error=%v writes=%d", err, stub.writes)
	}
	service = conditionalService(&resourceUpdatePlanDirectStub{}, exactPermissionAuthorizer{resourcePermissionKey("configuration", "ConfigMap", domainaccess.ActionDelete): true})
	if _, err := service.DeleteResourceConditional(t.Context(), domainidentity.Principal{}, "cluster-a", "team-a", "ConfigMap", "app", guard); !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatalf("legacy adapter accepted guard: %v", err)
	}
}

func TestConditionalDeleteReturnsAcceptanceAndPreservesGuard(t *testing.T) {
	stub := &conditionalDirectStub{}
	service := conditionalService(stub, exactPermissionAuthorizer{resourcePermissionKey("configuration", "ConfigMap", domainaccess.ActionDelete): true})
	guard := contractresource.ResourceMutationPreconditions{ExpectedUID: "uid", ExpectedResourceVersion: "rv"}
	receipt, err := service.DeleteResourceConditional(t.Context(), domainidentity.Principal{}, "cluster-a", "team-a", "ConfigMap", "app", guard)
	if err != nil || !receipt.Accepted || receipt.State != "deletion_requested" || stub.expected != guard || stub.writes != 1 {
		t.Fatalf("receipt=%+v err=%v stub=%+v", receipt, err, stub)
	}
}

func TestSecretIdentityUsesMetadataPermissionWithoutDataPermission(t *testing.T) {
	stub := &conditionalDirectStub{}
	service := conditionalService(stub, exactPermissionAuthorizer{resourcePermissionKey("configuration", "Secret", domainaccess.ActionView): true})
	if _, err := service.GetResourceIdentity(t.Context(), domainidentity.Principal{}, "cluster-a", "team-a", "Secret", "app"); err != nil || stub.reads != 1 {
		t.Fatalf("error=%v reads=%d", err, stub.reads)
	}
	if _, err := service.GetResourceYAML(t.Context(), domainidentity.Principal{}, "cluster-a", "team-a", "Secret", "app"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("Secret data permission bypassed: %v", err)
	}
}
