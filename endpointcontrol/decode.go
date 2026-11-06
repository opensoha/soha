package endpointcontrol

import (
	"errors"
	"time"
)

// Required booleans must be present, including explicit false. Missing input
// must never become a newer all-off policy through Go zero values.
func (c *Controls) UnmarshalJSON(raw []byte) error {
	var w struct {
		USB       *bool `json:"usb"`
		Print     *bool `json:"print"`
		Clipboard *bool `json:"clipboard"`
	}
	if err := Decode(raw, &w); err != nil {
		return err
	}
	if w.USB == nil || w.Print == nil || w.Clipboard == nil {
		return errors.New("all native channel decisions are required")
	}
	*c = Controls{USB: *w.USB, Print: *w.Print, Clipboard: *w.Clipboard}
	return nil
}
func (d *Desired) UnmarshalJSON(raw []byte) error {
	var w struct {
		Configured    *bool      `json:"configured"`
		SchemaVersion string     `json:"schemaVersion"`
		DeviceID      string     `json:"deviceId"`
		RuntimeID     string     `json:"runtimeId"`
		CredentialID  string     `json:"credentialId"`
		PolicyVersion *int64     `json:"policyVersion"`
		Controls      *Controls  `json:"controls"`
		Challenge     string     `json:"challenge"`
		IssuedAt      *time.Time `json:"issuedAt"`
		ValidUntil    *time.Time `json:"validUntil"`
	}
	if err := Decode(raw, &w); err != nil {
		return err
	}
	if w.Configured == nil || w.PolicyVersion == nil || w.Controls == nil || w.IssuedAt == nil || w.ValidUntil == nil {
		return errors.New("native channel envelope has missing required fields")
	}
	*d = Desired{Configured: *w.Configured, SchemaVersion: w.SchemaVersion, DeviceID: w.DeviceID, RuntimeID: w.RuntimeID, CredentialID: w.CredentialID, PolicyVersion: *w.PolicyVersion, Controls: *w.Controls, Challenge: w.Challenge, IssuedAt: *w.IssuedAt, ValidUntil: *w.ValidUntil}
	return nil
}
func (c *Channel) UnmarshalJSON(raw []byte) error {
	var w struct {
		Name       string `json:"name"`
		Scope      string `json:"scope"`
		State      string `json:"state"`
		Targets    *int   `json:"targets"`
		Controlled *int   `json:"controlled"`
		ReasonCode string `json:"reasonCode"`
	}
	if err := Decode(raw, &w); err != nil {
		return err
	}
	if w.Targets == nil || w.Controlled == nil {
		return errors.New("native channel counts are required")
	}
	*c = Channel{Name: w.Name, Scope: w.Scope, State: w.State, Targets: *w.Targets, Controlled: *w.Controlled, ReasonCode: w.ReasonCode}
	return nil
}
