// Package guest carries the personal VM guest registration and heartbeat protocol.
// Schemas in agent/ own the wire shapes. This protocol grants no task execution.
package guest

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	BindingVersion    = "opensoha.dev/personal-guest-binding/v1"
	EnrollmentVersion = "opensoha.dev/personal-guest-enrollment/v1"
	HeartbeatVersion  = "opensoha.dev/personal-guest-heartbeat/v1"
	MaxClockSkew      = 30 * time.Second
	MaxCounter        = 1<<53 - 1 // Exact integer range shared by Go and JavaScript consumers.
	EnrollmentMinTTL  = time.Minute
	EnrollmentMaxTTL  = 10 * time.Minute
	BindingsPath      = "/api/personal-guest/v1/bindings"
)

var (
	uuidPattern   = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
	digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	serialPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,20}$`)
)

// Binding is derived by Core and immutable for this guest registration. Version
// is the identity revision, independent of environment operation CAS versions.
type Binding struct {
	SchemaVersion        string    `json:"schemaVersion"`
	ID                   string    `json:"id"`
	Version              int64     `json:"version"`
	AssistantID          string    `json:"assistantId"`
	OwnerID              string    `json:"ownerId"`
	Provider             string    `json:"provider"`
	ComputeRef           string    `json:"computeRef"`
	ComputeGeneration    int64     `json:"computeGeneration"`
	ProviderIdentityHash string    `json:"providerIdentityHash"`
	DataRef              string    `json:"dataRef"`
	DataDiskSerial       string    `json:"dataDiskSerial"`
	DataDiskBytes        int64     `json:"dataDiskBytes"`
	PolicyRevision       string    `json:"policyRevision"`
	IssuedAt             time.Time `json:"issuedAt"`
	ExpiresAt            time.Time `json:"expiresAt"`
}

// Enrollment is sent over mutually authenticated TLS using a short, one-time
// challenge in the Authorization header. The certificate pins guest identity.
// No owner, capability, filesystem path or provider credentials are client input.
type Enrollment struct {
	SchemaVersion string `json:"schemaVersion"`
	EnrollmentID  string `json:"enrollmentId"`
}

// EnrollmentTokenHash accepts exactly Bearer plus a canonical unpadded base64url
// 32-byte challenge. Core generates it with crypto/rand and stores only the hash.
// Its 60–600 second grant pins the binding and guest certificate fingerprint;
// atomic consumption, expiry and current authorization remain service duties.
func EnrollmentTokenHash(authorization string) (string, error) {
	token, ok := strings.CutPrefix(authorization, "Bearer ")
	if !ok || len(token) != 43 {
		return "", errors.New("invalid personal guest enrollment authorization")
	}
	random, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(random) != 32 {
		return "", errors.New("invalid personal guest enrollment authorization")
	}
	digest := sha256.Sum256([]byte(token))
	return fmt.Sprintf("sha256:%x", digest), nil
}

// Heartbeat proves only current authenticated liveness. Core independently
// checks current authorization, frozen binding and monotonic sequence in storage.
// A guest cannot claim data/tool/browser readiness with this message.
type Heartbeat struct {
	SchemaVersion     string    `json:"schemaVersion"`
	BindingID         string    `json:"bindingId"`
	BindingVersion    int64     `json:"bindingVersion"`
	ComputeGeneration int64     `json:"computeGeneration"`
	Sequence          int64     `json:"sequence"`
	ObservedAt        time.Time `json:"observedAt"`
}

func (b Binding) Validate() error {
	if b.SchemaVersion != BindingVersion || b.Version < 1 || b.Version > MaxCounter || b.ComputeGeneration < 1 || b.ComputeGeneration > MaxCounter || b.Provider != "pve" && b.Provider != "kubevirt" {
		return errors.New("invalid personal guest binding version or provider")
	}
	for _, id := range []string{b.ID, b.AssistantID, b.OwnerID, b.ComputeRef, b.DataRef} {
		if !uuidPattern.MatchString(id) {
			return errors.New("invalid personal guest binding identity")
		}
	}
	if !serialPattern.MatchString(b.DataDiskSerial) || b.DataDiskBytes < 16<<20 || b.DataDiskBytes > 1<<40 || !digestPattern.MatchString(b.PolicyRevision) || !digestPattern.MatchString(b.ProviderIdentityHash) {
		return errors.New("invalid personal guest data identity or policy revision")
	}
	if b.IssuedAt.IsZero() || !b.ExpiresAt.After(b.IssuedAt) || b.ExpiresAt.Sub(b.IssuedAt) > 24*time.Hour {
		return errors.New("invalid personal guest binding lifetime")
	}
	return nil
}

func (e Enrollment) Validate() error {
	if e.SchemaVersion != EnrollmentVersion || !uuidPattern.MatchString(e.EnrollmentID) {
		return errors.New("invalid personal guest enrollment")
	}
	return nil
}

func (h Heartbeat) Validate() error {
	if h.SchemaVersion != HeartbeatVersion || !uuidPattern.MatchString(h.BindingID) || h.BindingVersion < 1 || h.BindingVersion > MaxCounter || h.ComputeGeneration < 1 || h.ComputeGeneration > MaxCounter || h.Sequence < 1 || h.Sequence > MaxCounter || h.ObservedAt.IsZero() {
		return errors.New("invalid personal guest heartbeat")
	}
	return nil
}

// ValidateFor is not authentication or a readiness grant. Callers must first
// authenticate the pinned guest certificate and recheck authoritative ownership.
func (h Heartbeat) ValidateFor(binding Binding, now time.Time, lastSequence int64) error {
	if err := binding.Validate(); err != nil {
		return err
	}
	if err := h.Validate(); err != nil {
		return err
	}
	if now.IsZero() || now.Before(binding.IssuedAt) || !now.Before(binding.ExpiresAt) || lastSequence < 0 {
		return errors.New("personal guest binding is not currently valid")
	}
	if h.BindingID != binding.ID || h.BindingVersion != binding.Version || h.ComputeGeneration != binding.ComputeGeneration || h.Sequence <= lastSequence {
		return errors.New("personal guest heartbeat binding or sequence mismatch")
	}
	if h.ObservedAt.Before(binding.IssuedAt) || !h.ObservedAt.Before(binding.ExpiresAt) || h.ObservedAt.Before(now.Add(-MaxClockSkew)) || h.ObservedAt.After(now.Add(MaxClockSkew)) {
		return errors.New("personal guest heartbeat observation is stale")
	}
	return nil
}

func decodeStrict(data []byte, value any, keys ...string) error {
	return decodeStrictBounded(data, value, 4096, keys...)
}

func decodeStrictBounded(data []byte, value any, limit int, keys ...string) error {
	if len(data) > limit || !utf8.Valid(data) {
		return errors.New("personal guest message must be bounded UTF-8")
	}
	if err := validateObjectKeys(data, keys); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("personal guest message must contain one JSON object")
	}
	return nil
}

func validateObjectKeys(data []byte, keys []string) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return errors.New("personal guest message must be a JSON object")
	}
	seen := make(map[string]bool, len(keys))
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := key.(string)
		if !ok || seen[name] || !slices.Contains(keys, name) {
			return errors.New("personal guest message contains a duplicate or unknown field")
		}
		seen[name] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func (b *Binding) UnmarshalJSON(data []byte) error {
	type wire Binding
	var value wire
	if err := decodeStrict(data, &value, "schemaVersion", "id", "version", "assistantId", "ownerId", "provider", "computeRef", "computeGeneration", "providerIdentityHash", "dataRef", "dataDiskSerial", "dataDiskBytes", "policyRevision", "issuedAt", "expiresAt"); err != nil {
		return err
	}
	decoded := Binding(value)
	if err := decoded.Validate(); err != nil {
		return err
	}
	*b = decoded
	return nil
}

func (e *Enrollment) UnmarshalJSON(data []byte) error {
	type wire Enrollment
	var value wire
	if err := decodeStrict(data, &value, "schemaVersion", "enrollmentId"); err != nil {
		return err
	}
	decoded := Enrollment(value)
	if err := decoded.Validate(); err != nil {
		return err
	}
	*e = decoded
	return nil
}

func (h *Heartbeat) UnmarshalJSON(data []byte) error {
	type wire Heartbeat
	var value wire
	if err := decodeStrict(data, &value, "schemaVersion", "bindingId", "bindingVersion", "computeGeneration", "sequence", "observedAt"); err != nil {
		return err
	}
	decoded := Heartbeat(value)
	if err := decoded.Validate(); err != nil {
		return err
	}
	*h = decoded
	return nil
}
