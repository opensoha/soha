package main

import "testing"

func TestUnknownCommandFailsBeforeLoadingSecrets(t *testing.T) {
	if code := run([]string{"unknown"}); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}
