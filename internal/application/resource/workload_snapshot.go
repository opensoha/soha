package resource

import (
	"context"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (w *Workloads) GenerateWorkloadSnapshot(ctx context.Context, principal domainidentity.Principal, clusterID string, request domainresource.WorkloadSnapshotRequest) (domainresource.WorkloadSnapshot, error) {
	request, err := normalizeWorkloadSnapshotRequest(request)
	if err != nil {
		return domainresource.WorkloadSnapshot{}, err
	}
	connection, _, err := w.authorize(ctx, principal, clusterID, request.Namespace, string(request.TargetKind), domainaccess.ActionCreate)
	if err != nil {
		return domainresource.WorkloadSnapshot{}, err
	}

	var source domainresource.ResourceYAMLView
	switch request.SourceKind {
	case domainresource.WorkloadSnapshotSourceDeployment:
		source, err = w.GetDeploymentYAML(ctx, principal, clusterID, request.Namespace, request.SourceName)
	case domainresource.WorkloadSnapshotSourceStatefulSet:
		source, err = w.GetStatefulSetYAML(ctx, principal, clusterID, request.Namespace, request.SourceName)
	case domainresource.WorkloadSnapshotSourceDaemonSet:
		source, err = w.GetDaemonSetYAML(ctx, principal, clusterID, request.Namespace, request.SourceName)
	}
	if err != nil {
		_ = w.recordAudit(ctx, principal, connection.Summary.ID, request.Namespace, string(request.TargetKind), request.TargetName, "generate_snapshot", "failure", err.Error())
		return domainresource.WorkloadSnapshot{}, err
	}
	if w.snapshot == nil {
		err = fmt.Errorf("%w: workload snapshot builder is not configured", apperrors.ErrClusterUnready)
	} else {
		var result domainresource.WorkloadSnapshot
		result, err = w.snapshot(source.Content, request)
		if err == nil {
			_ = w.recordAudit(ctx, principal, connection.Summary.ID, request.Namespace, string(request.TargetKind), request.TargetName, "generate_snapshot", "success",
				fmt.Sprintf("generated %s snapshot from %s %s", request.TargetKind, request.SourceKind, request.SourceName))
			return result, nil
		}
	}
	_ = w.recordAudit(ctx, principal, connection.Summary.ID, request.Namespace, string(request.TargetKind), request.TargetName, "generate_snapshot", "failure", err.Error())
	return domainresource.WorkloadSnapshot{}, err
}

func normalizeWorkloadSnapshotRequest(request domainresource.WorkloadSnapshotRequest) (domainresource.WorkloadSnapshotRequest, error) {
	request.Namespace = strings.TrimSpace(request.Namespace)
	request.SourceName = strings.TrimSpace(request.SourceName)
	request.SourceContainer = strings.TrimSpace(request.SourceContainer)
	request.TargetName = strings.TrimSpace(request.TargetName)
	request.Schedule = strings.TrimSpace(request.Schedule)
	request.Description = strings.TrimSpace(request.Description)
	if err := validateWorkloadSnapshotIdentity(request); err != nil {
		return request, err
	}
	if err := validateWorkloadSnapshotMetadata(request); err != nil {
		return request, err
	}
	if err := validateSnapshotStrings("command", request.Command); err != nil {
		return request, err
	}
	if err := validateSnapshotStrings("args", request.Args); err != nil {
		return request, err
	}
	if err := validateWorkloadSnapshotExecutionPolicy(request); err != nil {
		return request, err
	}
	return request, nil
}

func validateWorkloadSnapshotIdentity(request domainresource.WorkloadSnapshotRequest) error {
	if request.Namespace == "" || request.SourceName == "" || request.TargetName == "" {
		return fmt.Errorf("%w: namespace, sourceName, and targetName are required", apperrors.ErrInvalidArgument)
	}
	if !request.SourceKind.Valid() || !request.TargetKind.Valid() || !request.RestartPolicy.Valid() {
		return fmt.Errorf("%w: unsupported snapshot source, target, or restart policy", apperrors.ErrInvalidArgument)
	}
	if (request.TargetKind == domainresource.WorkloadSnapshotTargetCronJob || request.TargetKind == domainresource.WorkloadSnapshotTargetWorkloadCronJob) && request.Schedule == "" {
		return fmt.Errorf("%w: schedule is required for CronJob targets", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateWorkloadSnapshotMetadata(request domainresource.WorkloadSnapshotRequest) error {
	if utf8.RuneCountInString(request.Description) > 512 || len(request.Labels) > 64 || len(request.Annotations) > 64 {
		return fmt.Errorf("%w: snapshot metadata exceeds contract limits", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateWorkloadSnapshotExecutionPolicy(request domainresource.WorkloadSnapshotRequest) error {
	if request.Parallelism < 0 || request.Completions < 0 || request.BackoffLimit < 0 || request.ActiveDeadlineSeconds < 0 ||
		request.Parallelism > math.MaxInt32 || request.Completions > math.MaxInt32 || request.BackoffLimit > math.MaxInt32 {
		return fmt.Errorf("%w: invalid Job execution policy", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateSnapshotStrings(field string, values []string) error {
	if len(values) > 128 {
		return fmt.Errorf("%w: %s exceeds 128 items", apperrors.ErrInvalidArgument, field)
	}
	for _, value := range values {
		if utf8.RuneCountInString(value) > 4096 {
			return fmt.Errorf("%w: %s item exceeds 4096 characters", apperrors.ErrInvalidArgument, field)
		}
	}
	return nil
}
