//go:build soha_native_integration

package networkproxyruntime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/proxyadapter"
	"github.com/opensoha/soha-contracts/proxytransport"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"golang.org/x/net/proxy"
)

// Opt in with an operator-supplied directory of the three pinned real kernels.
// Run in a disposable Linux container. Upstreams only return their identity;
// they never dial the requested destination. No UDP claim is made here.
func TestRealEngineIntegration(t *testing.T) {
	dir := os.Getenv("SOHA_REAL_ENGINE_DIR")
	if dir == "" {
		t.Skip("set SOHA_REAL_ENGINE_DIR to the pinned real kernel directory")
	}
	for _, tc := range []struct{ engine, binary, version string }{
		{domain.EngineMihomo, "mihomo", "1.19.30"},
		{domain.EngineSingBox, "sing-box", "1.14.2"},
		{domain.EngineV2Ray, "v2ray", "5.53.0"},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			path := filepath.Join(dir, tc.binary)
			args := []string{"version"}
			if tc.engine == domain.EngineMihomo {
				args = []string{"-v"}
			}
			output, err := exec.Command(path, args...).CombinedOutput()
			if err != nil || !strings.Contains(string(output), tc.version) {
				t.Fatalf("pinned version: %s %v", output, err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("real binary=%s sha256=%x version=%s", path, sha256.Sum256(raw), strings.TrimSpace(string(output)))
			realEngineScenarios(t, tc.engine, path, false)
		})
	}
	t.Run("external-reference", func(t *testing.T) { realEngineScenarios(t, domain.EngineMihomo, filepath.Join(dir, "mihomo"), true) })
}

func realPrivateIP(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range addrs {
		ip, _, err := net.ParseCIDR(a.String())
		if err == nil && ip.To4() != nil && ip.IsPrivate() && !ip.IsLoopback() {
			return ip.String()
		}
	}
	t.Fatal("requires a disposable container private IPv4 interface (bootstrap rejects loopback upstreams)")
	return ""
}

func realHTTPUpstream(t *testing.T, ip, name string) int {
	t.Helper()
	listener, err := net.Listen("tcp4", net.JoinHostPort(ip, "0"))
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" || r.Host != "192.0.2.1:443" {
			t.Errorf("upstream fixture received forbidden destination %s %s", r.Method, r.Host)
			http.Error(w, "fixture destination rejected", 403)
			return
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
		_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = rw.Flush()
		// Respond only after application data: some HTTP proxy clients discard
		// bytes coalesced with CONNECT headers before the tunnel is established.
		first := make([]byte, 1)
		if _, err := io.ReadFull(rw, first); err != nil {
			return
		}
		if _, err := io.WriteString(conn, name+string(first)); err != nil {
			return
		}
		_, _ = io.Copy(conn, rw)
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return listener.Addr().(*net.TCPAddr).Port
}

func realFreePort(t *testing.T) int {
	t.Helper()
	l, p, err := listenEgressPort()
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Close()
	return p
}

func realEngineContent(engine, ip string, a, b, port int) string {
	switch engine {
	case domain.EngineMihomo:
		return fmt.Sprintf("mixed-port: %d\nlog-level: silent\nproxies:\n - {name: node-a, type: http, server: %s, port: %d}\n - {name: node-b, type: http, server: %s, port: %d}\nproxy-groups:\n - {name: choice, type: select, proxies: [node-a, node-b]}\nrules: ['MATCH,choice']\n", port, ip, a, ip, b)
	case domain.EngineSingBox:
		return fmt.Sprintf(`{"log":{"disabled":true},"inbounds":[{"type":"socks","tag":"client","listen":"127.0.0.1","listen_port":%d}],"outbounds":[{"type":"http","tag":"node-a","server":"%s","server_port":%d},{"type":"http","tag":"node-b","server":"%s","server_port":%d}],"route":{"final":"node-a"}}`, port, ip, a, ip, b)
	default:
		return fmt.Sprintf(`{"log":{"loglevel":"none"},"inbounds":[{"tag":"client","listen":"127.0.0.1","port":%d,"protocol":"socks","settings":{"auth":"noauth","udp":false}}],"outbounds":[{"tag":"node-a","protocol":"http","settings":{"servers":[{"address":"%s","port":%d}]}},{"tag":"node-b","protocol":"http","settings":{"servers":[{"address":"%s","port":%d}]}}],"routing":{"rules":[{"type":"field","network":"tcp","outboundTag":"node-a"}]}}`, port, ip, a, ip, b)
	}
}

