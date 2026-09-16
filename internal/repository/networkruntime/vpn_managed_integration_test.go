package networkruntime_test

import (
	"errors"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	networkrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	runtimerepo "github.com/opensoha/soha/internal/repository/networkruntime"
	"gorm.io/gorm"
)

func (s *runtimeRepositoryScenario) makeManagedConnection(t *testing.T, c *runtime.VPNConnection) {
	t.Helper()
	repo := networkrepo.New(s.store.DB())

	initial, err := s.repository.EnsureConfiguration(s.ctx, s.gatewayRuntimeID, s.snapshot, c.CreatedAt, c.CreatedAt.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.repository.ApplyConfiguration(s.ctx, s.gatewayRuntimeID, networkprotocol.ConfigurationApplied{ConfigurationVersion: initial.ConfigurationVersion, PolicyVersion: s.policyVersion, Status: "applied", ReadbackHash: digest("initial-gateway-ready")}, c.CreatedAt); err != nil {
		t.Fatal(err)
	}
	policyID, profileID, authID := "vpn-policy-"+s.suffix, "vpn-profile-"+s.suffix, "auth-"+s.suffix
	policy := domain.VPNSelectionPolicyConfig{Name: "priority", Strategy: "priority", ProviderOrder: []string{}, ProviderPreference: "prefer", MaxLatencyMs: 2000, MaxTimeoutPercent: 50, MaxSampleAgeSeconds: 60, MinSamples: 3, MissingMeasurements: "priority", MaxAttempts: 3, RetryCooldownSeconds: 30}
	profile := domain.VPNProfileConfig{Name: "office", Enabled: true, SiteID: s.siteID, NetworkSpaceID: s.spaceID, Mode: domain.ModeExternalVPN, GatewayIDs: []string{s.gatewayID}, SelectionPolicyID: policyID, ResourceIDs: []string{}, Assignments: domain.VPNAssignment{UserIDs: []string{s.subjectID}, TeamIDs: []string{}, DeviceIDs: []string{}}}
	if _, err := repo.SaveVPNSelectionPolicy(s.ctx, policyID, 0, policy, s.subjectID, c.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PublishVPNSelectionPolicy(s.ctx, policyID, 1, 0, s.subjectID, c.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveVPNProfile(s.ctx, profileID, 0, profile, s.subjectID, c.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.PublishVPNProfile(s.ctx, profileID, 1, 0, s.subjectID, c.CreatedAt); err != nil {
		t.Fatal(err)
	}
	if err := s.store.DB().Exec(`INSERT INTO sessions(id,user_id,refresh_token_id,provider_type,expires_at) VALUES (?,?::uuid,?,'local',?)`, authID, s.subjectID, "refresh-"+s.suffix, c.CreatedAt.Add(24*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	intent := domain.VPNIntent{ID: "intent-" + s.suffix, RuntimeID: s.runtimeID, CredentialID: s.credential2.ID, SubjectID: s.subjectID, DeviceID: s.deviceID, AuthSessionID: authID, ProfileID: profileID, ProfileRevision: 1, SelectionPolicyID: policyID, SelectionPolicyRevision: 1, Selection: "auto", Status: "issued", TokenHash: digest("intent-token"), CreatedAt: c.CreatedAt, ExpiresAt: c.CreatedAt.Add(2 * time.Minute)}
	if err := s.repository.CreateVPNIntent(s.ctx, intent); err != nil {
		t.Fatal(err)
	}
	d := domain.VPNDecision{ID: intent.ID, ProfileID: profileID, ProfileRevision: 1, SelectionPolicyID: policyID, SelectionPolicyRevision: 1, SiteID: s.siteID, NetworkSpaceID: s.spaceID, SubjectID: s.subjectID, DeviceID: s.deviceID, Selection: "auto", Strategy: "priority", ReasonCode: "auto_priority", State: "selected", CreatedAt: c.CreatedAt, UpdatedAt: c.CreatedAt, Candidates: []domain.VPNCandidateDecision{{GatewayID: s.gatewayID, Name: "gateway", ProviderCode: "isp", Rank: 1, Eligible: true, ReasonCode: "eligible"}}}
	c.ResourceIDs = []string{}
	c.Managed = &runtime.ManagedVPNConnection{Intent: intent, RequestID: c.RequestID, RequestHash: c.RequestHash, Profile: profile, Policy: policy, Decision: d}
}

func (s *runtimeRepositoryScenario) checkManagedDashboard(t *testing.T) {
	t.Helper()
	repo := networkrepo.New(s.store.DB())
	q := domain.VPNDashboardQuery{VPNDashboardFilter: domain.VPNDashboardFilter{From: s.connectedAt.Add(-time.Second), To: s.connectedAt.Add(time.Minute)}, Scopes: []domain.VPNScope{{ProfileID: "vpn-profile-" + s.suffix, SiteID: s.siteID, NetworkSpaceID: s.spaceID}}}
	rows, err := repo.VPNDashboardRecords(s.ctx, q, s.connectedAt)
	if err != nil || len(rows) != 1 || rows[0].Decision.State != "connecting" || rows[0].EstablishedAt != nil {
		t.Fatalf("issued session counted connected: %+v %v", rows, err)
	}
	gateway, err := s.repository.EnsureConfiguration(s.ctx, s.gatewayRuntimeID, s.snapshot, s.connectedAt, s.connectedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.repository.ApplyConfiguration(s.ctx, s.gatewayRuntimeID, networkprotocol.ConfigurationApplied{ConfigurationVersion: gateway.ConfigurationVersion, PolicyVersion: s.policyVersion, Status: "applied", ReadbackHash: digest("gateway-readback")}, s.connectedAt); err != nil {
		t.Fatal(err)
	}
	rows, err = repo.VPNDashboardRecords(s.ctx, q, s.connectedAt)
	if err != nil || len(rows) != 1 || rows[0].Decision.State != "connected" || rows[0].EstablishedAt == nil || rows[0].Connection == nil {
		t.Fatalf("confirmed session missing: %+v %v", rows, err)
	}

	s.checkDuplicateManagedDecision(t, repo, q)
	q.Scopes[0].NetworkSpaceID = "hidden-space"
	if rows, err := repo.VPNDashboardRecords(s.ctx, q, s.connectedAt); err != nil || len(rows) != 0 {
		t.Fatalf("foreign scope returned records: %+v %v", rows, err)
	}
	q.Scopes = nil
	if rows, err := repo.VPNDashboardRecords(s.ctx, q, s.connectedAt); err != nil || len(rows) != 0 {
		t.Fatalf("empty scope returned records: %+v %v", rows, err)
	}
	stored, found, err := s.repository.ManagedVPNResult(s.ctx, "intent-"+s.suffix, s.runtimeID, s.connection.RequestID, s.connection.RequestHash)
	if err != nil || !found || stored.SessionID != s.vpnResult.SessionID {
		t.Fatalf("managed replay: %+v %v", stored, err)
	}
	if _, _, err := s.repository.ManagedVPNResult(s.ctx, "intent-"+s.suffix, "other-runtime", s.connection.RequestID, s.connection.RequestHash); err == nil {
		t.Fatal("foreign runtime replay accepted")
	}
	s.checkManagedRevocation(t)
}

func (s *runtimeRepositoryScenario) checkManagedRevocation(t *testing.T) {
	t.Helper()
	rollback := errors.New("rollback isolated revocation test")
	err := s.store.DB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`UPDATE sessions SET status='revoked' WHERE id=?`, "auth-"+s.suffix).Error; err != nil {
			return err
		}
		repo := runtimerepo.New(tx)
		_, err := repo.RenewLeases(s.ctx, s.runtimeID, networkprotocol.LeaseRenewRequest{SessionID: s.vpnResult.SessionID, LeaseIDs: []string{s.vpnResult.NetworkLeases[0].ID}, ObservedConfigurationVersion: s.vpnResult.ConfigurationVersion}, s.snapshot, s.connectedAt.Add(time.Second), time.Minute)
		if err == nil {
			t.Error("revoked login session renewed managed VPN")
		}
		configuration, err := repo.EnsureConfiguration(s.ctx, s.runtimeID, s.snapshot, s.connectedAt.Add(time.Second), s.connectedAt.Add(time.Minute))
		if err != nil {
			return err
		}
		if configuration.Desired.WireGuard != nil || configuration.ConfigurationVersion <= s.vpnResult.ConfigurationVersion {
			t.Error("revoked login reused cached tunnel configuration")
		}
		var status string
		if err := tx.Raw(`SELECT status FROM network_runtime_sessions WHERE id=?`, s.vpnResult.SessionID).Row().Scan(&status); err != nil {
			return err
		}
		if status != "revoked" {
			t.Errorf("managed session status=%s after login revocation", status)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
}

func (s *runtimeRepositoryScenario) checkDuplicateManagedDecision(t *testing.T, repo *networkrepo.Repository, q domain.VPNDashboardQuery) {
	t.Helper()
	if err := s.repository.SaveVPNDecision(s.ctx, s.connection.Managed.Decision); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.VPNDashboardRecords(s.ctx, q, s.connectedAt)
	if err != nil || len(rows) != 1 || rows[0].Decision.State != "connected" {
		t.Fatalf("late duplicate request regressed terminal decision: %+v %v", rows, err)
	}
}
