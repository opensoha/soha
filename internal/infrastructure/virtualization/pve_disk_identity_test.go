package virtualization

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
)

func TestPVEDiskAdditionNeverReplacesOccupiedSlot(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writePVEAny(w, map[string]any{"digest": strings.Repeat("a", 40), "scsi0": "disk:vm-101-disk-0,size=40G"})
			return
		}
		writes++
		writePVEAny(w, nil)
	}))
	defer server.Close()
	_, err := NewPVEAdapter(server.Client()).ResizeVM(context.Background(), Connection{Endpoint: server.URL}, VM{ID: "101", Node: "pve-a"}, AdapterResizeVMInput{
		CPU: 4, Disks: []domain.AdapterDiskChange{{ID: "scsi0", Storage: "disk", SizeGiB: 8, Add: true, Serial: "personal-data"}},
	})
	if err == nil || writes != 0 {
		t.Fatalf("occupied disk must fail before any mutation: err=%v writes=%d", err, writes)
	}
	if details, ok := AdapterErrorDetails(err); !ok || !details.NoProviderWrite {
		t.Fatal("preflight failure lost proof that no provider write occurred")
	}
}

func TestPVEDiskWriteFailureNeverClaimsNoProviderWrite(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writePVEAny(w, map[string]any{"digest": strings.Repeat("a", 40), "scsi0": "disk:root,size=40G"})
			return
		}
		writes++
		http.Error(w, "provider failed after dispatch", http.StatusInternalServerError)
	}))
	defer server.Close()
	_, err := NewPVEAdapter(server.Client()).ResizeVM(context.Background(), Connection{Endpoint: server.URL}, VM{ID: "101", Node: "pve-a"}, AdapterResizeVMInput{
		Disks: []domain.AdapterDiskChange{{ID: "scsi1", Storage: "disk", SizeGiB: 8, Add: true, Serial: "personal-data"}},
	})
	details, _ := AdapterErrorDetails(err)
	if err == nil || writes != 1 || details.NoProviderWrite {
		t.Fatalf("provider write failure claimed no effect: err=%v writes=%d details=%#v", err, writes, details)
	}
}

func TestPVEDiskPlanRejectsInvalidChangesBeforeWriting(t *testing.T) {
	for _, change := range []domain.AdapterDiskChange{
		{ID: "scsi0", SizeGiB: 8},
		{ID: "scsi2", SizeGiB: 8},
		{ID: "scsihw", SizeGiB: 8, Storage: "disk", Add: true},
		{ID: "scsi1", SizeGiB: 8, Storage: "disk,media=cdrom", Add: true},
		{ID: "scsi1", SizeGiB: 8, Storage: "disk", Add: true, Serial: "data,media=cdrom"},
		{ID: "scsi1", SizeGiB: 8, Storage: "disk", Add: true, Serial: "123456789012345678901"},
		{ID: "scsi0", SizeGiB: 80, Serial: "new-identity"},
	} {
		_, _, err := preparePVEDiskChanges(map[string]any{"scsi0": "disk:root,size=40G"}, []domain.AdapterDiskChange{change}, "disk")
		if err == nil {
			t.Errorf("invalid disk accepted: %#v", change)
		}
	}
	for _, changes := range [][]domain.AdapterDiskChange{
		{{ID: "scsi1", SizeGiB: 8, Add: true}, {ID: "scsi1", SizeGiB: 8, Add: true}},
		{{ID: "scsi1", SizeGiB: 8, Add: true, Serial: "same-data"}, {ID: "scsi2", SizeGiB: 8, Add: true, Serial: "same-data"}},
	} {
		if _, _, err := preparePVEDiskChanges(map[string]any{}, changes, "disk"); err == nil {
			t.Error("duplicate disk slot or identity was accepted")
		}
	}
}

func TestPVECreatePreservesSerialAndProtectsBootDisk(t *testing.T) {
	input := CreateVMInput{Name: "personal", Node: "pve-a", DiskSize: "40G", ProviderParams: map[string]any{"vmid": "101", "storage": "disk"}, Disks: []domain.AdapterDiskChange{{ID: "scsi1", SizeGiB: 8, Add: true, Serial: "personal-data"}}}
	adapter := NewPVEAdapter(nil)
	plan, err := adapter.preparePVECreate(context.Background(), Connection{}, input)
	if err != nil || pveCreatePayload(plan)["scsi1"] != "disk:8,serial=personal-data" {
		t.Fatalf("new VM did not preserve disk identity: %v", err)
	}
	input.Disks[0].ID = "scsi0"
	if _, err := adapter.preparePVECreate(context.Background(), Connection{}, input); err == nil {
		t.Fatal("additional disk replaced the boot disk")
	}
}

func TestKubeVirtUnimplementedDiskChangesFailBeforeProviderAccess(t *testing.T) {
	adapter := NewKubeVirtAdapter(nil)
	changes := []domain.AdapterDiskChange{{ID: "data", SizeGiB: 8, Add: true, Serial: "personal-data"}}
	if _, err := adapter.PrepareVMCreate(context.Background(), Connection{}, CreateVMInput{Name: "personal", Disks: changes}); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("prepare ignored disk request: %v", err)
	}
	if _, err := adapter.CreateVM(context.Background(), Connection{}, CreateVMInput{Name: "personal", Disks: changes}); !errors.Is(err, domain.ErrUnsupported) {
		t.Fatalf("create ignored disk request: %v", err)
	}
	for _, input := range []AdapterResizeVMInput{{CPU: 4, DiskGiB: 80}, {CPU: 4, Disks: changes}} {
		if _, err := adapter.ResizeVM(context.Background(), Connection{}, VM{Name: "personal"}, input); !errors.Is(err, domain.ErrUnsupported) {
			t.Fatalf("resize partially applied an unsupported request: %v", err)
		}
	}
}

func TestPVEDiskIdentityFencesAdditionAndRetry(t *testing.T) {
	config := map[string]any{"digest": strings.Repeat("a", 40), "scsi0": "disk:vm-101-disk-0,size=40G"}
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writePVEAny(w, config)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["digest"] != config["digest"] || payload["scsi1"] != "disk:8,serial=personal-data" {
			t.Errorf("unfenced or unidentified disk: %#v", payload)
		}
		writes++
		config["scsi1"] = "disk:vm-101-disk-1,size=8G,serial=personal-data"
		writePVEAny(w, nil)
	}))
	defer server.Close()
	adapter, conn, vm := NewPVEAdapter(server.Client()), Connection{Endpoint: server.URL}, VM{ID: "101", Node: "pve-a"}
	input := AdapterResizeVMInput{Disks: []domain.AdapterDiskChange{{ID: "scsi1", Storage: "disk", SizeGiB: 8, Add: true, Serial: "personal-data"}}}
	for range 2 {
		if _, err := adapter.ResizeVM(context.Background(), conn, vm, input); err != nil {
			t.Fatal(err)
		}
	}
	if writes != 1 {
		t.Fatalf("retry allocated another disk: %d writes", writes)
	}
	config["scsi1"] = "disk:vm-101-disk-1,size=8G,serial=another-data"
	if _, err := adapter.ResizeVM(context.Background(), conn, vm, input); err == nil || writes != 1 {
		t.Fatal("changed identity was accepted or overwritten")
	}
}
