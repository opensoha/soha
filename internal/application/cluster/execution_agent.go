package cluster

import (
	"context"
	"fmt"
	"strings"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// ValidateExecutionAgent authorizes administrative assignment of a registered
// node. Kubernetes summary health is not execution-channel readiness: dedicated
// Docker Agents can disable Kubernetes and report a degraded platform summary.
func (s *Service) ValidateExecutionAgent(ctx context.Context, principal domainidentity.Principal, id string) error {
	_, err := s.personalExecutionConnection(ctx, principal, id, domainaccess.ActionUpdate)
	return err
}

func (s *Service) PersonalExecutionConnection(ctx context.Context, principal domainidentity.Principal, id string) (domaincluster.Connection, error) {
	return s.personalExecutionConnection(ctx, principal, id, domainaccess.ActionView)
}

func (s *Service) ValidatePersonalExecutionAccess(ctx context.Context, principal domainidentity.Principal, id string) error {
	_, err := s.PersonalExecutionConnection(ctx, principal, id)
	return err
}

func (s *Service) personalExecutionConnection(ctx context.Context, principal domainidentity.Principal, id string, action domainaccess.Action) (domaincluster.Connection, error) {
	if s.repo == nil {
		return domaincluster.Connection{}, apperrors.ErrServiceUnavailable
	}
	connection, err := s.repo.GetConnection(ctx, strings.TrimSpace(id))
	if err != nil {
		return domaincluster.Connection{}, err
	}
	if err := s.authorize(ctx, principal, connection.Summary, action); err != nil {
		return domaincluster.Connection{}, err
	}
	if connection.Summary.ConnectionMode != domaincluster.ConnectionModeAgent {
		return domaincluster.Connection{}, fmt.Errorf("%w: select a registered Agent-mode execution node", apperrors.ErrInvalidArgument)
	}
	token, _ := connection.Metadata["token"].(string)
	if strings.TrimSpace(token) == "" {
		return domaincluster.Connection{}, fmt.Errorf("%w: authenticated Agent connection required", apperrors.ErrServiceUnavailable)
	}
	return connection, nil
}
