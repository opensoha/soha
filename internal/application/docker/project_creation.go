package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func projectCreationClaim(principal domainidentity.Principal, input domaindocker.ProjectInput) (operationClaim, error) {
	if principal.UserID == "" || strings.TrimSpace(input.IdempotencyKey) == "" || input.ID != "" || input.SourceRef != "" || (input.SourceKind != "" && input.SourceKind != "compose" && input.SourceKind != "single_container") {
		return operationClaim{}, fmt.Errorf("%w: idempotent project creation requires an actor, key and inline Compose without a caller ID or remote source", apperrors.ErrInvalidArgument)
	}
	key := input.IdempotencyKey
	input.IdempotencyKey = ""
	return operationClaimFor("docker.project.create", principal, key, input)
}

func (s *Service) FindProjectCreation(ctx context.Context, principal domainidentity.Principal, input domaindocker.ProjectInput) (domaindocker.Project, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermDockerProjectsManage, "create")); err != nil {
		return domaindocker.Project{}, err
	}
	claim, err := projectCreationClaim(principal, input)
	if err != nil {
		return domaindocker.Project{}, err
	}
	return s.repo.FindProjectCreation(ctx, claim.id, claim.inputHash)
}

func (s *Service) createProjectIdempotent(ctx context.Context, principal domainidentity.Principal, input domaindocker.ProjectInput) (domaindocker.Project, error) {
	previous, err := s.FindProjectCreation(ctx, principal, input)
	if err == nil {
		return previous, nil
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		return domaindocker.Project{}, err
	}
	if err := validateProjectInput(input); err != nil {
		return domaindocker.Project{}, err
	}
	if _, err := s.repo.GetHost(ctx, input.HostID); err != nil {
		return domaindocker.Project{}, err
	}
	claim, err := projectCreationClaim(principal, input)
	if err != nil {
		return domaindocker.Project{}, err
	}
	input.ID, input.SourceKind = claim.id, firstNonEmpty(input.SourceKind, "compose")
	input.Status, input.DesiredState = "draft", ""
	item, err := s.repo.CreateProjectIdempotent(ctx, input, claim.inputHash, composeServiceNames(input.ComposeContent))
	if err != nil {
		return item, err
	}
	s.recordOperation(ctx, principal, "docker.project.create", item.ID, item.Name, "success", "saved docker project creation receipt", nil)
	return item, nil
}
