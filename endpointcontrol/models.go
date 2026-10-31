// Package endpointcontrol owns the bounded native endpoint-channel protocol.
package endpointcontrol

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"
)

const Version = "endpoint-controls/v1alpha1"
const Capability = "windows-channel-controls-v1"

type Controls struct {
	USB       bool `json:"usb"`
	Print     bool `json:"print"`
	Clipboard bool `json:"clipboard"`
}
type Policy struct {
	DeviceID         string    `json:"deviceId"`
	Revision         int64     `json:"revision"`
	PublishedVersion int64     `json:"publishedVersion"`
	Draft            Controls  `json:"draft"`
	Published        *Controls `json:"published,omitempty"`
	UpdatedAt        time.Time `json:"updatedAt"`
}
type SaveInput struct {
	ExpectedRevision int64    `json:"expectedRevision"`
	Draft            Controls `json:"draft"`
}
type PublishInput struct {
	ExpectedRevision int64 `json:"expectedRevision"`
}
type Desired struct {
	Configured    bool      `json:"configured"`
	SchemaVersion string    `json:"schemaVersion"`
	DeviceID      string    `json:"deviceId"`
	RuntimeID     string    `json:"runtimeId"`
	CredentialID  string    `json:"credentialId"`
	PolicyVersion int64     `json:"policyVersion"`
	Controls      Controls  `json:"controls"`
	Challenge     string    `json:"challenge"`
	IssuedAt      time.Time `json:"issuedAt"`
	ValidUntil    time.Time `json:"validUntil"`
}
type Channel struct {
	Name       string `json:"name"`
	Scope      string `json:"scope"`
	State      string `json:"state"`
	Targets    int    `json:"targets"`
	Controlled int    `json:"controlled"`
	ReasonCode string `json:"reasonCode"`
}
type Receipt struct {
	SchemaVersion string    `json:"schemaVersion"`
	DeviceID      string    `json:"deviceId"`
	RuntimeID     string    `json:"runtimeId"`
	CredentialID  string    `json:"credentialId"`
	PolicyVersion int64     `json:"policyVersion"`
	Challenge     string    `json:"challenge"`
	ObservedAt    time.Time `json:"observedAt"`
	Channels      []Channel `json:"channels"`
}
type View struct {
	Policy              Policy       `json:"policy"`
	Receipt             *Observation `json:"receipt,omitempty"`
	ReceivedAt          *time.Time   `json:"receivedAt,omitempty"`
	Fresh               bool         `json:"fresh"`
	RuntimeCount        int          `json:"runtimeCount"`
	PendingRuntimeCount int          `json:"pendingRuntimeCount"`
}
type Observation struct {
	RuntimeID     string    `json:"runtimeId"`
	CredentialID  string    `json:"credentialId"`
	PolicyVersion int64     `json:"policyVersion"`
	ObservedAt    time.Time `json:"observedAt"`
	Channels      []Channel `json:"channels"`
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var challenge = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var reason = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func Decode(raw []byte, out any) error {
	if len(raw) > 32768 {
		return errors.New("endpoint channel message too large")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing channel message")
	}
	return nil
}
func ValidDesired(d Desired, now time.Time) bool {
	return (d.Configured && d.PolicyVersion > 0 || !d.Configured && d.PolicyVersion == 0) && validIdentity(d.SchemaVersion, d.DeviceID, d.RuntimeID, d.CredentialID, d.PolicyVersion, d.Challenge) && !d.IssuedAt.After(now.Add(30*time.Second)) && d.ValidUntil.After(now) && d.ValidUntil.After(d.IssuedAt) && d.ValidUntil.Sub(d.IssuedAt) <= time.Minute
}
func ValidReceipt(r Receipt, now time.Time) bool {
	if !validIdentity(r.SchemaVersion, r.DeviceID, r.RuntimeID, r.CredentialID, r.PolicyVersion, r.Challenge) || len(r.Channels) != 3 || r.ObservedAt.Before(now.Add(-time.Minute)) || r.ObservedAt.After(now.Add(30*time.Second)) {
		return false
	}
	seen := map[string]bool{}
	for _, c := range r.Channels {
		if seen[c.Name] || !ValidChannel(c) {
			return false
		}
		seen[c.Name] = true
	}
	return seen["usb"] && seen["print"] && seen["clipboard"]
}
func validIdentity(version, device, runtime, credential string, revision int64, nonce string) bool {
	return version == Version && identifier.MatchString(device) && identifier.MatchString(runtime) && identifier.MatchString(credential) && revision >= 0 && challenge.MatchString(nonce)
}
func ValidChannel(c Channel) bool {
	scopes := map[string]string{"usb": "current_usb_volumes", "print": "local_new_print_jobs", "clipboard": "enrolled_console_win32"}
	if scopes[c.Name] == "" || c.Scope != scopes[c.Name] || c.Targets < 0 || c.Targets > 256 || c.Controlled < 0 || c.Controlled > c.Targets || !reason.MatchString(c.ReasonCode) {
		return false
	}
	if c.State == "controlled" && (c.Targets == 0 || c.Controlled != c.Targets) {
		return false
	}
	if (c.State == "off" || c.State == "unconfigured" || c.State == "not_present") && c.Controlled != 0 {
		return false
	}
	if (c.State == "unconfigured" || c.State == "not_present") && c.Targets != 0 {
		return false
	}
	switch c.State {
	case "off", "unconfigured", "not_present", "controlled", "partial", "failed", "unsupported", "conflict":
		return true
	}
	return false
}
