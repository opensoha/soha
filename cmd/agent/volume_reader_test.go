package main

import (
	"path/filepath"
	"testing"
)

func TestMalformedVolumeReaderInvocationNeverBootstrapsAgent(t *testing.T) {
	t.Setenv("SOHA_AGENT_CONFIG_FILE", filepath.Join(t.TempDir(), "must-not-load.yaml"))
	if code := run([]string{"volume-reader", "unexpected"}); code != 2 {
		t.Fatalf("exit=%d, want usage error without Agent bootstrap", code)
	}
}
