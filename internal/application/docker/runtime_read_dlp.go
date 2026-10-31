package docker

import (
	"context"
	"path"
	"unicode/utf8"

	protocol "github.com/opensoha/soha-contracts/docker/runtime"
	appdlp "github.com/opensoha/soha/internal/application/contentdlp"
	domaindlp "github.com/opensoha/soha/internal/domain/contentdlp"
	domain "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) prepareRuntimeFileDLP(ctx context.Context, principal identity.Principal, target boundReadTarget, request *protocol.ReadRequest) (*appdlp.Snapshot, error) {
	if request.Action != "files.read" {
		return nil, nil
	}
	if s.contentDLP == nil {
		return nil, apperrors.ErrServiceUnavailable
	}
	snapshot, err := s.contentDLP.Prepare(ctx, principal, target.project.ID)
	if err != nil {
		return nil, err
	}
	if snapshot.Project.ID != target.project.ID || snapshot.Project.HostID != target.host.ID ||
		snapshot.Project.ResourceUID != target.project.ResourceUID || snapshot.Project.ConfigurationRevision != target.project.ConfigurationRevision ||
		!snapshot.Project.UpdatedAt.Equal(target.project.UpdatedAt) {
		return nil, apperrors.ErrConflict
	}
	if snapshot.Policy.Published != nil && snapshot.Policy.Published.Enabled {
		// Inspect the policy's complete bounded window, even when the client
		// requested a smaller prefix. Truncate only after the Core decision.
		request.LimitBytes = domaindlp.MaxTextBytes
	}
	return &snapshot, nil
}

func (s *Service) checkRuntimeFileDLP(ctx context.Context, principal identity.Principal, snapshot *appdlp.Snapshot, result *protocol.ReadResult, limit int64) error {
	if snapshot == nil {
		return nil
	}
	file := domain.ProjectVolumeFileContent{
		ProjectID: result.ProjectID, ServiceName: result.Container.ServiceName,
		Target: result.Target, Path: path.Join("/", result.Path),
		Content: result.Content, SizeBytes: result.SizeBytes, Truncated: result.Truncated,
	}
	// This endpoint carries no transfer authorization. Warn/block decisions
	// must use the established DLP workflow, never an implicit approval here.
	decision, err := s.contentDLP.Check(ctx, principal, *snapshot, file)
	if err != nil {
		return err
	}
	if decision != nil && !decision.Allowed {
		return &appdlp.Denial{Decision: *decision}
	}
	if snapshot.Policy.Published != nil && snapshot.Policy.Published.Enabled && decision == nil {
		return apperrors.ErrServiceUnavailable
	}
	if int64(len(result.Content)) > limit {
		for limit > 0 && !utf8.RuneStart(result.Content[limit]) {
			limit--
		}
		result.Content = result.Content[:limit]
		result.ReturnedBytes = int64(len(result.Content))
		result.Truncated = result.SizeBytes > result.ReturnedBytes
		result.Complete = !result.Truncated
	}
	return nil
}
