package networkaccess

import "testing"

func TestVPNAssignmentRequiresOwnershipAndExplicitAssignment(t *testing.T) {
	subject := Subject{UserID: "user-1", Teams: []string{"team-1"}}
	device := Device{ID: "device-1", OwnerUserID: "user-1"}
	for _, tt := range []struct {
		name       string
		assignment VPNAssignment
		device     Device
		want       bool
	}{
		{"empty denies", VPNAssignment{}, device, false},
		{"assigned user", VPNAssignment{UserIDs: []string{"user-1"}}, device, true},
		{"assigned team", VPNAssignment{TeamIDs: []string{"team-1"}}, device, true},
		{"device only", VPNAssignment{DeviceIDs: []string{"device-1"}}, device, true},
		{"device restriction", VPNAssignment{UserIDs: []string{"user-1"}, DeviceIDs: []string{"another-device"}}, device, false},
		{"wrong owner", VPNAssignment{DeviceIDs: []string{"device-1"}}, Device{ID: "device-1", OwnerUserID: "other"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.assignment.Matches(subject, tt.device); got != tt.want {
				t.Fatalf("Matches=%v want %v", got, tt.want)
			}
		})
	}
}
