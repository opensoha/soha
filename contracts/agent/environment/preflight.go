package environment

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"
	"unicode/utf8"
)

const PreflightRequestVersion = "opensoha.dev/personal-environment-preflight-request/v1"
const PreflightResultVersion = "opensoha.dev/personal-environment-preflight-result/v1"

// A preflight may create only its disposable quota probe, never personal data.
type PreflightRequest struct {
	SchemaVersion string `json:"schemaVersion"`
	ID            string `json:"id"`
	Quota         Quota  `json:"quota"`
}

type PreflightResult struct {
	SchemaVersion string    `json:"schemaVersion"`
	ID            string    `json:"id"`
	Quota         Quota     `json:"quota"`
	Ready         bool      `json:"ready"`
	CgroupVersion string    `json:"cgroupVersion"`
	DiskMechanism string    `json:"diskMechanism"`
	ObservedAt    time.Time `json:"observedAt"`
}

func (r *PreflightRequest) UnmarshalJSON(data []byte) error {
	if !utf8.Valid(data) {
		return errors.New("preflight request must be UTF-8")
	}
	type wireRequest PreflightRequest
	var decoded wireRequest
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&decoded); err != nil {
		return err
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return errors.New("invalid preflight request")
	}
	*r = PreflightRequest(decoded)
	return r.Validate()
}

func (r PreflightRequest) Validate() error {
	if r.SchemaVersion != PreflightRequestVersion || !canonicalUUIDPattern.MatchString(r.ID) {
		return errors.New("invalid preflight identity")
	}
	return r.Quota.ValidatePersonalDocker()
}

func (r PreflightResult) ValidateFor(request PreflightRequest) error {
	if r.SchemaVersion != PreflightResultVersion || r.ID != request.ID || r.Quota != request.Quota || !r.Ready || r.CgroupVersion != "2" || r.DiskMechanism != "btrfs-qgroup" || r.ObservedAt.IsZero() {
		return errors.New("invalid personal environment preflight receipt")
	}
	return nil
}
