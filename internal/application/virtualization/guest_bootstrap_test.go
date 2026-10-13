package virtualization

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type publicGuestCredentials struct{}

func (publicGuestCredentials) PinSSHPublicKeys(_ context.Context, _ domainidentity.Principal, _ map[string]string, target domainsecret.Target) ([]domainsecret.Reference, []domainsecret.SSHPublicKey, error) {
	if target.Type != "vm_create" || target.Ref != "connection-one" || target.Purpose != "vm_cloud_init" {
		panic("incorrect guest target")
	}
	return []domainsecret.Reference{{URI: "soha://secrets/key/versions/1", Version: 1}}, []domainsecret.SSHPublicKey{{AuthorizedKey: "ssh-ed25519 public-only"}}, nil
}

type guestProofAdapter struct {
	fakeAdapter
	proof domain.VMIdentity
	input domain.AdapterCreateVMInput
}

func (a *guestProofAdapter) ValidateGuestBootstrap(_ context.Context, _ domain.AdapterConnection, input domain.AdapterCreateVMInput) (domain.VMIdentity, error) {
	a.input = input
	return a.proof, nil
}

func TestGuestImageProofPreservesSelectedNodeTransportAndRootSize(t *testing.T) {
	repo := newMemoryRepo()
	connection := repo.addConnection(domain.Connection{ID: "connection-one", Provider: ProviderPVE, Endpoint: "https://pve.example", Enabled: true})
	adapter := &guestProofAdapter{proof: domain.VMIdentity{ProviderVMID: "images:import/digest.qcow2", Name: "digest.qcow2", Node: "node-one", Generation: "sha256:digest"}}
	service := newTestService(repo, &captureOperations{}, adapter)
	service.guestCredentials = publicGuestCredentials{}
	params := map[string]any{"sshHost": "10.0.0.11", "storage": "local-lvm"}
	prepared := preparedVMCreate{connection: connection, sourceRef: adapter.proof.ProviderVMID,
		image: domain.Image{ID: "image-one", Config: map[string]any{"cloudInitSupported": true}},
		input: CreateVMInput{Node: "node-one", SourceMode: "cloud_image", DiskGiB: 20, ProviderParams: params,
			GuestAccess: &sohaapi.VirtualMachineGuestAccessInput{Username: "soha", PublicKeyRefs: []string{"soha://secrets/key"}}}}
	if _, _, err := service.prepareGuestBootstrap(t.Context(), testPrincipal(), &prepared); err != nil {
		t.Fatal(err)
	}
	assertInput := func() {
		t.Helper()
		if adapter.input.ProviderParams["sshHost"] != params["sshHost"] || adapter.input.DiskSize != "20Gi" {
			t.Fatalf("source inspection lost selected transport or root size: %+v", adapter.input)
		}
	}
	assertInput()
	task := domain.Task{Provider: ProviderPVE, ConnectionID: connection.ID, Payload: map[string]any{
		"guestAccess": prepared.input.GuestAccess, "sourceMode": "cloud_image", "diskGiB": 20, "providerParams": params,
		"guestSourceProof": adapter.proof, "guestPublicKeyRefs": []domainsecret.Reference{{Alias: "SSH_KEY_0", URI: "soha://secrets/key/versions/1", Version: 1}}}}
	if err := service.validateQueuedGuestReferences(t.Context(), testPrincipal(), task); err != nil {
		t.Fatal(err)
	}
	assertInput()
}

