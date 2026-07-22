package virtualization

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingPVESnippetWriter struct {
	calls []pveSnippetWriteCall
	err   error
}

type pveSnippetWriteCall struct {
	node     string
	storage  string
	filename string
	content  string
}

func (w *recordingPVESnippetWriter) WriteSnippet(_ context.Context, _ Connection, node string, storage string, filename string, content string, _ CreateVMInput) error {
	w.calls = append(w.calls, pveSnippetWriteCall{node: node, storage: storage, filename: filename, content: content})
	return w.err
}

func TestPVEAdapterTestConnection(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		if r.URL.Path != "/api2/json/nodes" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		writePVEData(w, []map[string]any{{"node": "pve-a", "status": "online"}})
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	result, err := adapter.TestConnection(context.Background(), Connection{Endpoint: server.URL})
	if err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	if !result.Healthy || result.Status != "healthy" {
		t.Fatalf("result = %#v, want healthy", result)
	}
	if strings.Join(seen, ",") != "GET /api2/json/nodes" {
		t.Fatalf("seen paths = %#v", seen)
	}
}

func TestPVEAdapterTestConnectionDegradesOnHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	result, err := adapter.TestConnection(context.Background(), Connection{Endpoint: server.URL})
	if err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	if result.Healthy || result.Status != "degraded" || result.Message == "" {
		t.Fatalf("result = %#v, want degraded with message", result)
	}
}

func TestPVEAdapterListVMDevices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api2/json/nodes/pve-a/qemu/101/config" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		writePVEAny(w, map[string]any{
			"scsi0": "local-lvm:vm-101-disk-0,size=20G",
			"scsi1": "ceph:vm-101-disk-1,size=64G",
			"ide2":  "local:iso/debian.iso,media=cdrom",
			"net0":  "virtio=AA:BB:CC:DD:EE:FF,bridge=vmbr0",
		})
	}))
	defer server.Close()

	devices, err := NewPVEAdapter(server.Client()).ListVMDevices(
		context.Background(),
		Connection{Endpoint: server.URL},
		VM{ID: "101", Node: "pve-a"},
	)
	if err != nil {
		t.Fatalf("ListVMDevices() error = %v", err)
	}
	if len(devices) != 3 {
		t.Fatalf("devices = %#v, want two disks and one network", devices)
	}
	if devices[0].ID != "net0" || devices[0].Network != "vmbr0" || devices[1].ID != "scsi0" || devices[1].Storage != "local-lvm" || devices[1].SizeGiB != 20 || devices[2].ID != "scsi1" || devices[2].Storage != "ceph" {
		t.Fatalf("devices = %#v", devices)
	}
}

func TestNextPVEDeviceIDSkipsOccupiedSlots(t *testing.T) {
	config := map[string]any{"scsi0": "disk-0", "scsi1": "disk-1", "net0": "network-0"}
	if got := nextPVEDeviceID(config, "scsi"); got != "scsi2" {
		t.Fatalf("next scsi id = %q, want scsi2", got)
	}
	if got := nextPVEDeviceID(config, "net"); got != "net1" {
		t.Fatalf("next network id = %q, want net1", got)
	}
}

func TestPVEAdapterHonorsInsecureSkipTLSVerify(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/nodes" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		writePVEData(w, []map[string]any{{"node": "pve-a", "status": "online"}})
	}))
	defer server.Close()

	adapter := NewPVEAdapter(nil)
	result, err := adapter.TestConnection(context.Background(), Connection{
		Endpoint:              server.URL,
		InsecureSkipTLSVerify: true,
	})
	if err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	if !result.Healthy || result.Status != "healthy" {
		t.Fatalf("result = %#v, want healthy", result)
	}
}

func TestPVEAdapterUsesTokenHeaderAndExpectedPaths(t *testing.T) {
	var seen []string
	var authHeaders []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/api2/json/nodes":
			writePVEData(w, []map[string]any{{"node": "pve-a", "status": "online"}})
		case "/api2/json/nodes/pve-a/network":
			writePVEData(w, []map[string]any{{"iface": "vmbr0", "type": "bridge", "active": 1}})
		case "/api2/json/nodes/pve-a/qemu":
			writePVEData(w, []map[string]any{{"vmid": 101, "name": "vm-a", "status": "running"}})
		case "/api2/json/nodes/pve-a/storage":
			writePVEData(w, []map[string]any{{"storage": "local", "type": "dir"}})
		case "/api2/json/nodes/pve-a/storage/local/content":
			writePVEData(w, []map[string]any{
				{"volid": "local:iso/demo.iso", "content": "iso"},
				{"volid": "local:vztmpl/debian.tar.zst", "content": "vztmpl"},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	result, err := adapter.SyncAssets(context.Background(), Connection{
		Endpoint: server.URL,
		Credential: map[string]any{
			"tokenID":     "root@pam!soha",
			"tokenSecret": "secret-token",
		},
	})
	if err != nil {
		t.Fatalf("SyncAssets() error = %v", err)
	}
	if result.Health.Status != "healthy" || len(result.Assets) != 6 {
		t.Fatalf("result = %#v", result)
	}
	for _, header := range authHeaders {
		if header != "PVEAPIToken=root@pam!soha=secret-token" {
			t.Fatalf("Authorization header = %q", header)
		}
	}
	want := []string{
		"GET /api2/json/nodes",
		"GET /api2/json/nodes/pve-a/network",
		"GET /api2/json/nodes/pve-a/qemu",
		"GET /api2/json/nodes/pve-a/storage",
		"GET /api2/json/nodes/pve-a/storage/local/content",
	}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Fatalf("seen paths = %#v", seen)
	}
}

func TestPVEAdapterUsesUsernamePasswordTicketAuth(t *testing.T) {
	var seen []string
	var csrfHeaders []string
	var ticketCookies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api2/json/access/ticket":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if r.Form.Get("username") != "root@pam" || r.Form.Get("password") != "secret-password" {
				t.Fatalf("login form = %#v", r.Form)
			}
			writePVEAny(w, map[string]any{"ticket": "ticket-1", "CSRFPreventionToken": "csrf-1"})
		case "/api2/json/nodes":
			if cookie, err := r.Cookie("PVEAuthCookie"); err == nil {
				ticketCookies = append(ticketCookies, cookie.Value)
			}
			csrfHeaders = append(csrfHeaders, r.Header.Get("CSRFPreventionToken"))
			writePVEData(w, []map[string]any{{"node": "pve-a", "status": "online"}})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	result, err := adapter.TestConnection(context.Background(), Connection{
		Endpoint: server.URL,
		Credential: map[string]any{
			"username": "root@pam",
			"password": "secret-password",
		},
	})
	if err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	if !result.Healthy || result.Status != "healthy" {
		t.Fatalf("result = %#v, want healthy", result)
	}
	if strings.Join(seen, ",") != "POST /api2/json/access/ticket,GET /api2/json/nodes" {
		t.Fatalf("seen paths = %#v", seen)
	}
	if strings.Join(ticketCookies, ",") != "ticket-1" || strings.Join(csrfHeaders, ",") != "csrf-1" {
		t.Fatalf("ticket cookies = %#v csrf headers = %#v", ticketCookies, csrfHeaders)
	}
}

