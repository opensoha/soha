package networkruntime_test

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	runtimerepo "github.com/opensoha/soha/internal/repository/networkruntime"
	"gorm.io/gorm"
)

func (s *runtimeRepositoryScenario) checkGatewayDNSReconfigurationScope(t *testing.T) {
	t.Run("gateway_dns_reconfiguration_scope", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Microsecond)
		until := now.Add(time.Hour)
		affected, unrelated := "dns-affected-"+s.suffix, "dns-unrelated-"+s.suffix
		peerGateway, peerRuntime := "dns-peer-gateway-"+s.suffix, "dns-peer-runtime-"+s.suffix
		peerSite := "dns-peer-site-" + s.suffix
		s.dnsReconfigurationExec(t, `INSERT INTO network_access_sites(id,name,status) VALUES(?,?,'active')`, peerSite, "DNS peer site "+s.suffix)
		s.dnsReconfigurationExec(t, `INSERT INTO network_access_gateways
 (id,runtime_id,site_id,name,status,administrative_status,public_endpoint_host,public_endpoint_port,overlay_cidr,routing_mode,mtu,persistent_keepalive_seconds,dns_servers)
 VALUES(?,?,?,'DNS peer gateway','offline','active','vpn.example.test',51821,'100.97.0.0/24','routed',1420,25,'[]')`, peerGateway, peerRuntime, peerSite)
		resource := "dns-resource-" + s.suffix
		s.dnsReconfigurationExec(t, `INSERT INTO network_access_resources(id,space_id,name,kind,target,protocol,ports,protected,path_mode)
 VALUES(?,?,'DNS application','fqdn','app.example.test','tcp','[443]',true,'automatic')`, resource, s.spaceID)
		s.dnsReconfigurationExec(t, `INSERT INTO network_site_dns_mappings
 (resource_id,runtime_id,credential_id,site_id,hostname,addresses,fingerprint,resolvers,policy_version,configuration_version,observed_at,expires_at)
 VALUES(?,?,?,?,'app.example.test','["10.77.0.10"]',?,'["10.77.0.53"]',?,1,?,?)`, resource, s.gatewayRuntimeID, s.gatewayCredential.ID, s.siteID, digest(resource), s.policyVersion, now, now.Add(time.Minute))
		// The unrelated runtime reconnected after an earlier DNS revocation.
		s.seedDNSReconfigurationSession(t, unrelated+"-old", unrelated, "external_vpn_ztna", "revoked", "resource_dns_changed", until)
		s.seedDNSReconfigurationSession(t, affected+"-session", affected, "external_vpn_ztna", "active", "", until)
		s.seedDNSReconfigurationSession(t, unrelated+"-session", unrelated, "external_vpn_ztna", "active", "", until)
		s.seedDNSResourceLease(t, affected+"-lease", affected+"-session", resource, "active", now, until)
		for _, state := range []string{"issued", "revoking"} {
			session := affected + "-" + state
			s.seedDNSReconfigurationSession(t, session, affected, "external_direct_ztna", "active", "", until)
			s.seedDNSResourceLease(t, session+"-lease", session, resource, state, now, until)
		}
		for i, runtime := range []string{affected, unrelated} {
			s.seedDNSPeer(t, runtime+"-peer", peerGateway, runtime, runtime+"-session", []string{"100.97.0.4/32", "100.97.0.5/32"}[i], now, until)
		}
		runtimes := []string{s.gatewayRuntimeID, peerRuntime, affected, unrelated}
		for _, runtime := range runtimes {
			s.dnsReconfigurationExec(t, `INSERT INTO network_runtime_configurations
 (runtime_id,configuration_version,policy_version,desired_payload,desired_hash,valid_until)
 VALUES(?,9999,?,'{}',?,?)`, runtime, s.policyVersion, digest(runtime), until)
		}
		s.dnsReconfigurationExec(t, `UPDATE network_access_gateways SET dns_servers=dns_servers WHERE id=?`, s.gatewayID)
		for _, runtime := range runtimes {
			s.assertDNSConfigurationExpiry(t, runtime, false)
		}
		s.dnsReconfigurationExec(t, `UPDATE network_access_gateways SET dns_servers='["10.77.0.54"]' WHERE id=?`, s.gatewayID)
		for _, runtime := range runtimes {
			s.assertDNSConfigurationExpiry(t, runtime, runtime != unrelated)
		}
		s.assertDNSReconfigurationStatus(t, "network_runtime_sessions", affected+"-session", "revoked")
		s.assertDNSReconfigurationStatus(t, "network_runtime_sessions", unrelated+"-session", "active")
		s.assertDNSReconfigurationStatus(t, "network_runtime_leases", affected+"-lease", "revoked")
		s.assertDNSReconfigurationStatus(t, "network_wireguard_peers", affected+"-peer", "revoked")
		s.assertDNSReconfigurationStatus(t, "network_wireguard_peers", unrelated+"-peer", "active")
		for _, state := range []string{"issued", "revoking"} {
			s.assertDNSReconfigurationStatus(t, "network_runtime_sessions", affected+"-"+state, "revoked")
			s.assertDNSReconfigurationStatus(t, "network_runtime_leases", affected+"-"+state+"-lease", "revoked")
		}
		// An update with no affected DNS mappings must not revisit either
		// runtime's historical revocation after their configurations refresh.
		for _, runtime := range runtimes {
			s.dnsReconfigurationExec(t, `UPDATE network_runtime_configurations SET valid_until=? WHERE runtime_id=? AND configuration_version=9999`, until, runtime)
		}
		s.dnsReconfigurationExec(t, `UPDATE network_access_gateways SET administrative_status='disabled' WHERE id=?`, peerGateway)
		for _, runtime := range runtimes {
			s.assertDNSConfigurationExpiry(t, runtime, runtime == peerRuntime)
		}
		s.checkDNSObservationRevocation(t, resource, affected, unrelated, now, until)
	})
}

