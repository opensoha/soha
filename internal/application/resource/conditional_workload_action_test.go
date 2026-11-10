package resource

import (
	"context"
	"errors"
	"testing"

	contractresource "github.com/opensoha/soha-contracts/resource"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type conditionalActionDirectStub struct {
	resourceUpdatePlanDirectStub
	calls int
	input contractresource.ConditionalWorkloadActionInput
}

func (s *conditionalActionDirectStub) ExecuteWorkloadActionConditional(_ context.Context, _, namespace, kind, name, action string, input contractresource.ConditionalWorkloadActionInput) (contractresource.ConditionalWorkloadActionReceipt, error) {
	s.calls++
	s.input = input
	return contractresource.ConditionalWorkloadActionReceipt{Applied: !input.DryRun, DryRun: input.DryRun, Kind: kind, Name: name, Namespace: namespace, Action: action, UID: input.Preconditions.ExpectedUID, ResourceVersion: "next", Preconditions: input.Preconditions}, nil
}

func TestConditionalWorkloadActionsUseExactActionPermission(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		for _, action := range []string{"restart", "scale", "rollback"} {
			t.Run(action+"/"+map[bool]string{true: "dry-run", false: "execute"}[dryRun], func(t *testing.T) {
				input := contractresource.ConditionalWorkloadActionInput{Preconditions: contractresource.ResourceMutationPreconditions{ExpectedUID: "reviewed", ExpectedResourceVersion: "rv"}, DryRun: dryRun}
				switch action {
				case "scale":
					count := int32(0)
					input.Replicas = &count
				case "rollback":
					input.Revision = "2"
				}
				stub := &conditionalActionDirectStub{}
				// YAML update does not authorize an independent workload action.
				service := conditionalService(stub, exactPermissionAuthorizer{resourcePermissionKey("workloads", "Deployment", domainaccess.ActionUpdate): true})
				if _, err := service.ExecuteWorkloadActionConditional(t.Context(), domainidentity.Principal{}, "cluster-a", "team-a", "Deployment", "app", action, input); !errors.Is(err, apperrors.ErrAccessDenied) || stub.calls != 0 {
					t.Fatalf("permission bypass: %v calls=%d", err, stub.calls)
				}
				permissions := exactPermissionAuthorizer{resourcePermissionKey("workloads", "Deployment", domainaccess.Action(action)): true}
				service = conditionalService(stub, permissions)
				item, err := service.ExecuteWorkloadActionConditional(t.Context(), domainidentity.Principal{}, "cluster-a", "team-a", "Deployment", "app", action, input)
				if err != nil || stub.calls != 1 || stub.input.Preconditions != input.Preconditions || item.DryRun != dryRun || item.Applied == dryRun {
					t.Fatalf("item=%+v err=%v calls=%d", item, err, stub.calls)
				}
				service = conditionalService(&resourceUpdatePlanDirectStub{}, permissions)
				if _, err := service.ExecuteWorkloadActionConditional(t.Context(), domainidentity.Principal{}, "cluster-a", "team-a", "Deployment", "app", action, input); !errors.Is(err, apperrors.ErrUnsupportedOperation) {
					t.Fatalf("legacy transport fallback: %v", err)
				}
			})
		}
	}
}
