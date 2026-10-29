package networkaccess_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type endpointResponseScenario struct {
	*accessRepositoryScenario
	now      time.Time
	presence domain.PresenceRecord
	hash     string
	packet   networkprotocol.RadiusAccountingRequest
	config   domain.EndpointResponsePolicyConfig
	policy   domain.EndpointResponsePolicy
}

func (s *accessRepositoryScenario) checkEndpointResponse(t *testing.T) {
	x := &endpointResponseScenario{accessRepositoryScenario: s, now: s.now.Add(30 * time.Minute)}
	var err error
	credentialID := "endpoint-credential-" + x.suffix
	x.presence = domain.PresenceRecord{EndpointPresence: domain.EndpointPresence{DeviceID: x.updatedDevice.ID, RuntimeID: "endpoint-runtime-" + x.suffix, BootID: "boot-1", Sequence: 1, ServiceState: sohaapi.NetworkEndpointPresenceServiceStateHealthy, UIState: sohaapi.NetworkEndpointPresenceUIStateUnknown, PolicyVersion: x.snapshot.PolicyVersion, LastObservedAt: &x.now, LastReceivedAt: &x.now}, CredentialID: credentialID, PayloadHash: "payload-1", HeartbeatIntervalSeconds: 30}
	t.Run("EndpointPresenceRejectsReplayAndRetiredBoot", x.checkPresenceReplay)
	if err := x.store.Exec(x.ctx, `UPDATE network_runtime_sessions SET subject_id=$1, authentication_method='eap-tls' WHERE id=$2`, x.userID, x.sessionID); err != nil {
		t.Fatal(err)
	}
	x.hash = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(x.suffix)))
	if err := x.store.Exec(x.ctx, `INSERT INTO network_nas_authorizations (runtime_id,request_id,request_hash,nas_id,subject_id,device_id,authentication_method,session_id,decision,access_profile,policy_version,reason_code,valid_until,created_at,credential_id,station_id,radius_attributes) VALUES ($1,$2,$3,$4,$5,$6,'eap-tls',$7,'allow','restricted',$8,'policy_allowed',$9,$10,$11,'aa:bb:cc:dd:ee:ff','{"sessionTimeoutSeconds":120}'::jsonb)`, x.runtimeID, "eap-"+x.suffix, x.hash, x.nasBinding.NASID, x.userID, x.updatedDevice.ID, x.sessionID, x.snapshot.PolicyVersion, x.now.Add(time.Hour), x.accessRepositoryScenario.now, credentialID); err != nil {
		t.Fatal(err)
	}
	x.packet = networkprotocol.RadiusAccountingRequest{SchemaVersion: networkprotocol.RadiusAccountingSchemaVersion, EventTimestamp: x.now.Unix(), RadiusAccounting: networkprotocol.RadiusAccounting{NASID: x.nasBinding.NASID, AccountingSessionID: "accounting-" + x.suffix, SessionID: x.sessionID, UserID: x.userID, DeviceID: "AA-BB-CC-DD-EE-FF", NASPortID: "port-1", StatusType: "start"}}
	t.Run("NASAccountingRequiresStrongBindingAndMonotonicTime", x.checkAccountingBinding)
	x.config = domain.EndpointResponsePolicyConfig{Name: "Response test", Enabled: true, SiteIDs: []string{x.site.ID}, NasBindingIDs: []string{x.nasBinding.ID}, DeviceIDs: []string{x.updatedDevice.ID}, Signal: sohaapi.NetworkEndpointResponseSignalServiceUnavailable, HeartbeatIntervalSeconds: 30, MissedHeartbeats: 2, AccountingFreshnessSeconds: 120, RestoreHeartbeats: 2, RestoreObservationSeconds: 30, Steps: []domain.EndpointResponseStep{{Action: sohaapi.NetworkEndpointResponseActionRecord}}, FailureAction: sohaapi.NetworkEndpointResponsePolicyConfigurationFailureActionRecord}
	x.policy.ID = "response-policy-" + x.suffix
	x.policy, err = x.repository.SaveEndpointResponsePolicy(x.ctx, x.policy.ID, 0, x.config, x.userID, x.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("EndpointResponseDraftPublishAndRollbackAreConditional", x.checkConditionalPublication)
	t.Run("EndpointRestrictionPersistsBeforeNASAndRestoreRejectsInFlight", x.checkRestrictionRecovery)
	t.Run("EndpointResponseVersionsRetainIndependentFloorsAndRecovery", x.checkVersionedRecovery)
	t.Run("EndpointResponseCandidateCursorUsesTheSameOrderAsTheQuery", x.checkCandidateCursor)
	deletedEvent := x.createVersionedResponseEvent(t, "response-deleted-policy-"+x.suffix, domain.ProfileRestricted)
	if err := x.repository.DeleteEndpointResponsePolicy(x.ctx, x.policy.ID, x.policy.Revision, x.now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	candidates, err := x.repository.EndpointResponseCandidates(x.ctx, "", 100)
	if err != nil || len(candidates) != 1 || candidates[0].EventID != deletedEvent.ID {
		t.Fatalf("deleted policy lost active event supervision: %#v error=%v", candidates, err)
	}
	_, err = x.repository.ReconcileEndpointResponse(x.ctx, x.policy.ID, x.sessionID, deletedEvent.ID, x.now, func(snapshot domain.EndpointResponseSnapshot) (*domain.EndpointResponseMutation, error) {
		if !snapshot.PolicySuperseded || snapshot.Policy.PublishedRevision != deletedEvent.PolicyRevision {
			t.Fatalf("deleted event lost retirement or immutable configuration: %#v", snapshot)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("EndpointResponsePolicyReadFailureStopsEvaluation", func(t *testing.T) {
		expected := errors.New("policy retirement query unavailable")
		injected, evaluated := false, false
		callbacks := x.store.DB().Callback().Row()
		const callbackName = "task53:policy-retirement-query-error"
		if err := callbacks.Before("gorm:row").Register(callbackName, func(tx *gorm.DB) {
			if strings.HasPrefix(tx.Statement.SQL.String(), "SELECT NOT EXISTS (SELECT 1 FROM network_endpoint_response_policies") {
				injected = true
				_ = tx.AddError(expected)
			}
		}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := callbacks.Remove(callbackName); err != nil {
				t.Error(err)
			}
		})
		_, err := x.repository.ReconcileEndpointResponse(x.ctx, x.policy.ID, x.sessionID, deletedEvent.ID, x.now, func(domain.EndpointResponseSnapshot) (*domain.EndpointResponseMutation, error) {
			evaluated = true
			return nil, nil
		})
		if !injected || evaluated || !errors.Is(err, expected) {
			t.Fatalf("policy query failure: injected=%v evaluated=%v error=%v", injected, evaluated, err)
		}
	})
}

func (x *endpointResponseScenario) checkPresenceReplay(t *testing.T) {
	var err error
	first, err := x.repository.SaveEndpointPresence(x.ctx, x.presence)
	if err != nil || first.Epoch != 1 || first.HealthySamples != 1 {
		t.Fatalf("initial presence=%#v error=%v", first, err)
	}
	duplicate, err := x.repository.SaveEndpointPresence(x.ctx, x.presence)
	if err != nil || duplicate.HealthySamples != 1 {
		t.Fatalf("duplicate=%#v error=%v", duplicate, err)
	}
	changed := x.presence
	changed.PayloadHash = "changed"
	if _, err := x.repository.SaveEndpointPresence(x.ctx, changed); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("sequence reuse=%v", err)
	}
	later := x.now.Add(30 * time.Second)
	changed = x.presence
	changed.Sequence = 2
	changed.PayloadHash = "payload-2"
	changed.LastObservedAt, changed.LastReceivedAt = &later, &later
	next, err := x.repository.SaveEndpointPresence(x.ctx, changed)
	if err != nil || next.HealthySamples != 2 {
		t.Fatalf("healthy continuity=%#v error=%v", next, err)
	}
	bootAt := later.Add(time.Second)
	changed.BootID = "boot-2"
	changed.Sequence = 1
	changed.PayloadHash = "boot-2"
	changed.LastObservedAt, changed.LastReceivedAt = &bootAt, &bootAt
	restarted, err := x.repository.SaveEndpointPresence(x.ctx, changed)
	if err != nil || restarted.Epoch != 2 || restarted.HealthySamples != 1 {
		t.Fatalf("new boot=%#v error=%v", restarted, err)
	}
	retiredAt := bootAt.Add(time.Second)
	changed.BootID = "boot-1"
	changed.Sequence = 3
	changed.LastObservedAt, changed.LastReceivedAt = &retiredAt, &retiredAt
	if _, err := x.repository.SaveEndpointPresence(x.ctx, changed); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("retired boot=%v", err)
	}
}

func (x *endpointResponseScenario) checkAccountingBinding(t *testing.T) {
	var err error
	if err := x.repository.RecordNASAccounting(x.ctx, x.runtimeID, x.packet, x.now, time.Minute); err != nil {
		t.Fatal(err)
	}
	x.packet.StatusType = "interim-update"
	x.packet.EventTimestamp = x.now.Add(10 * time.Second).Unix()
	x.packet.SessionTimeSeconds = 10
	if err := x.repository.RecordNASAccounting(x.ctx, x.runtimeID, x.packet, x.now.Add(10*time.Second), time.Minute); err != nil {
		t.Fatal(err)
	}
	oldStop := x.packet
	oldStop.StatusType = "stop"
	oldStop.EventTimestamp = x.now.Add(5 * time.Second).Unix()
	if err := x.repository.RecordNASAccounting(x.ctx, x.runtimeID, oldStop, x.now.Add(11*time.Second), time.Minute); err != nil {
		t.Fatal(err)
	}
	evidence, err := x.repository.NASSessionObservation(x.ctx, x.sessionID)
	if err != nil || evidence.State != sohaapi.NetworkNASSessionEvidenceStateActive || evidence.StoppedAt != nil {
		t.Fatalf("old Stop=%#v error=%v", evidence, err)
	}
	wrong := x.packet
	wrong.DeviceID = "aa:bb:cc:dd:ee:00"
	if err := x.repository.RecordNASAccounting(x.ctx, x.runtimeID, wrong, x.now.Add(11*time.Second), time.Minute); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("cross station=%v", err)
	}
	wrong = x.packet
	wrong.AccountingSessionID = "other-incarnation"
	if err := x.repository.RecordNASAccounting(x.ctx, x.runtimeID, wrong, x.now.Add(11*time.Second), time.Minute); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("cross incarnation=%v", err)
	}
}

