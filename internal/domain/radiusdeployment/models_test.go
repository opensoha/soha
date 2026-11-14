package radiusdeployment

import (
	"strings"
	"testing"
)

func TestApprovedNASMediumSSID(t *testing.T) {
	for _, tc := range []struct {
		medium, ssid string
		want         bool
	}{{"wired", "", true}, {"wired", "fake", false}, {"wifi", "", false}, {"wifi", "Soha", true}, {"wifi", strings.Repeat("a", 32), true}, {"wifi", strings.Repeat("a", 33), false}, {"wifi", strings.Repeat("网", 11), false}, {"wifi", strings.Repeat("网", 10), true}, {"unknown", "", false}} {
		if got := validSlotMedium(Slot{AccessMedium: tc.medium, SSID: tc.ssid}); got != tc.want {
			t.Fatalf("medium=%s SSID bytes=%d got=%v", tc.medium, len(tc.ssid), got)
		}
	}
}
