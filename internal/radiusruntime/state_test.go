package radiusruntime

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Tracker, *time.Time) {
	t.Helper()
	now := time.Unix(1800000000, 0)
	tracker, err := New(Options{Epoch: "child-1", Binding: strings.Repeat("a", 64), CertificateValidUntil: now.Add(time.Hour), HardUntil: now.Add(time.Hour), FreshChild: true, CompleteHooks: true, MaxStates: 128, MaxPending: 32, RequestTimeout: time.Second, Now: func() time.Time { return now }},
		Control{Epoch: "child-1", Binding: strings.Repeat("a", 64), Revision: 1, ConfigValidUntil: now.Add(time.Minute), LeaseValidUntil: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	return tracker, &now
}
func request(state string) Request {
	return Request{Epoch: "child-1", NASID: "nas-1", StationID: "02:00:00:00:00:01", State: state}
}
func challenge(t *testing.T, tracker *Tracker, state, next string) string {
	t.Helper()
	nonce, err := tracker.Admit(request(state))
	if err != nil {
		t.Fatal(err)
	}
	assigned, err := tracker.Challenge(Completion{Request: request(state), Nonce: nonce, NextState: next})
	if err != nil {
		t.Fatal(err)
	}
	return assigned
}
func TestDrainContinuesOnlyKnownBoundState(t *testing.T) {
	tracker, now := fixture(t)
	challenge(t, tracker, "", "0x0102")
	control := tracker.control
	control.Revision++
	control.Draining = true
	control.DrainUntil = now.Add(30 * time.Second)
	if err := tracker.UpdateControl(control); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.Admit(request("")); !errors.Is(err, ErrDenied) {
		t.Fatalf("new round during drain: %v", err)
	}
	for _, change := range []Request{request("0xffff"), {Epoch: "old", NASID: "nas-1", StationID: "02:00:00:00:00:01", State: "0x0102"}, {Epoch: "child-1", NASID: "other", StationID: "02:00:00:00:00:01", State: "0x0102"}, {Epoch: "child-1", NASID: "nas-1", StationID: "02:00:00:00:00:02", State: "0x0102"}} {
		if _, err := tracker.Admit(change); err == nil {
			t.Fatalf("accepted foreign request %+v", change)
		}
	}
	nonce, err := tracker.Admit(request("0x0102"))
	if err != nil {
		t.Fatal(err)
	}
	if err = tracker.Finish(Completion{Request: request("0x0102"), Nonce: nonce, Outcome: "accept"}); err != nil {
		t.Fatal(err)
	}
	snapshot := tracker.Snapshot()
	if !snapshot.Known || snapshot.Active == nil || *snapshot.Active != 0 {
		t.Fatalf("unexpected snapshot %+v", snapshot)
	}
}
func TestReplacementNeverExtendsHardBoundOrRevivesOldState(t *testing.T) {
	tracker, now := fixture(t)
	challenge(t, tracker, "", "0x01")
	*now = now.Add(500 * time.Millisecond)
	challenge(t, tracker, "0x01", "0x02")
	if _, err := tracker.Admit(request("0x01")); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	control := tracker.control
	control.Revision++
	control.ConfigValidUntil = now.Add(2 * time.Minute)
	control.LeaseValidUntil = now.Add(2 * time.Minute)
	if err := tracker.UpdateControl(control); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)
	snapshot := tracker.Snapshot()
	if snapshot.Known || snapshot.Active != nil || snapshot.Reason != "state_expiry_unconfirmed" {
		t.Fatalf("expiry fabricated known0: %+v", snapshot)
	}
	if err := tracker.UpdateControl(Control{Epoch: "child-1", Binding: strings.Repeat("a", 64), Revision: 3, ConfigValidUntil: now.Add(time.Minute), LeaseValidUntil: now.Add(time.Minute)}); !errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
}
func TestMissingCompletionAndRestartNeverKnownZero(t *testing.T) {
	tracker, now := fixture(t)
	if _, err := tracker.Admit(request("")); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Second)
	if snap := tracker.Snapshot(); snap.Known || snap.Active != nil || snap.Reason != "completion_missing" {
		t.Fatalf("missing hook: %+v", snap)
	}
	options := tracker.options
	options.Epoch = "child-2"
	options.FreshChild = false
	control := tracker.control
	control.Epoch = "child-2"
	restarted, err := New(options, control)
	if err != nil {
		t.Fatal(err)
	}
	if snap := restarted.Snapshot(); snap.Known || snap.Active != nil {
		t.Fatalf("restart restored certainty %+v", snap)
	}
	if _, err = restarted.Admit(request("0x01")); err == nil {
		t.Fatal("old epoch accepted")
	}
}
func TestGeneratedStateCollisionReplayAndRandomFailure(t *testing.T) {
	tracker, _ := fixture(t)
	state := challenge(t, tracker, "", "")
	if !strings.HasPrefix(state, "0x") || len(state) != 66 {
		t.Fatal(state)
	}
	nonce, err := tracker.Admit(request(state))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tracker.Challenge(Completion{Request: request(state), Nonce: nonce, NextState: "0x03"}); err != nil {
		t.Fatal(err)
	}
	if _, err = tracker.Challenge(Completion{Request: request(state), Nonce: nonce, NextState: "0x04"}); !errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
	if tracker.Snapshot().Known {
		t.Fatal("duplicate completion retained certainty")
	}
	tracker, _ = fixture(t)
	challenge(t, tracker, "", "0x01")
	nonce, err = tracker.Admit(request("0x01"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tracker.Challenge(Completion{Request: request("0x01"), Nonce: nonce, NextState: "0x02"}); err != nil {
		t.Fatal(err)
	}
	nonce, err = tracker.Admit(request("0x02"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tracker.Challenge(Completion{Request: request("0x02"), Nonce: nonce, NextState: "0x01"}); !errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
	tracker, _ = fixture(t)
	nonce, err = tracker.Admit(request(""))
	if err != nil {
		t.Fatal(err)
	}
	tracker.options.Random = errorReader{}
	if _, err = tracker.Challenge(Completion{Request: request(""), Nonce: nonce}); err == nil || tracker.Snapshot().Known {
		t.Fatal("random failure did not fail closed")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func TestStrictStateAndCapacity(t *testing.T) {
	for _, station := range []string{"02-00-00-00-AA-BB", "02:00:00:00:AA:BB"} {
		input := request("")
		input.StationID = station
		canonical, err := canonical(input)
		if err != nil || canonical.StationID != "02:00:00:00:aa:bb" {
			t.Fatalf("station canonicalization: %+v %v", canonical, err)
		}
	}
	for _, state := range []string{"0x", "0x0", "0xGG", "plain", "0x" + strings.Repeat("01", 254)} {
		if _, err := canonical(request(state)); err == nil {
			t.Fatalf("accepted %q", state)
		}
	}
	if _, err := canonical(request("0x" + strings.Repeat("ff", 253))); err != nil {
		t.Fatal(err)
	}
	tracker, _ := fixture(t)
	tracker.options.MaxPending = 1
	if _, err := tracker.Admit(request("")); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.Admit(request("")); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
	tracker, _ = fixture(t)
	tracker.options.MaxStates = 1
	challenge(t, tracker, "", "0x01")
	if _, err := tracker.Admit(request("")); !errors.Is(err, ErrDenied) {
		t.Fatal(err)
	}
}

func TestRandomFailureBeforeAdmissionDoesNotCreatePhantomRound(t *testing.T) {
	tracker, _ := fixture(t)
	tracker.options.Random = errorReader{}
	if _, err := tracker.Admit(request("")); err == nil {
		t.Fatal("entropy failure admitted request")
	}
	if snapshot := tracker.Snapshot(); !snapshot.Known || snapshot.Active == nil || *snapshot.Active != 0 || snapshot.Pending != 0 {
		t.Fatalf("phantom round after failed admission: %+v", snapshot)
	}
}

func TestGeneratedStateStillUsesOriginalControlHardBound(t *testing.T) {
	tracker, now := fixture(t)
	control := tracker.control
	control.Revision++
	tracker.options.HardUntil = now.Add(10 * time.Second)
	control.ConfigValidUntil = now.Add(30 * time.Second)
	if err := tracker.UpdateControl(control); err != nil {
		t.Fatal(err)
	}
	state := challenge(t, tracker, "", "")
	*now = now.Add(5 * time.Second)
	challenge(t, tracker, state, "")
	control.Revision++
	control.ConfigValidUntil = now.Add(time.Minute)
	if err := tracker.UpdateControl(control); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(5 * time.Second)
	if snapshot := tracker.Snapshot(); snapshot.Known || snapshot.Active != nil || snapshot.Reason != "immutable_authorization_expired" {
		t.Fatalf("generated State extended original hard bound: %+v", snapshot)
	}
}
func TestConcurrentAdmitsAreSerializedAndNoncesUnique(t *testing.T) {
	tracker, _ := fixture(t)
	var wait sync.WaitGroup
	nonces := make(chan string, 20)
	for range 20 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			nonce, err := tracker.Admit(request(""))
			if err != nil {
				t.Error(err)
				return
			}
			nonces <- nonce
		}()
	}
	wait.Wait()
	close(nonces)
	seen := map[string]bool{}
	for nonce := range nonces {
		if !validNonce(nonce) || seen[nonce] {
			t.Fatal("invalid/reused nonce")
		}
		seen[nonce] = true
	}
	snap := tracker.Snapshot()
	if snap.Active == nil || *snap.Active != 20 || snap.Pending != 20 {
		t.Fatalf("lost concurrent observation %+v", snap)
	}
}
func TestShorterControlLeaseClampsExistingRound(t *testing.T) {
	tracker, now := fixture(t)
	challenge(t, tracker, "", "0x01")
	control := tracker.control
	control.Revision++
	control.LeaseValidUntil = now.Add(time.Second)
	if err := tracker.UpdateControl(control); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Second)
	if snapshot := tracker.Snapshot(); snapshot.Known || snapshot.Active != nil {
		t.Fatalf("lease expiry treated as known0 %+v", snapshot)
	}
}

func TestRenewSameBindingCrossesOldShortWatchdogWithoutExtendingState(t *testing.T) {
	tracker, now := fixture(t)
	control := tracker.control
	control.ConfigValidUntil = now.Add(5 * time.Second)
	control.LeaseValidUntil = now.Add(5 * time.Second)
	if err := tracker.RenewControl(control); err != nil {
		t.Fatal(err)
	}
	state := challenge(t, tracker, "", "0x01")
	for step := 1; step <= 14; step++ {
		*now = now.Add(4 * time.Second)
		control.ConfigValidUntil = now.Add(5 * time.Second)
		control.LeaseValidUntil = now.Add(5 * time.Second)
		if err := tracker.RenewControl(control); err != nil {
			t.Fatal(err)
		}
		state = challenge(t, tracker, state, "0x"+strings.Repeat("01", step+1))
	}
	*now = now.Add(4 * time.Second)
	control.ConfigValidUntil = now.Add(5 * time.Second)
	control.LeaseValidUntil = now.Add(5 * time.Second)
	if err := tracker.RenewControl(control); !errors.Is(err, ErrUnknown) {
		t.Fatal("original 60s State hard bound renewed", err)
	}
	if snap := tracker.Snapshot(); snap.Known || snap.Active != nil || snap.Reason != "state_expiry_unconfirmed" {
		t.Fatalf("bad hard bound %+v", snap)
	}
}
func TestRenewCannotReviveExpiredUnknownOrChangeBindingIntent(t *testing.T) {
	for _, kind := range []string{"epoch", "binding", "revision", "draining", "drain-deadline", "expired-control", "expired-config", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			tracker, now := fixture(t)
			control := tracker.control
			switch kind {
			case "epoch":
				control.Epoch = "other"
			case "binding":
				control.Binding = strings.Repeat("b", 64)
			case "revision":
				control.Revision++
			case "draining":
				control.Draining = true
				control.DrainUntil = now.Add(5 * time.Second)
			case "drain-deadline":
				control.DrainUntil = now.Add(5 * time.Second)
			case "expired-control":
				tracker.control.LeaseValidUntil = *now
			case "expired-config":
				tracker.control.ConfigValidUntil = *now
			case "unknown":
				tracker.MarkUnknown()
			}
			control.ConfigValidUntil = now.Add(time.Minute)
			control.LeaseValidUntil = now.Add(time.Minute)
			if tracker.RenewControl(control) == nil {
				t.Fatal("invalid renew accepted")
			}
		})
	}
}
func TestCertificateAndFirstFixedDrainCannotBeExtended(t *testing.T) {
	tracker, now := fixture(t)
	tracker.options.CertificateValidUntil = now.Add(10 * time.Second)
	challenge(t, tracker, "", "0x01")
	control := tracker.control
	control.Revision++
	control.Draining = true
	control.DrainUntil = now.Add(8 * time.Second)
	if err := tracker.UpdateControl(control); err != nil {
		t.Fatal(err)
	}
	control.DrainUntil = now.Add(9 * time.Second)
	if tracker.RenewControl(control) == nil {
		t.Fatal("drain renew extended")
	}
	control.Revision++
	if tracker.UpdateControl(control) == nil {
		t.Fatal("new revision extended fixed drain")
	}
	*now = now.Add(8 * time.Second)
	if snap := tracker.Snapshot(); snap.Known || snap.Active != nil {
		t.Fatal("drain expiry fabricated known0", snap)
	}
	tracker, now = fixture(t)
	tracker.options.CertificateValidUntil = now.Add(3 * time.Second)
	challenge(t, tracker, "", "0x02")
	*now = now.Add(3 * time.Second)
	if tracker.RenewControl(tracker.control) == nil {
		t.Fatal("certificate expiry revived")
	}
}