func (x *endpointResponseScenario) checkConditionalPublication(t *testing.T) {
	var err error
	if x.policy.PublishedRevision != 0 {
		t.Fatal("draft was published")
	}
	x.policy, err = x.repository.PublishEndpointResponsePolicy(x.ctx, x.policy.ID, 1, 0, x.userID, x.now)
	if err != nil || x.policy.PublishedRevision != 1 {
		t.Fatalf("publish=%#v error=%v", x.policy, err)
	}
	x.config.Name = "edited"
	x.policy, err = x.repository.SaveEndpointResponsePolicy(x.ctx, x.policy.ID, 1, x.config, x.userID, x.now.Add(time.Second))
	if err != nil || x.policy.Revision != 2 || x.policy.PublishedRevision != 1 || x.policy.PublishedConfiguration.Name != "Response test" {
		t.Fatalf("draft isolation=%#v error=%v", x.policy, err)
	}
	if _, err := x.repository.SaveEndpointResponsePolicy(x.ctx, x.policy.ID, 1, x.config, x.userID, x.now); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale edit=%v", err)
	}
	x.policy, err = x.repository.PublishEndpointResponsePolicy(x.ctx, x.policy.ID, 2, 1, x.userID, x.now.Add(2*time.Second))
	if err != nil || x.policy.PublishedRevision != 3 || x.policy.Configuration.Name != "Response test" {
		t.Fatalf("rollback=%#v error=%v", x.policy, err)
	}
}

