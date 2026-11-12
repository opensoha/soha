package radiusdeployment

import (
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"github.com/opensoha/soha/internal/networkprotocol"
	"testing"
	"time"
)

func ptr(v int) *int { return &v }
func runtimeFixture() (domain.State, domain.MemberRecord, currentCredential, time.Time) {
	now := time.Now().UTC()
	s := domain.State{Pool: api.NetworkRADIUSPool{PoolID: "pool", NASBindingID: "binding", SiteID: "site", OwnerEpoch: 1, ConfigurationRevision: 1, ConfigurationHash: "hash", DesiredReplicas: 1, Enabled: true}, ControlRevision: 2}
	m := domain.MemberRecord{Member: domain.Member{MemberID: "member", RuntimeID: "radius-member:a"}}
	return s, m, currentCredential{ID: "cred", ExpiresAt: now.Add(time.Hour)}, now
}
func TestControlBootstrapRenewDrainAndUnknown(t *testing.T) {
	s, m, c, now := runtimeFixture()
	first, e := nextControl(s, &m, c, "epoch", "nas", now)
	if e != nil || !first.RunProcess || !first.AcceptNewAuthentications {
		t.Fatalf("bootstrap: %+v %v", first, e)
	}
	m.Control = &first
	renewed, e := nextControl(s, &m, c, "epoch", "nas", now.Add(time.Second))
	if e != nil || renewed.ControlRevision != first.ControlRevision || !renewed.ServeExistingUntil.Equal(first.ServeExistingUntil) || !renewed.ControlLeaseUntil.After(first.ControlLeaseUntil) {
		t.Fatalf("renew: %+v %v", renewed, e)
	}
	until := now.Add(5 * time.Second)
	confirm := until.Add(30 * time.Second)
	s.DrainUntil = &until
	s.StopConfirmUntil = &confirm
	s.RetiringEnd = 1
	s.Pool.DesiredReplicas = 0
	s.Pool.Enabled = false
	s.ControlRevision++
	drained, e := nextControl(s, &m, c, "epoch", "nas", now.Add(time.Second))
	if e != nil || !drained.RunProcess || drained.AcceptNewAuthentications || drained.StopRequired || !drained.ServeExistingUntil.Equal(until) {
		t.Fatalf("drain: %+v %v", drained, e)
	}
	m.Control = &drained
	stopped, e := nextControl(s, &m, c, "epoch", "nas", until.Add(time.Second))
	if e != nil || stopped.RunProcess || !stopped.StopRequired {
		t.Fatalf("stop: %+v %v", stopped, e)
	}
	a := api.NetworkRADIUSRuntimeACK{StateCountKnown: true, ActiveStates: ptr(0), ProcessStopped: true}
	if e := validateACK(&stopped, false, a, until.Add(time.Second)); e != nil {
		t.Fatalf("stop-only known0 with hooks stopped: %v", e)
	}
	if _, e := nextControl(s, &m, c, "new-epoch", "nas", now); e == nil {
		t.Fatal("unproven prior child replaced")
	}
	m.UnknownLatched = true
	m.Control = &stopped
	u, e := nextControl(s, &m, c, "epoch", "nas", until.Add(2*time.Second))
	if e != nil || !u.StopRequired || u.AcceptNewAuthentications {
		t.Fatal("unknown reenabled")
	}
}
func TestACKPresenceAndProofFences(t *testing.T) {
	s, m, c, now := runtimeFixture()
	control, _ := nextControl(s, &m, c, "epoch", "nas", now)
	a := api.NetworkRADIUSRuntimeACK{StateCountKnown: true, Ready: true, HooksReady: true, ConfigurationApplied: true, AcceptingNewAuthentications: true}
	if validateACK(&control, false, a, now) == nil {
		t.Fatal("known count omitted accepted")
	}
	a.ActiveStates = ptr(0)
	if e := validateACK(&control, false, a, now); e != nil {
		t.Fatal(e)
	}
	if validateACK(&control, true, a, now) == nil {
		t.Fatal("unknown revived")
	}
	a.ProcessStopped = true
	if validateACK(&control, false, a, now) == nil {
		t.Fatal("stopped and ready accepted")
	}
	a.ProcessStopped = false
	a.PoolID = s.Pool.PoolID
	a.MemberID = m.Member.MemberID
	a.RuntimeID = m.Member.RuntimeID
	a.CredentialID = c.ID
	a.ProcessEpoch = control.ProcessEpoch
	a.OwnerEpoch = s.Pool.OwnerEpoch
	a.ControlRevision = int(s.ControlRevision)
	a.ConfigurationRevision = s.Pool.ConfigurationRevision
	a.ConfigurationHash = s.Pool.ConfigurationHash
	m.Control = &control
	m.ACK = &a
	m.ReceivedAt = &now
	p := networkprotocol.ManagedNASProof{PoolID: a.PoolID, MemberID: a.MemberID, CredentialID: c.ID, ProcessEpoch: a.ProcessEpoch, OwnerEpoch: 1, ControlRevision: s.ControlRevision, ConfigurationRevision: 1, ConfigurationHash: a.ConfigurationHash}
	if e := validRuntimeProof(s, m, c.ID, &p, now); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*networkprotocol.ManagedNASProof){func(p *networkprotocol.ManagedNASProof) { p.ControlRevision-- }, func(p *networkprotocol.ManagedNASProof) { p.MemberID = "other" }, func(p *networkprotocol.ManagedNASProof) { p.CredentialID = "other" }, func(p *networkprotocol.ManagedNASProof) { p.ProcessEpoch = "other" }, func(p *networkprotocol.ManagedNASProof) { p.ConfigurationHash = "other" }} {
		copy := p
		change(&copy)
		if validRuntimeProof(s, m, c.ID, &copy, now) == nil {
			t.Fatalf("stale proof accepted: %+v", copy)
		}
	}
}
