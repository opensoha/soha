package handlers

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	controlhttp "github.com/opensoha/soha/internal/api/networkcontrol"
	access "github.com/opensoha/soha/internal/application/access"
	appnetworkaccess "github.com/opensoha/soha/internal/application/networkaccess"
	control "github.com/opensoha/soha/internal/application/networkcontrol"
	proxyapp "github.com/opensoha/soha/internal/application/networkproxy"
	publication "github.com/opensoha/soha/internal/application/proxypublication"
	accessdomain "github.com/opensoha/soha/internal/domain/access"
	audit "github.com/opensoha/soha/internal/domain/audit"
	identity "github.com/opensoha/soha/internal/domain/identity"
	proxy "github.com/opensoha/soha/internal/domain/networkproxy"
	operation "github.com/opensoha/soha/internal/domain/operation"
	deployment "github.com/opensoha/soha/internal/domain/proxydeployment"
	pubdomain "github.com/opensoha/soha/internal/domain/proxypublication"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/infrastructure/proxysource"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	proxyrepo "github.com/opensoha/soha/internal/repository/networkproxy"
	runtimerepo "github.com/opensoha/soha/internal/repository/networkruntime"
	deployrepo "github.com/opensoha/soha/internal/repository/proxydeployment"
	pubrepo "github.com/opensoha/soha/internal/repository/proxypublication"
	pgtest "github.com/opensoha/soha/internal/testsupport/postgres"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// PostgreSQL, migrations, permission resolution, compiler, publication, enrolled
// certificate authorization and HTTP handlers are real. Platform login, installed
// scope/pool and member apply observations are fixtures; no engine is executed.
func TestProxyPublicationHTTPWithPostgres(t *testing.T) {
	f := newPublicationPG(t)
	t.Run("sources_manual_conversion_cas", f.sourcesScenario)
	if t.Failed() {
		return
	}
	t.Run("publication_readback_activation", f.publicationScenario)
	if t.Failed() {
		return
	}
	t.Run("managed_member_atomicity_and_drain", f.atomicityScenario)
	if t.Failed() {
		return
	}
	t.Run("device_projection_renewal_and_revocation", f.projectionScenario)
	if t.Failed() {
		return
	}
	t.Run("source_security_diff", f.sourceSecurityScenario)
	if t.Failed() {
		return
	}
	t.Run("direct_projection_isolation", f.directScenario)
	if t.Failed() {
		return
	}
	t.Run("dangling_rule_repair", f.danglingRuleScenario)
	if t.Failed() {
		return
	}
	f.rollback()
	t.Run("source_credential_transaction_race", f.concurrentSourceScenario)
}

type publicationPG struct {
	t          *testing.T
	db         *gorm.DB
	baseDB     *gorm.DB
	rollback   func()
	race       *publicationRaceSeed
	nodes      []string
	ctx        context.Context
	router     *gin.Engine
	control    *gin.Engine
	server     *httptest.Server
	clients    map[string]*http.Client
	store      *pubrepo.Repository
	proxies    *proxyrepo.Repository
	management *proxyapp.Management
	bundles    *publication.Bundles
	sources    *publication.Sources
	pool       *publicationPGPool
	user       string
	node       string
	source     api.NetworkProxySourceInput
	bundle     api.NetworkProxyBundle
}

