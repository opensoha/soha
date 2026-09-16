package virtualization

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestPVEWorkerTemplateRejectsUnreservedDisksAndTracksConfiguration(t *testing.T) {
	config := map[string]any{"template": 1, "scsihw": "virtio-scsi-single", "scsi0": "disk:base-101-disk-0,size=20G", "ide2": "local:vm-101-cloudinit,media=cdrom", "cores": 2}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api2/json/nodes/pve-a/qemu/101/config" {
			t.Errorf("template inspection made unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", 400)
			return
		}
		writePVEAny(w, config)
	}))
	defer server.Close()
	adapter := NewPVEAdapter(server.Client())
	inspect := func() (string, error) {
		return adapter.InspectWorkerTemplate(context.Background(), Connection{Endpoint: server.URL}, "pve-a", "101", 80)
	}
	first, err := inspect()
	if err != nil || len(first) != 64 {
		t.Fatalf("supported image was rejected: %v", err)
	}
	config["cores"] = 4
	second, err := inspect()
	if err != nil || first == second {
		t.Fatalf("template config change was invisible: %v", err)
	}
	for _, change := range []struct {
		key   string
		value any
	}{{"template", 0}, {"lock", "clone"}, {"scsi0", "disk:base-101-disk-0,size=100G"}, {"scsi1", "disk:second,size=20G"}, {"ide2", "local:iso/install.iso,media=cdrom"}, {"efidisk0", "disk:efi,size=4M"}, {"tpmstate0", "disk:tpm,size=4M"}, {"hostpci0", "0000:01:00.0"}, {"hookscript", "local:snippets/setup.pl"}, {"net1", "virtio,bridge=vmbr1"}} {
		old, exists := config[change.key]
		config[change.key] = change.value
		if _, err := inspect(); err == nil {
			t.Errorf("unsupported image field %s was accepted", change.key)
		}
		if exists {
			config[change.key] = old
		} else {
			delete(config, change.key)
		}
	}
}

func TestPVEDiskIDsAndCloneCPUAllocation(t *testing.T) {
	for _, id := range []string{"scsi0", "virtio12", "sata0", "ide2"} {
		if !isPVEDiskID(id) {
			t.Errorf("disk %s was omitted", id)
		}
	}
	for _, id := range []string{"scsihw", "scsi", "ide-1", "sata1x", "virtio١"} {
		if isPVEDiskID(id) {
			t.Errorf("non-disk %s was counted as a disk", id)
		}
	}
	payload := pveCloudInitConfigPayload(CreateVMInput{CPU: 4}, "", "")
	if payload["cores"] != 4 || payload["sockets"] != 1 {
		t.Fatal("clone can inherit template sockets and exceed its reserved CPUs")
	}
}

func TestPVEReservedCloneChecksInheritedDemandBeforeWriting(t *testing.T) {
	reads, writes := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
			http.Error(w, "unexpected write", 400)
			return
		}
		reads++
		writePVEAny(w, map[string]any{"scsi0": "disk:root,size=100G"})
	}))
	defer server.Close()
	adapter := NewPVEAdapter(server.Client())
	input := CreateVMInput{Name: "worker", Node: "pve-a", TemplateID: "101", DiskSize: "80Gi", CapacityReserved: true, CloudInit: "#cloud-config\n", ProviderParams: map[string]any{"vmid": "702", "storage": "disk"}}
	if _, err := adapter.ObserveCapacity(context.Background(), Connection{Endpoint: server.URL}, input); err == nil {
		t.Fatal("oversized inherited disk admitted")
	}
	if _, err := adapter.CreateVM(context.Background(), Connection{Endpoint: server.URL}, input); err == nil {
		t.Fatal("changed clone template dispatched")
	}
	if reads != 2 || writes != 0 {
		t.Fatalf("checks must precede snippet or VM creation: reads=%d writes=%d", reads, writes)
	}
}

func TestPVEWorkerSystemIdentityComesFromOriginalOperation(t *testing.T) {
	id := uuid.NewString()
	for _, expected := range []string{id, "", uuid.NewString()} {
		payload := pveCloudInitConfigPayload(CreateVMInput{OperationID: id, WorkerSystemUUID: expected}, "", "")
		if expected == id {
			if payload["smbios1"] != "uuid="+id {
				t.Fatal("worker UUID was not fixed before boot")
			}
		} else if payload["smbios1"] != nil {
			t.Fatal("unrelated caller identity was used")
		}
	}
}
