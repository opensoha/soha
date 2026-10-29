package networkaccess

import (
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/networkprotocol"
)

func admissionSnapshot(now time.Time) domain.NASAdmissionSnapshot {
	return domain.NASAdmissionSnapshot{
		Session:       domain.Session{ID: "session", RuntimeID: "nas-runtime", NASID: "nas", SubjectID: "user", DeviceID: "device", SiteID: "site", Mode: domain.ModeInternalDirect, AccessProfile: domain.ProfileFull, Status: "active", PolicyVersion: 1, ExpiresAt: now.Add(time.Hour)},
		Subject:       domain.Subject{UserID: "user", Status: domain.StatusActive, Teams: []string{"staff"}},
		Device:        domain.Device{ID: "device", OwnerUserID: "user", SiteID: "site", Status: domain.DeviceStatusActive, PostureStatus: "compliant"},
		Site:          domain.Site{ID: "site", Status: domain.StatusActive},
		Binding:       domain.NASBinding{ID: "binding", SiteID: "site", RuntimeID: "nas-runtime", NASID: "nas", Status: domain.StatusActive, CoASupported: true, DisconnectSupported: true},
		Policies:      domain.PolicySnapshot{PolicyVersion: 2, Policies: []domain.Policy{{ID: "allow", Enabled: true, Effect: domain.PolicyEffectAllow, AccessProfile: domain.ProfileRestricted, Subjects: domain.PolicySubjects{Teams: []string{"staff"}}}}},
		Profiles:      map[string]domain.SiteProfileBinding{domain.ProfileRestricted: {ID: "restricted", AccessProfile: domain.ProfileRestricted, VLANID: 20, FilterID: "repair", SessionTimeoutSeconds: 120}, domain.ProfileFull: {ID: "full", AccessProfile: domain.ProfileFull, VLANID: 10, FilterID: "staff", SessionTimeoutSeconds: 120}},
		StrongBinding: true, CredentialActive: true,
		RadiusAttributes: &networkprotocol.RadiusAttributes{VLANID: 10, FilterID: "staff", SessionTimeoutSeconds: 120},
	}
}

func TestNASAdmissionReevaluationTightensAndFailsClosed(t *testing.T) {
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name                    string
		change                  func(*domain.NASAdmissionSnapshot)
		profile, action, status string
	}{
		{"policy downgrade", func(*domain.NASAdmissionSnapshot) {}, domain.ProfileRestricted, domain.SessionActionCoA, domain.SessionCommandPending},
		{"group removed", func(s *domain.NASAdmissionSnapshot) { s.Subject.Teams = nil }, domain.ProfileDeny, domain.SessionActionDisconnect, domain.SessionCommandPending},
		{"owner changed", func(s *domain.NASAdmissionSnapshot) { s.Device.OwnerUserID = "another" }, domain.ProfileDeny, domain.SessionActionDisconnect, domain.SessionCommandPending},
		{"credential revoked", func(s *domain.NASAdmissionSnapshot) { s.CredentialActive = false }, domain.ProfileDeny, domain.SessionActionDisconnect, domain.SessionCommandPending},
		{"expired", func(s *domain.NASAdmissionSnapshot) { s.Session.ExpiresAt = now }, domain.ProfileDeny, domain.SessionActionDisconnect, domain.SessionCommandPending},
		{"unsupported NAS", func(s *domain.NASAdmissionSnapshot) {
			s.Binding.CoASupported, s.Binding.DisconnectSupported = false, false
		}, domain.ProfileRestricted, domain.SessionActionDisconnect, domain.SessionCommandUnsupported},
		{"missing strong binding", func(s *domain.NASAdmissionSnapshot) { s.StrongBinding = false }, domain.ProfileRestricted, domain.SessionActionCoA, domain.SessionCommandUnsupported},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := admissionSnapshot(now)
			test.change(&snapshot)
			got := reconcileNASAdmission(snapshot, now)
			if got == nil || got.Profile != test.profile || got.Command == nil || got.Command.Action != test.action || got.Command.Status != test.status || got.Command.SessionID != snapshot.Session.ID {
				t.Fatalf("mutation=%#v", got)
			}
		})
	}
}

