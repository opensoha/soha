package networkaccess_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	userrepo "github.com/opensoha/soha/internal/repository/user"
)

func (s *accessRepositoryScenario) checkNASAdmission(t *testing.T) {
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

func (s *accessRepositoryScenario) prepareNASAdmissionRoleSession(t *testing.T, now time.Time) (string, string) {
	t.Helper()
	roleID := "nas-role-" + s.suffix
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
	sessionID := "reevaluated-" + s.suffix
	if err := s.store.Exec(s.ctx, `INSERT INTO network_runtime_sessions (id,runtime_id,subject_id,device_id,site_id,mode,access_profile,status,policy_version,configuration_version,posture_version,valid_until,nas_id,authentication_method,created_at,updated_at) SELECT $1,runtime_id,subject_id,device_id,site_id,'internal_direct','full','active',policy_version,configuration_version,posture_version,$2,nas_id,'eap-tls',$3,$3 FROM network_runtime_sessions WHERE id=$4`, sessionID, now.Add(time.Hour), now, s.sessionID); err != nil {
		t.Fatal(err)
	}
	if err := s.store.Exec(s.ctx, `INSERT INTO network_nas_authorizations (runtime_id,request_id,request_hash,nas_id,subject_id,device_id,authentication_method,session_id,decision,access_profile,policy_version,reason_code,valid_until,created_at,credential_id,station_id,radius_attributes) SELECT runtime_id,$1,request_hash,nas_id,subject_id,device_id,authentication_method,$2,decision,'full',policy_version,reason_code,$3,$4,credential_id,station_id,radius_attributes FROM network_nas_authorizations WHERE session_id=$5`, "reevaluated-request-"+s.suffix, sessionID, now.Add(time.Hour), now, s.sessionID); err != nil {
		t.Fatal(err)
	}
	return roleID, sessionID
}