func (x *endpointResponseScenario) checkRestrictionRecovery(t *testing.T) {
	var err error
	commandID := "response-command-" + x.suffix
	event, err := x.repository.ReconcileEndpointResponse(x.ctx, x.policy.ID, x.sessionID, "", x.now.Add(20*time.Second), func(snapshot domain.EndpointResponseSnapshot) (*domain.EndpointResponseMutation, error) {
		return &domain.EndpointResponseMutation{Event: domain.EndpointResponseRecord{EndpointResponseEvent: domain.EndpointResponseEvent{ID: "response-event-" + x.suffix, PolicyID: x.policy.ID, PolicyRevision: x.policy.PublishedRevision, Revision: 1, DeviceID: x.updatedDevice.ID, SubjectID: x.userID, SiteID: x.site.ID, SessionID: x.sessionID, NasID: x.nasBinding.NASID, Status: sohaapi.NetworkEndpointResponseEventStatusEnforcing, Signal: x.config.Signal, Action: sohaapi.NetworkEndpointResponseActionRestrict, ReasonCode: "endpoint_offline", CommandIDs: []string{commandID}, NotificationState: sohaapi.NetworkEndpointResponseNotificationStatePending, TriggeredAt: x.now, UpdatedAt: x.now}, RestrictionProfile: domain.ProfileRestricted, NotificationPolicyID: "test-notification"}, Command: &domain.SessionCommand{ID: commandID, SessionID: x.sessionID, RuntimeID: x.runtimeID, NASID: x.nasBinding.NASID, Action: domain.SessionActionCoA, TargetAccessProfile: domain.ProfileRestricted, PolicyVersion: x.snapshot.PolicyVersion, Status: domain.SessionCommandPending, ReasonCode: "endpoint_response", PlanHash: x.hash, RadiusAttributes: &networkprotocol.RadiusAttributes{SessionTimeoutSeconds: 120}, EffectiveAt: x.now, ExpiresAt: x.now.Add(time.Minute), CreatedAt: x.now}}, nil
	})
	if err != nil || event.RestrictionEpoch != 1 {
		t.Fatalf("durable event=%#v error=%v", event, err)
	}
	floor, err := x.repository.EndpointRestriction(x.ctx, x.updatedDevice.ID, x.site.ID)
	if err != nil || floor.AccessProfile != domain.ProfileRestricted || floor.Epoch != 1 {
		t.Fatalf("floor=%#v error=%v", floor, err)
	}
	broadening := domain.SessionCommand{SessionID: x.sessionID, RuntimeID: x.runtimeID, NASID: x.nasBinding.NASID, Action: domain.SessionActionCoA, TargetAccessProfile: domain.ProfileFull}
	if _, err := x.repository.CreateSessionCommand(x.ctx, broadening); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("manual broadening=%v", err)
	}
	claim, err := x.repository.ClaimNASSessionCommand(x.ctx, x.runtimeID, x.now.Add(21*time.Second))
	if err != nil || claim.ID != commandID || claim.StationID != "aa:bb:cc:dd:ee:ff" || claim.AccountingSessionID != x.packet.AccountingSessionID {
		t.Fatalf("targeted claim=%#v error=%v", claim, err)
	}
	x.checkRestoreReceipts(t, event)
}

