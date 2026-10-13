package copilot

import (
	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func validatePersonalVMBinding(item domain.PersonalEnvironmentRecord) error {
	if item.VMIdentity == nil || item.VMIdentity.Validate() != nil || item.Provider != "pve" || item.ComputeGeneration != 1 || item.AgentClusterID != "" || item.DataInitialized || item.DataCreationAttempted || item.OperationID != "" || item.ActiveTaskID != "" || !domain.ValidPersonalVMPolicyRevision(item.PolicyRevision) || item.Lease.Quota.DiskBytes > item.VMIdentity.DataDiskBytes {
		return apperrors.ErrInvalidArgument
	}
	for _, value := range []string{item.AssistantID, item.CreatedBy, item.ComputeRef, item.DataRef} {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || value != id.String() {
			return apperrors.ErrInvalidArgument
		}
	}
	return nil
}

func samePersonalBinding(item, candidate domain.PersonalEnvironmentRecord) bool {
	if item.TemplateID != candidate.TemplateID || item.Backend != candidate.Backend || item.Provider != candidate.Provider {
		return false
	}
	if candidate.Backend != "vm" {
		return true
	}
	return item.PolicyRevision == candidate.PolicyRevision && item.ComputeRef == candidate.ComputeRef && item.ComputeGeneration == candidate.ComputeGeneration &&
		item.VMIdentity != nil && candidate.VMIdentity != nil && *item.VMIdentity == *candidate.VMIdentity && item.Lease.Quota == candidate.Lease.Quota &&
		item.Lease.Mode == candidate.Lease.Mode && item.Lease.ScopeHash == candidate.Lease.ScopeHash && item.LeaseSeconds == candidate.LeaseSeconds
}
