package virtualization

import "testing"

func TestPVEVMMetadataSeparatesRootAndTotalDiskCapacity(t *testing.T) {
	metadata := map[string]string{}
	addPVEVMConfigMetadata(metadata, "vm", map[string]any{
		"scsi0": "local-lvm:vm-51049-disk-0,size=40G",
		"scsi1": "local-lvm:vm-51049-disk-1,size=8G",
		"ide2":  "local-lvm:vm-51049-cloudinit,media=cdrom,size=4M",
	})
	if metadata["rootDiskGiB"] != "40" || metadata["diskGiB"] != "48" || metadata["diskCount"] != "2" {
		t.Fatalf("root and total conflated: %#v", metadata)
	}
	addPVEVMConfigMetadata(metadata, "vm", map[string]any{"scsi1": "local-lvm:data,size=8G"})
	if metadata["rootDiskGiB"] != "0" {
		t.Fatal("removed root retained its old observed size")
	}
}
