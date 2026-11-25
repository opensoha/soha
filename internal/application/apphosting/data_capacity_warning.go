package apphosting

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha/internal/domain/governance"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	dataCapacitySweepInterval  = time.Minute
	dataCapacitySampleInterval = 10 * time.Minute
	dataCapacityClaimDuration  = 2 * time.Minute
	dataCapacityBatchSize      = 20
)

type DataCapacityCheck struct {
	AppID     string
	ClaimID   string
	WarningID string
}

type dataCapacityRepository interface {
	ClaimDataCapacityCheck(context.Context, string, time.Time, time.Time) (DataCapacityCheck, bool, error)
	RotateDataCapacityWarning(context.Context, DataCapacityCheck, string) error
	CompleteDataCapacityCheck(context.Context, DataCapacityCheck, bool, time.Time) error
}

type dataCapacitySampler interface {
	SampleDataUsage(context.Context, string) (int64, error)
}

type governanceAlertRecorder interface {
	EnsureActiveGovernanceAlert(context.Context, governance.AlertInput) (bool, error)
	ResolveGovernanceAlert(context.Context, governance.AlertInput) error
}

// RunDataCapacityWarnings samples managed databases independently of user requests.
func (s *Service) RunDataCapacityWarnings(ctx context.Context, recorder governanceAlertRecorder, onError func(error)) {
	repo, repoOK := s.repo.(dataCapacityRepository)
	sampler, samplerOK := s.dynamic.(dataCapacitySampler)
	if !repoOK || !samplerOK || recorder == nil {
		return
	}
	ticker := time.NewTicker(dataCapacitySweepInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := s.sweepDataCapacity(ctx, repo, sampler, recorder); err != nil && ctx.Err() == nil && onError != nil {
			onError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) sweepDataCapacity(ctx context.Context, repo dataCapacityRepository, sampler dataCapacitySampler, recorder governanceAlertRecorder) error {
	settings, err := s.repo.GetSettings(ctx)
	if err != nil || !settings.Enabled {
		return err
	}
	threshold := int64(settings.EffectiveGovernance().DataCapacityWarningMiB) << 20
	var firstError error
	for range dataCapacityBatchSize {
		now := s.now()
		check, ok, err := repo.ClaimDataCapacityCheck(ctx, uuid.NewString(), now, now.Add(dataCapacityClaimDuration))
		if err != nil || !ok {
			return errors.Join(firstError, err)
		}
		if err := s.sampleDataCapacity(ctx, repo, sampler, recorder, check, threshold); err != nil && firstError == nil {
			firstError = err
		}
	}
	return firstError
}

func (s *Service) sampleDataCapacity(ctx context.Context, repo dataCapacityRepository, sampler dataCapacitySampler, recorder governanceAlertRecorder, check DataCapacityCheck, threshold int64) error {
	probe, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	size, err := sampler.SampleDataUsage(probe, check.AppID)
	if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return err
	}
	above := err == nil && size >= threshold
	if err := s.reconcileDataCapacityWarning(ctx, repo, recorder, check, size, threshold, above); err != nil {
		return err
	}
	return repo.CompleteDataCapacityCheck(ctx, check, above, s.now().Add(dataCapacitySampleInterval))
}

func (s *Service) reconcileDataCapacityWarning(ctx context.Context, repo dataCapacityRepository, recorder governanceAlertRecorder, check DataCapacityCheck, size, threshold int64, above bool) error {
	alertCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	input := governance.AlertInput{
		Source: "app-hosting", EventID: check.WarningID,
		Action: "app_data.capacity", ResourceKind: "HostedApp", ResourceName: check.AppID,
		Summary:  "Hosted application database reached its configured usage warning threshold.",
		Severity: "warning", CreatedAt: s.now(),
		Labels: map[string]string{"alert": "app_data_capacity"},
		Annotations: map[string]string{
			"usageBytes":   strconv.FormatInt(size, 10),
			"warningBytes": strconv.FormatInt(threshold, 10),
		},
	}
	if !above {
		return recorder.ResolveGovernanceAlert(alertCtx, input)
	}
	active, err := recorder.EnsureActiveGovernanceAlert(alertCtx, input)
	if err != nil || active {
		return err
	}
	// Resolution may have committed before the previous lease completed.
	input.EventID = uuid.NewString()
	if err := repo.RotateDataCapacityWarning(alertCtx, check, input.EventID); err != nil {
		return err
	}
	_, err = recorder.EnsureActiveGovernanceAlert(alertCtx, input)
	return err
}
