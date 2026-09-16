package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestDeliverySourceCommitPinsOnlyExplicitMatchesWithoutMutatingWorkflow(t *testing.T) {
	definition := domainworkflow.DeliveryWorkflowDefinition{Targets: []domainworkflow.DeliveryTargetInput{
		{ID: "build", Action: "build", RepositoryRefs: []domainbuild.RepositoryRef{{RepositoryID: "repo", RefType: "branch", RefName: "main"}, {RepositoryID: "other", RefType: "tag", RefName: "v1"}}},
		{ID: "release", Action: "build_deploy", RepositoryRefs: []domainbuild.RepositoryRef{{RepositoryID: "repo", RefType: "branch", RefName: "main"}}},
	}}
	input := domainworkflow.DeliveryBatchInput{WorkflowID: "workflow", WorkflowVersion: 3, SourceCommit: &sohaapi.DeliverySourceCommit{RepositoryID: "repo", RefType: "branch", RefName: "main", Commit: strings.Repeat("a", 40)}}
	pinned, err := pinDeliverySourceCommit(definition, input)
	if err != nil {
		t.Fatal(err)
	}
	for i := range definition.Targets {
		if definition.Targets[i].RepositoryRefs[0].RefName != "main" || pinned.Targets[i].RepositoryRefs[0].RefType != "commit" || pinned.Targets[i].RepositoryRefs[0].RefName != input.SourceCommit.Commit {
			t.Fatal("saved workflow changed or matching target was not pinned")
		}
	}
	if pinned.Targets[0].RepositoryRefs[1] != definition.Targets[0].RepositoryRefs[1] {
		t.Fatal("unrelated repository changed")
	}
	for _, invalid := range []sohaapi.DeliverySourceCommit{
		{RepositoryID: "missing", RefType: "branch", RefName: "main", Commit: input.SourceCommit.Commit},
		{RepositoryID: "repo", RefType: "tag", RefName: "main", Commit: input.SourceCommit.Commit},
		{RepositoryID: "repo", RefType: "branch", RefName: "main", Commit: "main"},
	} {
		input.SourceCommit = &invalid
		if _, err := pinDeliverySourceCommit(definition, input); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("invalid event mapping accepted: %+v %v", invalid, err)
		}
	}
}