func (x *endpointResponseScenario) checkVersionedRecovery(t *testing.T) {
	var err error
	if err := x.store.Exec(x.ctx, `UPDATE network_runtime_sessions SET status='active' WHERE id=$1`, x.sessionID); err != nil {
		t.Fatal(err)
	}

	oldEvent := x.createVersionedResponseEvent(t, "response-old-version-"+x.suffix, domain.ProfileRestricted)
	x.config.RestoreHeartbeats = 4
	x.policy, err = x.repository.SaveEndpointResponsePolicy(x.ctx, x.policy.ID, x.policy.Revision, x.config, x.userID, x.now)
	if err != nil {
		t.Fatal(err)
	}
	x.policy, err = x.repository.PublishEndpointResponsePolicy(x.ctx, x.policy.ID, x.policy.Revision, 0, x.userID, x.now)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := x.repository.EndpointResponseCandidates(x.ctx, "", 100)
	if err != nil || len(candidates) != 2 {
		t.Fatalf("old event plus new revision candidates=%#v error=%v", candidates, err)
	}
	newEvent := x.createVersionedResponseEvent(t, "response-new-version-"+x.suffix, domain.ProfileDeny)
	if err := x.store.Exec(x.ctx, `UPDATE network_runtime_sessions SET status='expired' WHERE id=$1`, x.sessionID); err != nil {
		t.Fatal(err)
	}
	candidates, err = x.repository.EndpointResponseCandidates(x.ctx, "", 100)
	if err != nil || len(candidates) != 2 {
		t.Fatalf("active events on terminal session=%#v error=%v", candidates, err)
	}
	x.checkRecoveryCandidateVersions(t, candidates, oldEvent.ID)
	restore := func(snapshot domain.EndpointResponseSnapshot) (*domain.EndpointResponseMutation, error) {
		item := *snapshot.Event
		item.Revision++
		item.RestrictionProfile = ""
		item.RestoredAt = &x.now
		item.Status = sohaapi.NetworkEndpointResponseEventStatusRestored
		return &domain.EndpointResponseMutation{Event: item}, nil
	}
	if _, err := x.repository.ReconcileEndpointResponse(x.ctx, x.policy.ID, x.sessionID, newEvent.ID, x.now, restore); err != nil {
		t.Fatal(err)
	}
	floor, err := x.repository.EndpointRestriction(x.ctx, x.updatedDevice.ID, x.site.ID)
	if err != nil || floor.AccessProfile != domain.ProfileRestricted {
		t.Fatalf("restoring new event cleared old floor=%#v error=%v", floor, err)
	}
	x.config.Enabled = false
	x.policy, err = x.repository.SaveEndpointResponsePolicy(x.ctx, x.policy.ID, x.policy.Revision, x.config, x.userID, x.now)
	if err != nil {
		t.Fatal(err)
	}
	x.policy, err = x.repository.PublishEndpointResponsePolicy(x.ctx, x.policy.ID, x.policy.Revision, 0, x.userID, x.now)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err = x.repository.EndpointResponseCandidates(x.ctx, "", 100)
	if err != nil || len(candidates) != 1 || candidates[0].EventID != oldEvent.ID {
		t.Fatalf("disabled policy must supervise its old event without creating new events=%#v error=%v", candidates, err)
	}
	floor, err = x.repository.EndpointRestriction(x.ctx, x.updatedDevice.ID, x.site.ID)
	if err != nil || floor.AccessProfile != domain.ProfileRestricted {
		t.Fatalf("disabling policy cleared floor=%#v error=%v", floor, err)
	}
	x.checkRetiredCommands(t, oldEvent)
	if _, err := x.repository.ReconcileEndpointResponse(x.ctx, x.policy.ID, x.sessionID, oldEvent.ID, x.now, restore); err != nil {
		t.Fatal(err)
	}
}

