package networkaccess

import (
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/networkprotocol"
)

func responseTestSnapshot(now time.Time) domain.EndpointResponseSnapshot {
	old, received, start := now.Add(-3*time.Minute), now, now.Add(-10*time.Minute)
	config := domain.EndpointResponsePolicyConfig{Name: "Endpoint protection", Enabled: true, SiteIDs: []string{"site-1"}, DeviceIDs: []string{}, NasBindingIDs: []string{}, Signal: sohaapi.NetworkEndpointResponseSignalServiceUnavailable, HeartbeatIntervalSeconds: 30, MissedHeartbeats: 2, GracePeriodSeconds: 0, AccountingFreshnessSeconds: 120, RestoreHeartbeats: 2, RestoreObservationSeconds: 30, Steps: []domain.EndpointResponseStep{{Action: sohaapi.NetworkEndpointResponseActionRestrict, TargetAccessProfile: sohaapi.NetworkEndpointResponseStepTargetAccessProfileRestricted}}, FailureAction: sohaapi.NetworkEndpointResponsePolicyConfigurationFailureActionRecord}
	return domain.EndpointResponseSnapshot{Policy: domain.EndpointResponsePolicy{ID: "policy-1", Revision: 1, PublishedRevision: 1, Configuration: config, PublishedConfiguration: &config}, Presence: domain.PresenceRecord{EndpointPresence: domain.EndpointPresence{DeviceID: "device-1", ServiceState: sohaapi.NetworkEndpointPresenceServiceStateHealthy, UIState: sohaapi.NetworkEndpointPresenceUIStateUnknown, LastObservedAt: &old, LastReceivedAt: &old, PolicyVersion: 1}, CredentialID: "endpoint-1"}, Observation: domain.NASSessionObservation{NASSessionEvidence: domain.NASSessionEvidence{SessionID: "session-1", NasID: "nas-1", State: sohaapi.NetworkNASSessionEvidenceStateActive, StartedAt: &start, LastObservedAt: &received, LastReceivedAt: &received}, CredentialID: "endpoint-1", DeviceID: "device-1", SubjectID: "user-1"}, Session: domain.Session{ID: "session-1", RuntimeID: "radius-1", NASID: "nas-1", DeviceID: "device-1", SubjectID: "user-1", SiteID: "site-1", StartedAt: start}, Subject: domain.Subject{UserID: "user-1", Status: domain.StatusActive}, Device: domain.Device{ID: "device-1", OwnerUserID: "user-1", SiteID: "site-1", Status: domain.DeviceStatusActive}, Site: domain.Site{ID: "site-1", Status: domain.StatusActive}, Binding: domain.NASBinding{ID: "binding-1", SiteID: "site-1", NASID: "nas-1", CoASupported: true, DisconnectSupported: true}, AccessPolicies: domain.PolicySnapshot{PolicyVersion: 1, Policies: []domain.Policy{{ID: "allow-1", Enabled: true, Effect: domain.PolicyEffectAllow, AccessProfile: domain.ProfileFull}}}, Profiles: map[string]domain.SiteProfileBinding{domain.ProfileRestricted: {VLANID: 30, FilterID: "repair", SessionTimeoutSeconds: 600}}, CredentialActive: true}
}

func TestEndpointResponseNeedsFreshStrongNASBinding(t *testing.T) {
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name   string
		change func(*domain.EndpointResponseSnapshot)
	}{
		{"unknown session", func(s *domain.EndpointResponseSnapshot) { s.Observation.CredentialID = "" }},
		{"another device", func(s *domain.EndpointResponseSnapshot) { s.Observation.DeviceID = "other" }},
		{"another user", func(s *domain.EndpointResponseSnapshot) { s.Observation.SubjectID = "other" }},
		{"stopped", func(s *domain.EndpointResponseSnapshot) {
			s.Observation.State = sohaapi.NetworkNASSessionEvidenceStateStopped
		}},
		{"stale accounting", func(s *domain.EndpointResponseSnapshot) {
			old := now.Add(-10 * time.Minute)
			s.Observation.LastObservedAt = &old
		}},
		{"maintenance", func(s *domain.EndpointResponseSnapshot) { s.Maintenance = true }},
		{"healthy service", func(s *domain.EndpointResponseSnapshot) {
			s.Presence.LastObservedAt = &now
			s.Presence.LastReceivedAt = &now
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := responseTestSnapshot(now)
			test.change(&snapshot)
			mutation, err := reconcileEndpointResponse(snapshot, now)
			if err != nil || mutation != nil {
				t.Fatalf("mutation=%#v error=%v", mutation, err)
			}
		})
	}
	snapshot := responseTestSnapshot(now)
	mutation, err := reconcileEndpointResponse(snapshot, now)
	if err != nil || mutation == nil || mutation.Command == nil || mutation.Event.RestrictionProfile != domain.ProfileRestricted || mutation.Event.Status != sohaapi.NetworkEndpointResponseEventStatusEnforcing || mutation.Command.Action != domain.SessionActionCoA {
		t.Fatalf("restriction=%#v error=%v", mutation, err)
	}
	if !sha256Pattern.MatchString(mutation.Command.PlanHash) {
		t.Fatalf("command hash cannot be persisted: %s", mutation.Command.PlanHash)
	}
}

