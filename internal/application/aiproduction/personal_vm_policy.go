package aiproduction

import (
	"context"
	"strings"

	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Set during bootstrap before serving or starting background work.
func (s *Service) SetPersonalVMValidator(validate func(context.Context, identity.Principal, domain.PersonalVMPolicy) error) {
	s.personalVMValidator = validate
}

func personalVMPolicy(p sohaapi.PersonalVMEnvironmentPolicy) domain.PersonalVMPolicy {
	return domain.PersonalVMPolicy{OwnerID: p.OwnerID.String(), ComputeRef: p.ComputeRef.String(), Provider: string(p.Provider), DataDiskSerial: p.DataDiskSerial, DataDiskBytes: p.DataDiskBytes}
}

func personalVMQuota(p sohaapi.PersonalVMEnvironmentPolicy) contractenv.Quota {
	q := p.Quota
	return contractenv.Quota{DiskBytes: q.DiskBytes, MemoryBytes: q.MemoryBytes, CPUUnits: q.CPUUnits, MaxProcesses: q.MaxProcesses, NetworkEgress: q.NetworkEgress}
}

func validatePersonalVMPolicy(item EnvironmentTemplate) error {
	p := item.PersonalVM
	if p == nil {
		if item.Backend == "vm" {
			return apperrors.ErrInvalidArgument
		}
		return nil
	}
	if item.Backend != "vm" || item.PersonalDocker != nil || item.ID == "" || len(item.ID) > 128 || item.ID != strings.TrimSpace(item.ID) || len(item.ResourcePolicy) > 0 || len(item.NetworkPolicy) > 0 || p.LeaseSeconds < 60 || p.LeaseSeconds > 3600 {
		return apperrors.ErrInvalidArgument
	}
	if personalVMPolicy(*p).Validate() != nil || personalVMQuota(*p).ValidatePersonalDocker() != nil || p.Quota.DiskBytes > p.DataDiskBytes {
		return apperrors.ErrInvalidArgument
	}
	return nil
}
