package sohacli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPendingLoginConfigIsolation(t *testing.T) {
	for _, mode := range []string{"keyring", "file"} {
		t.Run(mode, func(t *testing.T) {
			path := authV2ConfigPath(t)
			ring := &memorySecretKeyring{values: map[string]string{}}
			a := Runtime{ConfigPath: path, Keyring: ring}
			b := Runtime{ConfigPath: filepath.Join(filepath.Dir(path), "second.json"), Keyring: ring}
			o := loginOptions{server: "https://pending.example.invalid", profile: "local", store: mode}
			storeA, err := openCredentialStorage(a, mode)
			if err != nil {
				t.Fatal(err)
			}
			pendingA := pendingIsolationFixture("a")
			cleanupA, err := savePendingLogin(a, storeA, o, pendingA)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cleanupA() }()
			if pending, _, _, err := loadPendingLogin(b, o); err != nil || pending != nil {
				t.Fatalf("another configuration affected a fresh login: pending=%v error=%v", pending != nil, err)
			}
			storeB, err := openCredentialStorage(b, mode)
			if err != nil {
				t.Fatal(err)
			}
			pendingB := pendingIsolationFixture("b")
			cleanupB, err := savePendingLogin(b, storeB, o, pendingB)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cleanupB() }()
			assertPendingIsolation(t, a, o, pendingA.ClaimRequestID)
			assertPendingIsolation(t, b, o, pendingB.ClaimRequestID)
			if err := cleanupA(); err != nil {
				t.Fatal(err)
			}
			assertPendingIsolation(t, b, o, pendingB.ClaimRequestID)
			if pending, _, _, err := loadPendingLogin(a, o); err != nil || pending != nil {
				t.Fatal("cleaned configuration retained pending state")
			}
		})
	}
}

func TestPendingLoginRejectsForeignConfigReference(t *testing.T) {
	path := authV2ConfigPath(t)
	a := Runtime{ConfigPath: path}
	b := Runtime{ConfigPath: filepath.Join(filepath.Dir(path), "second.json")}
	o := loginOptions{server: "https://pending.example.invalid", profile: "local", store: "file"}
	store, err := openCredentialStorage(a, "file")
	if err != nil {
		t.Fatal(err)
	}
	pending := pendingIsolationFixture("a")
	cleanup, err := savePendingLogin(a, store, o, pending)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cleanup() }()
	aPath, err := pendingLoginFile(a, o.server, o.profile)
	if err != nil {
		t.Fatal(err)
	}
	bPath, err := pendingLoginFile(b, o.server, o.profile)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(aPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicFile(bPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := loadPendingLogin(b, o); err == nil {
		t.Fatal("foreign configuration reference was accepted")
	}
	var reference pendingLoginReference
	if err := json.Unmarshal(raw, &reference); err != nil {
		t.Fatal(err)
	}
	reference.ConfigID, err = credentialConfigScope(b.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(reference)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicFile(bPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := loadPendingLogin(b, o); err == nil {
		t.Fatal("foreign pending envelope was accepted after reference relabeling")
	}
	assertPendingIsolation(t, a, o, pending.ClaimRequestID)
}

func TestCredentialLocksAreConfigSpecific(t *testing.T) {
	path := authV2ConfigPath(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	unlockA, err := acquireCredentialLock(ctx, path, "pending:shared-target")
	if err != nil {
		t.Fatal(err)
	}
	defer unlockA()
	unlockB, err := acquireCredentialLock(ctx, filepath.Join(filepath.Dir(path), "second.json"), "pending:shared-target")
	if err != nil {
		t.Fatalf("another configuration shared the pending lock: %v", err)
	}
	unlockB()
	busyCtx, stop := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer stop()
	if unlock, err := acquireCredentialLock(busyCtx, path, "pending:shared-target"); err == nil {
		unlock()
		t.Fatal("same configuration did not retain its lock")
	}
}

func pendingIsolationFixture(seed string) pendingDeviceLogin {
	return pendingDeviceLogin{DeviceCode: strings.Repeat(seed, 43), CodeVerifier: strings.Repeat("v", 43), ClaimRequestID: uuid.NewString(), VerificationURI: "https://pending.example.invalid/auth/device#handle=" + strings.Repeat("h", 43), UserCode: "123456", ExpiresAt: time.Now().Add(5 * time.Minute), Interval: 1}
}

func assertPendingIsolation(t *testing.T, rt Runtime, options loginOptions, requestID string) {
	t.Helper()
	pending, _, _, err := loadPendingLogin(rt, options)
	if err != nil || pending == nil || pending.ClaimRequestID != requestID {
		t.Fatalf("configuration did not recover its original pending login: error=%v", err)
	}
}
