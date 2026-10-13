package virtualization

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var physicalSourcePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func connectionCapacitySource(connection domain.Connection) string {
	return domain.CapacitySourceID(domain.AdapterConnection{Provider: connection.Provider, Endpoint: connection.Endpoint, ClusterID: connection.KubernetesClusterID, Options: connection.Config})
}

func (s *Service) capacityPolicy(ctx context.Context, connection domain.Connection) (domain.CapacityPolicy, error) {
	source := connectionCapacitySource(connection)
	if s.capacityPolicies == nil || connection.Provider != ProviderPVE {
		return domain.DefaultCapacityPolicy(source), nil
	}
	return s.capacityPolicies.GetCapacityPolicy(ctx, connection.ID, source)
}

func policyDTO(policy domain.CapacityPolicy) sohaapi.VirtualizationCapacityPolicy {
	var result sohaapi.VirtualizationCapacityPolicy
	encoded, _ := json.Marshal(policy)
	_ = json.Unmarshal(encoded, &result)
	return result
}

func (s *Service) GetCapacityPolicy(ctx context.Context, principal domainidentity.Principal, id string) (sohaapi.VirtualizationCapacityPolicy, error) {
	if err := s.authorize(ctx, principal, appaccess.PermVirtualizationClustersView); err != nil {
		return sohaapi.VirtualizationCapacityPolicy{}, err
	}
	connection, err := s.connections.GetConnection(ctx, strings.TrimSpace(id))
	if err != nil {
		return sohaapi.VirtualizationCapacityPolicy{}, mapNotFound(err)
	}
	if err := domain.CheckScope(ctx, connectionCapabilityScope(connection, "")); err != nil {
		return sohaapi.VirtualizationCapacityPolicy{}, err
	}
	policy, err := s.capacityPolicy(ctx, connection)
	return policyDTO(policy), err
}

func (s *Service) SaveCapacityPolicy(ctx context.Context, principal domainidentity.Principal, id string, input sohaapi.VirtualizationCapacityPolicyInput) (sohaapi.VirtualizationCapacityPolicy, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermVirtualizationClustersManage, "update")); err != nil {
		return sohaapi.VirtualizationCapacityPolicy{}, err
	}
	connection, err := s.connections.GetConnection(ctx, strings.TrimSpace(id))
	if err != nil {
		return sohaapi.VirtualizationCapacityPolicy{}, mapNotFound(err)
	}
	if err := domain.CheckScope(ctx, connectionCapabilityScope(connection, "")); err != nil {
		return sohaapi.VirtualizationCapacityPolicy{}, err
	}
	if connection.Provider != ProviderPVE || s.capacityPolicies == nil {
		return sohaapi.VirtualizationCapacityPolicy{}, apperrors.ErrUnsupportedOperation
	}
	previous, err := s.capacityPolicy(ctx, connection)
	if err != nil {
		return sohaapi.VirtualizationCapacityPolicy{}, err
	}
	source := previous.SourceID
	if previous.Revision == 0 || !previous.SourceConfirmed {
		if !physicalSourcePattern.MatchString(input.PhysicalSourceID) {
			return sohaapi.VirtualizationCapacityPolicy{}, fmt.Errorf("%w: confirm the physical PVE source identity and join all aliases before activation", apperrors.ErrInvalidArgument)
		}
		source = "pve/" + input.PhysicalSourceID
	} else if input.PhysicalSourceID != "" && source != "pve/"+input.PhysicalSourceID {
		return sohaapi.VirtualizationCapacityPolicy{}, fmt.Errorf("%w: physical source identity is immutable", apperrors.ErrConflict)
	}
	policy := domain.CapacityPolicy{SourceConfirmed: true, SourceID: source, Mode: string(input.Mode), CPU: domain.OvercommitPolicy{Enabled: input.CPU.Enabled, Percent: input.CPU.Percent}, Memory: domain.OvercommitPolicy{Enabled: input.Memory.Enabled, Percent: input.Memory.Percent}, UpdatedBy: principal.UserID}
	if input.ExpectedRevision < 0 {
		return sohaapi.VirtualizationCapacityPolicy{}, apperrors.ErrInvalidArgument
	}
	if err := policy.Validate(); err != nil {
		return sohaapi.VirtualizationCapacityPolicy{}, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	saved, err := s.capacityPolicies.SaveCapacityPolicy(ctx, connection.ID, previous.SourceID, policy, input.ExpectedRevision)
	if err == nil {
		s.recordOperation(ctx, principal, "virtualization.capacity.policy.update", connection.ID, connection.Name, "success", "updated physical source capacity policy", map[string]any{"sourceId": saved.SourceID, "revision": saved.Revision, "mode": saved.Mode, "cpuPercent": saved.CPU.Percent, "memoryPercent": saved.Memory.Percent})
	}
	return policyDTO(saved), err
}
