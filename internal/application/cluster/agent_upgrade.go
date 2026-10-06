package cluster

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var agentStableVersion = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type agentUpgradeClient interface {
	GetAgentUpgradeStatus(context.Context) (domaincluster.AgentUpgradeStatus, error)
	UpgradeAgent(context.Context, string) (string, error)
}

func (s *Service) agentUpgradeClient(ctx context.Context, principal domainidentity.Principal, clusterID string, action domainaccess.Action) (agentUpgradeClient, error) {
	if s.repo == nil || s.agents == nil {
		return nil, fmt.Errorf("%w: Agent connection is unavailable", apperrors.ErrServiceUnavailable)
	}
	connection, err := s.repo.GetConnection(ctx, strings.TrimSpace(clusterID))
	if err != nil {
		return nil, err
	}
	if err := s.authorize(ctx, principal, connection.Summary, action); err != nil {
		return nil, err
	}
	if connection.Summary.ConnectionMode != domaincluster.ConnectionModeAgent {
		return nil, fmt.Errorf("%w: cluster is not configured for Agent mode", apperrors.ErrConflict)
	}
	client, err := s.agents(connection)
	if err != nil {
		return nil, err
	}
	upgrade, ok := client.(agentUpgradeClient)
	if !ok {
		return nil, fmt.Errorf("%w: Agent upgrade is unavailable", apperrors.ErrServiceUnavailable)
	}
	return upgrade, nil
}

func (s *Service) GetAgentUpgradeStatus(ctx context.Context, principal domainidentity.Principal, clusterID string) (domaincluster.AgentUpgradeStatus, error) {
	client, err := s.agentUpgradeClient(ctx, principal, clusterID, domainaccess.ActionView)
	if err != nil {
		return domaincluster.AgentUpgradeStatus{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	status, err := client.GetAgentUpgradeStatus(ctx)
	status.RecommendedVersion = agentVersion
	return status, err
}

func (s *Service) UpgradeAgent(ctx context.Context, principal domainidentity.Principal, clusterID string, input domaincluster.AgentUpgradeInput) (domaincluster.AgentUpgradeResult, error) {
	client, err := s.agentUpgradeClient(ctx, principal, clusterID, domainaccess.ActionUpdate)
	if err != nil {
		return domaincluster.AgentUpgradeResult{}, err
	}
	if len(input.Version) > 64 || !agentStableVersion.MatchString(input.Version) {
		return domaincluster.AgentUpgradeResult{}, fmt.Errorf("%w: specify a released stable version such as v0.1.7", apperrors.ErrInvalidArgument)
	}
	image := agentImageRepository + ":v" + strings.TrimPrefix(input.Version, "v")
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	// Audit the request before changing the Deployment. Kubernetes owns the rollout;
	// accepting the image update does not mean the new Agent is already connected.
	if err := s.recordAudit(ctx, principal, clusterID, "Cluster", clusterID, string(domainaccess.ActionUpdate), "requested", "update Agent image to "+image); err != nil {
		return domaincluster.AgentUpgradeResult{}, fmt.Errorf("record Agent upgrade audit: %w", err)
	}
	previous, err := client.UpgradeAgent(ctx, image)
	if err != nil {
		return domaincluster.AgentUpgradeResult{}, err
	}
	s.recordOperation(ctx, principal, "platform.cluster.agent.upgrade", clusterID, clusterID, "accepted Agent image update to "+image)
	return domaincluster.AgentUpgradeResult{PreviousImage: previous, TargetImage: image}, nil
}
