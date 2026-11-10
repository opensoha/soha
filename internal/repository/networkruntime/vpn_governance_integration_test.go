package networkruntime_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha/internal/application/networkcontrol"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	accessrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
)

func (s *runtimeRepositoryScenario) checkVPNGovernance(t *testing.T) {
	t.Helper()
	s.checkMihomoSource(t)
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	control, err := networkcontrol.New(s.repository, schemas, networkcontrol.Options{
		MaxClockSkew: time.Minute, ConfigurationTTL: 5 * time.Minute, LeaseTTL: 4 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	policy := domain.Policy{
		ID: "governance-" + s.suffix, Enabled: true, Effect: domain.PolicyEffectAllow, AccessProfile: domain.ProfileFull,
		SiteIDs: []string{s.siteID}, Modes: []string{domain.ModeExternalVPN}, DeviceManagementStates: []string{domain.DeviceManagementUnknown},
	}
	db := s.store.DB().WithContext(s.ctx)
	s.publishGovernancePolicy(t, db, policy)
	if err := control.RefreshSnapshot(s.ctx); err != nil {
		t.Fatal(err)
	}
	requestID := "vpn-governance-" + s.suffix
	connect := func(ctx context.Context) (networkprotocol.VPNConnectResult, error) {
		return s.connectGovernedVPN(ctx, control, requestID)
	}
	initial := s.vpnGovernanceRows(t)
	deny := policy
	deny.Effect, deny.AccessProfile = domain.PolicyEffectDeny, domain.ProfileDeny

	t.Run("ConcurrentPublicationRejectsStaleAllow", func(t *testing.T) {
		done := make(chan error, 1)
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		defer cancel()
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := s.writeGovernancePolicy(ctx, tx, deny); err != nil {
				return err
			}
			go func() {
				_, err := connect(ctx)
				done <- err
			}()
			return waitForVPNPolicyReader(ctx, tx, done)
		})
		if err != nil {
			t.Fatal(err)
		}
		assertVPNGovernanceError(t, <-done, apperrors.ErrConflict, "stale_vpn_decision")
		if rows := s.vpnGovernanceRows(t); rows != initial {
			t.Fatalf("stale allow wrote rows: before=%+v after=%+v", initial, rows)
		}
	})

	s.publishGovernancePolicy(t, db, policy)
	if err := control.RefreshSnapshot(s.ctx); err != nil {
		t.Fatal(err)
	}
	t.Run("LateWriteFailureRollsBack", func(t *testing.T) {
		// The request row is written after the session, lease, peer and configurations.
		if err := db.Exec(`ALTER TABLE network_vpn_connection_requests ADD CONSTRAINT test_vpn_governance_request_write CHECK (false) NOT VALID`).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := db.Exec(`ALTER TABLE network_vpn_connection_requests DROP CONSTRAINT test_vpn_governance_request_write`).Error; err != nil {
				t.Error(err)
			}
		})
		if _, err := connect(s.ctx); err == nil || !strings.Contains(err.Error(), "test_vpn_governance_request_write") {
			t.Fatalf("late insert failure = %v", err)
		}
		if rows := s.vpnGovernanceRows(t); rows != initial {
			t.Fatalf("failed write changed rows: before=%+v after=%+v", initial, rows)
		}
	})

	var live networkprotocol.VPNConnectResult
	t.Run("RetryCommitsOnce", func(t *testing.T) {
		live = s.checkGovernanceRetry(t, connect, initial)
	})
	if live.SessionID == "" || len(live.NetworkLeases) != 1 {
		t.Fatal("committed VPN session is required for revocation checks")
	}

	s.checkGovernanceRevocation(t, control, db, deny, connect, live)
}

func (s *runtimeRepositoryScenario) checkGovernanceRetry(t *testing.T, connect func(context.Context) (networkprotocol.VPNConnectResult, error), initial vpnGovernanceCounts) networkprotocol.VPNConnectResult {
	t.Helper()
	live, err := connect(s.ctx)
	if err != nil || live.Decision != domain.DecisionAllow || len(live.NetworkLeases) != 1 {
		t.Fatalf("retry = %+v, %v", live, err)
	}
	replay, err := connect(s.ctx)
	if err != nil || replay.SessionID != live.SessionID || len(replay.NetworkLeases) != 1 || replay.NetworkLeases[0].ID != live.NetworkLeases[0].ID {
		t.Fatalf("idempotent replay = %+v, %v", replay, err)
	}
	want := initial
	want.Sessions++
	want.Leases++
	want.Peers++
	want.Configurations += 2
	want.Requests++
	if rows := s.vpnGovernanceRows(t); rows != want {
		t.Fatalf("retry wrote rows %+v, want %+v", rows, want)
	}
	return live
}