func (s *runtimeRepositoryScenario) checkDNSObservationRevocation(t *testing.T, resource, affected, unrelated string, now, until time.Time) {
	t.Helper()
	for _, state := range []string{"issued", "active", "revoking"} {
		session := affected + "-refreshed-" + state
		s.seedDNSReconfigurationSession(t, session, affected, "external_direct_ztna", "active", "", until)
		s.seedDNSResourceLease(t, session+"-lease", session, resource, state, now, until)
	}
	// An unrelated WireGuard session can coexist with direct-resource sessions
	// on one runtime. DNS revocation must not turn a runtime ID into a blanket revoke.
	sibling := affected + "-unrelated"
	s.seedDNSReconfigurationSession(t, sibling, affected, "external_vpn_ztna", "active", "", until)
	s.seedDNSResourceLease(t, sibling+"-lease", sibling, s.protectedID, "active", now, until)
	s.seedDNSPeer(t, sibling+"-peer", s.gatewayID, affected, sibling, "100.96.0.6/32", now, until)
	err := s.store.DB().WithContext(s.ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('network-wireguard-mutations'))`).Error; err != nil {
			return err
		}
		return runtimerepo.RevokeDNSResourceForTest(tx, resource, now)
	})
	if err != nil {
		t.Fatalf("DNS observation revocation: %v", err)
	}
	for _, state := range []string{"issued", "active", "revoking"} {
		session := affected + "-refreshed-" + state
		s.assertDNSReconfigurationStatus(t, "network_runtime_sessions", session, "revoked")
		s.assertDNSReconfigurationStatus(t, "network_runtime_leases", session+"-lease", "revoked")
	}
	s.assertDNSReconfigurationStatus(t, "network_runtime_sessions", sibling, "active")
	s.assertDNSReconfigurationStatus(t, "network_runtime_leases", sibling+"-lease", "active")
	s.assertDNSReconfigurationStatus(t, "network_wireguard_peers", sibling+"-peer", "active")
	s.assertDNSConfigurationExpiry(t, affected, true)
	s.assertDNSConfigurationExpiry(t, unrelated, false)
}

func (s *runtimeRepositoryScenario) dnsReconfigurationExec(t *testing.T, statement string, args ...any) {
	t.Helper()
	if err := s.store.DB().WithContext(s.ctx).Exec(statement, args...).Error; err != nil {
		t.Fatal(err)
	}
}

func (s *runtimeRepositoryScenario) seedDNSReconfigurationSession(t *testing.T, id, runtime, mode, status, reason string, until time.Time) {
	t.Helper()
	s.dnsReconfigurationExec(t, `INSERT INTO network_runtime_sessions
 (id,runtime_id,subject_id,device_id,site_id,mode,access_profile,status,policy_version,valid_until,revoke_reason)
 VALUES(?,?,?,?,?,?,'restricted',?,?,?,?)`, id, runtime, s.subjectID, s.deviceID, s.siteID, mode, status, s.policyVersion, until, reason)
}

func (s *runtimeRepositoryScenario) seedDNSResourceLease(t *testing.T, id, session, resource, status string, now, until time.Time) {
	t.Helper()
	s.dnsReconfigurationExec(t, `INSERT INTO network_runtime_leases
 (id,session_id,lease_kind,subject_id,device_id,resource_ids,policy_version,status,issued_at,expires_at)
 VALUES(?,?,'resource',?,?,jsonb_build_array(?::text),?,?,?,?)`, id, session, s.subjectID, s.deviceID, resource, s.policyVersion, status, now, until)
}

func (s *runtimeRepositoryScenario) seedDNSPeer(t *testing.T, id, gateway, runtime, session, address string, now, until time.Time) {
	t.Helper()
	key := sha256.Sum256([]byte(id))
	s.dnsReconfigurationExec(t, `INSERT INTO network_wireguard_peers
 (id,gateway_id,runtime_id,device_id,session_id,public_key,overlay_address,status,expires_at,created_at)
 VALUES(?,?,?,?,?,?,?::inet,'active',?,?)`, id, gateway, runtime, s.deviceID, session, base64.StdEncoding.EncodeToString(key[:]), address, until, now)
}

func (s *runtimeRepositoryScenario) assertDNSConfigurationExpiry(t *testing.T, runtime string, wantExpired bool) {
	t.Helper()
	var expired bool
	if err := s.store.DB().WithContext(s.ctx).Raw(`SELECT valid_until<=CURRENT_TIMESTAMP FROM network_runtime_configurations WHERE runtime_id=? AND configuration_version=9999`, runtime).Row().Scan(&expired); err != nil || expired != wantExpired {
		t.Fatalf("configuration %s expired=%v, want %v: %v", runtime, expired, wantExpired, err)
	}
}

func (s *runtimeRepositoryScenario) assertDNSReconfigurationStatus(t *testing.T, table, id, want string) {
	t.Helper()
	var status string
	if err := s.store.DB().WithContext(s.ctx).Table(table).Select("status").Where("id=?", id).Row().Scan(&status); err != nil || status != want {
		t.Fatalf("%s %s status=%s, want %s: %v", table, id, status, want, err)
	}
}
