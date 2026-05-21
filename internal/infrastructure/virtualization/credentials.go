package virtualization

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var ErrCredentialKeyRequired = errors.New("credential crypto key is required")

func EncryptCredentialJSON(key string, credential map[string]any) ([]byte, error) {
	normalized, err := NormalizeCredentialKey(key)
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(credential)
	if err != nil {
		return nil, fmt.Errorf("marshal credential json: %w", err)
	}
	block, err := aes.NewCipher(normalized)
	if err != nil {
		return nil, fmt.Errorf("build credential cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build credential gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate credential nonce: %w", err)
	}
	sealed := gcm.Seal(nil, nonce, plain, nil)
	out := make([]byte, 0, len(nonce)+len(sealed))
	out = append(out, nonce...)
	out = append(out, sealed...)
	return out, nil
}

func DecryptCredentialJSON(key string, encrypted []byte) (map[string]any, error) {
	normalized, err := NormalizeCredentialKey(key)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(normalized)
	if err != nil {
		return nil, fmt.Errorf("build credential cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build credential gcm: %w", err)
	}
	if len(encrypted) <= gcm.NonceSize() {
		return nil, errors.New("encrypted credential payload is too short")
	}
	nonce := encrypted[:gcm.NonceSize()]
	ciphertext := encrypted[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("decrypt credential json")
	}
	var credential map[string]any
	if err := json.Unmarshal(plain, &credential); err != nil {
		return nil, fmt.Errorf("unmarshal credential json: %w", err)
	}
	return credential, nil
}

func NormalizeCredentialKey(key string) ([]byte, error) {
	if key == "" {
		return nil, ErrCredentialKeyRequired
	}
	if decoded, err := base64.StdEncoding.DecodeString(key); err == nil && len(decoded) > 0 {
		return normalizeKeyBytes(decoded), nil
	}
	return normalizeKeyBytes([]byte(key)), nil
}

func normalizeKeyBytes(raw []byte) []byte {
	if len(raw) == 32 {
		out := make([]byte, 32)
		copy(out, raw)
		return out
	}
	sum := sha256.Sum256(raw)
	out := make([]byte, 32)
	copy(out, sum[:])
	return out
}
