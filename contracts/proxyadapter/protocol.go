// Package proxyadapter defines the bounded local host/adapter PX-v1 protocol.
// The host retains mTLS, lease/floor, authorization and the egress gate.
package proxyadapter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

const Major = 1
const Minor = 0
const MaxFrameBytes = 1 << 20
const MaxStderrBytes = 64 << 10
const MaxCommandLifetime = 30 * time.Second

// BinaryApproval is installer configuration, not caller-controlled input.
// Hosts must verify an absolute regular binary path, ownership and digest before spawn.
type BinaryApproval struct {
	AdapterID string `json:"adapterId"`
	Binary    string `json:"binary"`
	SHA256    string `json:"sha256"`
	Version   string `json:"version"`
}

type Method string

const (
	Handshake   Method = "handshake"
	Prepare     Method = "prepare"
	Apply       Method = "apply"
	Observe     Method = "observe"
	Close       Method = "close"
	Select      Method = "select"
	Probe       Method = "probe"
	Refresh     Method = "refresh"
	Logs        Method = "logs"
	Diagnostics Method = "diagnostics"
	Stop        Method = "stop"
)

type Payload struct {
	Challenge    string                               `json:"challenge,omitempty"`
	Descriptor   *api.NetworkProxyEngineDescriptor    `json:"descriptor,omitempty"`
	Revision     *int64                               `json:"revision,omitempty"`
	ContentHash  string                               `json:"contentHash,omitempty"`
	Content      string                               `json:"content,omitempty"`
	ValidUntil   *time.Time                           `json:"validUntil,omitempty"`
	Command      *api.NetworkProxyRuntimeCommandInput `json:"command,omitempty"`
	ConnectionID string                               `json:"connectionId,omitempty"`
	IssuedAt     *time.Time                           `json:"issuedAt,omitempty"`
	ExpiresAt    *time.Time                           `json:"expiresAt,omitempty"`
}
type Observation struct {
	SampleSequence int64                        `json:"sampleSequence"`
	UploadTotal    int64                        `json:"uploadTotal"`
	DownloadTotal  int64                        `json:"downloadTotal"`
	TotalActive    int64                        `json:"totalActive"`
	ReturnedCount  int64                        `json:"returnedCount"`
	Truncated      bool                         `json:"truncated"`
	Freshness      string                       `json:"freshness"`
	Connections    []api.NetworkProxyConnection `json:"connections"`
}
type Result struct {
	CloseStatus  string                                `json:"closeStatus,omitempty"`
	Challenge    string                                `json:"challenge,omitempty"`
	Descriptor   *api.NetworkProxyEngineDescriptor     `json:"descriptor,omitempty"`
	ReadbackHash string                                `json:"readbackHash,omitempty"`
	Revision     *int64                                `json:"revision,omitempty"`
	Observation  *Observation                          `json:"observation,omitempty"`
	Command      *api.NetworkProxyRuntimeCommandResult `json:"command,omitempty"`
}
type ProtocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Frame struct {
	Major        int            `json:"major"`
	Minor        int            `json:"minor"`
	RequestID    string         `json:"requestId"`
	Deadline     time.Time      `json:"deadline"`
	ProcessEpoch string         `json:"processEpoch"`
	Method       Method         `json:"method"`
	Payload      *Payload       `json:"payload,omitempty"`
	Result       *Result        `json:"result,omitempty"`
	Error        *ProtocolError `json:"error,omitempty"`
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var digest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// DecodeFrame accepts exactly one newline-delimited frame, including its newline
// in the limit. stdout must contain only these frames; logs use bounded stderr.
func DecodeFrame(line []byte, now time.Time) (Frame, error) {
	var f Frame
	if len(line) > MaxFrameBytes || len(line) == 0 || line[len(line)-1] != '\n' || bytes.Contains(line[:len(line)-1], []byte{'\n'}) {
		return f, errors.New("invalid frame boundary or size")
	}
	d := json.NewDecoder(bytes.NewReader(line))
	d.DisallowUnknownFields()
	if err := d.Decode(&f); err != nil {
		return f, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return f, errors.New("trailing frame data")
	}
	return f, f.Validate(now)
}
func (f Frame) Validate(now time.Time) error {
	if f.Major != Major || f.Minor != Minor {
		return errors.New("incompatible_version")
	}
	if !identifier.MatchString(f.RequestID) || !identifier.MatchString(f.ProcessEpoch) {
		return errors.New("invalid correlation identity")
	}
	if !f.Deadline.After(now) || f.Deadline.After(now.Add(MaxCommandLifetime)) {
		return errors.New("expired or excessive deadline")
	}
	switch f.Method {
	case Handshake, Prepare, Apply, Observe, Close, Select, Probe, Refresh, Logs, Diagnostics, Stop:
	default:
		return errors.New("unsupported method")
	}
	count := 0
	for _, v := range []bool{f.Payload != nil, f.Result != nil, f.Error != nil} {
		if v {
			count++
		}
	}
	if count != 1 {
		return errors.New("exactly one payload, result or error required")
	}
	if f.Error != nil {
		switch f.Error.Code {
		case "unsupported", "unavailable", "invalid_request", "expired", "epoch_mismatch", "incompatible_version", "failed":
		default:
			return errors.New("invalid error code")
		}
		if len(f.Error.Message) > 512 {
			return errors.New("error too long")
		}
	}
	if p := f.Payload; p != nil {
		switch f.Method {
		case Handshake:
			if len(p.Challenge) < 32 || len(p.Challenge) > 128 {
				return errors.New("random challenge required")
			}
		case Prepare, Apply:
			if p.Revision == nil || *p.Revision < 1 || !digest.MatchString(p.ContentHash) || p.Content == "" || len(p.Content) > MaxFrameBytes || p.ValidUntil == nil || !p.ValidUntil.After(now) {
				return errors.New("configuration identity and valid lease required")
			}
		case Close:
			if !identifier.MatchString(p.ConnectionID) || p.IssuedAt == nil || p.ExpiresAt == nil {
				return errors.New("close identity and times required")
			}
			if err := ValidateWindow(*p.IssuedAt, *p.ExpiresAt, now); err != nil {
				return err
			}
		case Select, Probe, Refresh, Logs, Diagnostics:
			if p.Command == nil {
				return errors.New("typed command required")
			}
			if err := ValidateCommand(*p.Command, f.ProcessEpoch, now); err != nil {
				return err
			}
			if string(p.Command.Kind) != string(f.Method) {
				return errors.New("command method mismatch")
			}
		}
	}
	if r := f.Result; r != nil {
		if r.Descriptor != nil {
			if err := ValidateDescriptor(*r.Descriptor); err != nil {
				return err
			}
		}
		switch f.Method {
		case Handshake:
			if len(r.Challenge) < 32 || r.Descriptor == nil {
				return errors.New("handshake challenge/descriptor required")
			}
		case Apply:
			if r.Revision == nil || *r.Revision < 1 || !digest.MatchString(r.ReadbackHash) {
				return errors.New("apply readback required")
			}
		case Close:
			if r.CloseStatus != "closed" && r.CloseStatus != "not-found" && r.CloseStatus != "failed" {
				return errors.New("close status required")
			}
		case Observe:
			if r.Observation == nil {
				return errors.New("observation required")
			}
			o := r.Observation
			if o.SampleSequence < 0 || o.UploadTotal < 0 || o.DownloadTotal < 0 || o.TotalActive < 0 || o.ReturnedCount != int64(len(o.Connections)) || o.TotalActive < o.ReturnedCount || o.Truncated != (o.TotalActive > o.ReturnedCount) || len(o.Connections) > 200 {
				return errors.New("invalid counts")
			}
			switch o.Freshness {
			case "fresh", "stale", "unavailable":
			default:
				return errors.New("invalid freshness")
			}
		}
	}
	return nil
}
func ValidateWindow(issuedAt, expiresAt, now time.Time) error {
	if issuedAt.After(now) || !expiresAt.After(now) || !expiresAt.After(issuedAt) || expiresAt.Sub(issuedAt) > MaxCommandLifetime {
		return errors.New("expired or excessive command lifetime")
	}
	return nil
}
func ValidateCommand(c api.NetworkProxyRuntimeCommandInput, epoch string, now time.Time) error {
	if !identifier.MatchString(c.RequestID) || c.ProcessEpoch != epoch || !identifier.MatchString(epoch) {
		return errors.New("epoch or request identity mismatch")
	}
	if err := ValidateWindow(c.IssuedAt, c.ExpiresAt, now); err != nil {
		return err
	}
	switch string(c.Kind) {
	case "groups", "rules", "dns", "refresh", "logs", "diagnostics":
	case "select":
		if c.GroupID == "" || c.NodeID == "" {
			return errors.New("selection refs required")
		}
	case "probe":
		if c.ProbeProfileID == "" {
			return errors.New("approved probe profile required")
		}
	default:
		return fmt.Errorf("unsupported command %q", c.Kind)
	}
	return nil
}

// MatchResponse prevents late or cross-process replies from satisfying a request.
func MatchResponse(request, response Frame, challenge string, now time.Time) error {
	if err := response.Validate(now); err != nil {
		return err
	}
	if response.Payload != nil || response.RequestID != request.RequestID || response.ProcessEpoch != request.ProcessEpoch || response.Method != request.Method || !response.Deadline.Equal(request.Deadline) {
		return errors.New("response correlation mismatch")
	}
	if request.Method == Handshake && response.Error == nil && (response.Result == nil || response.Result.Challenge != challenge) {
		return errors.New("challenge mismatch")
	}
	return nil
}

// EncodeFrame includes the delimiter in the maximum frame size.
func EncodeFrame(f Frame, now time.Time) ([]byte, error) {
	if err := f.Validate(now); err != nil {
		return nil, err
	}
	b, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	if len(b)+1 > MaxFrameBytes {
		return nil, errors.New("frame too large")
	}
	return append(b, '\n'), nil
}

// ValidateDescriptor checks identity/protocol only. Host security capabilities
// are installer verified and cannot be granted by an adapter declaration.
func ValidateDescriptor(d api.NetworkProxyEngineDescriptor) error {
	if !regexp.MustCompile(`^[a-z][a-z0-9.-]{0,63}$`).MatchString(d.EngineID) || !regexp.MustCompile(`^[a-z][a-z0-9.-]{0,63}$`).MatchString(d.AdapterID) || d.Version == "" || len(d.Version) > 64 || int(d.ProtocolMajor) != Major || int(d.ProtocolMinor) != Minor || len(d.Capabilities) > 64 {
		return errors.New("invalid descriptor")
	}
	seen := map[string]bool{}
	for _, c := range d.Capabilities {
		if c == "" || len(c) > 64 || seen[c] {
			return errors.New("invalid capabilities")
		}
		seen[c] = true
	}
	return nil
}
