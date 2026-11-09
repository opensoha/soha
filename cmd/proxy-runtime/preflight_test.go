package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/opensoha/soha/internal/networkproxyruntime"
)

func TestKernelPreflightCLIHelper(t *testing.T) {
	if os.Getenv("SOHA_PREFLIGHT_CLI_HELPER") != "1" {
		return
	}
	os.Args = []string{"proxy-runtime", "--kernel-preflight", os.Getenv("SOHA_PREFLIGHT_TEST_INPUT")}
	main()
	os.Exit(0)
}

func TestKernelPreflightCLIRunsBeforeNormalTLSConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "candidate")
	config := filepath.Join(dir, "engine.conf")
	script := []byte("#!/bin/sh\nif [ \"$1\" = -v ]; then echo 'Mihomo Meta v1.19.30'; exit 0; fi\n[ \"$1\" = -t ]\n")
	content := []byte("port: 0\nrules: []\n")
	if err := os.WriteFile(binary, script, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, content, 0600); err != nil {
		t.Fatal(err)
	}
	input := networkproxyruntime.KernelPreflightInput{SchemaVersion: "kernel-preflight/v1", Engine: "mihomo", Binary: binary, ExpectedSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(script)), ExpectedVersion: "v1.19.30", SourceRef: "installer-test", ExpectedGOOS: runtime.GOOS, ExpectedGOARCH: runtime.GOARCH, ConfigFile: config, ConfigurationRevision: 1, ConfigurationHash: fmt.Sprintf("sha256:%x", sha256.Sum256(content)), ProtectionVersion: 1}
	raw, _ := json.Marshal(input)
	path := filepath.Join(dir, "private.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(self, "-test.run=^TestKernelPreflightCLIHelper$")
	command.Env = []string{"PATH=/usr/bin:/bin", "SOHA_PREFLIGHT_CLI_HELPER=1", "SOHA_PREFLIGHT_TEST_INPUT=" + path}
	output, err := command.Output()
	if err != nil {
		t.Fatalf("CLI failed: %v", err)
	}
	var report networkproxyruntime.KernelPreflightReport
	if json.Unmarshal(output, &report) != nil || !report.Compatible || report.ActualSHA256 != input.ExpectedSHA256 {
		t.Fatalf("CLI report %s", output)
	}
}