func TestPVEAdapterSyncAssetsScansAllStoragesAndQEMUTemplates(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api2/json/nodes":
			writePVEData(w, []map[string]any{{"node": "pve-a", "status": "online"}})
		case "/api2/json/nodes/pve-a/network":
			writePVEData(w, []map[string]any{
				{"iface": "lo", "type": "loopback"},
				{"iface": "vmbr0", "type": "bridge", "active": 1, "cidr": "10.0.0.2/24"},
			})
		case "/api2/json/nodes/pve-a/qemu":
			writePVEData(w, []map[string]any{
				{"vmid": 101, "name": "vm-a", "status": "running", "cpus": 2, "maxmem": 4294967296},
				{"vmid": 9000, "name": "ubuntu-template", "template": 1, "status": "stopped"},
			})
		case "/api2/json/nodes/pve-a/storage":
			writePVEData(w, []map[string]any{
				{"storage": "local", "type": "dir", "content": "iso,vztmpl"},
				{"storage": "shared-nfs", "type": "nfs", "content": "iso,images"},
			})
		case "/api2/json/nodes/pve-a/storage/local/content":
			writePVEData(w, []map[string]any{
				{"volid": "local:iso/demo.iso", "content": "iso"},
				{"volid": "local:vztmpl/debian.tar.zst", "content": "vztmpl"},
			})
		case "/api2/json/nodes/pve-a/storage/shared-nfs/content":
			writePVEData(w, []map[string]any{{"volid": "shared-nfs:iso/ubuntu.iso", "content": "iso"}})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	result, err := adapter.SyncAssets(context.Background(), Connection{Endpoint: server.URL})
	if err != nil {
		t.Fatalf("SyncAssets() error = %v", err)
	}
	assertPVESyncedAssets(t, result, seen)
}

func assertPVESyncedAssets(t *testing.T, result AssetSyncResult, seen []string) {
	t.Helper()
	qemuTemplate, sharedISO, lxcTemplate, bridgeAsset, snippetStorage := classifyPVEAssets(t, result.Assets)
	if !qemuTemplate || !sharedISO || !lxcTemplate || !bridgeAsset || !snippetStorage {
		t.Fatalf("assets missing qemuTemplate=%v sharedISO=%v lxcTemplate=%v bridge=%v storageCapabilities=%v: %#v", qemuTemplate, sharedISO, lxcTemplate, bridgeAsset, snippetStorage, result.Assets)
	}
	if !strings.Contains(strings.Join(seen, ","), "GET /api2/json/nodes/pve-a/storage/shared-nfs/content") {
		t.Fatalf("seen paths = %#v", seen)
	}
}

func classifyPVEAssets(t *testing.T, assets []Asset) (bool, bool, bool, bool, bool) {
	t.Helper()
	var qemuTemplate, sharedISO, lxcTemplate, bridgeAsset, snippetStorage bool
	for _, asset := range assets {
		if asset.Type == "template" && asset.Metadata["sourceRef"] == "9000" {
			qemuTemplate = true
		}
		if asset.Type == "iso" && asset.Metadata["storage"] == "shared-nfs" {
			sharedISO = true
		}
		if asset.Type == "lxc_template" && asset.Metadata["sourceRef"] == "local:vztmpl/debian.tar.zst" {
			lxcTemplate = true
		}
		if asset.Type == "network" && asset.Name == "vmbr0" && asset.Metadata["bridge"] == "true" && asset.Metadata["active"] == "true" {
			bridgeAsset = true
		}
		if asset.Type == "storage" && asset.Name == "local" && asset.Metadata["supportsISO"] == "true" && asset.Metadata["supportsSnippets"] == "false" {
			snippetStorage = true
		}
		if asset.Type == "template" && asset.Metadata["sourceRef"] == "local:vztmpl/debian.tar.zst" {
			t.Fatalf("vztmpl storage content must not be exposed as a QEMU VM template: %#v", asset)
		}
	}
	return qemuTemplate, sharedISO, lxcTemplate, bridgeAsset, snippetStorage
}

