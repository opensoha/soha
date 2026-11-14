package auth

import "testing"

func TestOutpostIdentityHeaders(t *testing.T) {
	for _, name := range []string{"X-Soha-User-ID", "X-Soha-Projects", "X-Soha-Tags", " x-auth-request-email "} {
		if !IsOutpostIdentityHeader(name) {
			t.Errorf("supported identity header %q rejected", name)
		}
	}
	for _, name := range []string{"Set-Cookie", "Authorization", "Location", "X-Forwarded-Host", "X-Soha-Outpost-Token", "X-Soha-Session-Token", "X-Auth-Email", "X-Soha-Email\r\nInjected"} {
		if IsOutpostIdentityHeader(name) {
			t.Errorf("unsupported header %q accepted", name)
		}
	}
}