func (x *endpointResponseScenario) checkCandidateCursor(t *testing.T) {
	var err error
	if err := x.store.Exec(x.ctx, `UPDATE network_runtime_sessions SET status='active' WHERE id=$1`, x.sessionID); err != nil {
		t.Fatal(err)
	}
	x.config.Enabled = true
	x.policy, err = x.repository.SaveEndpointResponsePolicy(x.ctx, x.policy.ID, x.policy.Revision, x.config, x.userID, x.now)
	if err != nil {
		t.Fatal(err)
	}
	x.policy, err = x.repository.PublishEndpointResponsePolicy(x.ctx, x.policy.ID, x.policy.Revision, 0, x.userID, x.now)
	if err != nil {
		t.Fatal(err)
	}
	childID := x.policy.ID + "-child"
	child, err := x.repository.SaveEndpointResponsePolicy(x.ctx, childID, 0, x.config, x.userID, x.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := x.repository.PublishEndpointResponsePolicy(x.ctx, childID, child.Revision, 0, x.userID, x.now); err != nil {
		t.Fatal(err)
	}
	after, seen := "", map[string]bool{}
	for range 3 {
		page, err := x.repository.EndpointResponseCandidates(x.ctx, after, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		row := page[0]
		if seen[row.PolicyID] {
			t.Fatalf("candidate repeated: %#v", row)
		}
		seen[row.PolicyID] = true
		after = row.PolicyID + "/" + row.SessionID + "/" + row.EventID
	}
	if !seen[x.policy.ID] || !seen[childID] || len(seen) != 2 {
		t.Fatalf("prefix policy ID was skipped: %#v", seen)
	}
	if err := x.repository.DeleteEndpointResponsePolicy(x.ctx, childID, child.Revision, x.now); err != nil {
		t.Fatal(err)
	}
}

func (x *endpointResponseScenario) checkRestoreReceipts(t *testing.T, event domain.EndpointResponseRecord) {
	t.Helper()
	commandID := "response-command-" + x.suffix
	restore := func(snapshot domain.EndpointResponseSnapshot) (*domain.EndpointResponseMutation, error) {
		item := *snapshot.Event
		item.Revision++
		item.RestoredAt = &x.now
		item.Status = sohaapi.NetworkEndpointResponseEventStatusRestored
		item.RestrictionProfile = ""
		item.NotificationState = sohaapi.NetworkEndpointResponseNotificationStateFailed
		item.NotificationReason = "notification_cancelled_on_restore"
		return &domain.EndpointResponseMutation{Event: item}, nil
	}
	if _, err := x.repository.ReconcileEndpointResponse(x.ctx, x.policy.ID, x.sessionID, event.ID, x.now.Add(22*time.Second), restore); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("in-flight restore=%v", err)
	}
	result := networkprotocol.NASSessionCommandResult{CommandID: commandID, SessionID: x.sessionID, Status: domain.SessionCommandApplied, ReasonCode: "nas_ack", CompletedAt: x.now.Add(23 * time.Second)}
	if err := x.repository.CompleteNASSessionCommand(x.ctx, x.runtimeID, result, x.now.Add(23*time.Second)); err != nil {
		t.Fatal(err)
	}
	work, err := x.repository.ClaimEndpointNotification(x.ctx, x.now.Add(24*time.Second))
	if err != nil || work.ID != event.ID || work.NotificationAttempts != 1 {
		t.Fatalf("notification claim=%#v error=%v", work, err)
	}
	if _, err := x.repository.ClaimEndpointNotification(x.ctx, x.now.Add(24*time.Second)); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("concurrent notification=%v", err)
	}
	if _, err := x.repository.ReconcileEndpointResponse(x.ctx, x.policy.ID, x.sessionID, event.ID, x.now.Add(26*time.Second), restore); err != nil {
		t.Fatal(err)
	}
	if err := x.repository.CompleteEndpointNotification(x.ctx, event.ID, 1, true, x.now.Add(27*time.Second)); err != nil {
		t.Fatal(err)
	}
	restoredEvent, err := x.repository.EndpointResponseEvent(x.ctx, event.ID)
	if err != nil || restoredEvent.NotificationReason != "notification_cancelled_on_restore" {
		t.Fatalf("late notification rewrote restored event=%#v error=%v", restoredEvent, err)
	}
	floor, err := x.repository.EndpointRestriction(x.ctx, x.updatedDevice.ID, x.site.ID)
	if err != nil || floor.AccessProfile != "" || floor.Epoch != 2 {
		t.Fatalf("restored floor=%#v error=%v", floor, err)
	}
	if err := x.repository.CompleteNASSessionCommand(x.ctx, x.runtimeID, result, x.now.Add(27*time.Second)); err != nil {
		t.Fatal(err)
	}
	session, err := x.repository.GetSession(x.ctx, x.sessionID)
	if err != nil || session.Status != "expired" {
		t.Fatalf("late ACK reactivated=%#v error=%v", session, err)
	}
}