func TestNASAdmissionNeverRelaxesOldSessionAndIsIdempotent(t *testing.T) {
	now := time.Now().UTC()
	snapshot := admissionSnapshot(now)
	snapshot.FloorProfile = domain.ProfileRestricted
	snapshot.Policies.Policies[0].AccessProfile = domain.ProfileFull
	snapshot.RadiusAttributes = &networkprotocol.RadiusAttributes{VLANID: 20, FilterID: "repair", SessionTimeoutSeconds: 120}
	got := reconcileNASAdmission(snapshot, now)
	if got == nil || got.Profile != domain.ProfileRestricted || got.Command != nil {
		t.Fatalf("relaxed session=%#v", got)
	}
	snapshot.InputsHash, snapshot.Session.PolicyVersion = got.InputsHash, got.PolicyVersion
	if got := reconcileNASAdmission(snapshot, now.Add(time.Second)); got != nil {
		t.Fatalf("duplicate generated work=%#v", got)
	}
	delete(snapshot.Profiles, domain.ProfileRestricted)
	if got := reconcileNASAdmission(snapshot, now); got == nil || got.Profile != domain.ProfileDeny || got.Command == nil {
		t.Fatalf("missing mapping=%#v", got)
	}
}

func TestNASAdmissionMappingChangeRequiresReauthentication(t *testing.T) {
	now := time.Now().UTC()
	snapshot := admissionSnapshot(now)
	snapshot.Policies.Policies[0].AccessProfile = domain.ProfileFull
	snapshot.RadiusAttributes.FilterID = "old-acl"
	got := reconcileNASAdmission(snapshot, now)
	if got == nil || got.Profile != domain.ProfileDeny || got.Command == nil || got.Command.Action != domain.SessionActionDisconnect {
		t.Fatalf("mapping change=%#v", got)
	}
}

func TestNASAdmissionRoleRevocationPersistsDenialBeforeDisconnect(t *testing.T) {
	now := time.Now().UTC()
	snapshot := admissionSnapshot(now)
	snapshot.Subject.Roles = []string{"network-staff"}
	snapshot.Policies.Policies[0].Subjects.Roles = []string{"network-staff"}
	snapshot.Policies.Policies[0].AccessProfile = domain.ProfileFull
	current := reconcileNASAdmission(snapshot, now)
	if current == nil || current.Profile != domain.ProfileFull || current.Command != nil {
		t.Fatalf("role grant did not allow the existing session: %#v", current)
	}
	snapshot.InputsHash, snapshot.Session.PolicyVersion = current.InputsHash, current.PolicyVersion
	snapshot.Subject.Roles = nil
	revoked := reconcileNASAdmission(snapshot, now.Add(time.Second))
	if revoked == nil || revoked.Profile != domain.ProfileDeny || revoked.InputsHash == current.InputsHash || revoked.Command == nil || revoked.Command.Action != domain.SessionActionDisconnect {
		t.Fatalf("role revocation did not restrict the exact session: %#v", revoked)
	}
	snapshot.FloorProfile, snapshot.InputsHash = revoked.Profile, revoked.InputsHash
	snapshot.Subject.Roles = []string{"network-staff"}
	restored := reconcileNASAdmission(snapshot, now.Add(2*time.Second))
	if restored == nil || restored.Profile != domain.ProfileDeny {
		t.Fatalf("restoring a role silently relaxed the old session: %#v", restored)
	}
}

func TestNASAdmissionRetriesAreBoundedAndFallBackToDisconnect(t *testing.T) {
	now := time.Now().UTC()
	snapshot := admissionSnapshot(now)
	first := reconcileNASAdmission(snapshot, now)
	snapshot.FloorProfile, snapshot.InputsHash, snapshot.Session.PolicyVersion = first.Profile, first.InputsHash, first.PolicyVersion
	snapshot.RadiusAttributes = first.RadiusAttributes
	snapshot.Session.LatestCommand, snapshot.Attempts = first.Command, 1
	if got := reconcileNASAdmission(snapshot, now.Add(time.Minute)); got != nil {
		t.Fatalf("retried before backoff=%#v", got)
	}
	got := reconcileNASAdmission(snapshot, now.Add(91*time.Second))
	if got == nil || got.Command == nil || got.Command.Action != domain.SessionActionDisconnect || got.Attempts != 2 || got.Profile != domain.ProfileRestricted {
		t.Fatalf("fallback=%#v", got)
	}
	snapshot.Session.LatestCommand, snapshot.Attempts = got.Command, 2
	got = reconcileNASAdmission(snapshot, now.Add(212*time.Second))
	if got == nil || got.Command == nil || got.Attempts != 3 {
		t.Fatalf("final attempt=%#v", got)
	}
	snapshot.Session.LatestCommand, snapshot.Attempts = got.Command, 3
	if got := reconcileNASAdmission(snapshot, now.Add(10*time.Minute)); got != nil {
		t.Fatalf("unbounded retry=%#v", got)
	}
}
