package apphosting

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type runtimeDiagnosticsBackend interface {
	SampleDataUsage(context.Context, string) (int64, error)
	ObserveRuntimeSignals(context.Context, domain.Revision) ([]domain.RuntimeSignal, error)
}

func (s *Service) GetRuntimeUsage(ctx context.Context, actor identity.Principal, id string) (sohaapi.HostedAppRuntimeUsage, error) {
	app, _, err := s.loadAuthorizedAppRead(ctx, actor, id)
	if err != nil {
		return sohaapi.HostedAppRuntimeUsage{}, err
	}
	repo, ok := s.repo.(runtimeConfigRepository)
	if !ok || app.Kind != sohaapi.HostedAppKindDynamic {
		return sohaapi.HostedAppRuntimeUsage{}, apperrors.ErrNotFound
	}
	cfg, err := repo.GetRuntimeConfig(ctx, app.ID)
	if err != nil {
		return sohaapi.HostedAppRuntimeUsage{}, err
	}
	out := sohaapi.HostedAppRuntimeUsage{
		Status: sohaapi.HostedAppRuntimeUsageStatusUnavailable, WarningBytes: dataWarningBytes(cfg),
	}
	backend, ok := s.dynamic.(runtimeDiagnosticsBackend)
	if !ok {
		return out, nil
	}
	probe, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	size, err := backend.SampleDataUsage(probe, app.ID)
	switch {
	case errors.Is(err, apperrors.ErrNotFound):
		out.Status = sohaapi.HostedAppRuntimeUsageStatusNotEnabled
	case err == nil && size >= 0:
		now := s.now()
		out.Status, out.Bytes, out.SampledAt = sohaapi.HostedAppRuntimeUsageStatusAvailable, &size, &now
		out.Warning = size >= out.WarningBytes
	}
	return out, nil
}

func (s *Service) GetRevisionLogs(ctx context.Context, actor identity.Principal, id, revisionID string) (sohaapi.HostedAppRevisionLogs, error) {
	app, _, err := s.loadAuthorizedAppRead(ctx, actor, id)
	if err != nil {
		return sohaapi.HostedAppRevisionLogs{}, err
	}
	revisionID, err = validateIdentifier(revisionID, "revisionId")
	if err != nil {
		return sohaapi.HostedAppRevisionLogs{}, err
	}
	rev, err := s.repo.GetRevision(ctx, app.ID, revisionID)
	if err != nil {
		return sohaapi.HostedAppRevisionLogs{}, err
	}
	if rev.Kind != sohaapi.HostedAppKindDynamic {
		return sohaapi.HostedAppRevisionLogs{Items: []sohaapi.HostedAppRevisionLog{}}, nil
	}
	now := s.now()
	items := revisionPlatformEvents(rev, now)
	if backend, ok := s.dynamic.(runtimeDiagnosticsBackend); ok && rev.ImageRef != "" {
		probe, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		signals, err := backend.ObserveRuntimeSignals(probe, rev)
		items = appendRuntimeEvents(items, signals, err, now)
	}
	slices.SortStableFunc(items, func(a, b sohaapi.HostedAppRevisionLog) int { return a.At.Compare(b.At) })
	s.recordAudit(ctx, actor, "revision.logs.read", "success", app.ID, "Read hosted application diagnostics", map[string]any{"revisionId": rev.ID})
	return sohaapi.HostedAppRevisionLogs{Items: items}, nil
}

func revisionPlatformEvents(rev domain.Revision, now time.Time) []sohaapi.HostedAppRevisionLog {
	items := []sohaapi.HostedAppRevisionLog{revisionLog("build", "source_received", "Source submission received.", rev.CreatedAt)}
	switch {
	case rev.Status == sohaapi.HostedAppRevisionStatusFailed && rev.FailureStage == "starting":
		items = append(items, revisionLog("runtime", "startup_failed", "The revision could not finish starting.", now))
	case rev.Status == sohaapi.HostedAppRevisionStatusFailed && rev.FailureStage == "activate":
		items = append(items, revisionLog("runtime", "activation_rejected", "The revision did not pass activation checks.", now))
	case rev.Status == sohaapi.HostedAppRevisionStatusFailed:
		items = append(items, revisionLog("build", "build_failed", "The source or managed image build did not complete.", now))
	case rev.ActivatedAt != nil:
		items = append(items, revisionLog("runtime", "activated", "Revision passed readiness checks and was activated.", *rev.ActivatedAt))
	case rev.BuildPhase == "starting":
		items = append(items, revisionLog("runtime", "startup_pending", "Verified image is waiting for runtime readiness.", now))
	case rev.BuildPhase == "building":
		items = append(items, revisionLog("build", "build_running", "The managed build is running.", now))
	case rev.BuildPhase == "queued":
		items = append(items, revisionLog("build", "build_queued", "The source is waiting for a managed build.", now))
	}
	return items
}

func appendRuntimeEvents(items []sohaapi.HostedAppRevisionLog, signals []domain.RuntimeSignal, err error, now time.Time) []sohaapi.HostedAppRevisionLog {
	if err != nil {
		return append(items, revisionLog("runtime", "runtime_observation_unavailable", "Live runtime status is temporarily unavailable.", now))
	}
	for _, signal := range signals {
		if len(items) >= 50 {
			break
		}
		message := runtimeSignalMessages[signal.Code]
		if message != "" && !signal.At.IsZero() {
			items = append(items, revisionLog("runtime", signal.Code, message, signal.At))
		}
	}
	return items
}

var runtimeSignalMessages = map[string]string{
	"runtime_running":           "A serving instance started.",
	"runtime_crash_loop":        "A serving container is repeatedly restarting.",
	"runtime_image_pull_failed": "A serving container could not pull its image.",
	"runtime_oom":               "A serving container exceeded its memory limit.",
	"runtime_exit_error":        "A serving container exited with an error.",
}

func revisionLog(source sohaapi.HostedAppRevisionLogSource, code, message string, at time.Time) sohaapi.HostedAppRevisionLog {
	return sohaapi.HostedAppRevisionLog{Source: source, Code: code, Message: message, At: at}
}
