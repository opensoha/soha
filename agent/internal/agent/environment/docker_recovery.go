package environment

import (
	"context"
	"errors"
	"strings"
	"time"
)

// GC reads durable Docker receipts, including leases absent from a restarted
// RuntimeManager. Persistent user volumes remain; expired disposable quota
// probes are cleaned by their separate owner and exact receipt.
func (e *DockerExecutor) GC(ctx context.Context, now time.Time) ([]Lease, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultOperationTimeout)
	defer cancel()
	// The supervisor exits before the lease deadline. Filter ended sandboxes
	// before applying the batch bound, so active leases cannot starve cleanup.
	data, err := e.run(ctx, "container", "ls", "--all", "--no-trunc", "--filter", "label="+dockerEnvironmentLabel+"="+dockerEnvironmentBackend, "--filter", "status=exited", "--filter", "status=dead", "--format", `{{.ID}} {{.Label "soha.environment.deadline"}}`)
	if err != nil {
		return nil, err
	}
	released := []Lease{}
	var failures []error
	entries, err := expiredDockerReceiptIDs(string(data), now)
	if err != nil {
		return nil, err
	}
	for _, id := range entries {
		if ctx.Err() != nil {
			failures = append(failures, ctx.Err())
			break
		}
		lease, removed, err := e.gcDockerLease(ctx, id, now)
		if err != nil {
			failures = append(failures, err)
		}
		if removed {
			released = append(released, lease)
		}
	}
	if err := e.gcDockerCommandFences(ctx, now); err != nil {
		failures = append(failures, err)
	}
	if err := e.gcPersonalProbes(ctx, now); err != nil {
		failures = append(failures, err)
	}
	return released, errors.Join(failures...)
}

func (e *DockerExecutor) gcDockerLease(ctx context.Context, id string, now time.Time) (Lease, bool, error) {
	record, found, err := e.containerRecord(ctx, id)
	if err != nil || !found {
		return Lease{}, false, err
	}
	lease, err := dockerLeaseFromLabels(record.Labels)
	if err != nil {
		return Lease{}, false, err
	}
	handle := Handle{ID: record.ID, Ref: record.Labels[dockerEnvironmentLabel+".ref"], Backend: BackendLocalContainer}
	if err := validateDockerHandle(handle, record); err != nil {
		return Lease{}, false, err
	}
	if lease.ExpiresAt.After(now) {
		return Lease{}, false, nil
	}
	if err := e.Stop(ctx, handle); err != nil {
		return Lease{}, false, err
	}
	lease.ReleasedAt = now.UTC()
	return lease, true, nil
}

func (e *DockerExecutor) gcDockerCommandFences(ctx context.Context, now time.Time) error {
	data, err := e.run(ctx, "container", "ls", "--all", "--no-trunc", "--filter", "label="+dockerEnvironmentLabel+"=personal-docker-command-v1", "--format", `{{.ID}} {{.Label "soha.environment.deadline"}}`)
	if err != nil {
		return err
	}
	entries, err := expiredDockerReceiptIDs(string(data), now)
	if err != nil {
		return err
	}
	var failures []error
	for _, id := range entries {
		if ctx.Err() != nil {
			break
		}
		record, found, err := e.containerRecord(ctx, id)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !found {
			continue
		}
		labels := record.Labels
		parent := Handle{ID: labels[dockerEnvironmentLabel+".parent"], Ref: labels[dockerEnvironmentLabel+".ref"], Backend: BackendLocalContainer}
		if labels[dockerEnvironmentLabel] != "personal-docker-command-v1" || !dockerContainerIDPattern.MatchString(parent.ID) || !scopeDigestPattern.MatchString("sha256:"+parent.Ref) {
			failures = append(failures, errors.New("invalid environment command recovery receipt"))
			continue
		}
		// Stop the immutable parent first: the missing attempt may still be
		// running. An orphan marker whose parent is gone is removed by Stop too.
		if err := e.Stop(ctx, parent); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(append(failures, ctx.Err())...)
}

func expiredDockerReceiptIDs(data string, now time.Time) ([]string, error) {
	return expiredDockerReceiptNames(data, now, dockerContainerIDPattern.MatchString)
}

func expiredDockerReceiptNames(data string, now time.Time, validName func(string) bool) ([]string, error) {
	ids := []string{}
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 || !validName(fields[0]) {
			return nil, errors.New("invalid environment cleanup receipt")
		}
		deadline, err := time.Parse(time.RFC3339Nano, fields[1])
		if err != nil {
			return nil, errors.New("invalid environment cleanup deadline")
		}
		if !deadline.After(now) {
			ids = append(ids, fields[0])
		}
		if len(ids) == defaultGCBatchLimit {
			break
		}
	}
	return ids, nil
}