func TestPVEAdapterSyncAssetsHonorsResourceTypeSwitch(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api2/json/nodes":
			writePVEData(w, []map[string]any{{"node": "pve-a", "status": "online"}})
		case "/api2/json/nodes/pve-a/qemu":
			writePVEData(w, []map[string]any{{"vmid": 101, "name": "vm-a", "status": "running"}})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	result, err := adapter.SyncAssets(context.Background(), Connection{
		Endpoint: server.URL,
		Options:  map[string]any{"syncResourceTypes": "vm"},
	})
	if err != nil {
		t.Fatalf("SyncAssets() error = %v", err)
	}
	if len(result.Assets) != 1 || result.Assets[0].Type != "qemu" || result.Assets[0].Name != "vm-a" {
		t.Fatalf("assets = %#v, want only qemu vm", result.Assets)
	}
	if strings.Contains(strings.Join(seen, ","), "/storage") || strings.Contains(strings.Join(seen, ","), "/network") {
		t.Fatalf("sync resource switch was ignored, seen paths = %#v", seen)
	}
}

func TestPVEAdapterCreateClonePayloadDoesNotLeakToken(t *testing.T) {
	bodies := map[string]string{}
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			bodies[r.URL.Path] = string(raw)
		}
		if strings.Contains(string(raw), "secret-token") {
			t.Fatalf("payload leaked token: %s", raw)
		}
		if r.Method == http.MethodGet && r.URL.Path == "/api2/json/nodes/pve-a/qemu/200/config" {
			writePVEAny(w, map[string]any{"scsi0": "local-lvm:vm-200-disk-0,size=3.5G"})
			return
		}
		writePVEData(w, []map[string]any{})
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	vm, err := adapter.CreateVM(context.Background(), Connection{
		Endpoint: server.URL,
		Credential: map[string]any{
			"tokenID":     "root@pam!soha",
			"tokenSecret": "secret-token",
		},
		Options: map[string]any{"vmid": "200", "defaultStorage": "local-lvm"},
	}, CreateVMInput{
		Name:             "clone-a",
		Node:             "pve-a",
		CPU:              2,
		Memory:           "4096",
		Network:          "virtio,bridge=vmbr0",
		DiskSize:         "20Gi",
		TemplateID:       "9000",
		SourceMode:       "template_clone",
		SourceRef:        "9000",
		StartAfterCreate: true,
		ProviderParams: map[string]any{
			"ipconfig0":  "ip=10.0.3.250/24,gw=10.0.3.254",
			"nameserver": "10.0.3.254",
			"sshkeys":    "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey user@example",
		},
	})
	if err != nil {
		t.Fatalf("CreateVM() error = %v", err)
	}
	assertPVECloneVM(t, vm)
	assertPVEClonePayloads(t, bodies)
	assertPVEClonePaths(t, paths)
}

func assertPVECloneVM(t *testing.T, vm VM) {
	t.Helper()
	if vm.ID != "200" || vm.Node != "pve-a" {
		t.Fatalf("vm = %#v", vm)
	}
}

func assertPVEClonePayloads(t *testing.T, bodies map[string]string) {
	t.Helper()
	cloneBody := bodies["/api2/json/nodes/pve-a/qemu/9000/clone"]
	if !strings.Contains(cloneBody, `"newid":"200"`) || !strings.Contains(cloneBody, `"storage":"local-lvm"`) || !strings.Contains(cloneBody, `"full":1`) || strings.Contains(cloneBody, "token") {
		t.Fatalf("clone payload = %s", cloneBody)
	}
	if strings.Contains(cloneBody, `"cores"`) || strings.Contains(cloneBody, `"memory"`) || strings.Contains(cloneBody, `"net0"`) {
		t.Fatalf("clone payload contains config-only fields: %s", cloneBody)
	}
	resizeBody := bodies["/api2/json/nodes/pve-a/qemu/200/resize"]
	if !strings.Contains(resizeBody, `"disk":"scsi0"`) || !strings.Contains(resizeBody, `"size":"20G"`) {
		t.Fatalf("resize payload = %s", resizeBody)
	}
	configBody := bodies["/api2/json/nodes/pve-a/qemu/200/config"]
	if !strings.Contains(configBody, `"cores":2`) || !strings.Contains(configBody, `"memory":4096`) || !strings.Contains(configBody, `"net0":"virtio,bridge=vmbr0"`) || !strings.Contains(configBody, `"ipconfig0":"ip=10.0.3.250/24,gw=10.0.3.254"`) || !strings.Contains(configBody, `"nameserver":"10.0.3.254"`) || !strings.Contains(configBody, `"sshkeys":"ssh-ed25519%20AAAAC3NzaC1lZDI1NTE5AAAAITestKey%20user%40example"`) {
		t.Fatalf("config payload = %s", configBody)
	}
}

