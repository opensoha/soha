package helmrelease

import "testing"

func TestDigestEmptyString(t *testing.T) {
	if got := Digest("   "); got != "" {
		t.Fatalf("Digest(empty) = %q, want empty", got)
	}
}