func TestEndpointResponseScopeIncludesCurrentRoleBindings(t *testing.T) {
	snapshot := responseTestSnapshot(time.Now().UTC())
	config := snapshot.Policy.Configuration
	config.Subjects.Roles = []string{"network-staff"}
	if MatchesEndpointResponse(config, snapshot.Subject, snapshot.Device, snapshot.Binding) {
		t.Fatal("missing role matched a role-scoped response policy")
	}
	snapshot.Subject.Roles = []string{"network-staff", "another"}
	if !MatchesEndpointResponse(config, snapshot.Subject, snapshot.Device, snapshot.Binding) {
		t.Fatal("current role binding did not match its response policy")
	}
}

func TestEndpointResponseFailureDisconnectAndVerifiedRecovery(t *testing.T) {
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	snapshot := responseTestSnapshot(now)
	snapshot.Binding.CoASupported = false
	snapshot.Policy.PublishedConfiguration.FailureAction = sohaapi.NetworkEndpointResponsePolicyConfigurationFailureActionDisconnect
	first, err := reconcileEndpointResponse(snapshot, now)
	if err != nil || first == nil || first.Command != nil || first.Event.RestrictionProfile != domain.ProfileRestricted || first.Event.Status != sohaapi.NetworkEndpointResponseEventStatusEnforcementUnknown {
		t.Fatalf("unsupported=%#v error=%v", first, err)
	}
	snapshot.Event = &first.Event
	next, err := reconcileEndpointResponse(snapshot, now.Add(time.Second))
	if err != nil || next == nil || next.Command == nil || next.Command.Action != domain.SessionActionDisconnect || next.Event.RestrictionProfile != domain.ProfileDeny {
		t.Fatalf("fallback=%#v error=%v", next, err)
	}
	snapshot.Event = &next.Event
	command := *next.Command
	command.Status = domain.SessionCommandApplied
	completed := now.Add(2 * time.Second)
	command.CompletedAt = &completed
	snapshot.Commands = []domain.SessionCommand{command}
	healthySince := now.Add(-time.Minute)
	snapshot.Presence.LastReceivedAt = &now
	snapshot.Presence.LastObservedAt = &now
	snapshot.Presence.HealthySince = &healthySince
	snapshot.Presence.HealthySamples = 2
	if endpointRestoreAllowed(snapshot, now) == nil {
		t.Fatal("restored without NAS Stop proof")
	}
	stop := now.Add(3 * time.Second)
	snapshot.Observation.StoppedAt = &stop
	snapshot.Observation.State = sohaapi.NetworkNASSessionEvidenceStateStopped
	if err := endpointRestoreAllowed(snapshot, stop); err != nil {
		t.Fatalf("verified recovery: %v", err)
	}
	snapshot.AccessPolicies.PolicyVersion = 2
	if endpointRestoreAllowed(snapshot, stop) == nil {
		t.Fatal("restored with stale endpoint policy version")
	}
}

