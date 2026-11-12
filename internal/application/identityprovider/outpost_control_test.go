package identityprovider

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainruntime "github.com/opensoha/soha/internal/domain/outpostruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type testOutpostControl struct {
	control                               domainruntime.InstanceControl
	controlErr, accessErr, observationErr error
	instance, epoch                       string
	observations                          []domainruntime.InstanceObservation
}

func (p *testOutpostControl) ControlForInstance(_ context.Context, _, instance, epoch string, _ domainruntime.InstanceCredential) (domainruntime.InstanceControl, error) {
	p.instance, p.epoch = instance, epoch
	return p.control, p.controlErr
}
func (p *testOutpostControl) RecordInstanceObservation(_ context.Context, observation domainruntime.InstanceObservation) error {
	if p.observationErr != nil {
		return p.observationErr
	}
	if observation.InstanceID != p.instance || observation.ProcessEpoch != p.epoch || observation.Generation > p.control.Generation || !p.control.ExpiresAt.After(time.Now()) {
		return apperrors.ErrConflict
	}
	p.observations = append(p.observations, observation)
	return nil
}
func (p *testOutpostControl) AuthorizeAccess(context.Context, string) error { return p.accessErr }
func newTestOutpostControl() *testOutpostControl {
	now := time.Now().UTC()
	return &testOutpostControl{control: domainruntime.InstanceControl{ProtocolVersion: "v1", Generation: 3, Action: "serve", IssuedAt: now, ExpiresAt: now.Add(30 * time.Second)}, instance: "instance-1", epoch: "epoch-1"}
}
func TestOutpostControlPayloadOrderAndIndependentSignature(t *testing.T) {
	service, _, created, config := outpostLeaseFixture(t)
	now := time.Date(2026, 11, 7, 1, 2, 3, 0, time.UTC)
	control := sohaapi.IdentityOutpostRuntimeControl{OutpostID: "edge", ProtocolVersion: "v1", InstanceID: "instance", ProcessEpoch: "epoch", Generation: 7, Action: "drain", IssuedAt: now, ExpiresAt: now.Add(30 * time.Second), KeyID: "key"}
	payload, err := outpostControlPayload(control)
	ssoNoError(t, err)
	want := `{"outpostId":"edge","protocolVersion":"v1","instanceId":"instance","processEpoch":"epoch","generation":7,"action":"drain","issuedAt":"2026-11-07T01:02:03Z","expiresAt":"2026-11-07T01:02:33Z","keyId":"key"}`
	if string(payload) != want {
		t.Fatalf("payload = %s", payload)
	}
	port := newTestOutpostControl()
	service.SetOutpostRuntimeControl(port)
	before, err := outpostConfigPayload(config)
	ssoNoError(t, err)
	ssoNoError(t, service.attachOutpostRuntimeControl(context.Background(), &config, sohaapi.IdentityOutpostClaimRequest{SupportedRuntimeControlVersion: "v1", InstanceID: "instance-1", ProcessEpoch: "epoch-1"}, "authenticated-token-hash"))
	if config.RuntimeControl == nil || config.RuntimeControl.OutpostID != created.ID {
		t.Fatal("missing bound control")
	}
	after, err := outpostConfigPayload(config)
	ssoNoError(t, err)
	if string(before) != string(after) {
		t.Fatal("runtime control changed legacy configuration signature payload")
	}
	signature, err := base64.StdEncoding.DecodeString(config.RuntimeControl.Signature)
	ssoNoError(t, err)
	signed, err := outpostControlPayload(*config.RuntimeControl)
	ssoNoError(t, err)
	publicKey := service.outpostSigningKey.Public().(ed25519.PublicKey)
	if !ed25519.Verify(publicKey, signed, signature) {
		t.Fatal("control signature failed")
	}
	config.RuntimeControl.Action = "drain"
	tampered, err := outpostControlPayload(*config.RuntimeControl)
	ssoNoError(t, err)
	if ed25519.Verify(publicKey, tampered, signature) {
		t.Fatal("tampered control verified")
	}
}
func TestOutpostControlClaimNegotiation(t *testing.T) {
	for _, name := range []string{"legacy", "v1", "missing identity", "unsupported", "missing port", "port failure"} {
		t.Run(name, func(t *testing.T) {
			service, _, created, _ := outpostLeaseFixture(t)
			port := newTestOutpostControl()
			service.SetOutpostRuntimeControl(port)
			request := sohaapi.IdentityOutpostClaimRequest{AgentID: created.ID, SupportedProtocolVersion: "v1", SupportedRuntimeControlVersion: "v1", InstanceID: "instance-1", ProcessEpoch: "epoch-1"}
			wantOK := false
			switch name {
			case "legacy":
				request.SupportedRuntimeControlVersion = ""
				request.InstanceID = ""
				request.ProcessEpoch = ""
				wantOK = true
			case "v1":
				wantOK = true
			case "missing identity":
				request.ProcessEpoch = ""
			case "unsupported":
				request.SupportedRuntimeControlVersion = "v2"
			case "missing port":
				service.SetOutpostRuntimeControl(nil)
			case "port failure":
				port.controlErr = errors.New("runtime storage unavailable")
			}
			config, err := service.ClaimIdentityOutpostRuntime(context.Background(), created.Token, request)
			if !wantOK {
				if err == nil || config != nil {
					t.Fatal("returned config after invalid control negotiation")
				}
				return
			}
			ssoNoError(t, err)
			if (config.RuntimeControl != nil) != (name == "v1") {
				t.Fatalf("control negotiation = %#v", config.RuntimeControl)
			}
		})
	}
}
func TestOutpostControlRejectsInvalidGrant(t *testing.T) {
	for _, name := range []string{"expired", "future issued", "unbounded", "unknown action", "unknown version", "negative generation", "extends config"} {
		t.Run(name, func(t *testing.T) {
			service, _, _, config := outpostLeaseFixture(t)
			port := newTestOutpostControl()
			service.SetOutpostRuntimeControl(port)
			switch name {
			case "expired":
				port.control.IssuedAt = time.Now().Add(-time.Minute)
				port.control.ExpiresAt = time.Now().Add(-time.Second)
			case "future issued":
				port.control.IssuedAt = time.Now().Add(time.Second)
			case "unbounded":
				port.control.ExpiresAt = port.control.IssuedAt.Add(31 * time.Second)
			case "unknown action":
				port.control.Action = "stop"
			case "unknown version":
				port.control.ProtocolVersion = "v2"
			case "negative generation":
				port.control.Generation = -1
			case "extends config":
				config.ExpiresAt = time.Now().Add(time.Second)
			}
			expiry := config.ExpiresAt
			err := service.attachOutpostRuntimeControl(context.Background(), &config, sohaapi.IdentityOutpostClaimRequest{SupportedRuntimeControlVersion: "v1", InstanceID: "instance-1", ProcessEpoch: "epoch-1"}, "authenticated-token-hash")
			if err == nil || config.RuntimeControl != nil || !config.ExpiresAt.Equal(expiry) {
				t.Fatal("invalid control extended or attached to config")
			}
		})
	}
}
func TestOutpostControlHeartbeatBindings(t *testing.T) {
	for _, name := range []string{"accepted", "missing epoch", "missing accepting", "negative count", "future generation", "old epoch", "expired control", "missing config grant", "missing port", "store failure"} {
		t.Run(name, func(t *testing.T) {
			service, repo, created, config := outpostLeaseFixture(t)
			port := newTestOutpostControl()
			service.SetOutpostRuntimeControl(port)
			accepting, active, generation := false, 0, int64(3)
			request := sohaapi.IdentityOutpostHeartbeatRequest{AgentID: created.ID, Status: sohaapi.IdentityOutpostHeartbeatRequestStatusHealthy, ConfigurationVersion: config.ConfigurationVersion, ConfigurationExpiresAt: &config.ExpiresAt, InstanceID: "instance-1", ProcessEpoch: "epoch-1", AcceptingRequests: &accepting, ActiveChecks: &active, AppliedControlGeneration: &generation, CheckedAt: time.Now().Add(24 * time.Hour)}
			switch name {
			case "missing epoch":
				request.ProcessEpoch = ""
			case "missing accepting":
				request.AcceptingRequests = nil
			case "negative count":
				active = -1
			case "future generation":
				generation = 4
			case "old epoch":
				request.ProcessEpoch = "retired"
			case "expired control":
				port.control.ExpiresAt = time.Now().Add(-time.Second)
			case "missing config grant":
				delete(repo.leases, created.ID)
			case "missing port":
				service.SetOutpostRuntimeControl(nil)
			case "store failure":
				port.observationErr = errors.New("storage unavailable")
			}
			_, err := service.HeartbeatIdentityOutpostRuntime(context.Background(), created.ID, created.Token, request)
			if name != "accepted" {
				if err == nil || len(port.observations) != 0 {
					t.Fatal("invalid ACK accepted")
				}
				item, getErr := repo.GetOutpost(context.Background(), created.ID)
				ssoNoError(t, getErr)
				if item.LastHeartbeatAt != nil {
					t.Fatal("rejected ACK overwrote observations")
				}
				return
			}
			ssoNoError(t, err)
			if len(port.observations) != 1 {
				t.Fatal("ACK not recorded")
			}
			observation := port.observations[0]
			if observation.CredentialHash != repo.outposts[created.ID].TokenHash || observation.ConfigurationVersion != config.ConfigurationVersion || observation.ObservedAt.After(time.Now()) || !observation.LeaseExpiresAt.Equal(config.ExpiresAt) {
				t.Fatalf("untrusted ACK = %#v", observation)
			}
		})
	}
}
func TestOutpostControlAccessCheckFailsClosedDuringDrain(t *testing.T) {
	service, _, created, config := outpostLeaseFixture(t)
	port := newTestOutpostControl()
	port.accessErr = apperrors.ErrAccessDenied
	service.SetOutpostRuntimeControl(port)
	_, err := service.CheckIdentityOutpostAccess(context.Background(), created.ID, created.Token, sohaapi.IdentityOutpostAccessCheckRequest{ConfigurationVersion: config.ConfigurationVersion})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("managed drain check = %v", err)
	}
	port.accessErr = errors.New("runtime state unavailable")
	if err := service.authorizeOutpostRuntimeAccess(context.Background(), created.ID); err == nil {
		t.Fatal("runtime read failure allowed new check")
	}
	service.SetOutpostRuntimeControl(nil)
	ssoNoError(t, service.authorizeOutpostRuntimeAccess(context.Background(), created.ID))
}
