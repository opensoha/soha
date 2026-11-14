//go:build soha_native_integration

package networkproxyruntime

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/contracts/proxyadapter"
	"github.com/opensoha/soha/contracts/proxytransport"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Real native Mihomo and owned host gates; the online authorization and stop
// receipt endpoints are control-plane fixtures, not deployed Core/PG evidence.
func TestRealUDPIntegration(t *testing.T) {
	dir := os.Getenv("SOHA_REAL_ENGINE_DIR")
	if dir == "" {
		t.Fatal("SOHA_REAL_ENGINE_DIR is required for native UDP acceptance")
	}
	for _, external := range []bool{false, true} {
		name := "mihomo"
		if external {
			name = "external-reference"
		}
		t.Run(name, func(t *testing.T) { realUDPScenario(t, filepath.Join(dir, "mihomo"), external) })
	}
	t.Run("malicious-relay", func(t *testing.T) {
		for _, test := range []struct {
			name  string
			bound []byte
		}{{"different-ip", []byte{1, 127, 0, 0, 2}}, {"protected-ip", []byte{1, 10, 0, 0, 1}}, {"domain", []byte{3, 9, 'l', 'o', 'c', 'a', 'l', 'h', 'o', 's', 't'}}} {
			t.Run(test.name, func(t *testing.T) {
				ip := realPrivateIP(t)
				peer := realUDPSource(t, ip, "bad", test.bound)
				e, desired := realUDPApply(t, filepath.Join(dir, "mihomo"), false, ip, peer.port, peer.port)
				defer e.stop()
				backend := e.egress.backends["node-a"]
				c, err := dialOwnedUDP(context.Background(), backend, "1.1.1.1:443", desired.ValidUntil)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(500 * time.Millisecond))
				_, _ = c.Write([]byte("forbidden"))
				if _, err = c.Read(make([]byte, 100)); err == nil {
					t.Fatal("malicious relay returned data")
				}
				if peer.packets.Load() != 0 {
					t.Fatal("malicious relay received a datagram")
				}
			})
		}
	})
}

type realUDPPeer struct {
	port    int
	packets atomic.Int32
}

