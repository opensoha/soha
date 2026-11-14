package environment

import (
	"context"
	"errors"
	"time"

	contractenv "github.com/opensoha/soha/contracts/agent/environment"
)

// PersonalOperation recovers identity from durable Docker receipts, not an
// in-memory manager. The wire protocol never accepts or returns Docker handles.
func (e *DockerExecutor) PersonalOperation(ctx context.Context, request contractenv.Request) (contractenv.Result, error) {
	if err := request.Validate(); err != nil {
		return contractenv.Result{}, err
	}
	if request.Action == "export_data" {
		return contractenv.Result{}, errors.New("personal data export requires the dedicated stream endpoint")
	}
	ctx, cancel := context.WithTimeout(ctx, defaultOperationTimeout)
	defer cancel()
	lease := personalDockerLease(request.Lease)
	result := contractenv.Result{SchemaVersion: contractenv.ResultVersion, LeaseID: lease.ID, RunID: lease.RunID, TaskID: request.TaskID}
	handle, found, err := e.personalHandle(ctx, lease)
	if err != nil {
		return result, err
	}
	if request.Action == "delete_data" {
		if found {
			return result, errors.New("personal environment must be released before data deletion")
		}
		if err := e.DeletePersonalData(ctx, lease); err != nil {
			return result, err
		}
		result.State, result.ObservedAt = "deleted", time.Now().UTC()
		return result, nil
	}
	if request.Action == "release" {
		if !found {
			result.State, result.ObservedAt = "missing", time.Now().UTC()
			return result, nil
		}
		if err := e.Stop(ctx, handle); err != nil {
			return result, err
		}
		result.State, result.ObservedAt = "released", time.Now().UTC()
		return result, nil
	}
	if request.Action == "acquire" {
		handle, err = e.Start(ctx, LocalRequest{Lease: lease, Spec: Spec{Backend: BackendLocalContainer, Image: e.image, RequireExistingData: !request.CreateData}})
		if err != nil {
			return result, err
		}
	} else if !found {
		if request.Action == "execute" {
			return result, ErrLeaseNotFound
		}
		retired, err := e.personalDataRetired(ctx, lease)
		if err != nil {
			return result, err
		}
		result.State, result.ObservedAt = personalDataDeletedResult(retired), time.Now().UTC()
		return result, nil
	} else if !lease.ExpiresAt.After(time.Now()) {
		if err := e.Stop(ctx, handle); err != nil {
			return result, err
		}
		if request.Action == "execute" {
			return result, ErrLeaseExpired
		}
		result.State, result.ObservedAt = "expired", time.Now().UTC()
		return result, nil
	}
	if request.Action == "execute" {
		output, err := e.executeTask(ctx, handle, request.TaskID, request.Argv)
		if err != nil {
			return result, err
		}
		text := string(output)
		result.Output = &text
	}
	usage, refs, err := e.Inspect(ctx, handle)
	if err != nil {
		return result, err
	}
	result.State, result.ObservedAt = "active", time.Now().UTC()
	if usage.DiskBytes > lease.Quota.DiskBytes || usage.MemoryBytes > lease.Quota.MemoryBytes || usage.Processes > lease.Quota.MaxProcesses {
		result.State = "quota_exceeded"
	}
	result.Snapshot = &contractenv.Snapshot{SchemaVersion: contractenv.SnapshotVersion, LeaseID: lease.ID, RunID: lease.RunID,
		State: result.State, ObservedAt: result.ObservedAt, ArtifactRefs: refs,
		ResourceUsage: contractenv.Usage{DiskBytes: usage.DiskBytes, MemoryBytes: usage.MemoryBytes, CPUTimeNanos: usage.CPUTimeNanos, Processes: usage.Processes}}
	return result, nil
}

func personalDockerLease(l contractenv.Lease) Lease {
	return Lease{ID: l.ID, RunID: l.RunID, OwnerID: l.OwnerID, ScopeHash: l.ScopeHash, Mode: l.Mode,
		IssuedAt: l.IssuedAt, ExpiresAt: l.ExpiresAt,
		Quota: Quota{DiskBytes: l.Quota.DiskBytes, MemoryBytes: l.Quota.MemoryBytes, CPUUnits: l.Quota.CPUUnits, MaxProcesses: l.Quota.MaxProcesses, NetworkEgress: l.Quota.NetworkEgress}}
}

func (e *DockerExecutor) personalHandle(ctx context.Context, lease Lease) (Handle, bool, error) {
	name, ref := dockerLeaseIdentity(lease)
	record, found, err := e.containerRecord(ctx, name)
	if err != nil || !found {
		return Handle{}, found, err
	}
	handle := Handle{ID: record.ID, Backend: BackendLocalContainer, Ref: ref}
	if err := validateDockerHandle(handle, record); err != nil {
		return Handle{}, false, errors.New("environment belongs to another original lease")
	}
	return handle, true, nil
}