func (x *endpointResponseScenario) checkRecoveryCandidateVersions(t *testing.T, candidates []domain.EndpointResponseCandidate, oldEventID string) {
	t.Helper()
	for _, candidate := range candidates {
		if candidate.EventID == "" {
			t.Fatal("active current event was also scheduled as a new event")
		}
		_, err := x.repository.ReconcileEndpointResponse(x.ctx, candidate.PolicyID, candidate.SessionID, candidate.EventID, x.now, func(snapshot domain.EndpointResponseSnapshot) (*domain.EndpointResponseMutation, error) {
			want := 4
			if candidate.EventID == oldEventID {
				want = 2
			}
			if snapshot.Policy.PublishedConfiguration.RestoreHeartbeats != want || snapshot.Event.ID != candidate.EventID {
				t.Fatalf("event lost immutable recovery configuration: %#v", snapshot.Policy)
			}
			if snapshot.PolicySuperseded != (candidate.EventID == oldEventID) {
				t.Fatalf("event did not track publication changes: %#v", snapshot)
			}
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	last := candidates[len(candidates)-1]
	page, err := x.repository.EndpointResponseCandidates(x.ctx, last.PolicyID+"/"+last.SessionID+"/"+last.EventID, 100)
	if err != nil || len(page) != 0 {
		t.Fatalf("candidate cursor replay=%#v error=%v", page, err)
	}
}

func (x *endpointResponseScenario) createVersionedResponseEvent(t *testing.T, id, profile string) domain.EndpointResponseRecord {
	t.Helper()
	event, err := x.repository.ReconcileEndpointResponse(x.ctx, x.policy.ID, x.sessionID, "", x.now, func(snapshot domain.EndpointResponseSnapshot) (*domain.EndpointResponseMutation, error) {
		if snapshot.Event != nil {
			t.Fatal("new policy revision reused an old active event")
		}
		return &domain.EndpointResponseMutation{Event: domain.EndpointResponseRecord{EndpointResponseEvent: domain.EndpointResponseEvent{ID: id, PolicyID: x.policy.ID, PolicyRevision: snapshot.Policy.PublishedRevision, Revision: 1, DeviceID: x.updatedDevice.ID, SubjectID: x.userID, SiteID: x.site.ID, SessionID: x.sessionID, NasID: x.nasBinding.NASID, Status: sohaapi.NetworkEndpointResponseEventStatusEnforcementUnknown, Signal: x.config.Signal, Action: sohaapi.NetworkEndpointResponseActionRestrict, ReasonCode: "nas_action_unsupported", CommandIDs: []string{}, NotificationState: sohaapi.NetworkEndpointResponseNotificationStateNotRequested, TriggeredAt: x.now, UpdatedAt: x.now}, RestrictionProfile: profile}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func (x *endpointResponseScenario) checkRetiredCommands(t *testing.T, event domain.EndpointResponseRecord) {
	t.Helper()
	if err := x.store.Exec(x.ctx, `UPDATE network_runtime_sessions SET status='active' WHERE id=$1`, x.sessionID); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"pending", "delivered"} {
		id := "retired-" + status + "-" + x.suffix
		hash := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(id)))
		if err := x.store.Exec(x.ctx, `INSERT INTO network_nas_session_commands (id,session_id,runtime_id,nas_id,action,target_access_profile,policy_version,status,reason_code,plan_hash,radius_attributes,effective_at,expires_at,created_at,updated_at,delivered_at,response_event_id,response_epoch) VALUES ($1,$2,$3,$4,'coa','restricted',$5,$6,'endpoint_response',$7,'{"sessionTimeoutSeconds":120}'::jsonb,$8,$9,$8,$8,CASE WHEN $6='delivered' THEN $8::timestamptz ELSE NULL END,$10,$11)`, id, x.sessionID, x.runtimeID, x.nasBinding.NASID, x.snapshot.PolicyVersion, status, hash, x.now, x.now.Add(time.Minute), event.ID, event.RestrictionEpoch); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := x.repository.ClaimNASSessionCommand(x.ctx, x.runtimeID, x.now.Add(40*time.Second)); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("retired commands were delivered again: %v", err)
	}
	_, err := x.repository.ReconcileEndpointResponse(x.ctx, x.policy.ID, x.sessionID, event.ID, x.now, func(snapshot domain.EndpointResponseSnapshot) (*domain.EndpointResponseMutation, error) {
		if !snapshot.PolicySuperseded {
			t.Fatal("disabled policy was not marked superseded")
		}
		item := *snapshot.Event
		item.Revision++
		return &domain.EndpointResponseMutation{Event: item, CancelPendingCommands: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := x.repository.GetSessionCommand(x.ctx, "retired-pending-"+x.suffix)
	if err != nil || pending.Status != domain.SessionCommandExpired || pending.ReasonCode != "endpoint_response_policy_changed" {
		t.Fatalf("pending cancellation=%#v error=%v", pending, err)
	}
	delivered, err := x.repository.GetSessionCommand(x.ctx, "retired-delivered-"+x.suffix)
	if err != nil || delivered.Status != domain.SessionCommandDelivered {
		t.Fatalf("delivered receipt was discarded=%#v error=%v", delivered, err)
	}
	result := networkprotocol.NASSessionCommandResult{CommandID: delivered.ID, SessionID: x.sessionID, Status: domain.SessionCommandApplied, ReasonCode: "nas_ack", CompletedAt: x.now.Add(41 * time.Second)}
	if err := x.repository.CompleteNASSessionCommand(x.ctx, x.runtimeID, result, x.now.Add(41*time.Second)); err != nil {
		t.Fatalf("retired delivered command lost its receipt: %v", err)
	}
	delivered, err = x.repository.GetSessionCommand(x.ctx, delivered.ID)
	if err != nil || delivered.Status != domain.SessionCommandApplied {
		t.Fatalf("delivered completion=%#v error=%v", delivered, err)
	}
}
