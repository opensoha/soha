package runner

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"time"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

const outpostControlMaxLease = 30 * time.Second

type outpostControlFloor struct {
	Generation int64                                       `json:"generation"`
	IssuedAt   time.Time                                   `json:"issuedAt"`
	Action     sohaapi.IdentityOutpostRuntimeControlAction `json:"action"`
}

// Field order is the control protocol's signing order, independently of generated DTOs.
func outpostControlPayload(c sohaapi.IdentityOutpostRuntimeControl) []byte {
	payload := struct {
		OutpostID       string                                               `json:"outpostId"`
		ProtocolVersion sohaapi.IdentityOutpostRuntimeControlProtocolVersion `json:"protocolVersion"`
		InstanceID      string                                               `json:"instanceId"`
		ProcessEpoch    string                                               `json:"processEpoch"`
		Generation      int64                                                `json:"generation"`
		Action          sohaapi.IdentityOutpostRuntimeControlAction          `json:"action"`
		IssuedAt        time.Time                                            `json:"issuedAt"`
		ExpiresAt       time.Time                                            `json:"expiresAt"`
		KeyID           string                                               `json:"keyId"`
	}{c.OutpostID, c.ProtocolVersion, c.InstanceID, c.ProcessEpoch, c.Generation, c.Action, c.IssuedAt.UTC(), c.ExpiresAt.UTC(), c.KeyID}
	encoded, _ := json.Marshal(payload)
	return encoded
}

func (o *outpostRuntime) validateControl(c *sohaapi.IdentityOutpostRuntimeControl, outpostID, trustKeyID string, now time.Time) error {
	if c == nil {
		return errors.New("outpost runtime control is required")
	}
	if c.OutpostID != outpostID || c.InstanceID != o.instanceID || c.ProcessEpoch != o.processEpoch || c.ProtocolVersion != "v1" {
		return errors.New("outpost runtime control binding mismatch")
	}
	if !validOutpostControlLease(c, now) {
		return errors.New("outpost runtime control is malformed or expired")
	}
	key := o.trustedKeys[c.KeyID]
	if o.trustedKeys == nil && c.KeyID == trustKeyID {
		key = o.publicKey
	}
	signature, err := decodeBase64(c.Signature)
	if len(key) != ed25519.PublicKeySize || err != nil || !ed25519.Verify(key, outpostControlPayload(*c), signature) {
		return errors.New("outpost runtime control signature is invalid")
	}
	if f := o.floor.Control; f != nil && !f.accepts(c) {
		return errors.New("outpost runtime control regresses durable floor")
	}
	return nil
}

func validOutpostControlLease(c *sohaapi.IdentityOutpostRuntimeControl, now time.Time) bool {
	return c.Generation > 0 && (c.Action == "serve" || c.Action == "drain") && !c.IssuedAt.IsZero() && !c.IssuedAt.After(now) && c.ExpiresAt.After(c.IssuedAt) && c.ExpiresAt.Sub(c.IssuedAt) <= outpostControlMaxLease && now.Before(c.ExpiresAt)
}

func (f outpostControlFloor) accepts(c *sohaapi.IdentityOutpostRuntimeControl) bool {
	return c.Generation >= f.Generation && !c.IssuedAt.Before(f.IssuedAt) && (c.Generation != f.Generation || c.IssuedAt.After(f.IssuedAt))
}

func (o *outpostRuntime) applyControlLocked(c *sohaapi.IdentityOutpostRuntimeControl, outpostID, trustKeyID string, now time.Time) error {
	if err := o.validateControl(c, outpostID, trustKeyID, now); err != nil {
		o.controlError = "runtime_control_rejected"
		return err
	}
	next := o.floor
	next.Managed = true
	next.Control = &outpostControlFloor{Generation: c.Generation, IssuedAt: c.IssuedAt, Action: c.Action}
	if o.state != nil {
		if err := o.state.save(next); err != nil {
			o.controlError = "runtime_control_persistence_failed"
			o.persistenceFailed = true
			return err
		}
	}
	o.floor = next
	copy := *c
	o.control = &copy
	o.controlDeadline = now.Add(min(c.ExpiresAt.Sub(now), outpostControlMaxLease))
	o.controlError = ""
	// A fresh valid control can recover a prior rejected claim without extending its config lease.
	o.configurationError = ""
	return nil
}

func (o *outpostRuntime) controlStatusLocked(now time.Time) string {
	if o.controlError != "" {
		return o.controlError
	}
	if o.control == nil {
		return "runtime_control_unavailable"
	}
	if !now.Before(o.control.ExpiresAt) || !now.Before(o.controlDeadline) {
		return "runtime_control_expired"
	}
	if o.control.Action == "drain" {
		return "runtime_draining"
	}
	return ""
}

func (o *outpostRuntime) acceptingLocked(now time.Time) bool {
	return !o.persistenceFailed && o.config != nil && o.configurationError == "" && now.Before(o.config.ExpiresAt) && now.Before(o.leaseDeadline) && (!o.controlEnabled || o.controlStatusLocked(now) == "")
}

func (o *outpostRuntime) beginCheck(now time.Time) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.acceptingLocked(now) {
		return errOutpostUnavailable
	}
	o.activeChecks++
	return nil
}

func (o *outpostRuntime) finishCheck() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.activeChecks--
}
