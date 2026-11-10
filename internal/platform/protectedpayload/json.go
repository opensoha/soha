// Package protectedpayload seals private durable JSON with an authenticated
// record binding. Public views must be built from the owning domain separately.
package protectedpayload

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

const Format = "soha.private-json/v1"
const maxBytes = 16 << 20

var ErrUnavailable = errors.New("private durable payload is unavailable")

type envelope struct {
	Format     string `json:"format"`
	Ciphertext string `json:"ciphertext"`
}
type document struct {
	Binding string          `json:"binding"`
	Value   json.RawMessage `json:"value"`
}

func Seal(keys keyring.Ring, binding string, value json.RawMessage) (json.RawMessage, error) {
	if binding == "" || len(value) > maxBytes || !json.Valid(value) {
		return nil, ErrUnavailable
	}
	raw, err := json.Marshal(document{Binding: binding, Value: value})
	if err != nil {
		return nil, ErrUnavailable
	}
	ciphertext, err := secretcrypto.EncryptStringWithKeyring(keys, string(raw))
	if err != nil || !secretcrypto.Encrypted(ciphertext) {
		return nil, ErrUnavailable
	}
	sealed, err := json.Marshal(envelope{Format: Format, Ciphertext: ciphertext})
	if err != nil {
		return nil, ErrUnavailable
	}
	return sealed, nil
}

func IsSealed(raw json.RawMessage) bool {
	var value struct {
		Format string `json:"format"`
	}
	return json.Unmarshal(raw, &value) == nil && value.Format == Format
}

func Open(keys keyring.Ring, binding string, sealed json.RawMessage) (json.RawMessage, error) {
	var wrapper envelope
	if binding == "" || len(sealed) > maxBytes*2 || strictJSON(sealed, &wrapper) != nil || wrapper.Format != Format || !secretcrypto.Encrypted(wrapper.Ciphertext) {
		return nil, ErrUnavailable
	}
	raw, err := secretcrypto.DecryptStringWithKeyring(keys, wrapper.Ciphertext)
	if err != nil {
		return nil, ErrUnavailable
	}
	var value document
	if strictJSON([]byte(raw), &value) != nil || value.Binding != binding || len(value.Value) > maxBytes || !json.Valid(value.Value) {
		return nil, ErrUnavailable
	}
	return value.Value, nil
}

// Decode retains opaque JSON integer values rather than rounding through float64.
func Decode(raw json.RawMessage, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return ErrUnavailable
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return ErrUnavailable
	}
	return nil
}

func strictJSON(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return ErrUnavailable
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return ErrUnavailable
	}
	return nil
}