func realUDPSource(t *testing.T, ip, name string, bound []byte) *realUDPPeer {
	t.Helper()
	listener, err := net.Listen("tcp4", net.JoinHostPort(ip, "0"))
	if err != nil {
		t.Fatal(err)
	}
	relay, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(ip)})
	if err != nil {
		t.Fatal(err)
	}
	f := &realUDPPeer{port: listener.Addr().(*net.TCPAddr).Port}
	t.Cleanup(func() { listener.Close(); relay.Close() })
	go func() {
		buf := make([]byte, 65536)
		for {
			n, from, err := relay.ReadFromUDP(buf)
			if err != nil {
				return
			}
			f.packets.Add(1)
			if n < 10 || string(buf[4:8]) != string([]byte{1, 1, 1, 1}) || binary.BigEndian.Uint16(buf[8:10]) != 443 {
				continue
			}
			response := append(append([]byte(nil), buf[:10]...), []byte(name)...)
			response = append(response, buf[10:n]...)
			_, _ = relay.WriteToUDP(response, from)
		}
	}()
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(30 * time.Second))
				var greeting [3]byte
				if _, err := io.ReadFull(c, greeting[:]); err != nil || greeting != [3]byte{5, 1, 2} {
					return
				}
				_, _ = c.Write([]byte{5, 2})
				var h [2]byte
				if _, err := io.ReadFull(c, h[:]); err != nil {
					return
				}
				user := make([]byte, int(h[1]))
				if _, err := io.ReadFull(c, user); err != nil {
					return
				}
				var size [1]byte
				if _, err := io.ReadFull(c, size[:]); err != nil {
					return
				}
				secret := make([]byte, int(size[0]))
				if _, err := io.ReadFull(c, secret); err != nil || string(user) != "upstream-user" || string(secret) != "short-secret" {
					return
				}
				_, _ = c.Write([]byte{1, 0})
				var request [10]byte
				if _, err := io.ReadFull(c, request[:]); err != nil || request[1] != 3 {
					return
				}
				address := bound
				if address == nil {
					address = append([]byte{1}, net.ParseIP(ip).To4()...)
				}
				response := append([]byte{5, 0, 0}, address...)
				response = binary.BigEndian.AppendUint16(response, uint16(relay.LocalAddr().(*net.UDPAddr).Port))
				_, _ = c.Write(response)
				_, _ = io.Copy(io.Discard, c)
			}()
		}
	}()
	return f
}
func realUDPApply(t *testing.T, binaryPath string, external bool, ip string, a, b int) (*engine, domain.Configuration) {
	t.Helper()
	state := t.TempDir()
	config := Config{Engine: domain.EngineMihomo, EngineBinary: binaryPath, StateDir: state, ConfigFile: filepath.Join(state, "config.yaml"), ControllerURL: fmt.Sprintf("http://127.0.0.1:%d", realFreePort(t))}
	desired := lifecycleDesired(1, realFreePort(t))
	desired.Content = fmt.Sprintf("mixed-port: %d\nlog-level: silent\nproxies:\n - {name: node-a, type: socks5, server: %s, port: %d, username: upstream-user, password: short-secret, udp: true}\n - {name: node-b, type: socks5, server: %s, port: %d, username: upstream-user, password: short-secret, udp: true}\nrules: ['MATCH,node-a']\n", realFreePort(t), ip, a, ip, b)
	desired.ContentHash = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(desired.Content)))
	desired.Publication = &api.NetworkProxyServerPublication{DeliveryMode: "mapped_nodes", ValidUntil: desired.ValidUntil, ServerHash: desired.ContentHash, BundleID: "real-udp", Revision: 1, CredentialEpoch: 1, Nodes: []api.NetworkProxyServerPublicationNode{{NodeID: "node-a", OutboundID: "node-a"}, {NodeID: "node-b", OutboundID: "node-b"}}}
	if external {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		wrapper := "#!/bin/sh\nexec env SOHA_REFERENCE_FIXTURE=1 '" + strings.ReplaceAll(self, "'", "'\\''") + "' -test.run '^TestReferenceStdioProcess$'\n"
		path, digest := pinnedScript(t, wrapper)
		raw, err := os.ReadFile(binaryPath)
		if err != nil {
			t.Fatal(err)
		}
		config.EngineBinary = filepath.Join(state, "approved-kernel")
		if err = os.WriteFile(config.EngineBinary, raw, 0700); err != nil {
			t.Fatal(err)
		}
		policy := adapterPolicy{BinaryApproval: proxyadapter.BinaryApproval{AdapterID: "soha-reference", Binary: path, SHA256: digest, Version: "1.0.0"}, EngineID: "reference-mihomo", Dialect: domain.EngineMihomo, EngineSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(raw))}
		payload, _ := json.Marshal(policy)
		config.AdapterPolicyFile = filepath.Join(state, "policy.json")
		if err = os.WriteFile(config.AdapterPolicyFile, payload, 0600); err != nil {
			t.Fatal(err)
		}
		config.Engine = domain.EngineExternal
		desired.Engine = domain.EngineExternal
		desired.EngineDescriptor = &domain.EngineDescriptor{EngineID: policy.EngineID, AdapterID: policy.AdapterID, Version: policy.Version, ProtocolMajor: 1}
	}
	e := newEngine(config, "private-real-udp-controller")
	if external {
		if err := e.configureExternal(); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(e.stop)
	if result := e.apply(context.Background(), desired); result.Status != "applied" {
		t.Fatalf("real UDP apply: %+v", result)
	}
	if e.egress == nil || !e.egress.udpVerified.Load() {
		t.Fatal("real kernel lacks verified UDP guard")
	}
	return e, desired
}
func realUDPScenario(t *testing.T, binary string, external bool) {
	ip := realPrivateIP(t)
	a, b := realUDPSource(t, ip, "A", nil), realUDPSource(t, ip, "B", nil)
	e, desired := realUDPApply(t, binary, external, ip, a.port, b.port)
	pid, epoch := e.process.Process.Pid, e.processEpoch
	var revokeA, late atomic.Bool
	var ack atomic.Pointer[api.NetworkProxyDeploymentRuntimeAck]
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "receipt") {
			var input api.NetworkProxyDeploymentRuntimeAck
			_ = json.NewDecoder(r.Body).Decode(&input)
			ack.Store(&input)
			_ = json.NewEncoder(w).Encode(input)
			return
		}
		var input api.NetworkProxyIngressCheckInput
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			w.WriteHeader(400)
			return
		}
		if input.Network != "udp" {
			t.Error("control fixture received non-UDP authorization")
		}
		if late.Load() {
			time.Sleep(500 * time.Millisecond)
		}
		ttl := 5 * time.Second
		if strings.Contains(input.CredentialID, "short") {
			ttl = 250 * time.Millisecond
		}
		_ = json.NewEncoder(w).Encode(api.NetworkProxyIngressCheckResult{Allowed: !(input.NodeID == "node-a" && revokeA.Load()), BundleID: input.BundleID, NodeID: input.NodeID, Revision: input.Revision, CredentialEpoch: input.CredentialEpoch, ExpiresAt: time.Now().Add(ttl)})
	}))
	defer control.Close()
	scope, err := proxytransport.NewScope(desired.Protection.CIDRs, desired.Protection.Hosts)
	if err != nil {
		t.Fatal(err)
	}
	p := &publicationIngress{scope: scope, packetEnabled: true, accepting: true, until: desired.ValidUntil, publication: *desired.Publication, epoch: epoch, client: NewClient(control.URL, "runtime", nil), backends: e.egress.backends, udpGuard: e.egress, controller: e.controller, handlers: map[*proxytransport.Handler]struct{}{}}
	p.udpPolicy.Store(copyUDPPolicy(desired))
	server := httptest.NewTLSServer(p)
	p.server, p.listener = server.Config, server.Listener
	defer server.Close()
	defer p.close()
	dial := func(node, target, credential string) (net.Conn, error) {
		conn, err := tls.Dial("tcp", server.Listener.Addr().String(), server.Client().Transport.(*http.Transport).TLSClientConfig.Clone())
		if err != nil {
			return nil, err
		}
		user, _ := proxytransport.EncodeMappedUsername(credential, node)
		return proxytransport.DialPacketTunnel(context.Background(), conn, target, user, "012345678901234567890123456789012345", time.Now().Add(5*time.Second))
	}
	echo := func(c net.Conn, name string) {
		t.Helper()
		_ = c.SetDeadline(time.Now().Add(time.Second))
		if _, err := c.Write([]byte("packet")); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 100)
		n, err := c.Read(buf)
		if err != nil || string(buf[:n]) != name+"packet" {
			t.Fatalf("real UDP identity/boundary %q %v", buf[:n], err)
		}
	}
	ca, err := dial("node-a", "1.1.1.1:443", "credential")
	if err != nil {
		t.Fatal(err)
	}
	defer ca.Close()
	cb, err := dial("node-b", "1.1.1.1:443", "credential")
	if err != nil {
		t.Fatal(err)
	}
	defer cb.Close()
	echo(ca, "A")
	echo(cb, "B")
	before := a.packets.Load() + b.packets.Load()
	for _, target := range []string{"10.0.0.1:443", "protected.example:443"} {
		if c, err := dial("node-b", target, "credential"); err == nil {
			c.Close()
			t.Fatal("protected UDP accepted")
		}
	}
	mandatory := copyUDPPolicy(desired)
	mandatory.publication.Rules = []api.NetworkProxyRule{{RuleID: "mandatory", Source: "organization_required", Enabled: true, Matcher: api.NetworkProxyRuleMatcher{Type: "all"}, Action: api.NetworkProxyRuleAction{Type: "reject"}}}
	p.udpPolicy.Store(mandatory)
	if c, err := dial("node-b", "1.1.1.1:443", "credential"); err == nil {
		c.Close()
		t.Fatal("mandatory UDP accepted")
	}
	p.udpPolicy.Store(copyUDPPolicy(desired))
	if a.packets.Load()+b.packets.Load() != before {
		t.Fatal("refused destination reached upstream")
	}
	revokeA.Store(true)
	narrow := copyUDPPolicy(desired)
	narrow.publication.Nodes = narrow.publication.Nodes[1:]
	p.udpPolicy.Store(narrow)
	_, _ = ca.Write([]byte("revoked"))
	assertPacketClosed(t, ca, time.Now().Add(3*time.Second))
	echo(cb, "B")
	if e.process.Process.Pid != pid || e.processEpoch != epoch {
		t.Fatal("selective revocation restarted kernel")
	}
	short, err := dial("node-b", "1.1.1.1:443", "short")
	if err != nil {
		t.Fatal(err)
	}
	echo(short, "B")
	late.Store(true)
	assertPacketClosed(t, short, time.Now().Add(2*time.Second))
	short.Close()
	late.Store(false)
	p.setAccepting(false)
	bound := time.Now().Add(250 * time.Millisecond)
	p.mu.Lock()
	p.until = bound
	p.mu.Unlock()
	p.capPacketLeases()
	echo(cb, "B")
	if c, err := dial("node-b", "1.1.1.1:443", "credential"); err == nil {
		c.Close()
		t.Fatal("drain admitted new UDP")
	}
	assertPacketClosed(t, cb, bound.Add(time.Second))
	r := &Runner{config: Config{RuntimeID: "runtime", ManagedDeployment: true}, hostBootEpoch: "native-host", client: NewClient(control.URL, "runtime", nil), engine: e, ingress: p, deployment: deploymentState{grant: &api.NetworkProxyDeploymentRuntimeControl{StopRequired: true, ExpiresAt: time.Now().Add(5 * time.Second)}}}
	if err := r.confirmOwnedStop(context.Background()); err != nil {
		t.Fatal(err)
	}
	proof := ack.Load()
	if proof == nil || !proof.ProcessStopped || !proof.ActiveCountKnown || proof.ActiveConnections != 0 || e.process != nil || p.active.Load() != 0 {
		t.Fatal("real native UDP stop did not prove owned zero/process exit")
	}
}

