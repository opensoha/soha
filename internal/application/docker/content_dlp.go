package docker

import (
	"context"
	"encoding/base64"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	appdlp "github.com/opensoha/soha/internal/application/contentdlp"
	domaindlp "github.com/opensoha/soha/internal/domain/contentdlp"
	domain "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ContentDLP interface {
	Prepare(context.Context, identity.Principal, string) (appdlp.Snapshot, error)
	Check(context.Context, identity.Principal, appdlp.Snapshot, domain.ProjectVolumeFileContent, ...domaindlp.TransferAccess) (*domaindlp.Decision, error)
}

func WithContentDLP(guard ContentDLP) Option { return func(s *Service) { s.contentDLP = guard } }

func cleanVolumePath(value string) (string, error) {
	if len(value) == 0 || len(value) > 4096 || strings.ContainsAny(value, "\\\x00\r\n") || !strings.HasPrefix(value, "/") {
		return "", apperrors.ErrInvalidArgument
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return "", apperrors.ErrInvalidArgument
		}
	}
	return path.Clean(value), nil
}

func (s *Service) readVolumeContent(ctx context.Context, p identity.Principal, target dockerRuntimeTarget, input domain.ProjectVolumeFileReadInput) (domain.ProjectVolumeFileContent, error) {
	var zero domain.ProjectVolumeFileContent
	if err := validateVolumeAuthorization(input.Authorization); err != nil {
		return zero, err
	}

	if s.contentDLP == nil {
		return zero, apperrors.ErrServiceUnavailable
	}
	select {
	case s.volumeReads <- struct{}{}:
		defer func() { <-s.volumeReads }()
	default:
		return zero, apperrors.ErrServiceUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cleanTarget, err := cleanVolumePath(input.Target)
	if err != nil {
		return zero, err
	}
	cleanPath, err := cleanVolumePath(input.Path)
	if err != nil {
		return zero, err
	}
	snapshot, err := s.contentDLP.Prepare(ctx, p, target.Project.ID)
	if err != nil {
		return zero, err
	}
	if snapshot.Project.HostID != target.Project.HostID || !snapshot.Project.UpdatedAt.Equal(target.Project.UpdatedAt) {
		return zero, apperrors.ErrConflict
	}
	req := target.runtimeRequest()
	req.Target = cleanTarget
	req.Path = cleanPath
	req.LimitBytes = normalizeVolumeReadLimit(input.LimitBytes)
	if snapshot.Policy.Published != nil && snapshot.Policy.Published.Enabled {
		req.LimitBytes = domaindlp.MaxTextBytes
	}
	file, err := postDockerRuntime[domain.ProjectVolumeFileContent](ctx, target.Endpoint, target.BearerToken, "/docker/runtime/volume-file", req)
	if err != nil {
		return zero, err
	}
	file.DLP = nil // Only Core can issue a decision, never trust runtime-supplied metadata.
	if file.ProjectID != target.Project.ID || file.ServiceName != target.ServiceName || file.Target != cleanTarget || file.Path != cleanPath {
		return zero, apperrors.ErrConflict
	}
	decision, err := s.contentDLP.Check(ctx, p, snapshot, file, domaindlp.TransferAccess{Token: input.Authorization, SessionID: input.SessionID})
	if err != nil {
		return zero, err
	}
	file.DLP = decision
	limit := normalizeVolumeReadLimit(input.LimitBytes)
	if int64(len(file.Content)) > limit {
		for limit > 0 && !utf8.RuneStart(file.Content[limit]) {
			limit--
		}
		file.Content = file.Content[:limit]
		file.Truncated = true
	}
	return file, nil
}

func validateVolumeAuthorization(token string) error {
	if token == "" {
		return nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 || len(token) != 43 {
		return apperrors.ErrInvalidArgument
	}
	return nil
}