func TestEndpointResponseDoesNotCallACKVerified(t *testing.T) {
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	snapshot := responseTestSnapshot(now)
	completed := now.Add(-time.Second)
	snapshot.Commands = []domain.SessionCommand{{Action: domain.SessionActionCoA, Status: domain.SessionCommandApplied, CompletedAt: &completed, RadiusAttributes: &networkprotocol.RadiusAttributes{VLANID: 30, FilterID: "repair"}}}
	status, _ := endpointCommandProgress(snapshot, now)
	if status != sohaapi.NetworkEndpointResponseEventStatusEnforcementUnknown {
		t.Fatal("ACK was treated as verified VLAN/ACL")
	}
	snapshot.Observation.VLANID, snapshot.Observation.FilterID = 30, "repair"
	status, _ = endpointCommandProgress(snapshot, now)
	if status != sohaapi.NetworkEndpointResponseEventStatusRestricted {
		t.Fatal("matching subsequent accounting was not verified")
	}
}

func TestEndpointResponseUnsupportedActionRequiresNASStopBeforeRestore(t *testing.T) {
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	snapshot := responseTestSnapshot(now)
	healthySince := now.Add(-time.Minute)
	snapshot.Presence.LastObservedAt, snapshot.Presence.LastReceivedAt = &now, &now
	snapshot.Presence.HealthySince, snapshot.Presence.HealthySamples = &healthySince, 2
	snapshot.Event = &domain.EndpointResponseRecord{EndpointResponseEvent: domain.EndpointResponseEvent{TriggeredAt: now}, RestrictionProfile: domain.ProfileRestricted}
	if endpointRestoreAllowed(snapshot, now) == nil {
		t.Fatal("unsupported enforcement restored while old NAS session was still active")
	}
	snapshot.Observation.State = sohaapi.NetworkNASSessionEvidenceStateStopped
	snapshot.Observation.StoppedAt = &healthySince
	if endpointRestoreAllowed(snapshot, now) == nil {
		t.Fatal("an earlier Stop was used to restore the current restriction")
	}
	snapshot.Observation.StoppedAt = &now
	if err := endpointRestoreAllowed(snapshot, now); err != nil {
		t.Fatalf("strongly bound NAS Stop did not allow fresh authentication: %v", err)
	}
}

func TestEndpointResponseRejectsBroadeningAndUnroutedNotification(t *testing.T) {
	config := responseTestSnapshot(time.Now()).Policy.Configuration
	if err := validateResponseTiming(config); err != nil {
		t.Fatal(err)
	}
	for _, steps := range [][]domain.EndpointResponseStep{
		{{Action: sohaapi.NetworkEndpointResponseActionRestrict, TargetAccessProfile: "quarantine"}, {AfterSeconds: 60, Action: sohaapi.NetworkEndpointResponseActionRestrict, TargetAccessProfile: "restricted"}},
		{{Action: sohaapi.NetworkEndpointResponseActionNotify}},
		{{Action: sohaapi.NetworkEndpointResponseActionDisconnect}, {AfterSeconds: 60, Action: sohaapi.NetworkEndpointResponseActionRecord}},
		{{Action: sohaapi.NetworkEndpointResponseActionRecord}, {Action: sohaapi.NetworkEndpointResponseActionDisconnect}},
		{{AfterSeconds: 60, Action: sohaapi.NetworkEndpointResponseActionRecord}, {AfterSeconds: 30, Action: sohaapi.NetworkEndpointResponseActionDisconnect}},
	} {
		config.Steps = steps
		if validateResponseSteps(config) == nil {
			t.Fatalf("accepted steps=%#v", steps)
		}
	}
}

