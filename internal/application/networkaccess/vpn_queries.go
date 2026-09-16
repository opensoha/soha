package networkaccess

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *VPNService) Decision(ctx context.Context, p identity.Principal, id string) (domain.VPNDecision, error) {
	filter := domain.VPNDashboardFilter{From: s.now().UTC().Add(-7 * 24 * time.Hour), To: s.now().UTC().Add(time.Second)}
	store, scopes, err := s.dashboardScopes(ctx, p, filter)
	if err != nil {
		return domain.VPNDecision{}, err
	}
	records, err := store.VPNDashboardRecords(ctx, domain.VPNDashboardQuery{VPNDashboardFilter: filter, Scopes: scopes, DecisionID: id}, s.now().UTC())
	if err != nil {
		return domain.VPNDecision{}, err
	}
	if len(records) == 0 {
		return domain.VPNDecision{}, apperrors.ErrNotFound
	}
	return records[0].Decision, nil
}

func (s *VPNService) CurrentConnection(ctx context.Context, p identity.Principal, deviceID string) (*domain.VPNConnectionView, error) {
	if _, _, err := s.ownedDevice(ctx, p, deviceID); err != nil {
		return nil, err
	}
	store, ok := s.profiles.(VPNDashboardStore)
	if !ok {
		return nil, apperrors.ErrServiceUnavailable
	}
	filter := domain.VPNDashboardFilter{From: s.now().UTC().Add(-7 * 24 * time.Hour), To: s.now().UTC().Add(time.Second), SubjectID: p.UserID}
	scopes, err := store.VPNScopes(ctx, filter)
	if err != nil {
		return nil, err
	}
	records, err := store.VPNDashboardRecords(ctx, domain.VPNDashboardQuery{VPNDashboardFilter: filter, Scopes: scopes, DeviceID: deviceID}, s.now().UTC())
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		c := record.Connection
		if c == nil || (c.State != "connecting" && c.State != "connected") {
			continue
		}
		if s.metrics != nil {
			metrics, err := s.metrics.VPNMetrics(ctx, vpnMetricBindings([]domain.VPNDashboardRecord{record}, filter))
			if err == nil {
				for _, item := range metrics.Sessions {
					if item.SessionID == c.SessionID {
						mergeVPNSessionMetrics(c, item)
					}
				}
			}
		}
		return c, nil
	}
	return nil, nil
}

func (s *VPNService) PreviewSelection(ctx context.Context, p identity.Principal, input domain.VPNPreviewInput) (domain.VPNDecision, error) {
	const key = "network_access.vpn_selection_policies.preview"
	profile, err := s.profiles.GetVPNProfile(ctx, input.ProfileID)
	if err != nil {
		return domain.VPNDecision{}, err
	}
	if err := s.authorizeProfile(ctx, p, key, profile); err != nil {
		return domain.VPNDecision{}, err
	}
	if profile.PublishedConfiguration == nil {
		return domain.VPNDecision{}, apperrors.ErrNotFound
	}
	config := *profile.PublishedConfiguration
	device, err := s.base.store.GetDevice(ctx, input.DeviceID)
	if err != nil {
		return domain.VPNDecision{}, err
	}
	if err := s.authorize(ctx, p, key, "NetworkVPNSubject", device.OwnerUserID, config.SiteID, config.NetworkSpaceID); err != nil {
		return domain.VPNDecision{}, err
	}
	subject, err := s.base.store.GetSubject(ctx, device.OwnerUserID)
	if err != nil {
		return domain.VPNDecision{}, err
	}
	if err := validateVPNIntentSelection(config, domain.VPNIntentInput{Selection: input.Selection, GatewayID: input.GatewayID}); err != nil {
		return domain.VPNDecision{}, err
	}
	if _, err := s.authorizeConnection(ctx, subject, device, config); err != nil {
		return domain.VPNDecision{}, err
	}
	policy, err := s.policies.GetVPNSelectionPolicy(ctx, config.SelectionPolicyID)
	if err != nil {
		return domain.VPNDecision{}, err
	}
	if policy.PublishedConfiguration == nil {
		return domain.VPNDecision{}, apperrors.ErrNotFound
	}
	facts, err := s.connections.VPNGatewayCandidates(ctx, config, s.now().UTC())
	if err != nil {
		return domain.VPNDecision{}, err
	}
	views := VPNGatewayViews(facts, s.now().UTC(), 2*time.Minute)
	if err := s.previewMeasurements(ctx, input, device.ID, profile, policy, views); err != nil {
		return domain.VPNDecision{}, err
	}
	ids, candidates, reason := RankVPNCandidates(*policy.PublishedConfiguration, input.Selection, input.GatewayID, device.ID, views, s.now().UTC())
	now := s.now().UTC()
	d := domain.VPNDecision{ID: uuid.NewString(), ProfileID: profile.ID, ProfileRevision: profile.PublishedRevision, SelectionPolicyID: policy.ID, SelectionPolicyRevision: policy.PublishedRevision, SubjectID: subject.UserID, DeviceID: device.ID, Selection: input.Selection, RequestedGatewayID: input.GatewayID, Strategy: policy.PublishedConfiguration.Strategy, ReasonCode: reason, State: "selected", CreatedAt: now, UpdatedAt: now, Candidates: candidates}
	if len(ids) > 0 {
		d.EffectiveGatewayID = ids[0]
	} else {
		d.State = "failed"
	}
	return d, nil
}

func ApplyVPNProbeViews(views []domain.VPNCandidate, batch networkprotocol.VPNProbeBatch) {
	for _, sample := range batch.Results {
		index := slices.IndexFunc(views, func(c domain.VPNCandidate) bool { return c.GatewayID == sample.GatewayID })
		if index < 0 {
			continue
		}
		timeout := 100 * float64(sample.SentCount-len(sample.RTTSamplesMs)) / float64(sample.SentCount)
		views[index].MeasuredAt, views[index].TimeoutPercent, views[index].SampleCount = &batch.WindowEndedAt, &timeout, len(sample.RTTSamplesMs)
		if len(sample.RTTSamplesMs) > 0 {
			samples := slices.Clone(sample.RTTSamplesMs)
			slices.Sort(samples)
			median := samples[len(samples)/2]
			views[index].LatencyMs = &median
		}
	}
}

func (s *VPNService) previewMeasurements(ctx context.Context, input domain.VPNPreviewInput, deviceID string, profile domain.VPNProfile, policy domain.VPNSelectionPolicy, views []domain.VPNCandidate) error {
	if s.metrics != nil && input.ProbeBatchID != "" {
		credential, err := s.connections.VPNEndpointCredential(ctx, deviceID, s.now().UTC())
		if err != nil {
			return err
		}
		batch, err := s.metrics.VPNProbe(ctx, credential.RuntimeID, "", input.ProbeBatchID)
		if err == nil && batch != nil && batch.ProfileID == profile.ID && batch.ProfileRevision == profile.PublishedRevision && batch.SelectionPolicyRevision == policy.PublishedRevision && networkprotocol.ValidateVPNProbeBatch(*batch, s.now().UTC()) == nil {
			ApplyVPNProbeViews(views, *batch)
		}
	}
	return nil
}