// Run separately in a network=none container. This proves only native parser
// compatibility with the pinned artifact, never guarded data-plane behavior.
func TestRealKernelPreflightIntegration(t *testing.T) {
	dir := os.Getenv("SOHA_REAL_ENGINE_DIR")
	if dir == "" {
		t.Fatal("SOHA_REAL_ENGINE_DIR required")
	}
	for _, tc := range []struct{ engine, binary, version, content string }{{domain.EngineMihomo, "mihomo", "v1.19.30", "log-level: silent\nmode: rule\nproxies: []\nrules: ['MATCH,REJECT']\n"}, {domain.EngineSingBox, "sing-box", "1.14.2", `{"log":{"disabled":true},"inbounds":[],"outbounds":[]}`}, {domain.EngineV2Ray, "v2ray", "5.53.0", `{"log":{"loglevel":"none"},"inbounds":[],"outbounds":[{"protocol":"blackhole","tag":"block"}]}`}} {
		t.Run(tc.engine, func(t *testing.T) {
			path := filepath.Join(dir, tc.binary)
			binary, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(t.TempDir(), "native.conf")
			if err = os.WriteFile(config, []byte(tc.content), 0600); err != nil {
				t.Fatal(err)
			}
			input := KernelPreflightInput{SchemaVersion: "kernel-preflight/v1", Engine: tc.engine, Binary: path, ExpectedSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(binary)), ExpectedVersion: tc.version, SourceRef: "task122-pinned-official-artifact", ExpectedGOOS: runtime.GOOS, ExpectedGOARCH: runtime.GOARCH, ConfigFile: config, ConfigurationRevision: 1, ConfigurationHash: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(tc.content))), ProtectionVersion: 1}
			report := RunKernelPreflight(context.Background(), writePreflightInput(t, input))
			if !report.Compatible || report.VerificationLayer != "native-parser-compatibility" || report.ActualSHA256 != input.ExpectedSHA256 || report.ActualVersion != tc.version {
				t.Fatalf("real native preflight %+v", report)
			}
			t.Logf("native parser %s %s %s", tc.engine, report.ActualVersion, report.ActualSHA256)
		})
	}
}
