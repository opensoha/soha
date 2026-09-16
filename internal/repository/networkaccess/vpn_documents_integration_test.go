package networkaccess_test

import (
	"errors"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *accessRepositoryScenario) checkVPNDocuments(t *testing.T) {
	t.Helper()
	policyID, profileID := "vpn-policy-"+s.suffix, "vpn-profile-"+s.suffix
	policy, err := s.repository.SaveVPNSelectionPolicy(s.ctx, policyID, 0, domain.VPNSelectionPolicyConfig{Name: "priority", Strategy: "priority"}, s.userID, s.now)
	if err != nil || policy.Revision != 1 || policy.PublishedConfiguration != nil {
		t.Fatalf("save policy: %+v %v", policy, err)
	}
	profileConfig := domain.VPNProfileConfig{Name: "office", Enabled: true, SelectionPolicyID: policyID, SiteID: s.site.ID, NetworkSpaceID: s.space.ID, Mode: "external_vpn", GatewayIDs: []string{s.gateway.ID}, Assignments: domain.VPNAssignment{UserIDs: []string{s.userID}}}
	profile, err := s.repository.SaveVPNProfile(s.ctx, profileID, 0, profileConfig, s.userID, s.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.repository.PublishVPNProfile(s.ctx, profileID, 1, 0, s.userID, s.now); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("unpublished policy accepted: %v", err)
	}
	if _, err := s.repository.PublishVPNSelectionPolicy(s.ctx, policyID, 1, 0, s.userID, s.now); err != nil {
		t.Fatal(err)
	}
	profile, err = s.repository.PublishVPNProfile(s.ctx, profileID, 1, 0, s.userID, s.now)
	if err != nil || profile.PublishedRevision != 1 {
		t.Fatalf("publish profile: %+v %v", profile, err)
	}
	if err := s.repository.DeleteVPNSelectionPolicy(s.ctx, policyID, 1, s.now); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("referenced policy deleted: %v", err)
	}
	profileConfig.Enabled = false
	profile, err = s.repository.SaveVPNProfile(s.ctx, profileID, 1, profileConfig, s.userID, s.now.Add(time.Second))
	if err != nil || profile.Revision != 2 || !profile.PublishedConfiguration.Enabled {
		t.Fatalf("draft changed published config: %+v %v", profile, err)
	}
	if _, err := s.repository.SaveVPNProfile(s.ctx, profileID, 1, profileConfig, s.userID, s.now); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	s.checkVPNProfileRevocationAndRollback(t, profileID, policyID)
}

func (s *accessRepositoryScenario) checkVPNProfileRevocationAndRollback(t *testing.T, profileID, policyID string) {
	t.Helper()
	var profile domain.VPNProfile
	var err error
	if err := s.store.DB().Exec(`UPDATE network_runtime_sessions SET vpn_profile_id = ?, status = 'active' WHERE id = ?`, profileID, s.sessionID).Error; err != nil {
		t.Fatal(err)
	}
	profile, err = s.repository.PublishVPNProfile(s.ctx, profileID, 2, 0, s.userID, s.now.Add(2*time.Second))
	if err != nil || profile.PublishedConfiguration.Enabled {
		t.Fatalf("disable profile: %+v %v", profile, err)
	}
	var status string
	if err := s.store.DB().Raw(`SELECT status FROM network_runtime_sessions WHERE id = ?`, s.sessionID).Row().Scan(&status); err != nil || status != "revoked" {
		t.Fatalf("profile disable did not revoke: %q %v", status, err)
	}
	profile, err = s.repository.PublishVPNProfile(s.ctx, profileID, 2, 1, s.userID, s.now.Add(3*time.Second))
	if err != nil || profile.Revision != 3 || profile.PublishedRevision != 3 || !profile.Configuration.Enabled {
		t.Fatalf("rollback must create a new revision: %+v %v", profile, err)
	}
	history, err := s.repository.VPNProfileRevisions(s.ctx, profileID)
	if err != nil || len(history) != 3 || history[1].Configuration.Enabled || !history[2].Configuration.Enabled {
		t.Fatalf("immutable revisions changed: %+v %v", history, err)
	}
	if err := s.repository.DeleteVPNProfile(s.ctx, profileID, 3, s.now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.repository.DeleteVPNSelectionPolicy(s.ctx, policyID, 1, s.now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
}
