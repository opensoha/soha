package middleware

import "testing"

func TestMatchesOriginFailsClosedForEmptyAndWildcardConfiguration(t *testing.T) {
	for _, allowed := range [][]string{nil, {}, {"*"}} {
		if matchesOrigin(allowed, "https://attacker.example") {
			t.Fatalf("matchesOrigin(%v) allowed an arbitrary credentialed origin", allowed)
		}
	}
	if !matchesOrigin([]string{"https://console.example"}, "https://console.example") {
		t.Fatal("configured origin was rejected")
	}
}