func pgRequire(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func (f *publicationPG) sql(t *testing.T, query string, args ...any) {
	t.Helper()
	pgRequire(t, f.db.Exec(query, args...).Error)
}
func newPublicationPG(t *testing.T) *publicationPG {
	t.Helper()
	portText := os.Getenv("SOHA_PROXY_PUBLICATION_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_PROXY_PUBLICATION_TEST_POSTGRES_PORT for disposable PG acceptance")
	}
	port, err := strconv.Atoi(portText)
	pgRequire(t, err)
	// Never log connection errors containing the private connection string.
	store, err := dbstore.New(pgtest.Configure(t, config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: os.Getenv("SOHA_PROXY_PUBLICATION_TEST_POSTGRES_DATABASE"), User: os.Getenv("SOHA_PROXY_PUBLICATION_TEST_POSTGRES_USER"), Password: os.Getenv("SOHA_TEST_POSTGRES_PASSWORD"), SSLMode: "disable", MaxOpenConns: 2, MaxIdleConns: 2}), zap.NewNop())
	if err != nil {
		t.Fatal("cannot open disposable PostgreSQL")
	}
	t.Cleanup(func() { _ = store.Close() })
	pgRequire(t, store.MigrateFromFile(context.Background(), filepath.Join("..", "..", "..", "migrations", "postgres")))
	db := store.DB().Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Begin()
	pgRequire(t, db.Error)
	var migrations int64
	pgRequire(t, db.Raw("SELECT count(*) FROM schema_migrations WHERE filename LIKE '%0146_proxy_publication.sql' OR filename LIKE '%0147_proxy_deployment.sql' OR filename LIKE '%0148_proxy_runtime_commands.sql' OR filename LIKE '%0149_proxy_source_invalidation.sql'").Scan(&migrations).Error)
	if migrations != 4 {
		t.Fatal("publication migration receipts missing")
	}
	var rollbackOnce sync.Once
	rollback := func() { rollbackOnce.Do(func() { pgRequire(t, db.Rollback().Error) }) }
	t.Cleanup(rollback)
	f := &publicationPG{t: t, db: db, baseDB: store.DB().Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}), rollback: rollback, ctx: context.Background(), user: uuid.NewString(), clients: map[string]*http.Client{}}
	var version string
	pgRequire(t, db.Raw("SELECT version()").Scan(&version).Error)
	t.Log(version)
	key, err := keyring.NewKey("publication-pg", "synthetic-test-encryption-key", time.Now(), nil)
	pgRequire(t, err)
	keys, err := keyring.New(key, nil)
	pgRequire(t, err)
	f.sql(t, "INSERT INTO users(id,username,email,status) VALUES(?,?,?,'active')", f.user, f.user, f.user+"@example.test")
	f.sql(t, "INSERT INTO network_access_sites(id,name,status) VALUES('px-site','PX site','active')")
	f.sql(t, "INSERT INTO network_access_devices(id,owner_user_id,name,platform,status,site_id) VALUES('px-device',?,'PX device','macos','active','px-site')", f.user)
	f.sql(t, "INSERT INTO network_access_policy_snapshots(content_hash,policies,policy_count,protected_resource_count) VALUES(?, '[]',0,0)", "sha256:"+strings.Repeat("a", 64))
	for _, id := range []string{"px-master", "px-a", "px-b"} {
		f.sql(t, "INSERT INTO network_proxy_instances(id,name,engine) VALUES(?,?,'mihomo')", id, id)
	}
	f.sql(t, `UPDATE network_proxy_instances SET binding='{"siteId":"px-site","deviceId":"px-device","resourceIds":[]}'::jsonb, desired_protection=jsonb_build_object('policyVersion',(SELECT max(policy_version) FROM network_access_policy_snapshots),'cidrs','[]'::jsonb,'hosts','[]'::jsonb)`)
	members := []deployment.Member{{RuntimeID: "px-a", InstanceID: "pod-a", Ordinal: 0}, {RuntimeID: "px-b", InstanceID: "pod-b", Ordinal: 1}}
	hash := (deployment.Slot{Members: members}).MemberSetHash()
	raw, _ := json.Marshal(members)
	f.sql(t, "INSERT INTO network_proxy_deployments(proxy_id,slot_id,generation,state,owner_epoch,member_set_hash,members) VALUES('px-master','px-slot',1,'{}',1,?,?::jsonb)", hash, string(raw))
	for _, m := range members {
		f.sql(t, "INSERT INTO network_proxy_deployment_members(runtime_id,proxy_id,instance_id,ordinal) VALUES(?,'px-master',?,?)", m.RuntimeID, m.InstanceID, m.Ordinal)
	}
	f.setupPublicationServices(t, keys, hash)
	f.setupPublicationRoutes(t, keys)
	f.source = api.NetworkProxySourceInput{Name: "Synthetic source", Format: "manual", DeliveryMode: "mapped_nodes", Nodes: []api.NetworkProxyNode{{NodeID: "node-a", Label: "private alias", Protocol: "http", Server: "upstream.example.test", Port: 8080, Username: "private-user", Secret: "private-upstream-secret"}}}
	f.source.Nodes = append(f.source.Nodes, api.NetworkProxyNode{Protocol: "http", Server: "surviving-upstream.example.test", Port: 8081, Secret: "surviving-secret"})
	ir, err := proxysource.NewCompiler().Compile(f.source)
	pgRequire(t, err)
	f.node = ir.Nodes[0].NodeID
	for _, n := range ir.Nodes {
		f.nodes = append(f.nodes, n.NodeID)
	}
	return f
}
func (f *publicationPG) request(t *testing.T, method, path string, input any, viewer bool) []byte {
	t.Helper()
	raw, _ := json.Marshal(input)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if viewer {
		req.Header.Set("X-Test-Role", "viewer")
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return append([]byte(strconv.Itoa(w.Code)+"\n"), w.Body.Bytes()...)
}
func publicationResponse(t *testing.T, raw []byte, status int, out any) []byte {
	t.Helper()
	parts := bytes.SplitN(raw, []byte("\n"), 2)
	if string(parts[0]) != strconv.Itoa(status) {
		t.Fatalf("HTTP status %s, expected %d (response withheld)", parts[0], status)
	}
	if out != nil {
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		pgRequire(t, json.Unmarshal(parts[1], &envelope))
		pgRequire(t, json.Unmarshal(envelope.Data, out))
	}
	return parts[1]
}
func (f *publicationPG) sourcesScenario(t *testing.T) {
	publicationResponse(t, f.request(t, "PUT", "/instances/px-master/sources/source", f.source, true), 403, nil)
	var source api.NetworkProxySource
	body := publicationResponse(t, f.request(t, "PUT", "/instances/px-master/sources/source", f.source, false), 200, &source)
	if source.Revision != 1 || !source.Configured {
		t.Fatal("missing configured revision")
	}
	f.noUpstream(t, body)
	body = publicationResponse(t, f.request(t, "GET", "/instances/px-master/sources/source", nil, true), 200, &source)
	f.noUpstream(t, body)
	record, err := f.store.GetSource(f.ctx, "px-master", "source")
	pgRequire(t, err)
	if strings.Contains(record.EncryptedInput, "private-upstream-secret") || strings.Contains(record.EncryptedIR, "upstream.example.test") {
		t.Fatal("plaintext persisted")
	}
	preview := api.NetworkProxySourceInput{Name: "Text", Format: "mihomo_yaml", Content: "proxies:\n  - name: A\n    type: http\n    server: upstream.example.test\n    port: 8080\n"}
	var result api.NetworkProxyConversionPreview
	publicationResponse(t, f.request(t, "POST", "/instances/px-master/preview", preview, false), 200, &result)
	if !result.Supported || len(result.Nodes) != 1 {
		t.Fatal("text conversion failed")
	}
	preview.Content += "external-controller: private-controller\n"
	body = publicationResponse(t, f.request(t, "POST", "/instances/px-master/preview", preview, false), 200, &result)
	if result.Supported || len(result.Errors) == 0 || result.Errors[0].Code != "unsupported_field" {
		t.Fatal("unknown field not typed")
	}
	f.noUpstream(t, body)
	stale := f.source
	stale.Name = "must not persist"
	publicationResponse(t, f.request(t, "PUT", "/instances/px-master/sources/source", stale, false), 409, nil)
	read, err := f.store.GetSource(f.ctx, "px-master", "source")
	pgRequire(t, err)
	if read.Public.Revision != 1 || read.Public.Name != f.source.Name || read.EncryptedIR != record.EncryptedIR {
		t.Fatal("CAS partially wrote")
	}
}
func (f *publicationPG) noUpstream(t *testing.T, body []byte) {
	t.Helper()
	for _, secret := range []string{"upstream.example.test", "private-upstream-secret", "private-user", "private alias", "private-controller", "surviving-upstream.example.test", "surviving-secret", "added-c.example.test"} {
		if bytes.Contains(body, []byte(secret)) {
			t.Fatal("private source material disclosed")
		}
	}
}
func (f *publicationPG) prepare(t *testing.T, id, mode string) api.NetworkProxyBundle {
	t.Helper()
	input := api.NetworkProxyBundleInput{RequestID: id, SourceID: "source", SourceRevision: f.source.ExpectedRevision + 1, DeliveryMode: api.NetworkProxyDeliveryMode(mode), Scope: api.NetworkProxyScope{UserIDs: []string{f.user}, DeviceIDs: []string{"px-device"}, NodeIDs: append([]string(nil), f.nodes...)}}
	if mode != "direct_subscription" {
		input.Endpoints = []api.NetworkProxyEndpointRef{{EndpointID: "px-entry"}}
	}
	var out api.NetworkProxyBundle
	publicationResponse(t, f.request(t, "PUT", "/instances/px-master/bundles/"+id, input, false), 202, &out)
	return out
}
func (f *publicationPG) publicationScenario(t *testing.T) {
	f.bundle = f.prepare(t, "mapped", "mapped_nodes")
	activate := api.NetworkProxyBundleActivateInput{ExpectedRevision: 1, RequestID: "activate"}
	publicationResponse(t, f.request(t, "POST", "/instances/px-master/bundles/mapped/activate", activate, false), 409, nil)
	for _, id := range []string{"px-a", "px-b"} {
		states, err := f.proxies.ObserveMemberConfigurations(f.ctx, []string{id})
		pgRequire(t, err)
		if len(states) != 1 {
			t.Fatal("missing real member desired")
		}
		s := states[0]
		receipt := api.NetworkProxyBundleReadback{BundleID: "mapped", Revision: 1, ConfigurationRevision: s.DesiredRevision, ServerHash: f.bundle.ServerHash, ProcessEpoch: "fixture-process", IngressReady: true, ProtectionReady: true, ObservedAt: time.Now().UTC()}
		f.controlRequest(t, id, "POST", "/proxies/"+id+"/ready", receipt, 409, nil)
		// Explicit member-applied fixture, read back from real PG by ServerReady.
		f.sql(t, "UPDATE network_proxy_instances SET observed_revision=desired_revision,observed_hash=?,observed_policy_version=?,process_epoch='fixture-process',last_seen_at=now() WHERE id=?", s.DesiredHash, s.PolicyVersion, id)
		pgRequire(t, f.proxies.GrantConfigurationLease(f.ctx, id, s.DesiredRevision, s.DesiredHash, time.Now().Add(4*time.Minute), time.Now()))
		f.controlRequest(t, id, "POST", "/proxies/"+id+"/ready", receipt, 200, nil)
	}
	publicationResponse(t, f.request(t, "POST", "/instances/px-master/bundles/mapped/activate", activate, true), 403, nil)
	publicationResponse(t, f.request(t, "POST", "/instances/px-master/bundles/mapped/activate", activate, false), 202, &f.bundle)
	if f.bundle.State != "activated" {
		t.Fatal("not activated")
	}
	f.profileBindingScenario(t)
	f.seedProfile(t, "mapped", "mapped_nodes")
}
func (f *publicationPG) seedProfile(t *testing.T, bundle, mode string) {
	record, err := f.store.GetBundle(f.ctx, "px-master", bundle)
	pgRequire(t, err)
	sourceRevision := record.Public.SourceRevision
	f.sql(t, "DELETE FROM network_mihomo_profiles WHERE id='px-profile'")
	f.sql(t, `INSERT INTO network_mihomo_profiles(id,device_id,name,mode,status,mixed_port,controller_port,dns_mode,selector_group,fail_closed,created_at,updated_at,delivery_mode,bundle_id,source_revision) VALUES('px-profile','px-device','PX','managed_follow','active',7890,9090,'disabled','Proxy',true,now(),now(),?,?,?)`, mode, bundle, sourceRevision)
	protection, err := f.store.CurrentProtection(f.ctx)
	pgRequire(t, err)
	var version int
	pgRequire(t, f.db.Raw("SELECT max(policy_version) FROM network_access_policy_snapshots").Scan(&version).Error)
	desired := networkprotocol.ConfigurationDesired{ConfigurationVersion: 1, PolicyVersion: version, ValidUntil: time.Now().Add(4 * time.Minute), Mihomo: &networkprotocol.MihomoConfiguration{ProfileID: "px-profile", BundleID: bundle, DeliveryMode: mode, SourceRevision: sourceRevision, Protection: &networkprotocol.MihomoProtection{PolicyVersion: version, CIDRs: protection.CIDRs, Hosts: protection.Hosts}}}
	raw, _ := json.Marshal(desired)
	f.sql(t, "DELETE FROM network_runtime_configurations WHERE runtime_id='px-endpoint'")
	f.sql(t, "INSERT INTO network_runtime_configurations(runtime_id,configuration_version,policy_version,desired_payload,desired_hash,valid_until) VALUES('px-endpoint',1,?,?::jsonb,?,?)", version, string(raw), "sha256:"+strings.Repeat("b", 64), desired.ValidUntil)
}
func (f *publicationPG) projectionScenario(t *testing.T) {
	var a, b api.NetworkProxyEmployeeProjection
	body := f.controlRequest(t, "px-endpoint", "GET", "/profiles/px-profile/projection?processEpoch=boot", nil, 200, &a)
	f.noUpstream(t, body)
	f.controlRequest(t, "px-endpoint", "GET", "/profiles/px-profile/projection?processEpoch=boot", nil, 200, &b)
	if a.Credential.CredentialID == "" || a.Credential.CredentialID != b.Credential.CredentialID || a.Credential.Secret != b.Credential.Secret {
		t.Fatal("same scope rotated credential")
	}
	receipt := api.NetworkProxyProjectionApplied{BundleID: a.BundleID, Revision: a.Revision, ProjectionHash: a.ProjectionHash, ProcessEpoch: a.ProcessEpoch, Status: "rejected", ReasonCode: "private-client-diagnostic"}
	f.controlRequest(t, "px-endpoint", "POST", "/profiles/px-profile/receipt", receipt, 200, nil)
	var storedRaw []byte
	pgRequire(t, f.db.Raw("SELECT receipt FROM network_proxy_projection_receipts WHERE credential_id='px-endpoint' AND profile_id='px-profile' AND process_epoch='boot'").Row().Scan(&storedRaw))
	var stored api.NetworkProxyProjectionApplied
	pgRequire(t, json.Unmarshal(storedRaw, &stored))
	if stored.Status != "rejected" || stored.ReasonCode != "projection_apply_rejected" {
		t.Fatal("rejected receipt was not normalized and persisted")
	}
	receipt.Status = "failed"
	f.controlRequest(t, "px-endpoint", "POST", "/profiles/px-profile/receipt", receipt, 409, nil)
	var persistedStatus string
	pgRequire(t, f.db.Raw("SELECT receipt->>'status' FROM network_proxy_projection_receipts WHERE credential_id='px-endpoint' AND profile_id='px-profile' AND process_epoch='boot'").Scan(&persistedStatus).Error)
	if persistedStatus != "rejected" {
		t.Fatal("invalid receipt changed persisted status")
	}
	f.controlRequest(t, "px-other", "GET", "/profiles/px-profile/projection?processEpoch=boot", nil, 403, nil)
	f.controlRequest(t, "px-endpoint", "GET", "/profiles/px-profile/direct?processEpoch=boot", nil, 403, nil)
	input := api.NetworkProxyIngressCheckInput{BundleID: "mapped", Revision: 1, CredentialEpoch: a.CredentialEpoch, CredentialID: a.Credential.CredentialID, CredentialSecret: a.Credential.Secret, NodeID: f.node, ProcessEpoch: "fixture-process", DestinationHost: "8.8.8.8", DestinationPort: 443, Network: "tcp"}
	var decision api.NetworkProxyIngressCheckResult
	f.controlRequest(t, "px-a", "POST", "/proxies/px-a/check", input, 200, &decision)
	if !decision.Allowed {
		t.Fatal("valid ingress denied")
	}
	// Stale CAS must preserve issued credentials; successful source edit revokes them.
	publicationResponse(t, f.request(t, "PUT", "/instances/px-master/sources/source", f.source, false), 409, nil)
	f.controlRequest(t, "px-a", "POST", "/proxies/px-a/check", input, 200, &decision)
	if !decision.Allowed {
		t.Fatal("failed CAS revoked credential")
	}
	changed := f.source
	changed.ExpectedRevision = 1
	changed.Nodes = append([]api.NetworkProxyNode(nil), changed.Nodes[1:]...)
	publicationResponse(t, f.request(t, "PUT", "/instances/px-master/sources/source", changed, false), 200, nil)
	f.controlRequest(t, "px-a", "POST", "/proxies/px-a/check", input, 200, &decision)
	if decision.Allowed {
		t.Fatal("old credential survived source update")
	}
	f.controlRequest(t, "px-endpoint", "GET", "/profiles/px-profile/projection?processEpoch=boot", nil, 200, &b)
	if len(b.Nodes) != 1 || b.Nodes[0].NodeID != f.nodes[1] || b.Credential.CredentialID != a.Credential.CredentialID || b.Credential.Secret != a.Credential.Secret {
		t.Fatal("source removal did not narrow to surviving credential scope")
	}
	f.source = changed
	f.race = capturePublicationRace(t, f, b.Credential.CredentialID)
}
func (f *publicationPG) directScenario(t *testing.T) {
	input := api.NetworkProxySourceInput{Name: "Two-node direct source", Format: "mihomo_yaml", DeliveryMode: "direct_subscription", Content: "proxies:\n  - name: A\n    type: http\n    server: upstream.example.test\n    port: 8080\n    username: private-user\n    password: private-upstream-secret\n  - name: B\n    type: http\n    server: excluded-upstream.example.test\n    port: 8081\n    password: excluded-node-secret\nproxy-groups:\n  - name: Both\n    type: select\n    proxies: [A, B]\n"}
	publicationResponse(t, f.request(t, "PUT", "/instances/px-master/sources/direct-source", input, false), 200, nil)
	ir, err := proxysource.NewCompiler().Compile(input)
	pgRequire(t, err)
	scopeA := ir.Nodes[0].NodeID
	bundleInput := api.NetworkProxyBundleInput{RequestID: "direct", SourceID: "direct-source", SourceRevision: 1, DeliveryMode: "direct_subscription", Scope: api.NetworkProxyScope{UserIDs: []string{f.user}, DeviceIDs: []string{"px-device"}, NodeIDs: []string{scopeA}}}
	var bundle api.NetworkProxyBundle
	publicationResponse(t, f.request(t, "PUT", "/instances/px-master/bundles/direct", bundleInput, false), 202, &bundle)
	publicationResponse(t, f.request(t, "POST", "/instances/px-master/bundles/direct/activate", api.NetworkProxyBundleActivateInput{ExpectedRevision: bundle.Revision, RequestID: "activate-direct"}, false), 202, nil)
	f.seedProfile(t, "direct", "direct_subscription")
	f.controlRequest(t, "px-endpoint", "GET", "/profiles/px-profile/projection?processEpoch=direct-boot", nil, 403, nil)
	var out api.NetworkProxyDirectProjection
	f.controlRequest(t, "px-endpoint", "GET", "/profiles/px-profile/direct?processEpoch=direct-boot", nil, 200, &out)
	if out.UpstreamDisclosure != "upstream_parameters" || !strings.Contains(out.Content, "upstream.example.test") {
		t.Fatal("direct disclosure missing")
	}
	raw, _ := json.Marshal(out)
	for _, marker := range []string{"excluded-upstream.example.test", "excluded-node-secret", ir.Nodes[1].NodeID} {
		if bytes.Contains(raw, []byte(marker)) {
			t.Fatal("excluded direct node disclosed")
		}
	}
	if len(out.Groups) != 1 || len(out.Groups[0].NodeIDs) != 1 || out.Groups[0].NodeIDs[0] != scopeA || (out.Groups[0].SelectedNodeID != "" && out.Groups[0].SelectedNodeID != scopeA) {
		t.Fatal("direct group retains dangling node")
	}
	f.directSecurity(t, input, ir)
	var issued int64
	pgRequire(t, f.db.Raw("SELECT count(*) FROM network_proxy_employee_credentials WHERE bundle_id='direct'").Scan(&issued).Error)
	if issued != 1 {
		t.Fatal("direct delivery was not persisted")
	}
	pgRequire(t, f.store.SaveRules(f.ctx, "px-master", "", api.NetworkProxyRules{OrgRevision: 1, Rules: []api.NetworkProxyRule{}}, 0, 0))
	pgRequire(t, f.db.Raw("SELECT count(*) FROM network_proxy_employee_credentials WHERE instance_id='px-master'").Scan(&issued).Error)
	if issued != 0 {
		t.Fatal("org update retained old credentials")
	}
	f.controlRequest(t, "px-endpoint", "GET", "/profiles/px-profile/direct?processEpoch=direct-boot", nil, 403, nil)
}
func (f *publicationPG) danglingRuleScenario(t *testing.T) {
	f.sql(t, "INSERT INTO network_proxy_instances(id,name,engine) VALUES('px-rules','Rule repair','mihomo')")
	input := api.NetworkProxySourceInput{Name: "Rule targets", Format: "manual", DeliveryMode: "direct_subscription", Nodes: []api.NetworkProxyNode{{Protocol: "http", Server: "rule-a.example.test", Port: 8080}, {Protocol: "http", Server: "rule-b.example.test", Port: 8080}}}
	ir, err := proxysource.NewCompiler().Compile(input)
	pgRequire(t, err)
	publicationResponse(t, f.request(t, "PUT", "/instances/px-rules/sources/rule-source", input, false), 200, nil)
	draft := api.NetworkProxyRulesInput{Rules: []api.NetworkProxyRule{{RuleID: "required-node", Source: "organization_required", Enabled: true, Matcher: api.NetworkProxyRuleMatcher{Type: "domain", Value: "target.example.test"}, Action: api.NetworkProxyRuleAction{Type: "node", TargetID: ir.Nodes[0].NodeID}}}}
	var rules api.NetworkProxyRules
	publicationResponse(t, f.request(t, "PUT", "/instances/px-rules/rules", draft, false), 200, &rules)
	if rules.OrgRevision != 1 {
		t.Fatal("rule not persisted")
	}
	input.ExpectedRevision = 1
	input.Nodes = input.Nodes[1:]
	publicationResponse(t, f.request(t, "PUT", "/instances/px-rules/sources/rule-source", input, false), 200, nil)
	publicationResponse(t, f.request(t, "GET", "/instances/px-rules/rules", nil, false), 200, &rules)
	if len(rules.Rules) != 1 || len(rules.Conflicts) != 1 || rules.Conflicts[0].ReasonCode != "invalid_target" || len(rules.EffectiveRules) != 0 {
		t.Fatal("dangling rule draft is not visible and blocked")
	}
	repair := api.NetworkProxyRulesInput{ExpectedOrgRevision: 1, Rules: []api.NetworkProxyRule{}}
	publicationResponse(t, f.request(t, "PUT", "/instances/px-rules/rules", repair, false), 200, &rules)
	if rules.OrgRevision != 2 || len(rules.Rules) != 0 || len(rules.Conflicts) != 0 {
		t.Fatal("empty repair did not clear dangling rule")
	}
	publicationResponse(t, f.request(t, "PUT", "/instances/px-rules/rules", repair, false), 409, nil)
	publicationResponse(t, f.request(t, "GET", "/instances/px-rules/rules", nil, false), 200, &rules)
	if rules.OrgRevision != 2 || len(rules.Rules) != 0 {
		t.Fatal("stale repair CAS mutated rules")
	}
}
func (f *publicationPG) atomicityScenario(t *testing.T) {
	_, err := f.proxies.UpdateConfiguration(f.ctx, "px-a", 1, true, "encrypted", "sha256:"+strings.Repeat("c", 64), nil, false, time.Now())
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("managed single mutation accepted")
	}
	before, err := f.proxies.ObserveMemberConfigurations(f.ctx, []string{"px-a", "px-b"})
	pgRequire(t, err)
	content := "proxies: []"
	batch := proxy.DesiredBatch{PublicationID: "stale-batch", MasterID: "px-master", OwnerEpoch: 1, MemberSetHash: f.pool.pool.MemberSetHash, Members: []proxy.DesiredMember{{InstanceID: "px-a", ExpectedRevision: 1, Content: &content, Enabled: true}, {InstanceID: "px-b", ExpectedRevision: 0, Content: &content, Enabled: true}}}
	if err = f.management.CommitDesiredBatch(f.ctx, identity.Principal{UserID: f.user, Roles: []string{"admin"}}, batch); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("stale member CAS accepted")
	}
	after, err := f.proxies.ObserveMemberConfigurations(f.ctx, []string{"px-a", "px-b"})
	pgRequire(t, err)
	for i := range before {
		if before[i].DesiredRevision != after[i].DesiredRevision || before[i].DesiredHash != after[i].DesiredHash {
			t.Fatal("partial batch mutation")
		}
	}
	// Representative fixed drain bound through the production publication lease path.

	original := f.pool.pool.Members
	f.pool.pool.Members = append([]pubdomain.PoolMember(nil), original...)
	defer func() { f.pool.pool.Members = original }()
	bound := time.Now().Add(30 * time.Second)
	for i := range f.pool.pool.Members {
		f.pool.pool.Members[i].Running = false
		f.pool.pool.Members[i].ServingUntil = &bound
	}
	first, err := f.bundles.ServerPublication(f.ctx, "px-a", "mapped", 1, time.Now().Add(5*time.Minute))
	pgRequire(t, err)
	second, err := f.bundles.ServerPublication(f.ctx, "px-a", "mapped", 1, time.Now().Add(5*time.Minute))
	pgRequire(t, err)
	if first.ValidUntil.After(bound) || !first.ValidUntil.Equal(second.ValidUntil) {
		t.Fatal("drain polling extended hard bound")
	}
	for i := range f.pool.pool.Members {
		f.pool.pool.Members[i].Serving = false
	}
	if _, err = f.bundles.ServerPublication(f.ctx, "px-a", "mapped", 1, time.Now().Add(time.Minute)); err == nil {
		t.Fatal("unknown/nonserving member granted publication")
	}
}

