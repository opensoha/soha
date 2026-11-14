package networkproxyruntime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/proxyadapter"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

func TestReferenceStdioProcess(t *testing.T) {
	if os.Getenv("SOHA_REFERENCE_FIXTURE") != "1" {
		return
	}
	if err := ServeReferenceAdapter(os.Stdin, os.Stdout); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func pinnedScript(t *testing.T, script string) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "adapter")
	if err := os.WriteFile(path, []byte(script), 0700); err != nil { // #nosec G306 -- owner-only executable adapter fixture in t.TempDir.
		t.Fatal(err)
	}
	return path, fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(script)))
}

func TestExternalAdapterProcessLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	config, _ := lifecycleFixture(t, "")
	kernel, err := os.ReadFile(config.EngineBinary)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := "#!/bin/sh\nexec env SOHA_REFERENCE_FIXTURE=1 '" + strings.ReplaceAll(config.EngineBinary, "'", "'\\''") + "' -test.run '^TestReferenceStdioProcess$'\n"
	path, digest := pinnedScript(t, wrapper)
	policy := adapterPolicy{BinaryApproval: proxyadapter.BinaryApproval{AdapterID: "soha-reference", Binary: path, SHA256: digest, Version: "1.0.0"}, EngineID: "reference-mihomo", Dialect: domain.EngineMihomo, EngineSHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(kernel))}
	raw, _ := json.Marshal(policy)
	config.AdapterPolicyFile = filepath.Join(config.StateDir, "policy.json")
	if err := os.WriteFile(config.AdapterPolicyFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	config.Engine = domain.EngineExternal
	e := newEngine(config, "test-controller-secret")
	defer e.stop()
	if err := e.configureExternal(); err != nil {
		t.Fatal(err)
	}
	desired := lifecycleDesired(1, 7890)
	desired.Engine = domain.EngineExternal
	desired.EngineDescriptor = &domain.EngineDescriptor{EngineID: policy.EngineID, AdapterID: policy.AdapterID, Version: policy.Version, ProtocolMajor: 1}
	if result := e.apply(context.Background(), desired); result.Status != "applied" {
		t.Fatalf("apply %+v", result)
	}
	first := e.processEpoch
	sample, _ := e.adapter.observe(context.Background())
	if sample.Health != "healthy" || sample.UploadTotal != 1 || sample.DownloadTotal != 2 {
		t.Fatalf("sample %+v", sample)
	}
	desired.Revision++
	if result := e.apply(context.Background(), desired); result.Status != "applied" {
		t.Fatalf("reload %+v", result)
	}
	if e.processEpoch == first {
		t.Fatal("reload retained engine epoch")
	}
	e.stop()
	a := fixtureValue[*externalEngineAdapter](t, e.adapter)
	if a.session != nil || e.process != nil || e.egress != nil {
		t.Fatal("owned processes or egress survived stop")
	}
	// A changed installation cannot be re-enabled by descriptor/capability claims.
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil { // #nosec G306 -- replace the pinned owner-only adapter fixture to verify digest rejection.
		t.Fatal(err)
	}
	if result := e.apply(context.Background(), desired); result.Status == "applied" || e.process != nil {
		t.Fatalf("changed adapter executed: %+v", result)
	}
}

func TestStdioRejectsFramingAndDeadline(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	for _, test := range []struct{ name, script string }{{"invalid stdout", "#!/bin/sh\necho not-json\n"}, {"oversized stdout", "#!/bin/sh\nhead -c 1048577 /dev/zero\n"}, {"deadline", "#!/bin/sh\nsleep 20\n"}} {
		t.Run(test.name, func(t *testing.T) {
			path, digest := pinnedScript(t, test.script)
			s, err := startStdio(adapterPolicy{BinaryApproval: proxyadapter.BinaryApproval{Binary: path, SHA256: digest}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			if _, err := s.call(ctx, "epoch", proxyadapter.Handshake, proxyadapter.Payload{Challenge: strings.Repeat("a", 32)}); err == nil {
				t.Fatal("invalid adapter accepted")
			}
			if !s.closed {
				s.mu.Lock()
				s.abort()
				s.mu.Unlock()
				t.Fatal("bad adapter process retained")
			}
		})
	}
}

func TestReferenceRejectsWrongMajor(t *testing.T) {
	err := ServeReferenceAdapter(strings.NewReader(`{"major":2}`+"\n"), io.Discard)
	if err == nil {
		t.Fatal("unknown protocol accepted")
	}
}
