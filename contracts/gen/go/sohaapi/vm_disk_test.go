package sohaapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVirtualMachineDiskChangePreservesTypedSDKAndSerial(t *testing.T) {
	value, err := json.Marshal(VirtualMachineDiskChange{Add: true, Serial: "personal-data"})
	if err != nil || !strings.Contains(string(value), `"serial":"personal-data"`) {
		t.Fatalf("typed disk identity was lost: %s %v", value, err)
	}
}

func TestVirtualMachineActionPreservesCapacityOptIn(t *testing.T) {
	value, err := json.Marshal(VirtualMachineActionInput{Action: VirtualMachineActionInputActionResize, RequireCapacity: true})
	if err != nil || !strings.Contains(string(value), `"requireCapacity":true`) {
		t.Fatalf("capacity opt-in was lost: %s %v", value, err)
	}
}
