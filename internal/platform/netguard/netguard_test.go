package netguard

import (
	"net"
	"testing"
)

func TestBlockedOutboundIP(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "10.0.0.1", "100.64.0.1", "169.254.169.254", "192.0.2.1", "198.18.0.1", "203.0.113.1", "::1", "2001:db8::1"} {
		if !BlockedOutboundIP(net.ParseIP(value)) {
			t.Fatalf("address %s should be blocked", value)
		}
	}
	if BlockedOutboundIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("public address should be allowed")
	}
}
