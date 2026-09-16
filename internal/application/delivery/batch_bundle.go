package delivery

import (
	"context"
	"fmt"
	"maps"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// A shared build keeps its original environment and provenance. Each other
// environment receives a bundle derived by the active plan node, before approval.
func (s *Service) batchTargetBundle(ctx context.Context, principal domainidentity.Principal, target domainworkflow.DeliveryTargetInput, bundleID string) (string, error) {
	bundle, err := s.GetReleaseBundle(ctx, principal, bundleID)
	if err != nil {
		return "", err
	}
	if _, err := s.manifestArtifacts(ctx, principal, target.ApplicationID, target.ServiceID, bundleID); err != nil {
		return "", err
	}
	if bundle.ApplicationEnvironmentID == target.ApplicationEnvironmentID {
		return bundle.ID, nil
	}
	node, ok := domainworkflow.NodeExecutionFrom(ctx)
	if !ok || node.Stage != "plan" || node.TargetID != target.ID {
		return "", fmt.Errorf("%w: bundle promotion requires its active delivery plan node", apperrors.ErrConflict)
	}
	build, err := s.bundleBuildTask(ctx, principal, bundle)
	if err != nil {
		return "", err
	}
	bundle.Metadata = maps.Clone(bundle.Metadata)
	if bundle.Metadata == nil {
		bundle.Metadata = map[string]any{}
	}
	// Flatten successive promotions to the actual completed build, never another
	// promotion or a caller-provided digest.
	bundle.Metadata["sourceReleaseBundleId"], bundle.Metadata["sourceBuildTaskId"] = build.ReleaseBundleID, build.ID
	bundle.ID, bundle.ApplicationEnvironmentID = node.ResourceID("bundle"), target.ApplicationEnvironmentID
	bundle.SourceType, bundle.Artifacts = "promotion", nil
	bundle.CreatedAt, bundle.UpdatedAt = time.Now().UTC(), time.Now().UTC()
	promoted, err := s.repository.CreateReleaseBundle(ctx, bundle)
	if err != nil {
		return "", err
	}
	if promoted.ApplicationID != target.ApplicationID || promoted.ApplicationEnvironmentID != target.ApplicationEnvironmentID || promoted.ArtifactRef != bundle.ArtifactRef || promoted.ArtifactDigest != bundle.ArtifactDigest || promoted.Metadata["sourceReleaseBundleId"] != build.ReleaseBundleID || promoted.Metadata["sourceBuildTaskId"] != build.ID {
		return "", fmt.Errorf("%w: recovered promotion does not match the frozen build", apperrors.ErrConflict)
	}
	return promoted.ID, nil
}