type publicationPGRoles struct{}

func (publicationPGRoles) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"admin": {access.PermNetworkAccessProxyInstancesView, access.PermNetworkAccessProxyInstancesUpdate, access.PermNetworkAccessMihomoProfilesCreate, access.PermNetworkAccessMihomoProfilesUpdate}, "viewer": {access.PermNetworkAccessProxyInstancesView}, "profiles-only": {access.PermNetworkAccessMihomoProfilesCreate, access.PermNetworkAccessMihomoProfilesUpdate}}, nil
}

type publicationPGScope struct{}

func (publicationPGScope) Authorize(context.Context, accessdomain.Request) (accessdomain.Decision, error) {
	return accessdomain.Decision{Allowed: true}, nil
}

type publicationPGAudit struct{}

func (publicationPGAudit) Record(context.Context, audit.Entry) error { return nil }

type publicationPGOperations struct{}

func (publicationPGOperations) Record(context.Context, operation.Entry) error { return nil }

type publicationPGAuthority struct{ management *proxyapp.Management }

func (a publicationPGAuthority) Authorize(ctx context.Context, p identity.Principal, id, action string) error {
	return a.management.AuthorizePublication(ctx, p, id, action)
}
func (publicationPGAuthority) RecordPublication(context.Context, identity.Principal, string, string, string, int64) error {
	return nil
}