func TestEndpointResponseRecoveryAfterStagedRestrictionAndDisconnect(t *testing.T) {
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	completed, stopped := now.Add(-20*time.Second), now.Add(-10*time.Second)
	healthySince := now.Add(-time.Minute)
	base := responseTestSnapshot(now)
	base.Presence.LastObservedAt, base.Presence.LastReceivedAt = &now, &now
	base.Presence.HealthySince, base.Presence.HealthySamples = &healthySince, 2
	base.Observation.State, base.Observation.StoppedAt = sohaapi.NetworkNASSessionEvidenceStateStopped, &stopped
	base.Commands = []domain.SessionCommand{
		{Action: domain.SessionActionCoA, Status: domain.SessionCommandApplied, CreatedAt: now.Add(-40 * time.Second), CompletedAt: &completed, RadiusAttributes: &networkprotocol.RadiusAttributes{VLANID: 30, FilterID: "repair"}},
		{Action: domain.SessionActionDisconnect, Status: domain.SessionCommandApplied, CreatedAt: now.Add(-30 * time.Second), CompletedAt: &completed},
	}
	for _, test := range []struct {
		name        string
		edit        func(*domain.EndpointResponseSnapshot)
		wantAllowed bool
	}{
		{"later stop covers earlier CoA", func(*domain.EndpointResponseSnapshot) {}, true},
		{"ACK without Stop", func(s *domain.EndpointResponseSnapshot) { s.Observation.StoppedAt = nil }, false},
		{"active session with old Stop", func(s *domain.EndpointResponseSnapshot) {
			s.Observation.State = sohaapi.NetworkNASSessionEvidenceStateActive
		}, false},
		{"Stop before final command", func(s *domain.EndpointResponseSnapshot) { s.Commands[1].CreatedAt = stopped.Add(time.Second) }, false},
		{"disconnect ACK with active session", func(s *domain.EndpointResponseSnapshot) {
			s.Commands = s.Commands[1:]
			s.Observation.State = sohaapi.NetworkNASSessionEvidenceStateActive
		}, false},
		{"earlier CoA still in flight", func(s *domain.EndpointResponseSnapshot) {
			s.Commands[0].Status = domain.SessionCommandDelivered
			s.Commands[0].ExpiresAt = now.Add(time.Minute)
		}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := base
			snapshot.Commands = append([]domain.SessionCommand(nil), base.Commands...)
			test.edit(&snapshot)
			if err := endpointRestoreAllowed(snapshot, now); (err == nil) != test.wantAllowed {
				t.Fatalf("recovery allowed=%v, want=%v: %v", err == nil, test.wantAllowed, err)
			}
		})
	}
}

func TestEndpointResponseSupersededPolicyDoesNotEscalateOrClearFloor(t *testing.T) {
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	snapshot := responseTestSnapshot(now)
	first, err := reconcileEndpointResponse(snapshot, now)
	if err != nil || first == nil || first.Command == nil {
		t.Fatalf("initial=%#v error=%v", first, err)
	}
	snapshot.Event, snapshot.Commands = &first.Event, []domain.SessionCommand{*first.Command}
	snapshot.PolicySuperseded = true
	snapshot.Policy.PublishedConfiguration.Steps = append(snapshot.Policy.PublishedConfiguration.Steps, domain.EndpointResponseStep{AfterSeconds: 1, Action: sohaapi.NetworkEndpointResponseActionDisconnect})
	retired, err := reconcileEndpointResponse(snapshot, now.Add(2*time.Second))
	if err != nil || retired == nil || retired.Command != nil || !retired.CancelPendingCommands || retired.Event.RestoredAt != nil || retired.Event.RestrictionProfile != domain.ProfileRestricted || retired.Event.ReasonCode != "endpoint_response_policy_changed" {
		t.Fatalf("retired=%#v error=%v", retired, err)
	}
	snapshot.Commands[0].Status = domain.SessionCommandApplied
	completed, observed := now.Add(3*time.Second), now.Add(4*time.Second)
	snapshot.Commands[0].CompletedAt = &completed
	snapshot.Observation.LastObservedAt, snapshot.Observation.LastReceivedAt = &observed, &observed
	snapshot.Observation.VLANID, snapshot.Observation.FilterID = 30, "repair"
	verified, err := reconcileEndpointResponse(snapshot, observed)
	if err != nil || verified == nil || verified.Command != nil || verified.Event.Status != sohaapi.NetworkEndpointResponseEventStatusRestricted || verified.Event.RestoredAt != nil {
		t.Fatalf("retired receipt=%#v error=%v", verified, err)
	}
	snapshot.Policy.PublishedConfiguration.AutoRestore = true
	healthySince := now.Add(-time.Minute)
	snapshot.Presence.LastObservedAt, snapshot.Presence.LastReceivedAt = &observed, &observed
	snapshot.Presence.HealthySince, snapshot.Presence.HealthySamples = &healthySince, 2
	restored, err := reconcileEndpointResponse(snapshot, observed)
	if err != nil || restored == nil || restored.Event.RestoredAt == nil || restored.Event.RestrictionProfile != "" {
		t.Fatalf("verified recovery=%#v error=%v", restored, err)
	}
}
