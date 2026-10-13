package aiproduction

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ExecutionAgentValidator func(context.Context, domainidentity.Principal, string) error

func validatePersonalDockerPolicy(item EnvironmentTemplate) error {
	if item.PersonalDocker != nil && item.PersonalVM != nil {
		return apperrors.ErrInvalidArgument
	}
	p := item.PersonalDocker
	if p == nil {
		return nil
	}
	if item.Backend != "container" || len(item.ID) > 128 || p.AgentClusterID == uuid.Nil || p.LeaseSeconds < 60 || p.LeaseSeconds > 3600 || len(item.ResourcePolicy) > 0 || len(item.NetworkPolicy) > 0 {
		return fmt.Errorf("%w: invalid personal Docker policy", apperrors.ErrInvalidArgument)
	}
	if err := PersonalDockerQuota(*p).ValidatePersonalDocker(); err != nil {
		return fmt.Errorf("%w: unsupported personal Docker quota", apperrors.ErrInvalidArgument)
	}
	return nil
}

func PersonalDockerQuota(p sohaapi.PersonalDockerEnvironmentPolicy) contractenv.Quota {
	q := p.Quota
	return contractenv.Quota{DiskBytes: q.DiskBytes, MemoryBytes: q.MemoryBytes, CPUUnits: q.CPUUnits, MaxProcesses: q.MaxProcesses, NetworkEgress: q.NetworkEgress}
}

// Personal users select an administrator-created template, never a node URL or
// quota. Their current node scope is checked separately before every dispatch.
func (s *Service) ResolvePersonalEnvironmentPolicy(ctx context.Context, id string) (domaincopilot.PersonalEnvironmentPolicy, error) {
	items, err := s.store.ListEnvironmentTemplates(ctx)
	if err != nil {
		return domaincopilot.PersonalEnvironmentPolicy{}, err
	}
	for _, item := range items {
		if item.ID != id {
			continue
		}
		return personalEnvironmentPolicy(item)
	}
	return domaincopilot.PersonalEnvironmentPolicy{}, apperrors.ErrNotFound
}

func (s *Service) ListPersonalEnvironmentPolicies(ctx context.Context) ([]domaincopilot.PersonalEnvironmentPolicy, error) {
	items, err := s.store.ListEnvironmentTemplates(ctx)
	if err != nil {
		return nil, err
	}
	policies := make([]domaincopilot.PersonalEnvironmentPolicy, 0)
	for _, item := range items {
		if p, err := personalEnvironmentPolicy(item); err == nil {
			policies = append(policies, p)
		}
	}
	return policies, nil
}

func personalEnvironmentPolicy(item EnvironmentTemplate) (domaincopilot.PersonalEnvironmentPolicy, error) {
	if item.Status != "active" || (item.PersonalDocker == nil && item.PersonalVM == nil) {
		return domaincopilot.PersonalEnvironmentPolicy{}, apperrors.ErrServiceUnavailable
	}
	if err := validatePersonalDockerPolicy(item); err != nil {
		return domaincopilot.PersonalEnvironmentPolicy{}, err
	}
	if err := validatePersonalVMPolicy(item); err != nil {
		return domaincopilot.PersonalEnvironmentPolicy{}, err
	}
	if strings.TrimSpace(item.ID) == "" || item.ID != strings.TrimSpace(item.ID) || !utf8.ValidString(item.Name) || strings.TrimSpace(item.Name) == "" || utf8.RuneCountInString(item.Name) > 200 {
		return domaincopilot.PersonalEnvironmentPolicy{}, apperrors.ErrInvalidArgument
	}
	mode := "observe-only"
	if item.IsolationMode == "disposable-write" {
		mode = "restricted-write"
	} else if item.IsolationMode != "read-only" {
		return domaincopilot.PersonalEnvironmentPolicy{}, apperrors.ErrInvalidArgument
	}
	policy := domaincopilot.PersonalEnvironmentPolicy{TemplateID: item.ID, Name: item.Name, Mode: mode}
	if p := item.PersonalDocker; p != nil {
		policy.Backend, policy.Provider = "container", "docker"
		policy.AgentClusterID, policy.LeaseSeconds, policy.Quota = p.AgentClusterID.String(), p.LeaseSeconds, PersonalDockerQuota(*p)
	} else {
		p := item.PersonalVM
		vm := personalVMPolicy(*p)
		policy.Backend, policy.Provider, policy.VM = "vm", vm.Provider, &vm
		policy.LeaseSeconds = p.LeaseSeconds
		policy.Quota = personalVMQuota(*p)
	}
	frozen, err := json.Marshal(policy)
	if err != nil {
		return domaincopilot.PersonalEnvironmentPolicy{}, err
	}
	policy.PolicyRevision = fmt.Sprintf("sha256:%x", sha256.Sum256(frozen))
	return policy, nil
}

// Saving a policy is configuration only. User provisioning must still obtain a
// real Agent receipt before exposing readiness or permitting task execution.
func (s *Service) PutEnvironmentTemplateForPrincipal(ctx context.Context, principal domainidentity.Principal, item EnvironmentTemplate) (EnvironmentTemplate, error) {
	if err := validatePersonalDockerPolicy(item); err != nil {
		return EnvironmentTemplate{}, err
	}
	if err := validatePersonalVMPolicy(item); err != nil {
		return EnvironmentTemplate{}, err
	}
	if item.PersonalVM != nil {
		if s.personalVMValidator == nil || principal.UserID == "" {
			return EnvironmentTemplate{}, apperrors.ErrServiceUnavailable
		}
		if err := s.personalVMValidator(ctx, principal, personalVMPolicy(*item.PersonalVM)); err != nil {
			return EnvironmentTemplate{}, err
		}
	}
	if item.PersonalDocker != nil {
		if strings.TrimSpace(principal.UserID) == "" {
			return EnvironmentTemplate{}, apperrors.ErrAccessDenied
		}
		if s.executionAgentValidator == nil {
			return EnvironmentTemplate{}, apperrors.ErrServiceUnavailable
		}
		if err := s.executionAgentValidator(ctx, principal, item.PersonalDocker.AgentClusterID.String()); err != nil {
			return EnvironmentTemplate{}, err
		}
	}
	return s.putEnvironmentTemplate(ctx, item)
}