type publicationPGPool struct{ pool pubdomain.ApprovedPool }

func (p *publicationPGPool) ApprovedPool(context.Context, string) (pubdomain.ApprovedPool, error) {
	return p.pool, nil
}

func (f *publicationPG) setupTLS(t *testing.T) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	pgRequire(t, err)
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "PX acceptance CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, pub, key)
	pgRequire(t, err)
	ca, err := x509.ParseCertificate(caDER)
	pgRequire(t, err)
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	f.server = httptest.NewUnstartedServer(f.control)
	f.server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}
	f.server.StartTLS()
	t.Cleanup(f.server.Close)
	for index, id := range []string{"px-a", "px-b", "px-endpoint", "px-other"} {
		kind := "proxy"
		device := "px-device"
		if strings.HasPrefix(id, "px-e") || id == "px-other" {
			kind = "endpoint"
		}
		if id == "px-other" {
			device = "other-device"
		}
		uri, _ := url.Parse("spiffe://opensoha.local/network-control/" + kind + "/" + id)
		cp, ck, err := ed25519.GenerateKey(rand.Reader)
		pgRequire(t, err)
		template := &x509.Certificate{SerialNumber: big.NewInt(int64(index + 2)), URIs: []*url.URL{uri}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
		der, err := x509.CreateCertificate(rand.Reader, template, ca, cp, key)
		pgRequire(t, err)
		cert, err := x509.ParseCertificate(der)
		pgRequire(t, err)
		ident, err := networkidentity.ParseCertificate(cert, networkidentity.ScopeNetworkControl)
		pgRequire(t, err)
		base, ok := f.server.Client().Transport.(*http.Transport)
		if !ok || base == nil {
			t.Fatal("unexpected test TLS transport")
		}
		transport := base.Clone()
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		transport.TLSClientConfig.Certificates = []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: ck}}
		f.clients[id] = &http.Client{Transport: transport, Timeout: 10 * time.Second}
		t.Cleanup(transport.CloseIdleConnections)
		f.sql(t, `INSERT INTO network_runtime_enrollments(id,challenge_id,challenge_hash,runtime_id,runtime_kind,device_id,subject_id,expires_at,created_by) VALUES(?,?,?,?,?,?,?,now()+interval '1 hour',?)`, id, id, "sha256:"+strings.Repeat("d", 64), id, kind, device, f.user, f.user)
		f.sql(t, `INSERT INTO network_runtime_credentials(id,enrollment_id,runtime_id,runtime_kind,device_id,subject_id,certificate_fingerprint,public_key_fingerprint,certificate_serial,generation,capabilities,not_before,expires_at) VALUES(?,?,?,?,?,?,?,?,?,1,'["mihomo","mihomo-policy-v1","mihomo-dns-pin-v1"]',now()-interval '1 minute',now()+interval '1 hour')`, id, id, id, kind, device, f.user, ident.CertificateFingerprint, ident.PublicKeyFingerprint, cert.SerialNumber.Text(16))
	}
}
func (f *publicationPG) controlRequest(t *testing.T, id, method, path string, input any, status int, out any) []byte {
	t.Helper()
	raw, _ := json.Marshal(input)
	req, err := http.NewRequest(method, f.server.URL+path, bytes.NewReader(raw))
	pgRequire(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := f.clients[id].Do(req)
	pgRequire(t, err)
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	pgRequire(t, err)
	if res.StatusCode != status {
		t.Fatalf("control HTTP %d expected %d (body withheld)", res.StatusCode, status)
	}
	if status == 200 {
		if res.Header.Get("Cache-Control") != "private, no-store" {
			t.Fatal("missing no-store")
		}
		if out != nil {
			pgRequire(t, json.Unmarshal(body, out))
		}
	}
	return body
}

func (f *publicationPG) setupPublicationServices(t *testing.T, keys keyring.Ring, hash string) {
	t.Helper()
	var err error
	db := f.db
	f.proxies = proxyrepo.New(db)
	f.proxies.SetBatchFence(deployrepo.New(db))
	f.store = pubrepo.New(db)
	f.management, err = proxyapp.NewManagement(f.proxies, nil, access.NewPermissionResolver(publicationPGRoles{}), publicationPGScope{}, publicationPGAudit{}, publicationPGOperations{}, keys)
	pgRequire(t, err)
	authority := publicationPGAuthority{f.management}
	compiler := proxysource.NewCompiler()
	fetcher, err := proxysource.NewFetcher(proxysource.Options{Protected: func(context.Context, netip.Addr) bool { return true }, ProtectedHost: func(context.Context, string) bool { return true }})
	pgRequire(t, err)
	f.sources, err = publication.NewSources(f.store, authority, authority, compiler, fetcher, keys)
	pgRequire(t, err)
	f.pool = &publicationPGPool{pool: pubdomain.ApprovedPool{MasterID: "px-master", OwnerEpoch: 1, MemberSetHash: hash, Members: []pubdomain.PoolMember{{RuntimeID: "px-a", Running: true, Serving: true}, {RuntimeID: "px-b", Running: true, Serving: true}}, Endpoints: []api.NetworkProxyEmployeeEndpoint{{EndpointID: "px-entry", URL: "https://entry.example.test"}}}}
	runtimeStore := runtimerepo.New(db, appnetworkaccess.VPNGovernancePolicy{})
	schemas, err := networkprotocol.CompileSchemas()
	pgRequire(t, err)
	service, err := control.New(runtimeStore, schemas, control.Options{MaxClockSkew: time.Minute, ConfigurationTTL: 5 * time.Minute, LeaseTTL: 5 * time.Minute})
	pgRequire(t, err)
	endpoint, err := control.NewProjectionAuthorizer(service, runtimeStore)
	pgRequire(t, err)
	destination, err := publication.NewDestinationPolicy(f.store, proxysource.Resolver{})
	pgRequire(t, err)
	f.bundles, err = publication.NewBundles(f.store, f.store, f.store, authority, authority, f.pool, f.management, compiler, service, endpoint, runtimeStore, destination, keys)
	pgRequire(t, err)

}

func (f *publicationPG) setupPublicationRoutes(t *testing.T, keys keyring.Ring) {
	t.Helper()
	authority := publicationPGAuthority{f.management}
	gin.SetMode(gin.TestMode)
	f.router = gin.New()
	f.router.Use(func(c *gin.Context) {
		role := "admin"
		if c.GetHeader("X-Test-Role") == "profiles-only" {
			role = "profiles-only"
		}
		if c.GetHeader("X-Test-Role") == "viewer" {
			role = "viewer"
		}
		c.Set("principal", identity.Principal{UserID: f.user, Roles: []string{role}})
	})
	sh := NewProxyPublicationSourcesHandler(f.sources)
	bh := NewProxyPublicationBundlesHandler(f.bundles)
	f.router.PUT("/instances/:instanceID/sources/:sourceID", sh.Put)
	f.router.GET("/instances/:instanceID/sources/:sourceID", sh.Get)
	f.router.DELETE("/instances/:instanceID/sources/:sourceID", sh.Delete)
	f.router.POST("/instances/:instanceID/preview", sh.Preview)
	f.router.PUT("/instances/:instanceID/bundles/:bundleID", bh.Prepare)
	f.router.POST("/instances/:instanceID/bundles/:bundleID/activate", bh.Activate)
	rules, err := publication.NewRules(f.store, authority, authority)
	pgRequire(t, err)
	rh := NewProxyPublicationRulesHandler(rules, f.sources)
	f.router.GET("/instances/:instanceID/rules", rh.Get)
	f.router.PUT("/instances/:instanceID/rules", rh.Put)
	ch, err := controlhttp.NewPublicationHandler(f.bundles, controlhttp.Options{MaxBodyBytes: 1 << 20, RequestsPerMinute: 1000})
	pgRequire(t, err)
	f.control = gin.New()
	f.control.POST("/proxies/:runtimeID/ready", ch.ServerReady)
	f.control.POST("/proxies/:runtimeID/check", ch.IngressCheck)
	f.control.GET("/profiles/:profileID/projection", ch.Projection)
	f.control.POST("/profiles/:profileID/receipt", ch.ProjectionReceipt)
	f.control.GET("/profiles/:profileID/direct", ch.DirectProjection)
	f.setupTLS(t)
	f.installProfileHandlers(t, keys)

}
