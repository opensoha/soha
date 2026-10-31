package protectedpayload

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/platform/keyring"
)

func testKeys(t *testing.T, id, secret string) keyring.Ring {
	t.Helper()
	key, err := keyring.NewKey(id, secret, time.Now().UTC(), nil)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ring
}

func TestPrivatePayloadBindsIdentityAndKeepsExactJSON(t *testing.T) {
	keys := testKeys(t, "first", "task115-private-payload-test-key")
	raw := json.RawMessage(`{"content":"PRIVATE_MARKER","number":9007199254740993,"pending":[{"argv":["sh","private"]}]}`)
	sealed, err := Seal(keys, "workflow:actor:task-1", raw)
	if err != nil || bytes.Contains(sealed, []byte("PRIVATE_MARKER")) || !IsSealed(sealed) {
		t.Fatal("payload was not sealed", err)
	}
	opened, err := Open(keys, "workflow:actor:task-1", sealed)
	if err != nil || !bytes.Equal(opened, raw) {
		t.Fatal("payload changed", err)
	}
	var decoded map[string]any
	if err := Decode(opened, &decoded); err != nil {
		t.Fatal(err)
	}
	number, ok := decoded["number"].(json.Number)
	if !ok || number.String() != "9007199254740993" {
		t.Fatal("integer precision lost")
	}
	if _, err = Open(keys, "workflow:actor:other-task", sealed); err == nil {
		t.Fatal("payload transplanted")
	}
	if _, err = Open(testKeys(t, "second", "different-test-key"), "workflow:actor:task-1", sealed); err == nil {
		t.Fatal("wrong key accepted")
	}
	if _, err = Seal(keyring.Ring{}, "workflow:actor:task-1", raw); err == nil {
		t.Fatal("missing key stored plaintext")
	}
}

func TestPrivatePayloadRejectsCorruptionAndAmbiguousEnvelopes(t *testing.T) {
	keys := testKeys(t, "first", "task115-private-payload-test-key")
	sealed, err := Seal(keys, "record-1", json.RawMessage(`{"content":"private"}`))
	if err != nil {
		t.Fatal(err)
	}
	var wrapper map[string]any
	if err := json.Unmarshal(sealed, &wrapper); err != nil {
		t.Fatal(err)
	}
	cipher, ok := wrapper["ciphertext"].(string)
	if !ok || len(cipher) < 5 {
		t.Fatal("missing bounded ciphertext fixture")
	}
	wrapper["ciphertext"] = cipher[:len(cipher)-5] + "AAAAA"
	changed, _ := json.Marshal(wrapper)
	for _, raw := range []json.RawMessage{changed, append(append([]byte{}, sealed...), []byte(` {}`)...), []byte(`{"format":"soha.private-json/v1","ciphertext":"plain"}`), []byte(`{"content":"plain"}`)} {
		if _, err := Open(keys, "record-1", raw); err == nil {
			t.Fatal("malformed private payload accepted")
		}
	}
	if _, err := Seal(keys, "record-1", json.RawMessage(strings.Repeat(" ", maxBytes+1))); err == nil {
		t.Fatal("unbounded payload accepted")
	}
}
