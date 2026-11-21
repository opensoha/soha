package networkaccess_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	userrepo "github.com/opensoha/soha/internal/repository/user"
)

func (s *accessRepositoryScenario) checkNASAdmission(t *testing.T) {
	t.Run("NASAccountingTTLSBindsClassAndPersists64BitTotals", s.checkNASAccountingTTLS)
	t.Run("AccessNetworkNASReadback", s.checkAccessNetworkNASReadback)

	t.Run("NASAdmissionFloorSurvivesReevaluationAndRejectsLateACK", func(t *testing.T) {
		now := s.now.Add(40 * time.Minute)
		roleID, sessionID := s.prepareNASAdmissionRoleSession(t, now)
		users := userrepo.New(s.store.DB())
		planHash := func(prefix string) string { return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(prefix+s.suffix))) }
		command := domain.SessionCommand{ID: "reevaluated-coa-" + s.suffix, SessionID: sessionID, RuntimeID: s.runtimeID, NASID: s.nasBinding.NASID, Action: domain.SessionActionCoA, TargetAccessProfile: domain.ProfileRestricted, PolicyVersion: s.snapshot.PolicyVersion, Status: domain.SessionCommandPending, ReasonCode: "policy_changed", PlanHash: planHash("reevaluated"), RadiusAttributes: &networkprotocol.RadiusAttributes{SessionTimeoutSeconds: 60}, EffectiveAt: now, ExpiresAt: now.Add(time.Minute), CreatedAt: now}
		if err := s.repository.ReconcileNASAdmission(s.ctx, sessionID, now, func(snapshot domain.NASAdmissionSnapshot) *domain.NASAdmissionMutation {
			if !snapshot.StrongBinding || snapshot.Subject.UserID != s.userID || snapshot.Binding.ID != s.nasBinding.ID || fmt.Sprint(snapshot.Subject.Roles) != "["+roleID+"]" {
				t.Fatalf("snapshot=%#v", snapshot)
			}
			return &domain.NASAdmissionMutation{Profile: domain.ProfileRestricted, InputsHash: "restricted-inputs", PolicyVersion: s.snapshot.PolicyVersion, Command: &command}
		}); err != nil {
			t.Fatal(err)
		}
		claimed, err := s.repository.ClaimNASSessionCommand(s.ctx, s.runtimeID, now.Add(time.Second))
		if err != nil || claimed.ID != command.ID {
			t.Fatalf("claim=%#v error=%v", claimed, err)
		}
		if err := users.ReplaceRoleBindings(s.ctx, s.userID, nil); err != nil {
			t.Fatal(err)
		}
		if err := s.repository.ReconcileNASAdmission(s.ctx, sessionID, now.Add(2*time.Second), func(snapshot domain.NASAdmissionSnapshot) *domain.NASAdmissionMutation {
			if snapshot.FloorProfile != domain.ProfileRestricted || snapshot.Revision != 1 || snapshot.InputsHash != "restricted-inputs" || len(snapshot.Subject.Roles) != 0 {
				t.Fatalf("persisted floor=%#v", snapshot)
			}
			command.ID, command.PlanHash, command.Action, command.TargetAccessProfile = "reevaluated-disconnect-"+s.suffix, planHash("disconnect"), domain.SessionActionDisconnect, domain.ProfileDeny
			command.RadiusAttributes = nil
			return &domain.NASAdmissionMutation{Profile: domain.ProfileDeny, InputsHash: "revoked-inputs", PolicyVersion: s.snapshot.PolicyVersion, Command: &command}
		}); err != nil {
			t.Fatal(err)
		}
		oldACK := networkprotocol.NASSessionCommandResult{CommandID: claimed.ID, SessionID: sessionID, Status: domain.SessionCommandApplied, ReasonCode: "late_ack", CompletedAt: now.Add(3 * time.Second)}
		if err := s.repository.CompleteNASSessionCommand(s.ctx, s.runtimeID, oldACK, now.Add(3*time.Second)); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("late ACK=%v", err)
		}
		broadening := command
		broadening.ID, broadening.PlanHash, broadening.Action, broadening.TargetAccessProfile = "manual-broadening-"+s.suffix, planHash("manual"), domain.SessionActionCoA, domain.ProfileFull
		if _, err := s.repository.CreateSessionCommand(s.ctx, broadening); !errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("broadening=%v", err)
		}
		if err := s.repository.ReconcileNASAdmission(s.ctx, sessionID, now.Add(4*time.Second), func(snapshot domain.NASAdmissionSnapshot) *domain.NASAdmissionMutation {
			if snapshot.FloorProfile != domain.ProfileDeny || snapshot.Revision != 2 {
				t.Fatalf("durable deny=%#v", snapshot)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func (s *accessRepositoryScenario) checkAccessNetworkNASReadback(t *testing.T) {
	device := s.updatedDevice
	networkID := "readback-network-" + s.suffix
	add := func(label string, at time.Time) string {
		t.Helper()
		id := "readback-" + label + "-" + s.suffix
		validUntil := at.Add(time.Hour)
		if err := s.store.Exec(s.ctx, `INSERT INTO network_runtime_sessions
			(id,runtime_id,subject_id,device_id,site_id,mode,access_profile,status,policy_version,valid_until,nas_id,authentication_method,created_at,updated_at)
			SELECT $1,runtime_id,subject_id,device_id,site_id,'internal_direct','full','active',policy_version,$2,nas_id,'eap-tls',$3,$3
			FROM network_runtime_sessions WHERE id=$4`, id, validUntil, at, s.sessionID); err != nil {
			t.Fatal(err)
		}
		if err := s.store.Exec(s.ctx, `INSERT INTO network_nas_authorizations
			(runtime_id,request_id,request_hash,nas_id,subject_id,device_id,authentication_method,session_id,decision,access_profile,
			 policy_version,reason_code,valid_until,created_at,credential_id,station_id,radius_attributes,access_network_id,admission_binding_id)
			SELECT runtime_id,$1,request_hash,nas_id,subject_id,device_id,'eap-tls',$1,'allow','full',policy_version,
			 'policy_allowed',$2,$3,credential_id,station_id,radius_attributes,$4,$5 FROM network_nas_authorizations WHERE session_id=$6`,
			id, validUntil, at, networkID, s.nasBinding.ID, s.sessionID); err != nil {
			t.Fatal(err)
		}
		return id
	}
	assertReadback := func(owner domain.Device, id, state string) domain.ConnectionNASSession {
		t.Helper()
		items, err := s.repository.LatestAccessNetworkNASSessions(s.ctx, owner, []string{id})
		if item := items[id]; err == nil && item.State == state {
			return item
		}
		t.Fatalf("readback %q: got %+v, %v, want %q", id, items, err, state)
		return domain.ConnectionNASSession{}
	}
	packet := networkprotocol.RadiusAccountingRequest{RadiusAccounting: networkprotocol.RadiusAccounting{
		NASID: s.nasBinding.NASID, UserID: device.OwnerUserID, DeviceID: "AA-BB-CC-DD-EE-FF",
	}}
	record := func(id, status string, at time.Time) {
		t.Helper()
		packet.SessionID, packet.AccountingSessionID = id, id
		packet.EventTimestamp, packet.StatusType = at.Unix(), status
		if err := s.repository.RecordNASAccounting(s.ctx, s.runtimeID, packet, at, time.Minute); err != nil {
			t.Fatal(err)
		}
	}

	// The prior endpoint-response fixture creates an accounting epoch.
	now := s.now.Add(50 * time.Minute).Truncate(time.Second)
	first := add("first", now)
	assertReadback(device, networkID, "pending")
	record(first, "start", now.Add(time.Second))
	got := assertReadback(device, networkID, "observed")
	if got.LastReceivedAt == nil || !got.LastReceivedAt.Equal(now.Add(time.Second)) {
		t.Fatalf("received at %v", got.LastReceivedAt)
	}
	other := device
	other.OwnerUserID = "other-user"
	assertReadback(other, networkID, "")
	other = device
	other.ID = "other-device"
	assertReadback(other, networkID, "")
	assertReadback(device, "other-network", "")

	next := add("next", now.Add(2*time.Second))
	assertReadback(device, networkID, "pending")
	record(next, "start", now.Add(3*time.Second))
	assertReadback(device, networkID, "observed")
	if err := s.store.Exec(s.ctx, `UPDATE network_nas_authorizations SET admission_binding_id=$1 WHERE session_id=$2`, "removed-"+s.suffix, next); err != nil {
		t.Fatal(err)
	}
	assertReadback(device, networkID, "ended")
	if err := s.store.Exec(s.ctx, `UPDATE network_nas_authorizations SET admission_binding_id=$1 WHERE session_id=$2`, s.nasBinding.ID, next); err != nil {
		t.Fatal(err)
	}
	record(next, "stop", now.Add(4*time.Second))
	assertReadback(device, networkID, "ended")

	t.Run("StrictDisconnectRequiresExactCurrentAccounting", func(t *testing.T) {
		at := now.Add(5 * time.Minute)
		strict := add("strict", at)
		if err := s.store.Exec(s.ctx, `UPDATE network_nas_authorizations SET strict_ui_epoch=$1 WHERE session_id=$2`, strings.Repeat("b", 64), strict); err != nil {
			t.Fatal(err)
		}
		claim := func(label string, when time.Time, expectedAccounting string) {
			t.Helper()
			id := strict + "-" + label
			command := domain.SessionCommand{ID: id, SessionID: strict, RuntimeID: s.runtimeID, NASID: s.nasBinding.NASID,
				Action: domain.SessionActionDisconnect, TargetAccessProfile: domain.ProfileDeny, PolicyVersion: s.snapshot.PolicyVersion,
				Status: domain.SessionCommandPending, ReasonCode: "strict_ui_lease_expired", PlanHash: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(id))),
				EffectiveAt: when, ExpiresAt: when.Add(time.Minute), CreatedAt: when}
			if _, err := s.repository.CreateSessionCommand(s.ctx, command); err != nil {
				t.Fatal(err)
			}
			got, err := s.repository.ClaimNASSessionCommand(s.ctx, s.runtimeID, when)
			if expectedAccounting == "" {
				if !errors.Is(err, apperrors.ErrNotFound) {
					t.Fatalf("unconfirmed or superseded strict command was delivered: %+v, %v", got, err)
				}
				saved, err := s.repository.GetSessionCommand(s.ctx, id)
				if err != nil || saved.Status != domain.SessionCommandUnsupported {
					t.Fatalf("strict command suppression was not persisted: %+v, %v", saved, err)
				}
			} else if err != nil || got.ID != id || !got.StrictOnline || got.AccountingSessionID != expectedAccounting {
				t.Fatalf("strict command lost its exact accounting target: %+v, %v", got, err)
			}
		}
		claim("unconfirmed", at, "")
		record(strict, "start", at.Add(time.Second))
		claim("confirmed", at.Add(2*time.Second), strict)
		add("strict-newer", at.Add(3*time.Second))
		claim("superseded", at.Add(4*time.Second), "")
	})
}

func (s *accessRepositoryScenario) prepareNASAdmissionRoleSession(t *testing.T, now time.Time) (string, string) {
	t.Helper()
	return s.prepareNASAdmissionRoleSessionWithSuffix(t, now, "revalidate")
}

func (s *accessRepositoryScenario) prepareNASAdmissionRoleSessionWithSuffix(t *testing.T, now time.Time, purpose string) (string, string) {
	t.Helper()
	roleID := "nas-role-" + purpose + "-" + s.suffix
	if err := s.store.Exec(s.ctx, `INSERT INTO roles (id,name) VALUES ($1,$2)`, roleID, roleID); err != nil {
		t.Fatal(err)
	}
	users := userrepo.New(s.store.DB())
	if err := users.ReplaceRoleBindings(s.ctx, s.userID, []string{roleID}); err != nil {
		t.Fatal(err)
	}
	preview, err := s.repository.AdmissionPreview(s.ctx, s.registeredDevice.ID, s.nasBinding.ID)
	if err != nil || fmt.Sprint(preview.Subject.Roles) != "["+roleID+"]" || preview.Binding.ID != s.nasBinding.ID || preview.Device.ID != s.registeredDevice.ID {
		t.Fatalf("consistent admission preview=%#v error=%v", preview, err)
	}
	sessionID := "reevaluated-" + purpose + "-" + s.suffix
	if err := s.store.Exec(s.ctx, `INSERT INTO network_runtime_sessions (id,runtime_id,subject_id,device_id,site_id,mode,access_profile,status,policy_version,configuration_version,posture_version,valid_until,nas_id,authentication_method,created_at,updated_at) SELECT $1,runtime_id,subject_id,device_id,site_id,'internal_direct','full','active',policy_version,configuration_version,posture_version,$2,nas_id,'eap-tls',$3,$3 FROM network_runtime_sessions WHERE id=$4`, sessionID, now.Add(time.Hour), now, s.sessionID); err != nil {
		t.Fatal(err)
	}
	if err := s.store.Exec(s.ctx, `INSERT INTO network_nas_authorizations (runtime_id,request_id,request_hash,nas_id,subject_id,device_id,authentication_method,session_id,decision,access_profile,policy_version,reason_code,valid_until,created_at,credential_id,station_id,radius_attributes) SELECT runtime_id,$1,request_hash,nas_id,subject_id,device_id,authentication_method,$2,decision,'full',policy_version,reason_code,$3,$4,credential_id,station_id,radius_attributes FROM network_nas_authorizations WHERE session_id=$5`, "reevaluated-request-"+purpose+"-"+s.suffix, sessionID, now.Add(time.Hour), now, s.sessionID); err != nil {
		t.Fatal(err)
	}
	return roleID, sessionID
}

func (s *accessRepositoryScenario) checkNASAccountingTTLS(t *testing.T) {
	now := s.now.Add(35 * time.Minute)
	roleID, sessionID := s.prepareNASAdmissionRoleSessionWithSuffix(t, now, "ttls")
	t.Cleanup(func() {
		if err := userrepo.New(s.store.DB()).ReplaceRoleBindings(s.ctx, s.userID, nil); err != nil {
			t.Error(err)
		}
	})
	const outer = "anonymous@hq.example"
	if err := s.store.Exec(s.ctx, `UPDATE network_nas_authorizations SET authentication_method='eap-ttls-pap', outer_identity=$1, admission_binding_id=$2 WHERE session_id=$3`, outer, s.nasBinding.ID, sessionID); err != nil {
		t.Fatal(err)
	}
	packet := networkprotocol.RadiusAccountingRequest{
		EventTimestamp: now.Unix(), RadiusAccounting: networkprotocol.RadiusAccounting{
			NASID: s.nasBinding.NASID, SessionID: sessionID, AccountingSessionID: "ttls-" + roleID,
			UserID: outer, DeviceID: "AA-BB-CC-DD-EE-FF", NASPortID: "GigabitEthernet1/0/1", StatusType: "start",
		},
	}
	wrong := packet
	wrong.SessionID = "forged-class"
	if err := s.repository.RecordNASAccounting(s.ctx, s.runtimeID, wrong, now, time.Minute); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("forged Class = %v", err)
	}
	wrong = packet
	wrong.UserID = "different@hq.example"
	if err := s.repository.RecordNASAccounting(s.ctx, s.runtimeID, wrong, now, time.Minute); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("unbound outer identity = %v", err)
	}
	if err := s.repository.RecordNASAccounting(s.ctx, s.runtimeID, packet, now, time.Minute); err != nil {
		t.Fatal(err)
	}
	high := int64(2)
	packet.EventTimestamp++
	packet.StatusType, packet.SessionTimeSeconds = "interim-update", 1
	packet.InputGigawords, packet.OutputGigawords = &high, &high
	packet.InputOctets, packet.OutputOctets = 17, 42
	if !packet.NormalizeOctets() {
		t.Fatal("valid RADIUS octets rejected")
	}
	for range 2 {
		if err := s.repository.RecordNASAccounting(s.ctx, s.runtimeID, packet, now.Add(time.Second), time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	evidence, err := s.repository.NASSessionObservation(s.ctx, sessionID)
	if err != nil || evidence.SubjectID != s.userID || evidence.InputOctets != 2<<32+17 || evidence.OutputOctets != 2<<32+42 {
		t.Fatalf("observed accounting = %#v, error = %v", evidence, err)
	}
}
