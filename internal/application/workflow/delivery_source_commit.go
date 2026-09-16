package workflow

import (
	"fmt"
	"regexp"
	"slices"

	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var deliverySourceCommitPattern = regexp.MustCompile(`^(?:[a-f0-9]{40}|[a-f0-9]{64})$`)

func pinDeliverySourceCommit(definition domainworkflow.DeliveryWorkflowDefinition, input domainworkflow.DeliveryBatchInput) (domainworkflow.DeliveryWorkflowDefinition, error) {
	commit := input.SourceCommit
	if commit == nil {
		return definition, nil
	}
	if input.WorkflowID == "" || commit.RepositoryID == "" || commit.RefName == "" || commit.RefType != "branch" && commit.RefType != "tag" || !deliverySourceCommitPattern.MatchString(commit.Commit) {
		return definition, fmt.Errorf("%w: source commit requires a versioned workflow and an explicit repository ref", apperrors.ErrInvalidArgument)
	}
	definition.Targets = slices.Clone(definition.Targets)
	matched := false
	for i, target := range definition.Targets {
		target.RepositoryRefs = slices.Clone(target.RepositoryRefs)
		for j, ref := range target.RepositoryRefs {
			if ref.RepositoryID == commit.RepositoryID && ref.RefType == string(commit.RefType) && ref.RefName == commit.RefName {
				if target.Action != "build" && target.Action != "build_deploy" {
					return definition, apperrors.ErrInvalidArgument
				}
				target.RepositoryRefs[j].RefType, target.RepositoryRefs[j].RefName = "commit", commit.Commit
				matched = true
			}
		}
		definition.Targets[i] = target
	}
	if !matched {
		return definition, fmt.Errorf("%w: event repository ref is not explicitly used by the workflow", apperrors.ErrInvalidArgument)
	}
	return definition, nil
}
