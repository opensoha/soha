package contenttransfer

import (
	"net/netip"
	"testing"
)

func TestPrivateAllowanceCannotReachMetadataOrLoopback(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "169.254.169.254", "::1", "fe80::1"} {
		if approvedIP(netip.MustParseAddr(address), []string{"10.0.0.0/8", "fc00::/7"}) {
			t.Fatal("unsafe outbound target allowed", address)
		}
	}
	if approvedIP(netip.MustParseAddr("10.1.2.3"), nil) {
		t.Fatal("private target allowed by default")
	}
	if !approvedIP(netip.MustParseAddr("10.1.2.3"), []string{"10.1.0.0/16"}) {
		t.Fatal("explicit private scope rejected")
	}
	if approvedIP(netip.MustParseAddr("10.2.1.1"), []string{"10.1.0.0/16"}) {
		t.Fatal("another private scope allowed")
	}
}
