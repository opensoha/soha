package apphosting

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/domain/governance"
	"github.com/stretchr/testify/require"
)

type capacityWarningHarness struct {
	usage         int64
	deliveryErr   error
	completionErr error
	warnings      []governance.AlertInput
	resolved      []governance.AlertInput
	active        map[string]bool
	rotatedID     string
}

func (*capacityWarningHarness) ClaimDataCapacityCheck(context.Context, string, time.Time, time.Time) (DataCapacityCheck, bool, error) {
	return DataCapacityCheck{}, false, nil
}

func (h *capacityWarningHarness) RotateDataCapacityWarning(_ context.Context, _ DataCapacityCheck, id string) error {
	h.rotatedID = id
	return nil
}

func (h *capacityWarningHarness) CompleteDataCapacityCheck(context.Context, DataCapacityCheck, bool, time.Time) error {
	return h.completionErr
}

func (h *capacityWarningHarness) SampleDataUsage(context.Context, string) (int64, error) {
	return h.usage, nil
}

func (h *capacityWarningHarness) EnsureActiveGovernanceAlert(_ context.Context, input governance.AlertInput) (bool, error) {
	if h.deliveryErr != nil {
		return false, h.deliveryErr
	}
	if active, exists := h.active[input.EventID]; exists {
		return active, nil
	}
	h.active[input.EventID] = true
	h.warnings = append(h.warnings, input)
	return true, nil
}

func (h *capacityWarningHarness) ResolveGovernanceAlert(_ context.Context, input governance.AlertInput) error {
	if h.deliveryErr != nil {
		return h.deliveryErr
	}
	if h.active[input.EventID] {
		h.active[input.EventID] = false
		h.resolved = append(h.resolved, input)
	}
	return nil
}

func TestDataCapacityWarningCrossingRetryAndRearm(t *testing.T) {
	s := Service{now: func() time.Time { return time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC) }}
	h := &capacityWarningHarness{usage: 1024, active: make(map[string]bool)}
	check := DataCapacityCheck{AppID: "app-test", ClaimID: "claim-test", WarningID: "warning-1"}

	h.deliveryErr = errors.New("record unavailable")
	require.ErrorIs(t, s.sampleDataCapacity(t.Context(), h, h, h, check, 1024), h.deliveryErr)
	require.Empty(t, h.warnings)
	h.deliveryErr = nil

	// The monitoring event commits, but the lease completion is lost.
	h.completionErr = errors.New("completion unavailable")
	require.ErrorIs(t, s.sampleDataCapacity(t.Context(), h, h, h, check, 1024), h.completionErr)
	require.ErrorIs(t, s.sampleDataCapacity(t.Context(), h, h, h, check, 1024), h.completionErr)
	require.Len(t, h.warnings, 1)
	require.Equal(t, "warning-1", h.warnings[0].EventID)
	require.Equal(t, map[string]string{"usageBytes": "1024", "warningBytes": "1024"}, h.warnings[0].Annotations)

	// A lower sample closes the existing event, including after a lost completion.
	h.usage = 1023
	require.ErrorIs(t, s.sampleDataCapacity(t.Context(), h, h, h, check, 1024), h.completionErr)
	require.Len(t, h.resolved, 1)
	require.Equal(t, check.WarningID, h.resolved[0].EventID)
	require.False(t, h.active[check.WarningID])

	// Another crossing must persist a new event ID before raising the new warning.
	h.usage, h.completionErr = 1024, nil
	require.NoError(t, s.sampleDataCapacity(t.Context(), h, h, h, check, 1024))
	require.NotEmpty(t, h.rotatedID)
	require.NotEqual(t, check.WarningID, h.rotatedID)
	require.Len(t, h.warnings, 2)
	require.Equal(t, h.rotatedID, h.warnings[1].EventID)
	require.True(t, h.active[h.rotatedID])
}