func realEngineScenarios(t *testing.T, dialect, binary string, external bool) {
	t.Helper()
	ctx := context.Background()
	state := t.TempDir()
	ip := realPrivateIP(t)
	a, b := realHTTPUpstream(t, ip, "A"), realHTTPUpstream(t, ip, "B")
	port := realFreePort(t)
	config := Config{Engine: dialect, EngineBinary: binary, StateDir: state, ConfigFile: filepath.Join(state, "engine-config"), ControllerURL: fmt.Sprintf("http://127.0.0.1:%d", realFreePort(t))}
	desired := lifecycleDesired(1, port)
	desired.Engine = dialect
	desired.Content = realEngineContent(dialect, ip, a, b, port)
	if external {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		wrapper := "#!/bin/sh\nexec env SOHA_REFERENCE_FIXTURE=1 '" + strings.ReplaceAll(self, "'", "'\\''") + "' -test.run '^TestReferenceStdioProcess$'\n"
		path, digest := pinnedScript(t, wrapper)
		raw, err := os.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		config.EngineBinary = filepath.Join(state, "approved-kernel")
		if err := os.WriteFile(config.EngineBinary, raw, 0700); err != nil {
			t.Fatal(err)
		}
		policy := adapterPolicy{BinaryApproval: proxyadapter.BinaryApproval{AdapterID: "soha-reference", Binary: path, SHA256: digest, Version: "1.0.0"}, EngineID: "reference-mihomo", Dialect: dialect, EngineSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(raw))}
		payload, _ := json.Marshal(policy)
		config.AdapterPolicyFile = filepath.Join(state, "policy.json")
		if err := os.WriteFile(config.AdapterPolicyFile, payload, 0600); err != nil {
			t.Fatal(err)
		}
		config.Engine = domain.EngineExternal
		desired.Engine = domain.EngineExternal
		desired.EngineDescriptor = &domain.EngineDescriptor{EngineID: policy.EngineID, AdapterID: policy.AdapterID, Version: policy.Version, ProtocolMajor: 1}
	}
	e := newEngine(config, "real-test-controller-secret")
	t.Cleanup(e.stop)
	if external {
		if err := e.configureExternal(); err != nil {
			t.Fatal(err)
		}
	}
	if result := e.apply(ctx, desired); result.Status != "applied" {
		t.Fatalf("real apply: %+v", result)
	}
	if dialect == domain.EngineMihomo {
		// Explicitly select both A and B: Mihomo can persist a previous choice.
		if _, err := e.controller.request(ctx, http.MethodPut, "/proxies/choice", map[string]string{"name": "node-a"}); err != nil {
			t.Fatal(err)
		}
	}
	dial := func(destination string) (net.Conn, error) {
		d, err := proxy.SOCKS5("tcp", fmt.Sprintf("127.0.0.1:%d", port), nil, &net.Dialer{Timeout: 2 * time.Second})
		if err != nil {
			return nil, err
		}
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		return d.(proxy.ContextDialer).DialContext(cctx, "tcp", destination)
	}
	identity := func(t *testing.T, c net.Conn, want string) {
		t.Helper()
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err := c.Write([]byte("!")); err != nil {
			t.Fatal(err)
		}
		raw := make([]byte, 2)
		if _, err := io.ReadFull(c, raw); err != nil || string(raw) != want+"!" {
			t.Fatalf("real upstream identity=%q want=%s err=%v", raw, want, err)
		}
	}
	t.Run("tcp-observe-close", func(t *testing.T) {
		c, err := dial("192.0.2.1:443")
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		identity(t, c, "A")
		sample, connections := e.adapter.observe(ctx)
		if sample.Health != "healthy" || sample.DownloadTotal == 0 {
			t.Fatalf("real observation: %+v", sample)
		}
		if dialect == domain.EngineV2Ray {
			if e.adapter.supportsClose() || len(connections) != 0 || sample.ActiveConnections != nil {
				t.Fatal("V2Ray falsely advertised connection inventory")
			}
			if _, err := e.close(ctx, "unsupported"); err == nil {
				t.Fatal("V2Ray close falsely succeeded")
			}
			return
		}
		if !e.adapter.supportsClose() || len(connections) == 0 {
			t.Fatalf("missing real connection inventory: %+v", sample)
		}
		survivor, err := dial("192.0.2.1:443")
		if err != nil {
			t.Fatal(err)
		}
		defer survivor.Close()
		identity(t, survivor, "A")
		// Match the public client's source port; backend tunnels are independent.
		raw, err := e.controller.request(ctx, http.MethodGet, "/connections", nil)
		if err != nil {
			t.Fatal(err)
		}
		var snapshot struct {
			Connections []struct {
				ID       string `json:"id"`
				Metadata struct {
					SourcePort json.RawMessage `json:"sourcePort"`
				} `json:"metadata"`
			} `json:"connections"`
		}
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			t.Fatal(err)
		}
		id := ""
		for _, item := range snapshot.Connections {
			if strings.Trim(string(item.Metadata.SourcePort), `"`) == fmt.Sprint(c.LocalAddr().(*net.TCPAddr).Port) {
				id = item.ID
				break
			}
		}
		if id == "" {
			t.Fatalf("public connection missing in real controller: %s", raw)
		}
		if _, err := e.close(ctx, id); err != nil {
			t.Fatal(err)
		}
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		one := make([]byte, 1)
		_, err = c.Read(one)
		if err == nil {
			t.Fatal("single close left stream open")
		}
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			t.Fatal("single close timed out")
		}
		_ = survivor.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := survivor.Write([]byte("?")); err != nil {
			t.Fatal(err)
		}
		if _, err := io.ReadFull(survivor, one); err != nil || string(one) != "?" {
			t.Fatalf("single close disrupted unrelated stream: %q %v", one, err)
		}
	})
	if dialect == domain.EngineMihomo {
		t.Run("select-maps-B", func(t *testing.T) {
			if _, err := e.controller.request(ctx, http.MethodPut, "/proxies/choice", map[string]string{"name": "node-b"}); err != nil {
				t.Fatal(err)
			}
			c, err := dial("192.0.2.1:443")
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			identity(t, c, "B")
		})
	}
	t.Run("protected-fixed-listener", func(t *testing.T) {
		for _, target := range []string{"10.0.0.1:443", "protected.example:443"} {
			c, err := dial(target)
			if err == nil {
				_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
				one := make([]byte, 1)
				_, readErr := c.Read(one)
				c.Close()
				if readErr == nil {
					t.Fatalf("public listener reached protected %s", target)
				}
			}
		}
		// publicationIngress is the host gate before the fixed engine listener.
		scope, err := proxytransport.NewScope(desired.Protection.CIDRs, desired.Protection.Hosts)
		if err != nil {
			t.Fatal(err)
		}
		p := &publicationIngress{scope: scope, backends: e.egress.backends}
		backend := e.egress.backends["node-a"]
		c, err := p.dial(ctx, "192.0.2.1:443", "node-a", "node-a", backend)
		if err != nil {
			t.Fatal(err)
		}
		identity(t, c, "A")
		c.Close()
		for _, target := range []string{"10.0.0.1:443", "protected.example:443"} {
			if c, err := p.dial(ctx, target, "node-a", "node-a", backend); err == nil {
				c.Close()
				t.Fatalf("protected fixed listener bypassed for %s", target)
			}
		}
		p.publication.Rules = []api.NetworkProxyRule{{RuleID: "mandatory", Source: "organization_required", Enabled: true, Matcher: api.NetworkProxyRuleMatcher{Type: "all"}, Action: api.NetworkProxyRuleAction{Type: "reject"}}}
		if c, err := p.dial(ctx, "192.0.2.1:443", "node-a", "node-a", backend); err == nil {
			c.Close()
			t.Fatal("mandatory reject bypassed fixed listener")
		}
	})
	t.Run("invalid-replacement-fail-closed", func(t *testing.T) {
		bad := desired
		bad.Revision++
		bad.Content = strings.Replace(desired.Content, "log-level: silent", "log-level: invalid-log-level", 1)
		if dialect == domain.EngineSingBox {
			bad.Content = strings.Replace(desired.Content, fmt.Sprintf(`"listen_port":%d`, port), `"listen_port":-1`, 1)
		}
		if dialect == domain.EngineV2Ray {
			bad.Content = strings.Replace(desired.Content, fmt.Sprintf(`"port":%d`, port), `"port":-1`, 1)
		}
		if result := e.apply(ctx, bad); result.Status == "applied" || e.process != nil || e.egress != nil {
			t.Fatalf("invalid config retained engine: %+v", result)
		}
		if c, err := dial("192.0.2.1:443"); err == nil {
			c.Close()
			t.Fatal("rejected replacement still accepts TCP")
		}
	})
	t.Run("process-exit-fail-closed", func(t *testing.T) {
		desired.Revision += 2
		if result := e.apply(ctx, desired); result.Status != "applied" {
			t.Fatalf("restore %+v", result)
		}
		r := &Runner{config: config, engine: e, enabled: true}
		if err := e.process.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-e.wait:
			e.wait <- err
		case <-time.After(5 * time.Second):
			t.Fatal("real kernel did not exit")
		}
		r.ensureRunning(ctx)
		if r.enabled || !r.suspended || e.process != nil || e.egress != nil {
			t.Fatal("exited real engine was not suspended")
		}
		if c, err := dial("192.0.2.1:443"); err == nil {
			c.Close()
			t.Fatal("exited kernel accepts TCP")
		}
	})
}
