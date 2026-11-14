package docker

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

func (s *Service) GetHostIdentity(ctx context.Context, principal domainidentity.Principal, id string) (_ sohaapi.DockerResourceIdentity, retErr error) {
	defer func() { s.recordIdentityRead(ctx, principal, "DockerHost", "docker.host.identity.read", id, retErr) }()
	if err := s.authorize(ctx, principal, appaccess.PermDockerHostsView); err != nil {
		return sohaapi.DockerResourceIdentity{}, err
	}
	// This read must not reconcile provisioning tasks or contact a provider.
	item, err := s.repo.GetHost(ctx, id)
	if err != nil {
		return sohaapi.DockerResourceIdentity{}, err
	}
	if err := domaindocker.CheckScope(ctx, map[string]string{"hostId": item.ID}); err != nil {
		return sohaapi.DockerResourceIdentity{}, err
	}
	return resourceIdentity("docker.host", item.ID, item.ResourceUID, item.ConfigurationRevision, "")
}

func (s *Service) GetProjectIdentity(ctx context.Context, principal domainidentity.Principal, id string) (_ sohaapi.DockerResourceIdentity, retErr error) {
	defer func() {
		s.recordIdentityRead(ctx, principal, "DockerProject", "docker.project.identity.read", id, retErr)
	}()
	if err := s.authorize(ctx, principal, appaccess.PermDockerProjectsView); err != nil {
		return sohaapi.DockerResourceIdentity{}, err
	}
	item, err := s.repo.GetProject(ctx, id)
	if err != nil {
		return sohaapi.DockerResourceIdentity{}, err
	}
	if err := checkProjectScope(ctx, item); err != nil {
		return sohaapi.DockerResourceIdentity{}, err
	}
	return resourceIdentity("docker.project", item.ID, item.ResourceUID, item.ConfigurationRevision, item.HostID)
}

func (s *Service) recordIdentityRead(ctx context.Context, principal domainidentity.Principal, kind, action, id string, readErr error) {
	if s.audit == nil {
		return
	}
	result, summary := "success", "read Docker resource identity"
	if readErr != nil {
		result, summary = "failure", "Docker resource identity read failed"
	}
	meta := requestctx.FromContext(ctx)
	_ = s.audit.Record(ctx, domainaudit.Entry{
		ActorID: principal.UserID, ActorName: principal.UserName, Roles: principal.Roles, Teams: principal.Teams,
		ResourceKind: kind, ResourceName: id, Action: action, Result: result, Summary: summary,
		RequestPath: meta.Path, RequestMethod: meta.Method, RequestID: meta.RequestID, SourceIP: meta.SourceIP,
		Metadata: map[string]any{"source": meta.Source},
	})
}

func resourceIdentity(kind, id, uidText, revisionText, hostID string) (sohaapi.DockerResourceIdentity, error) {
	uid, uidErr := uuid.Parse(uidText)
	revision, revisionErr := uuid.Parse(revisionText)
	if uidErr != nil || revisionErr != nil || uid == uuid.Nil || revision == uuid.Nil || id == "" || kind == "docker.project" && hostID == "" {
		return sohaapi.DockerResourceIdentity{}, fmt.Errorf("%w: Docker configuration identity is unavailable", apperrors.ErrUnsupportedOperation)
	}
	return sohaapi.DockerResourceIdentity{ResourceKind: sohaapi.DockerResourceIdentityResourceKind(kind), ID: id, UID: uid, ConfigurationRevision: revision, HostID: hostID}, nil
}
