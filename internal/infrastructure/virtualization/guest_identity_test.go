package virtualization

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManagedPVEGuestSourceRequiresReadOnlyDriveAndGeneration(t *testing.T) {
	config := map[string]any{"name": "cloud-source", "ide2": "disk:vm-9000-cloudinit", "smbios1": "uuid=stable-source", "meta": "creation-qemu=9.0,ctime=1720000000"}
	reads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api2/json/nodes/node-one/qemu/9000/config" {
			t.Error("source configuration was mutated", r.Method, r.URL.Path)
		}
		reads++
		writePVEAny(w, config)
	}))
	defer server.Close()
	adapter := NewPVEAdapter(server.Client())
	connection := Connection{Endpoint: server.URL}
	input := CreateVMInput{Node: "node-one", SourceRef: "9000", SourceMode: "template_clone"}
	proof, err := adapter.ValidateGuestBootstrap(context.Background(), connection, input)
	if err != nil || proof.ProviderVMID != "9000" || proof.Generation != "stable-source/1720000000" {
		t.Fatalf("source proof=%+v err=%v", proof, err)
	}
	delete(config, "ide2")
	if _, err = adapter.ValidateGuestBootstrap(t.Context(), connection, input); err == nil {
		t.Fatal("image without cloud-init accepted")
	}
	config["ide2"] = "disk:vm-9000-cloudinit"
	delete(config, "smbios1")
	if _, err = adapter.ValidateGuestBootstrap(t.Context(), connection, input); err == nil {
		t.Fatal("unknown source generation accepted")
	}
	input.SourceMode = "create"
	prior := reads
	if _, err = adapter.ValidateGuestBootstrap(t.Context(), connection, input); err == nil || reads != prior {
		t.Fatal("unverified native image accepted")
	}
}

func TestPVEPersonalDataProofExcludesBootAndInitializationDisks(t *testing.T) {
	config := map[string]any{"boot": "order=scsi0;net0", "scsi0": "local:root,size=40G,serial=rootserial",
		"scsi1": "local:data,size=8G,serial=testdata01", "ide2": "local:cloudinit,size=8G,serial=initialization",
		"scsi2": "local:root,size=40G,serial=rootalias",
		"sata1": "local:installer,size=8G,serial=installer,media=cdrom"}
	disks := pvePersonalDataDisks(config)
	if len(disks) != 1 || disks[0].ID != "scsi1" || disks[0].Serial != "testdata01" || disks[0].SizeGiB != 8 {
		t.Fatalf("boot or initialization disk entered data proof: %+v", disks)
	}
	config["boot"] = "order=scsi0;scsi1"
	if len(pvePersonalDataDisks(config)) != 0 {
		t.Fatal("boot-eligible disk accepted as private data")
	}
	config["boot"] = ""
	if len(pvePersonalDataDisks(config)) != 0 {
		t.Fatal("unknown boot order accepted")
	}
}

func TestPVEPersonalDataProofRequiresExactWholeGiB(t *testing.T) {
	for _, test := range []struct {
		size string
		want int64
	}{{"8G", 8}, {"8192M", 8}, {"8.5G", 0}, {"512M", 0}, {"8191M", 0}, {"", 0}, {"NaNG", 0}, {"-8G", 0}, {"2147483648G", 0}} {
		t.Run(test.size, func(t *testing.T) {
			config := map[string]any{"boot": "order=scsi0", "scsi0": "local:root,size=40G",
				"scsi1": "local:data,serial=testdata01,size=" + test.size}
			disks := pvePersonalDataDisks(config)
			if len(disks) != 1 || disks[0].SizeGiB != test.want {
				t.Fatalf("data size %q was rounded into identity proof: %+v", test.size, disks)
			}
		})
	}
}
