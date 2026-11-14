package guest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnrollmentAuthorizationUsesCanonicalBoundedChallenge(t *testing.T) {
	valid := "Bearer " + strings.Repeat("A", 43)
	digest, err := EnrollmentTokenHash(valid)
	if err != nil || !digestPattern.MatchString(digest) {
		t.Fatalf("challenge hash: %q %v", digest, err)
	}
	for _, invalid := range []string{"", "Basic " + strings.Repeat("A", 43), valid + "=", valid + " ", " " + valid, "Bearer " + strings.Repeat("A", 42) + "B"} {
		if _, err := EnrollmentTokenHash(invalid); err == nil {
			t.Fatal("noncanonical enrollment challenge accepted")
		}
	}
}

func TestSchemaFixtures(t *testing.T) {
	for name, decode := range map[string]func([]byte) error{
		"binding":    func(data []byte) error { return json.Unmarshal(data, new(Binding)) },
		"enrollment": func(data []byte) error { return json.Unmarshal(data, new(Enrollment)) },
		"heartbeat":  func(data []byte) error { return json.Unmarshal(data, new(Heartbeat)) },
	} {
		for _, kind := range []string{"valid", "invalid"} {
			files, err := filepath.Glob(filepath.Join("../../fixtures/json-schema/personal-guest-"+name, kind, "*.json"))
			if err != nil || len(files) == 0 {
				t.Fatalf("missing %s/%s fixtures: %v", name, kind, err)
			}
			for _, file := range files {
				t.Run(name+"/"+kind+"/"+filepath.Base(file), func(t *testing.T) {
					data, err := os.ReadFile(file) // #nosec G304 -- Glob uses only the fixed repository fixture names and valid/invalid categories above.
					if err != nil {
						t.Fatal(err)
					}
					if err := decode(data); (err == nil) != (kind == "valid") {
						t.Fatalf("fixture result: %v", err)
					}
				})
			}
		}
	}
}

func guestFixture[T any](t *testing.T, name string) T {
	t.Helper()
	data, err := os.ReadFile("../../fixtures/json-schema/personal-guest-" + name + "/valid/minimal.json") // #nosec G304 -- All callers pass constant test fixture names, never external input.
	if err != nil {
		t.Fatal(err)
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestHeartbeatRequiresCurrentBindingAndFreshMonotonicObservation(t *testing.T) {
	binding := guestFixture[Binding](t, "binding")
	heartbeat := guestFixture[Heartbeat](t, "heartbeat")
	now := heartbeat.ObservedAt
	if err := heartbeat.ValidateFor(binding, now, 0); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Heartbeat){
		"different binding": func(h *Heartbeat) { h.BindingID = binding.DataRef },
		"old revision":      func(h *Heartbeat) { h.BindingVersion++ },
		"old compute":       func(h *Heartbeat) { h.ComputeGeneration++ },
		"zero sequence":     func(h *Heartbeat) { h.Sequence = 0 },
		"inexact sequence":  func(h *Heartbeat) { h.Sequence = MaxCounter + 1 },
		"future clock":      func(h *Heartbeat) { h.ObservedAt = now.Add(MaxClockSkew + time.Nanosecond) },
		"old clock":         func(h *Heartbeat) { h.ObservedAt = now.Add(-MaxClockSkew - time.Nanosecond) },
		"expired observation": func(h *Heartbeat) {
			h.ObservedAt = binding.ExpiresAt
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := heartbeat
			mutate(&h)
			if h.ValidateFor(binding, now, 0) == nil {
				t.Fatal("invalid heartbeat accepted")
			}
		})
	}
	for name, serverNow := range map[string]time.Time{
		"zero clock": {}, "not yet valid": binding.IssuedAt.Add(-time.Nanosecond), "expired": binding.ExpiresAt,
	} {
		t.Run(name, func(t *testing.T) {
			if heartbeat.ValidateFor(binding, serverNow, 0) == nil {
				t.Fatal("invalid binding deadline accepted")
			}
		})
	}
	if heartbeat.ValidateFor(binding, now, heartbeat.Sequence) == nil || heartbeat.ValidateFor(binding, now, -1) == nil {
		t.Fatal("replay or invalid server sequence accepted")
	}
	heartbeat.Sequence++
	if err := heartbeat.ValidateFor(binding, now, heartbeat.Sequence-1); err != nil {
		t.Fatal(err)
	}
}

func TestBindingRejectsInvalidLifetime(t *testing.T) {
	binding := guestFixture[Binding](t, "binding")
	for _, expiresAt := range []time.Time{binding.IssuedAt, binding.IssuedAt.Add(-time.Second), binding.IssuedAt.Add(24*time.Hour + time.Nanosecond)} {
		binding.ExpiresAt = expiresAt
		if binding.Validate() == nil {
			t.Fatal("invalid credential lifetime accepted")
		}
	}
}

func TestDecodeRejectsUnknownFieldsInvalidUTF8AndOversizedMessages(t *testing.T) {
	valid, err := json.Marshal(guestFixture[Heartbeat](t, "heartbeat"))
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"invalid UTF8": append(append([]byte{}, valid[:len(valid)-1]...), 0xff, '}'),
		"null":         []byte("null"),
		"oversized":    append(valid, make([]byte, 4096)...),
		"legacy owner": append(append([]byte{}, valid[:len(valid)-1]...), []byte(",\"ownerId\":\"other\"}")...),
		"duplicate":    append(append([]byte{}, valid[:len(valid)-1]...), []byte(",\"sequence\":2}")...),
		"wrong case":   []byte(strings.Replace(string(valid), "\"sequence\"", "\"Sequence\"", 1)),
	} {
		t.Run(name, func(t *testing.T) {
			if json.Unmarshal(data, new(Heartbeat)) == nil {
				t.Fatal("invalid wire message accepted")
			}
		})
	}
	type wireHeartbeat Heartbeat
	if decodeStrict(append(valid, []byte(" {}")...), new(wireHeartbeat), "schemaVersion", "bindingId", "bindingVersion", "computeGeneration", "sequence", "observedAt") == nil {
		t.Fatal("trailing object accepted")
	}
}