func TestQueuedGuestBootstrapRejectsReplacedSource(t *testing.T) {
	repo := newMemoryRepo()
	connection := repo.addConnection(domain.Connection{ID: "connection-one", Provider: ProviderPVE, Endpoint: "https://pve.example", Enabled: true})
	adapter := &guestProofAdapter{proof: domain.VMIdentity{ProviderVMID: "9000", Name: "source", Node: "node-one", Generation: "original"}}
	service := newTestService(repo, &captureOperations{}, adapter)
	service.guestCredentials = publicGuestCredentials{}
	task := domain.Task{Provider: ProviderPVE, ConnectionID: connection.ID, Payload: map[string]any{"guestAccess": map[string]any{"username": "soha"}, "sourceMode": "template_clone", "guestSourceProof": adapter.proof, "guestPublicKeyRefs": []domainsecret.Reference{{Alias: "SSH_KEY_0", URI: "soha://secrets/key/versions/1", Version: 1}}}}
	if err := service.validateQueuedGuestReferences(t.Context(), testPrincipal(), task); err != nil {
		t.Fatal(err)
	}
	adapter.proof.Generation = "replacement"
	if err := service.validateQueuedGuestReferences(t.Context(), testPrincipal(), task); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("queued operation adopted replaced source", err)
	}
	delete(task.Payload, "guestSourceProof")
	if err := service.validateQueuedGuestReferences(t.Context(), testPrincipal(), task); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("missing proof accepted", err)
	}
}

func TestManagedGuestBootstrapBoundaries(t *testing.T) {
	service := &Service{guestCredentials: publicGuestCredentials{}}
	prepared := preparedVMCreate{input: CreateVMInput{GuestAccess: &sohaapi.VirtualMachineGuestAccessInput{Username: "soha", PublicKeyRefs: []string{"soha://secrets/key"}}}, image: domain.Image{ID: "image-one", Config: map[string]any{"cloudInitSupported": true}}, connection: domain.Connection{ID: "connection-one"}}
	content, refs, err := service.prepareGuestBootstrap(context.Background(), testPrincipal(), &prepared)
	if err != nil || len(refs) != 1 || !strings.Contains(content, "public-only") || strings.Contains(content, "PRIVATE KEY") || !strings.Contains(content, `"disable_root":true`) {
		t.Fatal("managed bootstrap failed", err)
	}
	tests := map[string]func(*preparedVMCreate){"raw conflict": func(p *preparedVMCreate) { p.input.CloudInit = "#cloud-config\nusers: []" }, "native conflict": func(p *preparedVMCreate) { p.input.ProviderParams = map[string]any{"sshkeys": "raw"} }, "unknown image": func(p *preparedVMCreate) { p.image.Config = nil }, "root account": func(p *preparedVMCreate) {
		access := *p.input.GuestAccess
		access.Username = "root"
		p.input.GuestAccess = &access
	}}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input := prepared
			mutate(&input)
			if _, _, err := service.prepareGuestBootstrap(context.Background(), testPrincipal(), &input); err == nil {
				t.Fatal("unsafe bootstrap accepted")
			}
		})
	}
}

func TestManagedGuestBootstrapMergesRuntimeCloudConfig(t *testing.T) {
	fields, err := guestBootstrapFields("#cloud-config\nwrite_files:\n  - path: /etc/soha-agent.yaml\n    permissions: '0600'\n    content: agent-registration\nruncmd:\n  - [systemctl, enable, --now, docker]\npackages: [docker.io]\n")
	if err != nil {
		t.Fatal(err)
	}
	content, err := managedGuestCloudInit("soha", []string{"ssh-ed25519 public-only"}, fields)
	if err != nil || !strings.Contains(content, "agent-registration") || !strings.Contains(content, "docker.io") || !strings.Contains(content, `"ssh_authorized_keys":["ssh-ed25519 public-only"]`) {
		t.Fatal("runtime bootstrap or managed public key lost", err)
	}
	for _, raw := range []string{
		"#cloud-config\nusers: []", "#cloud-config\nssh_pwauth: true", "#cloud-config\nchpasswd: {list: canary-password}",
		"#cloud-config\npackages: [a]\npackages: [b]", "#cloud-config\npackages: &a [docker]", "#cloud-config\npackages: [docker]\n---\nusers: []",
		"#cloud-config\nwrite_files: [{path: /home/soha/../soha/.ssh/authorized_keys, content: bad}]", "#cloud-config\nwrite_files: [{path: /etc/ssh/sshd_config, content: bad}]", "#cloud-config\npackages: !custom docker", "#cloud-config\n1: docker",
	} {
		if _, err := guestBootstrapFields(raw); !errors.Is(err, apperrors.ErrInvalidArgument) || strings.Contains(err.Error(), "canary-password") {
			t.Fatal("unsafe cloud-config accepted or leaked")
		}
	}
}
