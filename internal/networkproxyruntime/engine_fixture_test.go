package networkproxyruntime

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"sigs.k8s.io/yaml"
)

// The test binary supplies a portable, loopback-only engine process. Its SOCKS
// probe listeners forward through the real authenticated guard, never to WAN.
func init() {
	if os.Getenv("SOHA_PROXY_ENGINE_FIXTURE") != "1" {
		return
	}
	for _, arg := range os.Args[1:] {
		if arg == "-t" {
			if os.Getenv("SOHA_PROXY_FIXTURE_VALIDATION_FAIL") == "1" {
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	if err := runEngineFixture(); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

type fixtureProxy struct {
	Name, Server, Username, Password string
	Port                             int
}
type fixtureListener struct {
	Listen, Proxy string
	Port          int
	Users         []struct{ Username, Password string }
}

func runEngineFixture() error {
	path := ""
	for i, arg := range os.Args {
		if arg == "-f" && i+1 < len(os.Args) {
			path = os.Args[i+1]
		}
	}
	base := os.Getenv("SOHA_PROXY_FIXTURE_ROOT")
	root, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	name, err := filepath.Rel(base, path)
	if err != nil {
		return err
	}
	raw, err := root.ReadFile(name)
	if err != nil {
		return err
	}
	var doc struct {
		Proxies   []fixtureProxy
		Listeners []fixtureListener
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return err
	}
	fronts := map[string]fixtureProxy{}
	for _, front := range doc.Proxies {
		if front.Server == "127.0.0.1" {
			fronts[front.Name] = front
		}
	}
	for _, listener := range doc.Listeners {
		front, ok := fronts[listener.Proxy]
		if !ok || listener.Listen != "127.0.0.1" || len(listener.Users) != 1 {
			continue
		}
		sock, err := net.Listen("tcp4", net.JoinHostPort(listener.Listen, strconv.Itoa(listener.Port)))
		if err != nil {
			return err
		}
		go func() {
			for {
				conn, err := sock.Accept()
				if err != nil {
					return
				}
				go fixtureSOCKS(conn, listener, front)
			}
		}()
	}
	<-time.After(2 * time.Minute)
	return nil
}

func fixtureSOCKS(conn net.Conn, listener fixtureListener, front fixtureProxy) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if !fixtureSOCKSAuth(conn, listener) {
		return
	}
	var header [4]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil || header[0] != 5 || header[1] != 1 || header[3] != 3 {
		return
	}
	var size [1]byte
	if _, err := io.ReadFull(conn, size[:]); err != nil {
		return
	}
	address := make([]byte, int(size[0])+2)
	if _, err := io.ReadFull(conn, address); err != nil {
		return
	}
	host := string(address[:len(address)-2])
	port := binary.BigEndian.Uint16(address[len(address)-2:])
	if !strings.HasPrefix(host, "soha-probe-") || !strings.HasSuffix(host, ".invalid") || port != 80 {
		return
	}
	upstream, err := net.DialTimeout("tcp4", net.JoinHostPort(front.Server, strconv.Itoa(front.Port)), time.Second)
	if err != nil {
		return
	}
	defer func() { _ = upstream.Close() }()
	_ = upstream.SetDeadline(time.Now().Add(5 * time.Second))
	auth := base64.StdEncoding.EncodeToString([]byte(front.Username + ":" + front.Password))
	target := net.JoinHostPort(host, "80")
	if _, err := fmt.Fprintf(upstream, "CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: Basic %s\r\n\r\n", target, target, auth); err != nil {
		return
	}
	reader := bufio.NewReader(upstream)
	response, err := http.ReadResponse(reader, &http.Request{Method: "CONNECT"})
	if err != nil {
		return
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != 200 {
		return
	}
	if _, err := conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0}); err != nil {
		return
	}
	done := make(chan struct{})
	go func() { _, _ = io.Copy(upstream, conn); close(done) }()
	_, _ = io.Copy(conn, reader)
	_ = conn.Close()
	_ = upstream.Close()
	<-done
}

func fixtureSOCKSAuth(conn net.Conn, listener fixtureListener) bool {
	var header [2]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil || header[0] != 5 || header[1] == 0 {
		return false
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return false
	}
	if _, err := conn.Write([]byte{5, 2}); err != nil {
		return false
	}
	if _, err := io.ReadFull(conn, header[:]); err != nil || header[0] != 1 {
		return false
	}
	user := make([]byte, int(header[1]))
	if _, err := io.ReadFull(conn, user); err != nil {
		return false
	}
	var size [1]byte
	if _, err := io.ReadFull(conn, size[:]); err != nil {
		return false
	}
	secret := make([]byte, int(size[0]))
	if _, err := io.ReadFull(conn, secret); err != nil {
		return false
	}
	if string(user) != listener.Users[0].Username || string(secret) != listener.Users[0].Password {
		return false
	}
	_, err := conn.Write([]byte{1, 0})
	return err == nil
}

func lifecycleFixture(t *testing.T, failure string) (Config, *atomic.Int32) {
	t.Helper()
	t.Setenv("SOHA_PROXY_ENGINE_FIXTURE", "1")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Setenv("SOHA_PROXY_FIXTURE_ROOT", dir)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	file := filepath.Join(dir, "engine.yaml")
	probes := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, err := root.ReadFile("engine.yaml")
		if err != nil {
			w.WriteHeader(503)
			return
		}
		if req.URL.Path == "/version" && strings.Contains(string(raw), "port: 7891") {
			probes.Add(1)
			if failure == "start failure" {
				w.WriteHeader(503)
				return
			}
			if failure == "readback changed" {
				if err := os.WriteFile(file, []byte("tampered"), 0600); err != nil {
					t.Error(err)
				}
			}
		}
		switch req.URL.Path {
		case "/version":
			_, _ = io.WriteString(w, `{"version":"test"}`)
		case "/configs":
			_, _ = io.WriteString(w, `{"mode":"rule"}`)
		case "/rules":
			rules, err := expectedMihomoRules(raw)
			if err != nil {
				w.WriteHeader(503)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"rules": rules})
		default:
			_, _ = io.WriteString(w, `{"uploadTotal":1,"downloadTotal":2,"connections":[]}`)
		}
	}))
	t.Cleanup(server.Close)
	return Config{Engine: domain.EngineMihomo, EngineBinary: binary, ConfigFile: file, StateDir: dir, ControllerURL: server.URL}, probes
}

func lifecycleDesired(revision int64, port int) domain.Configuration {
	return domain.Configuration{Revision: revision, Engine: domain.EngineMihomo, Enabled: true,
		Content: fmt.Sprintf("port: %d\n", port), ValidUntil: time.Now().Add(2 * time.Minute),
		Binding:    &domain.Binding{SiteID: "site-test", DeviceID: "device-test", ResourceIDs: []string{}},
		Protection: &domain.Protection{PolicyVersion: 1, CIDRs: []string{"10.0.0.0/8"}, Hosts: []string{"protected.example"}}}
}