func (s *runtimeRepositoryScenario) checkGovernanceRevocation(t *testing.T, control *networkcontrol.Service, db *gorm.DB, deny domain.Policy, connect func(context.Context) (networkprotocol.VPNConnectResult, error), live networkprotocol.VPNConnectResult) {
	t.Helper()
	prior, err := control.CurrentSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	const newTarget = "10.77.0.20"
	newProtectedID := "protected-" + s.suffix
	if err := db.Exec(`INSERT INTO network_access_resources
 (id, space_id, name, kind, target, protocol, ports, protected, path_mode)
 VALUES (?, ?, 'New protected resource', 'ip', ?, 'tcp', '[443]'::jsonb, true, 'automatic')`, newProtectedID, s.spaceID, newTarget).Error; err != nil {
		t.Fatal(err)
	}
	published, err := accessrepo.New(db).PublishPolicySnapshot(s.ctx, domain.PolicySnapshot{
		ContentHash: digest(uuid.NewString()), Policies: []domain.Policy{deny}, ProtectedResourceIDs: []string{s.protectedID, newProtectedID},
		PolicyCount: 1, ProtectedResourceCount: 2, PublishedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	t.Run("ReplayAndRenewRejectRevokedPolicy", func(t *testing.T) {
		before := s.vpnGovernanceRows(t)
		var priorExpiry time.Time
		const leaseExpirySQL = `SELECT expires_at FROM network_runtime_leases WHERE id=?`
		if err := db.Raw(leaseExpirySQL, live.NetworkLeases[0].ID).Row().Scan(&priorExpiry); err != nil {
			t.Fatal(err)
		}
		_, err := connect(s.ctx)
		assertVPNGovernanceError(t, err, apperrors.ErrAccessDenied, "device_governance_revoked")
		_, err = s.repository.RenewLeases(s.ctx, s.runtimeID, networkprotocol.LeaseRenewRequest{
			SessionID: live.SessionID, LeaseIDs: []string{live.NetworkLeases[0].ID}, ObservedConfigurationVersion: live.ConfigurationVersion,
		}, prior, now, time.Minute)
		assertVPNGovernanceError(t, err, apperrors.ErrAccessDenied, "device_governance_revoked")
		if rows := s.vpnGovernanceRows(t); rows != before {
			t.Fatalf("rejected replay or renewal changed rows: before=%+v after=%+v", before, rows)
		}
		var leaseExpiry time.Time
		if err := db.Raw(leaseExpirySQL, live.NetworkLeases[0].ID).Row().Scan(&leaseExpiry); err != nil || !leaseExpiry.Equal(priorExpiry) {
			t.Fatalf("rejected renewal changed lease expiry to %v: %v", leaseExpiry, err)
		}
	})
	t.Run("StaleConfigurationPolicyCannotWriteOrReturnDesired", func(t *testing.T) {
		before := s.vpnGovernanceRows(t)
		cfg, err := s.repository.EnsureConfiguration(s.ctx, s.runtimeID, prior, now, now.Add(time.Minute))
		assertVPNGovernanceError(t, err, apperrors.ErrConflict, "stale_configuration_policy")
		if cfg.ConfigurationVersion != 0 || cfg.Desired.PolicyVersion != 0 {
			t.Fatalf("stale request returned configuration %d at policy %d", cfg.ConfigurationVersion, cfg.Desired.PolicyVersion)
		}
		if rows := s.vpnGovernanceRows(t); rows != before {
			t.Fatalf("stale configuration request changed rows: before=%+v after=%+v", before, rows)
		}
	})
	t.Run("ConfigurationReadRevokesSessionAndPeer", func(t *testing.T) {
		s.checkGovernanceConfigurationRevocation(t, control, db, live, published.PolicyVersion, newProtectedID, newTarget)
	})
}

func (s *runtimeRepositoryScenario) checkGovernanceConfigurationRevocation(t *testing.T, control *networkcontrol.Service, db *gorm.DB, live networkprotocol.VPNConnectResult, policyVersion int, newProtectedID, newTarget string) {
	t.Helper()
	identity := networkidentity.Identity{
		Scope: networkidentity.ScopeNetworkControl, ID: s.runtimeID, Kind: "endpoint", CertificateFingerprint: s.credential2.CertificateFingerprint,
	}
	message, err := control.Configuration(s.ctx, identity)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := networkprotocol.DecodePayload[networkprotocol.ConfigurationDesired](message.Payload)
	if err != nil || cfg.WireGuard != nil || len(cfg.NetworkLeases) != 0 || cfg.ConfigurationVersion <= live.ConfigurationVersion {
		t.Fatalf("configuration after policy revoke = %+v, %v", cfg, err)
	}
	if cfg.PolicyVersion != policyVersion || !slices.Contains(cfg.ProtectedResourceIDs, newProtectedID) {
		t.Fatalf("configuration omitted newly protected resource: policy=%d want=%d ids=%v", cfg.PolicyVersion, policyVersion, cfg.ProtectedResourceIDs)
	}
	if cfg.Mihomo == nil || cfg.Mihomo.Protection == nil || cfg.Mihomo.Protection.PolicyVersion != policyVersion ||
		!slices.Contains(cfg.Mihomo.Protection.CIDRs, newTarget+"/32") {
		t.Fatalf("Mihomo omitted newly protected target: %+v", cfg.Mihomo)
	}
	var status, reason, peerStatus, leaseStatus string
	err = db.Raw(`SELECT s.status,s.revoke_reason,p.status,l.status FROM network_runtime_sessions s
 JOIN network_wireguard_peers p ON p.session_id=s.id JOIN network_runtime_leases l ON l.session_id=s.id WHERE s.id=?`, live.SessionID).
		Row().Scan(&status, &reason, &peerStatus, &leaseStatus)
	if err != nil || status != "revoked" || reason != "device_governance_revoked" || peerStatus != "revoked" || leaseStatus != "revoked" {
		t.Fatalf("revoke persisted session=%q reason=%q peer=%q lease=%q, %v", status, reason, peerStatus, leaseStatus, err)
	}
}

func (s *runtimeRepositoryScenario) connectGovernedVPN(ctx context.Context, service *networkcontrol.Service, requestID string) (networkprotocol.VPNConnectResult, error) {
	payload, err := json.Marshal(networkprotocol.VPNConnectRequest{
		RequestID: requestID, SiteID: s.siteID, NetworkSpaceID: s.spaceID, GatewayID: s.gatewayID, Mode: domain.ModeExternalVPN,
	})
	if err != nil {
		return networkprotocol.VPNConnectResult{}, err
	}
	now := time.Now().UTC()
	raw, err := json.Marshal(networkprotocol.RuntimeMessage{
		SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageID: uuid.NewString(), MessageType: networkprotocol.MessageVPNConnectRequest,
		ProducerID: s.runtimeID, RuntimeID: s.runtimeID, RuntimeKind: "endpoint", OccurredAt: now, ExpiresAt: now.Add(time.Minute), Payload: payload,
	})
	if err != nil {
		return networkprotocol.VPNConnectResult{}, err
	}
	identity := networkidentity.Identity{
		Scope: networkidentity.ScopeNetworkControl, ID: s.runtimeID, Kind: "endpoint", CertificateFingerprint: s.credential2.CertificateFingerprint,
	}
	response, err := service.ConnectVPN(ctx, identity, raw)
	if err != nil {
		return networkprotocol.VPNConnectResult{}, err
	}
	return networkprotocol.DecodePayload[networkprotocol.VPNConnectResult](response.Payload)
}

func (s *runtimeRepositoryScenario) publishGovernancePolicy(t *testing.T, db *gorm.DB, policy domain.Policy) {
	t.Helper()
	if err := s.writeGovernancePolicy(s.ctx, db, policy); err != nil {
		t.Fatal(err)
	}
}

func (s *runtimeRepositoryScenario) writeGovernancePolicy(ctx context.Context, db *gorm.DB, policy domain.Policy) error {
	_, err := accessrepo.New(db).PublishPolicySnapshot(ctx, domain.PolicySnapshot{
		ContentHash: digest(uuid.NewString()), Policies: []domain.Policy{policy}, ProtectedResourceIDs: []string{s.protectedID},
		PolicyCount: 1, ProtectedResourceCount: 1, PublishedAt: time.Now().UTC(),
	})
	return err
}

func waitForVPNPolicyReader(ctx context.Context, tx *gorm.DB, done <-chan error) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM pg_locks WHERE locktype='advisory' AND mode='ShareLock' AND NOT granted
 AND objid::bigint=(hashtext('network-access-policy-snapshot:default:default')::bigint & 4294967295))`).Row().Scan(&waiting); err != nil {
			return err
		}
		if waiting {
			return nil
		}
		select {
		case err := <-done:
			return fmt.Errorf("VPN request completed before reaching the publication lock: %v", err)
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func assertVPNGovernanceError(t *testing.T, err, kind error, code string) {
	t.Helper()
	var business *apperrors.BusinessError
	if !errors.Is(err, kind) || !errors.As(err, &business) || business.Code() != code {
		t.Fatalf("error = %v, want %s (%v)", err, code, kind)
	}
}

type vpnGovernanceCounts struct {
	Sessions, Leases, Peers, Configurations, Requests int
}

func (s *runtimeRepositoryScenario) vpnGovernanceRows(t *testing.T) vpnGovernanceCounts {
	t.Helper()
	var counts vpnGovernanceCounts
	err := s.store.DB().WithContext(s.ctx).Raw(`SELECT
 (SELECT count(*) FROM network_runtime_sessions WHERE runtime_id=?),
 (SELECT count(*) FROM network_runtime_leases WHERE session_id IN (SELECT id FROM network_runtime_sessions WHERE runtime_id=?)),
 (SELECT count(*) FROM network_wireguard_peers WHERE runtime_id=?),
 (SELECT count(*) FROM network_runtime_configurations WHERE runtime_id IN (?,?)),
 (SELECT count(*) FROM network_vpn_connection_requests WHERE runtime_id=?)`,
		s.runtimeID, s.runtimeID, s.runtimeID, s.runtimeID, s.gatewayRuntimeID, s.runtimeID).
		Row().Scan(&counts.Sessions, &counts.Leases, &counts.Peers, &counts.Configurations, &counts.Requests)
	if err != nil {
		t.Fatal(err)
	}
	return counts
}
