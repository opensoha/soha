package helmrelease

import (
	"errors"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

// ValidateCompletedResult binds completion to the frozen plan and native revision.
func ValidateCompletedResult(payload sohaapi.HelmExecutionTaskPayload, result sohaapi.HelmExecutionTaskResult) error {
	if !result.Stopped || result.RenderedDigest != payload.Snapshot.RenderedDigest || !helmSHA256.MatchString(result.RenderedDigest) {
		return errors.New("helm completion must confirm executor stop and the approved render")
	}
	if payload.Action == sohaapi.Preflight {
		if !result.Ready || result.Status != "preflighted" || result.Revision != payload.Snapshot.ExpectedRevision {
			return errors.New("helm preflight must match the expected native revision")
		}
		return nil
	}
	if payload.Action != sohaapi.Apply && payload.Action != sohaapi.Observe || result.Status != "deployed" || result.Revision != payload.Snapshot.ExpectedRevision+1 {
		return errors.New("helm completion must match the deployed native revision")
	}
	if payload.Action == sohaapi.Observe && !result.Ready {
		return errors.New("helm health observation is not ready")
	}
	return nil
}
