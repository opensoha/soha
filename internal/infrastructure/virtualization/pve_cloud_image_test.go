package virtualization

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"golang.org/x/crypto/ssh"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testImageDigest = "b2aca2bee42c7082fd6aba4a5bb1a89612087a405e99896920f3346f7b23b9d1"

func TestPVECloudImageBoundary(t *testing.T) {
	source := "cloud-images:import/" + testImageDigest + ".qcow2"
	if storage, digest, err := pveCloudImageSource(source); err != nil || storage != "cloud-images" || digest != testImageDigest {
		t.Fatal("valid source rejected", err)
	}
	for _, source := range []string{"/etc/shadow", "cloud-images:images/100/disk.qcow2", "cloud-images:import/../file.qcow2", "cloud-images:import/" + testImageDigest + ".qcow2,discard=on", "cloud-images:import/invalid.qcow2"} {
		if _, _, err := pveCloudImageSource(source); err == nil {
			t.Fatal("unsafe source accepted", source)
		}
	}
	tests := []struct {
		name, digest, format, backing, disk string
		size                                int64
		valid                               bool
		external                            string
	}{
		{"valid", testImageDigest, "qcow2", "", "20Gi", 3 << 30, true, ""}, {"digest changed", "different", "qcow2", "", "20Gi", 3 << 30, false, ""}, {"raw", testImageDigest, "raw", "", "20Gi", 3 << 30, false, ""}, {"backing", testImageDigest, "qcow2", "/etc/shadow", "20Gi", 3 << 30, false, ""}, {"undersized", testImageDigest, "qcow2", "", "2Gi", 3 << 30, false, ""}, {"invalid size", testImageDigest, "qcow2", "", "20Gi", 0, false, ""}, {"external data", testImageDigest, "qcow2", "", "20Gi", 3 << 30, false, "/etc/shadow"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"format": test.format, "virtual-size": test.size, "backing-filename": test.backing, "format-specific": map[string]any{"data": map[string]any{"data-file": test.external}}})
			err := validatePVECloudImageInfo([]byte(test.digest+"  /import/source.qcow2\n"+string(raw)), testImageDigest, test.disk)
			if (err == nil) != test.valid {
				t.Fatalf("unexpected source verdict: %v", err)
			}
		})
	}
	input := CreateVMInput{Name: "new-vm", SourceMode: "cloud_image", SourceRef: source, CPU: 2, Memory: "2048Mi", DiskSize: "20Gi", ProviderParams: map[string]any{"ipconfig0": "ip=dhcp"}}
	payload := pveCreatePayload(pveCreatePlan{input: input, storage: "local-lvm", bridge: "vmbr0", vmid: "51001", cicustom: "user=cloud-images:snippets/soha-51001-cloud-init.yaml"})
	if payload["scsi0"] != "local-lvm:0,import-from="+source || payload["ide2"] != "local-lvm:cloudinit" || payload["boot"] != "order=scsi0" || payload["agent"] != 1 || payload["ipconfig0"] != "ip=dhcp" {
		t.Fatalf("cloud image payload missing root import/cloud-init: %+v", payload)
	}
}

func TestPVECloudImageProofReadsOnlyTrustedSource(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	commands := make(chan string, 1)
	go serveCloudImageProof(listener, signer, commands)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api2/json/access/ticket" {
			// #nosec G101 -- Synthetic PVE authentication response used only by this fixture.
			writePVEAny(w, map[string]any{"ticket": "fixture-ticket", "CSRFPreventionToken": "fixture-csrf"})
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api2/json/storage/cloud-images" {
			t.Error("source storage mutated", r.Method, r.URL.Path)
		}
		writePVEAny(w, map[string]any{"type": "dir", "path": "/var/lib/cloud", "content": "import,snippets", "nodes": "node-one"})
	}))
	defer server.Close()
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	portNumber, _ := strconv.Atoi(port)
	connection := Connection{Endpoint: server.URL, Credential: map[string]any{"username": "root@pam", "password": "fixture-only"}, Options: map[string]any{"sshHost": host, "sshPort": portNumber, "sshHostKeys": map[string]any{"node-one": string(ssh.MarshalAuthorizedKey(signer.PublicKey()))}}}
	source := "cloud-images:import/" + testImageDigest + ".qcow2"
	identity, err := NewPVEAdapter(server.Client()).ValidateGuestBootstrap(context.Background(), connection, CreateVMInput{Node: "node-one", SourceRef: source, SourceMode: "cloud_image", DiskSize: "20Gi"})
	if err != nil || identity.Generation != "sha256:"+testImageDigest || identity.ProviderVMID != source {
		t.Fatalf("source proof rejected: %+v %v", identity, err)
	}
	select {
	case command := <-commands:
		if !strings.Contains(command, "lsattr -d") || !strings.Contains(command, "sha256sum") || !strings.Contains(command, "qemu-img info") || strings.Contains(command, "cat >") {
			t.Fatal("source inspection was not read-only")
		}
	case <-time.After(time.Second):
		t.Fatal("source command missing")
	}
}

func serveCloudImageProof(listener net.Listener, signer ssh.Signer, commands chan<- string) {
	raw, err := listener.Accept()
	if err != nil {
		return
	}
	defer func() { _ = raw.Close() }()
	_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
	config := &ssh.ServerConfig{PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) { return &ssh.Permissions{}, nil }}
	config.AddHostKey(signer)
	connection, channels, requests, err := ssh.NewServerConn(raw, config)
	if err != nil {
		return
	}
	defer func() { _ = connection.Close() }()
	go ssh.DiscardRequests(requests)
	for request := range channels {
		channel, requests, err := request.Accept()
		if err != nil {
			return
		}
		for req := range requests {
			if req.Type != "exec" {
				_ = req.Reply(false, nil)
				continue
			}
			var command struct{ Command string }
			if ssh.Unmarshal(req.Payload, &command) != nil {
				return
			}
			commands <- command.Command
			_ = req.Reply(true, nil)
			_, _ = io.WriteString(channel, testImageDigest+"  /var/lib/cloud/import/source.qcow2\n{\"format\":\"qcow2\",\"virtual-size\":3221225472}")
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Code uint32 }{0}))
			_ = channel.Close()
			return
		}
	}
}