func assertPVEClonePaths(t *testing.T, paths []string) {
	t.Helper()
	want := []string{
		"POST /api2/json/nodes/pve-a/qemu/9000/clone",
		"GET /api2/json/nodes/pve-a/qemu/200/config",
		"PUT /api2/json/nodes/pve-a/qemu/200/resize",
		"POST /api2/json/nodes/pve-a/qemu/200/config",
		"PUT /api2/json/nodes/pve-a/qemu/200/cloudinit",
		"POST /api2/json/nodes/pve-a/qemu/200/status/start",
		"GET /api2/json/nodes/pve-a/qemu/200/status/current",
	}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestPVEAdapterCreateISOPayloadUsesProviderParams(t *testing.T) {
	var body string
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			body = string(raw)
		}
		writePVEData(w, []map[string]any{})
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	vm, err := adapter.CreateVM(context.Background(), Connection{
		Endpoint: server.URL,
		Options:  map[string]any{"vmid": "201", "defaultNode": "pve-1", "defaultStorage": "local-lvm"},
	}, CreateVMInput{
		Name:         "iso-a",
		Architecture: "arm64",
		SourceMode:   "iso_install",
		SourceRef:    "local:iso/ubuntu.iso",
		DiskSize:     "20Gi",
		Memory:       "4096Mi",
		ProviderParams: map[string]any{
			"bridge":   "vmbr0",
			"storage":  "local-lvm",
			"cicustom": "user=local:snippets/docker-agent.yaml",
		},
	})
	if err != nil {
		t.Fatalf("CreateVM() error = %v", err)
	}
	if vm.ID != "201" || vm.Node != "pve-1" || vm.Metadata["vmid"] != "201" {
		t.Fatalf("vm = %#v", vm)
	}
	if !strings.Contains(body, `"arch":"aarch64"`) || !strings.Contains(body, `"cicustom":"user=local:snippets/docker-agent.yaml"`) || !strings.Contains(body, `"ide2":"local:iso/ubuntu.iso,media=cdrom"`) || !strings.Contains(body, `"memory":4096`) || !strings.Contains(body, `"net0":"virtio,bridge=vmbr0"`) || !strings.Contains(body, `"scsi0":"local-lvm:20"`) {
		t.Fatalf("payload = %s", body)
	}
	want := []string{"POST /api2/json/nodes/pve-1/qemu", "GET /api2/json/nodes/pve-1/qemu/201/status/current"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestNormalizePVEDiskSizeUsesCreateVolumeSyntax(t *testing.T) {
	cases := map[string]string{
		"20Gi":   "20",
		"20G":    "20",
		"1024Mi": "1",
		"512Mi":  "1",
		"1":      "1",
	}
	for input, want := range cases {
		if got := normalizePVEDiskSize(input); got != want {
			t.Fatalf("normalizePVEDiskSize(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPVEDiskSizeGiBParsesConfigDiskSize(t *testing.T) {
	cases := map[string]int{
		"local-lvm:vm-200-disk-0,size=3.5G": 4,
		"local-lvm:vm-200-disk-0,size=20G":  20,
		"20Gi":                              20,
		"512Mi":                             1,
	}
	for input, want := range cases {
		if got := pveDiskSizeGiB(input); got != want {
			t.Fatalf("pveDiskSizeGiB(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestMergePVECloudInitIdentityAddsUserAndSSHKeys(t *testing.T) {
	content := "#cloud-config\nwrite_files:\n  - path: /etc/soha-agent.yaml\n    content: |\n      control_plane:\n        enabled: true\nruncmd:\n  - [bash, -lc, 'echo ready']\n"
	got := mergePVECloudInitIdentity(content, map[string]any{
		"ciuser":  "ubuntu",
		"sshkeys": "ssh-ed25519%20AAAAC3NzaC1lZDI1NTE5AAAAITestKey%20user%40example",
	})
	for _, want := range []string{
		"user: ubuntu",
		"ssh_authorized_keys:",
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey user@example",
		"write_files:",
		"runcmd:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("merged cloud-init missing %q:\n%s", want, got)
		}
	}
}

func TestMergePVECloudInitIdentityPreservesExistingUsers(t *testing.T) {
	content := "#cloud-config\nusers:\n  - name: soha\n    sudo: ALL=(ALL) NOPASSWD:ALL\n"
	got := mergePVECloudInitIdentity(content, map[string]any{
		"ciuser":  "ubuntu",
		"sshkeys": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey user@example",
	})
	if strings.Contains(got, "user: ubuntu") {
		t.Fatalf("merged cloud-init overwrote explicit users:\n%s", got)
	}
	if !strings.Contains(got, "name: soha") || !strings.Contains(got, "ssh_authorized_keys:") {
		t.Fatalf("merged cloud-init =\n%s", got)
	}
}

func TestPVECloudInitConfigPayloadDoesNotDuplicateSSHKeysWithCICustom(t *testing.T) {
	payload := pveCloudInitConfigPayload(CreateVMInput{ProviderParams: map[string]any{
		"ciuser":  "alpine",
		"sshkeys": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey user@example",
	}}, "user=local:snippets/soha-102-cloud-init.yaml", "vmbr0")
	if payload["sshkeys"] != nil {
		t.Fatalf("payload duplicated sshkeys alongside cicustom: %#v", payload)
	}
	if payload["ciuser"] != "alpine" || payload["cicustom"] == nil {
		t.Fatalf("payload lost cloud-init identity or snippet reference: %#v", payload)
	}
}

func TestNormalizePVECloudInitSSHKeysPreservesBase64Plus(t *testing.T) {
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest+Key user@example"
	keys := normalizePVECloudInitSSHKeys(key)
	if len(keys) != 1 || keys[0] != key {
		t.Fatalf("normalizePVECloudInitSSHKeys() = %#v, want raw key preserved", keys)
	}
}

func TestPVESSHWriteSnippetCommandInstallsWorldReadableSnippet(t *testing.T) {
	command := pveSSHWriteSnippetCommand("/var/lib/vz/snippets", "soha-106-cloud-init.yaml")
	if !strings.Contains(command, "install -m 0644") {
		t.Fatalf("command = %q, want install -m 0644", command)
	}
	if !strings.Contains(command, "soha-106-cloud-init.yaml.tmp") || !strings.Contains(command, "rm -f") {
		t.Fatalf("command = %q, want temporary write and cleanup", command)
	}
}

func TestClassifyPVEHTTPErrorIncludesProviderMessage(t *testing.T) {
	err := classifyPVEHTTPError(http.StatusInternalServerError, "/nodes/cc/qemu", []byte(`{"message":"unable to parse lvm volume name '1G'\n"}`))
	if err == nil || !strings.Contains(err.Error(), "unable to parse lvm volume name") {
		t.Fatalf("error = %v, want provider message", err)
	}
}

func TestClassifyPVEHTTPErrorDetectsUnsupportedSnippetUpload(t *testing.T) {
	err := classifyPVEHTTPError(http.StatusBadRequest, "/nodes/cc/storage/local/upload", []byte(`{"errors":{"content":"value 'snippets' does not have a value in the enumeration 'iso, vztmpl, import'"},"message":"Parameter verification failed.\n"}`))
	details, ok := AdapterErrorDetails(err)
	if !ok {
		t.Fatalf("error = %T, want AdapterError", err)
	}
	if details.Reason != "snippet_upload_unsupported" || !strings.Contains(details.NextAction, "cicustom") || !strings.Contains(details.Message, "snippets") {
		t.Fatalf("details = %#v", details)
	}
}

func TestPVEAdapterUploadsRawCloudInitToSnippetStorage(t *testing.T) {
	var createBody string
	var uploadBody string
	var uploadContentType string
	var uploadCookie string
	var uploadCSRF string
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/api2/json/access/ticket":
			writePVEAny(w, map[string]any{"ticket": "ticket-upload", "CSRFPreventionToken": "csrf-upload"})
			return
		case "/api2/json/nodes/pve-1/storage/local/upload":
			uploadBody = string(raw)
			uploadContentType = r.Header.Get("Content-Type")
			if cookie, err := r.Cookie("PVEAuthCookie"); err == nil {
				uploadCookie = cookie.Value
			}
			uploadCSRF = r.Header.Get("CSRFPreventionToken")
		case "/api2/json/nodes/pve-1/qemu":
			createBody = string(raw)
		}
		writePVEData(w, []map[string]any{})
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	_, err := adapter.CreateVM(context.Background(), Connection{
		Endpoint: server.URL,
		Credential: map[string]any{
			"username": "root@pam",
			"password": "secret-password",
		},
		Options: map[string]any{"vmid": "202", "defaultNode": "pve-1", "defaultStorage": "local-lvm", "defaultSnippetStorage": "local"},
	}, CreateVMInput{
		Name:       "docker-agent-a",
		SourceMode: "iso_install",
		SourceRef:  "local:iso/ubuntu.iso",
		CloudInit:  "#cloud-config\npackages:\n  - docker.io",
		DiskSize:   "20Gi",
		Memory:     "4096Mi",
		ProviderParams: map[string]any{
			"ciuser":  "ubuntu",
			"sshkeys": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey user@example",
		},
	})
	if err != nil {
		t.Fatalf("CreateVM() error = %v", err)
	}
	if !strings.HasPrefix(uploadContentType, "multipart/form-data") || !strings.Contains(uploadBody, "#cloud-config") || !strings.Contains(uploadBody, "snippets") {
		t.Fatalf("upload content type=%q body=%s", uploadContentType, uploadBody)
	}
	if !strings.Contains(uploadBody, "user: ubuntu") || !strings.Contains(uploadBody, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey user@example") {
		t.Fatalf("upload body did not preserve PVE cloud-init identity: %s", uploadBody)
	}
	if uploadCookie != "ticket-upload" || uploadCSRF != "csrf-upload" {
		t.Fatalf("upload auth cookie=%q csrf=%q", uploadCookie, uploadCSRF)
	}
	if !strings.Contains(createBody, `"cicustom":"user=local:snippets/soha-202-cloud-init.yaml"`) {
		t.Fatalf("create body = %s", createBody)
	}
	want := []string{
		"POST /api2/json/access/ticket",
		"POST /api2/json/nodes/pve-1/storage/local/upload",
		"POST /api2/json/access/ticket",
		"POST /api2/json/nodes/pve-1/qemu",
		"POST /api2/json/access/ticket",
		"GET /api2/json/nodes/pve-1/qemu/202/status/current",
	}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestPVEAdapterFallsBackToSSHWhenSnippetUploadUnsupported(t *testing.T) {
	var createBody string
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/api2/json/access/ticket":
			writePVEAny(w, map[string]any{"ticket": "ticket-upload", "CSRFPreventionToken": "csrf-upload"})
			return
		case "/api2/json/nodes/pve-1/storage/local/upload":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errors":{"content":"value 'snippets' does not have a value in the enumeration 'iso, vztmpl, import'"},"message":"Parameter verification failed.\n"}`))
			return
		case "/api2/json/nodes/pve-1/qemu":
			createBody = string(raw)
		}
		writePVEData(w, []map[string]any{})
	}))
	defer server.Close()

	writer := &recordingPVESnippetWriter{}
	adapter := NewPVEAdapter(server.Client())
	adapter.snippetWriter = writer
	_, err := adapter.CreateVM(context.Background(), Connection{
		Endpoint: server.URL,
		Credential: map[string]any{
			"username": "root@pam",
			"password": "secret-password",
		},
		Options: map[string]any{"vmid": "203", "defaultNode": "pve-1", "defaultStorage": "local-lvm", "defaultSnippetStorage": "local"},
	}, CreateVMInput{
		Name:       "docker-agent-b",
		SourceMode: "iso_install",
		SourceRef:  "local:iso/ubuntu.iso",
		CloudInit:  "#cloud-config\npackages:\n  - docker.io",
		DiskSize:   "20Gi",
		Memory:     "4096Mi",
	})
	if err != nil {
		t.Fatalf("CreateVM() error = %v", err)
	}
	if len(writer.calls) != 1 {
		t.Fatalf("snippet writer calls = %#v, want one call", writer.calls)
	}
	call := writer.calls[0]
	if call.node != "pve-1" || call.storage != "local" || call.filename != "soha-203-cloud-init.yaml" || !strings.Contains(call.content, "docker.io") {
		t.Fatalf("snippet writer call = %#v", call)
	}
	if !strings.Contains(createBody, `"cicustom":"user=local:snippets/soha-203-cloud-init.yaml"`) {
		t.Fatalf("create body = %s", createBody)
	}
	want := []string{
		"POST /api2/json/access/ticket",
		"POST /api2/json/nodes/pve-1/storage/local/upload",
		"POST /api2/json/access/ticket",
		"POST /api2/json/nodes/pve-1/qemu",
		"POST /api2/json/access/ticket",
		"GET /api2/json/nodes/pve-1/qemu/203/status/current",
	}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestPVEAdapterSnippetWriteMethodSSHSkipsRESTUpload(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api2/json/access/ticket":
			writePVEAny(w, map[string]any{"ticket": "ticket-upload", "CSRFPreventionToken": "csrf-upload"})
		case "/api2/json/nodes/pve-1/qemu", "/api2/json/nodes/pve-1/qemu/204/status/current":
			writePVEData(w, []map[string]any{})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	writer := &recordingPVESnippetWriter{}
	adapter := NewPVEAdapter(server.Client())
	adapter.snippetWriter = writer
	_, err := adapter.CreateVM(context.Background(), Connection{
		Endpoint: server.URL,
		Credential: map[string]any{
			"username": "root@pam",
			"password": "secret-password",
		},
		Options: map[string]any{"vmid": "204", "defaultNode": "pve-1", "defaultStorage": "local-lvm", "defaultSnippetStorage": "local"},
	}, CreateVMInput{
		Name:       "docker-agent-c",
		SourceMode: "iso_install",
		SourceRef:  "local:iso/ubuntu.iso",
		CloudInit:  "#cloud-config\npackages:\n  - docker.io",
		DiskSize:   "20Gi",
		ProviderParams: map[string]any{
			"snippetWriteMethod": "ssh",
		},
	})
	if err != nil {
		t.Fatalf("CreateVM() error = %v", err)
	}
	if len(writer.calls) != 1 || writer.calls[0].filename != "soha-204-cloud-init.yaml" {
		t.Fatalf("snippet writer calls = %#v", writer.calls)
	}
	want := []string{
		"POST /api2/json/access/ticket",
		"POST /api2/json/nodes/pve-1/qemu",
		"POST /api2/json/access/ticket",
		"GET /api2/json/nodes/pve-1/qemu/204/status/current",
	}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestPVEAdapterCreateISOPayloadFallsBackToNextIDAndFetchesStatus(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api2/json/cluster/nextid":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":"310"}`))
		case "/api2/json/nodes/pve-a/qemu":
			writePVEData(w, []map[string]any{})
		case "/api2/json/nodes/pve-a/qemu/310/status/current":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"name":"iso-b","status":"running","qmpstatus":"running"}}`))
		default:
			writePVEData(w, []map[string]any{})
		}
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	vm, err := adapter.CreateVM(context.Background(), Connection{
		Endpoint: server.URL,
		Options:  map[string]any{"defaultNode": "pve-a", "defaultStorage": "local-lvm"},
	}, CreateVMInput{
		Name:       "iso-b",
		SourceMode: "iso_install",
		SourceRef:  "local:iso/debian.iso",
		DiskSize:   "10Gi",
	})
	if err != nil {
		t.Fatalf("CreateVM() error = %v", err)
	}
	if vm.ID != "310" || vm.Name != "iso-b" || vm.Status != "running" || vm.Metadata["qmpstatus"] != "running" {
		t.Fatalf("vm = %#v", vm)
	}
	want := []string{
		"GET /api2/json/cluster/nextid",
		"POST /api2/json/nodes/pve-a/qemu",
		"GET /api2/json/nodes/pve-a/qemu/310/status/current",
	}
	if strings.Join(paths[:3], ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestPVEAdapterCreateVMWaitsForUPIDAndReadsGuestAgentIP(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/api2/json/nodes/pve-a/qemu":
			writePVEAny(w, "UPID:pve-a:00000001:00000002:00000003:qmcreate:320:root@pam:")
		case strings.Contains(r.URL.Path, "/api2/json/nodes/pve-a/tasks/"):
			writePVEAny(w, map[string]any{"status": "stopped", "exitstatus": "OK"})
		case r.URL.Path == "/api2/json/nodes/pve-a/qemu/320/status/current":
			writePVEAny(w, map[string]any{"name": "iso-c", "status": "running", "qmpstatus": "running"})
		case r.URL.Path == "/api2/json/nodes/pve-a/qemu/320/agent/network-get-interfaces":
			writePVEAny(w, map[string]any{
				"result": []map[string]any{
					{"name": "lo", "ip-addresses": []map[string]any{{"ip-address": "127.0.0.1"}}},
					{"name": "eth0", "ip-addresses": []map[string]any{
						{"ip-address": "10.0.0.22", "ip-address-type": "ipv4"},
						{"ip-address": "fe80::1", "ip-address-type": "ipv6"},
					}},
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	vm, err := adapter.CreateVM(context.Background(), Connection{
		Endpoint: server.URL,
		Options:  map[string]any{"vmid": "320", "taskPollIntervalMillis": 1},
	}, CreateVMInput{
		Name:       "iso-c",
		Node:       "pve-a",
		SourceMode: "iso_install",
		SourceRef:  "local:iso/debian.iso",
	})
	if err != nil {
		t.Fatalf("CreateVM() error = %v", err)
	}
	if len(vm.IPAddresses) != 1 || vm.IPAddresses[0] != "10.0.0.22" || vm.Endpoint != "10.0.0.22" {
		t.Fatalf("vm IP/endpoint = %#v endpoint=%q", vm.IPAddresses, vm.Endpoint)
	}
	if vm.Metadata["pveCreateUpid"] == "" || vm.Metadata["ipAddress"] != "10.0.0.22" {
		t.Fatalf("metadata = %#v", vm.Metadata)
	}
	want := []string{
		"POST /api2/json/nodes/pve-a/qemu",
	}
	if !strings.Contains(strings.Join(paths, ","), strings.Join(want, ",")) || !strings.Contains(strings.Join(paths, ","), "/tasks/") || !strings.Contains(strings.Join(paths, ","), "/agent/network-get-interfaces") {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestPVEAdapterGetVMMetricsNormalizesRRDUnits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/nodes/pve-a/qemu/101/rrddata" || r.URL.Query().Get("timeframe") != "hour" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		writePVEData(w, []map[string]any{
			{"time": 1700000000, "cpu": 0.42, "mem": 1073741824, "netin": 2048, "netout": 4096},
		})
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	result, err := adapter.GetVMMetrics(context.Background(), Connection{Endpoint: server.URL}, VM{Node: "pve-a", Metadata: map[string]string{"vmid": "101"}}, 60, 60)
	if err != nil {
		t.Fatalf("GetVMMetrics() error = %v", err)
	}
	if !result.Ready || result.Source != "pve-rrd" {
		t.Fatalf("metrics result = %#v, want ready pve-rrd", result)
	}
	values := map[string]MetricSeries{}
	for _, series := range result.Series {
		values[series.Key] = series
	}
	if values["cpu"].Unit != "percent" || len(values["cpu"].Points) != 1 || values["cpu"].Points[0].Value != 42 {
		t.Fatalf("cpu series = %#v, want 42 percent", values["cpu"])
	}
	if values["memory"].Unit != "bytes" || values["memory"].Points[0].Value != 1073741824 {
		t.Fatalf("memory series = %#v", values["memory"])
	}
	if values["networkRx"].Unit != "bytes/s" || values["networkRx"].Points[0].Value != 2048 || values["networkTx"].Points[0].Value != 4096 {
		t.Fatalf("network series rx=%#v tx=%#v", values["networkRx"], values["networkTx"])
	}
}

func TestPVEAdapterGetConsoleURLReturnsBackendWebsocketURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/nodes/pve-a/qemu/101/vncproxy":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"ticket":"ticket-1","port":"5901"}}`))
		default:
			writePVEData(w, []map[string]any{})
		}
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	result, err := adapter.GetConsoleURL(context.Background(), Connection{Endpoint: server.URL, InsecureSkipTLSVerify: true}, VM{ID: "vm-local", Node: "pve-a", Metadata: map[string]string{"vmid": "101"}})
	if err != nil {
		t.Fatalf("GetConsoleURL() error = %v", err)
	}
	if !result.Ready || result.Provider != "pve" || result.ProxyMode != "backend-ws-proxy" {
		t.Fatalf("result = %#v", result)
	}
	if result.URL != "/api/v1/virtualization/vms/vm-local/console/novnc" {
		t.Fatalf("result.URL = %q", result.URL)
	}
	if !strings.Contains(result.BackendURL, "/api2/json/nodes/pve-a/qemu/101/vncwebsocket") || !strings.Contains(result.BackendURL, "port=5901") || !strings.Contains(result.BackendURL, "vncticket=ticket-1") {
		t.Fatalf("result.BackendURL = %q", result.BackendURL)
	}
	if !result.BackendTLS.InsecureSkipVerify {
		t.Fatalf("BackendTLS.InsecureSkipVerify = false, want true")
	}
}

func TestPVEAdapterGetConsoleURLAuthenticatesVNCProxyAndPreservesEndpointBasePath(t *testing.T) {
	var method string
	var authHeader string
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pve/api2/json/nodes/pve-a/qemu/101/vncproxy":
			method = r.Method
			authHeader = r.Header.Get("Authorization")
			raw, _ := io.ReadAll(r.Body)
			body = string(raw)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"ticket":"ticket-2","port":5902}}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	result, err := adapter.GetConsoleURL(context.Background(), Connection{
		Endpoint: server.URL + "/pve",
		Credential: map[string]any{
			"tokenID":     "root@pam!soha",
			"tokenSecret": "secret-token",
		},
	}, VM{ID: "vm-local", Node: "pve-a", Metadata: map[string]string{"vmid": "101"}})
	if err != nil {
		t.Fatalf("GetConsoleURL() error = %v", err)
	}
	if method != http.MethodPost || authHeader != "PVEAPIToken=root@pam!soha=secret-token" || !strings.Contains(body, `"websocket":"1"`) {
		t.Fatalf("vncproxy method=%q auth=%q body=%s", method, authHeader, body)
	}
	if !strings.Contains(result.BackendURL, "/pve/api2/json/nodes/pve-a/qemu/101/vncwebsocket") || !strings.Contains(result.BackendURL, "port=5902") || !strings.Contains(result.BackendURL, "vncticket=ticket-2") {
		t.Fatalf("result.BackendURL = %q", result.BackendURL)
	}
	if got := ConsoleBackendHeaders(result).Get("Authorization"); got != "PVEAPIToken=root@pam!soha=secret-token" {
		t.Fatalf("backend Authorization = %q", got)
	}
}

func TestPVEAdapterPowerActions(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		writePVEData(w, []map[string]any{})
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	conn := Connection{Endpoint: server.URL, Credential: map[string]any{"ticket": "ticket-value", "csrfToken": "csrf"}}
	for _, action := range []PowerAction{PowerActionStart, PowerActionStop, PowerActionRestart, PowerActionDelete} {
		if _, err := adapter.PowerAction(context.Background(), conn, VM{ID: "100", Node: "pve-a"}, action); err != nil {
			t.Fatalf("PowerAction(%s) error = %v", action, err)
		}
	}
	want := []string{
		"POST /api2/json/nodes/pve-a/qemu/100/status/start",
		"POST /api2/json/nodes/pve-a/qemu/100/status/stop",
		"POST /api2/json/nodes/pve-a/qemu/100/status/reboot",
		"GET /api2/json/nodes/pve-a/qemu/100/config",
		"GET /api2/json/nodes/pve-a/qemu/100/status/current",
		"DELETE /api2/json/nodes/pve-a/qemu/100",
	}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestPVEAdapterDeleteCleansGeneratedCloudInitSnippet(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api2/json/nodes/pve-a/qemu/205/config":
			writePVEAny(w, map[string]any{"cicustom": "user=local:snippets/soha-205-cloud-init.yaml"})
		case "/api2/json/nodes/pve-a/qemu/205/status/current":
			writePVEAny(w, map[string]any{"status": "stopped"})
		case "/api2/json/nodes/pve-a/qemu/205":
			writePVEData(w, []map[string]any{})
		case "/api2/json/nodes/pve-a/storage/local/content/local:snippets/soha-205-cloud-init.yaml":
			writePVEData(w, []map[string]any{})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	conn := Connection{Endpoint: server.URL, Credential: map[string]any{"ticket": "ticket-value", "csrfToken": "csrf"}}
	result, err := adapter.PowerAction(context.Background(), conn, VM{ID: "205", Node: "pve-a"}, PowerActionDelete)
	if err != nil {
		t.Fatalf("PowerAction(delete) error = %v", err)
	}
	if !strings.Contains(result.Message, "snippet cleaned up") {
		t.Fatalf("result message = %q", result.Message)
	}
	want := []string{
		"GET /api2/json/nodes/pve-a/qemu/205/config",
		"GET /api2/json/nodes/pve-a/qemu/205/status/current",
		"DELETE /api2/json/nodes/pve-a/qemu/205",
		"DELETE /api2/json/nodes/pve-a/storage/local/content/local:snippets/soha-205-cloud-init.yaml",
	}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestPVEAdapterDeleteStopsRunningVMBeforeDestroy(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/api2/json/nodes/pve-a/qemu/206/config":
			writePVEAny(w, map[string]any{"cicustom": "user=local:snippets/soha-206-cloud-init.yaml"})
		case r.URL.Path == "/api2/json/nodes/pve-a/qemu/206/status/current":
			writePVEAny(w, map[string]any{"status": "running", "qmpstatus": "running"})
		case r.URL.Path == "/api2/json/nodes/pve-a/qemu/206/status/stop":
			writePVEAny(w, "UPID:stop")
		case r.URL.Path == "/api2/json/nodes/pve-a/qemu/206":
			writePVEAny(w, "UPID:destroy")
		case strings.Contains(r.URL.Path, "/api2/json/nodes/pve-a/tasks/UPID:stop/status"):
			writePVEAny(w, map[string]any{"status": "stopped", "exitstatus": "OK"})
		case strings.Contains(r.URL.Path, "/api2/json/nodes/pve-a/tasks/UPID:destroy/status"):
			writePVEAny(w, map[string]any{"status": "stopped", "exitstatus": "OK"})
		case r.URL.Path == "/api2/json/nodes/pve-a/storage/local/content/local:snippets/soha-206-cloud-init.yaml":
			writePVEData(w, []map[string]any{})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	conn := Connection{Endpoint: server.URL, Credential: map[string]any{"ticket": "ticket-value", "csrfToken": "csrf"}}
	result, err := adapter.PowerAction(context.Background(), conn, VM{ID: "206", Node: "pve-a"}, PowerActionDelete)
	if err != nil {
		t.Fatalf("PowerAction(delete) error = %v", err)
	}
	if result.Action != PowerActionDelete || !strings.Contains(result.Message, "snippet cleaned up") {
		t.Fatalf("result = %#v", result)
	}
	want := []string{
		"GET /api2/json/nodes/pve-a/qemu/206/config",
		"GET /api2/json/nodes/pve-a/qemu/206/status/current",
		"POST /api2/json/nodes/pve-a/qemu/206/status/stop",
		"GET /api2/json/nodes/pve-a/tasks/UPID:stop/status",
		"DELETE /api2/json/nodes/pve-a/qemu/206",
		"GET /api2/json/nodes/pve-a/tasks/UPID:destroy/status",
		"DELETE /api2/json/nodes/pve-a/storage/local/content/local:snippets/soha-206-cloud-init.yaml",
	}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %#v", paths)
	}
}

func writePVEData(w http.ResponseWriter, data []map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func writePVEAny(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}
