package secretcrypto

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
	"strings"
	"time"

	"github.com/opensoha/soha/internal/platform/keyring"
)

const (
	Prefix   = "soha:v1:"
	PrefixV2 = "soha:v2:"
)

type SecretStorage string

const (
	SecretStorageNone            SecretStorage = "none"
	SecretStorageLegacyPlaintext SecretStorage = "legacy_plaintext"
	SecretStorageEncrypted       SecretStorage = "encrypted"
)

var ErrKeyRequired = errors.New("secret crypto key is required")

func EncryptString(key, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	normalized, err := NormalizeKey(key)
	if err != nil {
		return "", err
	}
	payload, err := encryptPayload(normalized, value, nil)
	if err != nil {
		return "", err
	}
	return Prefix + payload, nil
}

func EncryptStringV2(keyID, key, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	keyID = strings.TrimSpace(keyID)
	if keyID == "" || strings.Contains(keyID, ":") {
		return "", errors.New("secret crypto key id is invalid")
	}
	normalized, err := NormalizeKey(key)
	if err != nil {
		return "", err
	}
	prefix := PrefixV2 + keyID + ":"
	payload, err := encryptPayload(normalized, value, []byte(prefix))
	if err != nil {
		return "", err
	}
	return prefix + payload, nil
}

func EncryptStringWithKeyring(keys keyring.Ring, value string) (string, error) {
	active := keys.Active()
	if active.ID() == "" {
		return "", ErrKeyRequired
	}
	return EncryptStringV2(active.ID(), active.Secret(), value)
}

func DecryptString(key, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !strings.HasPrefix(value, Prefix) {
		return value, nil
	}
	normalized, err := NormalizeKey(key)
	if err != nil {
		return "", err
	}
	return decryptPayload(normalized, strings.TrimPrefix(value, Prefix), nil)
}

func DecryptStringWithKeyring(keys keyring.Ring, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, PrefixV2) {
		keyID, payload, err := parseV2(value)
		if err != nil {
			return "", err
		}
		key, ok := keys.Find(keyID, time.Now().UTC())
		if !ok {
			return "", fmt.Errorf("encrypted secret references unknown key id %q", keyID)
		}
		normalized, err := NormalizeKey(key.Secret())
		if err != nil {
			return "", err
		}
		return decryptPayload(normalized, payload, []byte(PrefixV2+keyID+":"))
	}
	if !strings.HasPrefix(value, Prefix) {
		return value, nil
	}
	if keys.Active().ID() == "" {
		return "", ErrKeyRequired
	}
	payload := strings.TrimPrefix(value, Prefix)
	for _, key := range keys.ValidKeys(time.Now().UTC()) {
		normalized, err := NormalizeKey(key.Secret())
		if err != nil {
			continue
		}
		plaintext, err := decryptPayload(normalized, payload, nil)
		if err == nil {
			return plaintext, nil
		}
	}
	return "", errors.New("decrypt secret with credential keyring")
}

func Encrypted(value string) bool {
	return SecretStorageLabel(value) == SecretStorageEncrypted
}

func SecretStorageLabel(value string) SecretStorage {
	value = strings.TrimSpace(value)
	if value == "" {
		return SecretStorageNone
	}
	if strings.HasPrefix(value, Prefix) || strings.HasPrefix(value, PrefixV2) {
		return SecretStorageEncrypted
	}
	return SecretStorageLegacyPlaintext
}

func KeyID(value string) (string, bool, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, PrefixV2) {
		return "", false, nil
	}
	keyID, _, err := parseV2(value)
	if err != nil {
		return "", false, err
	}
	return keyID, true, nil
}

func encryptPayload(key []byte, value string, additionalData []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("build secret cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("build secret gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate secret nonce: %w", err)
	}
	sealed := gcm.Seal(nil, nonce, []byte(value), additionalData)
	payload := append(append([]byte{}, nonce...), sealed...)
	return base64.StdEncoding.EncodeToString(payload), nil
}

func decryptPayload(key []byte, encoded string, additionalData []byte) (string, error) {
	payload, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode encrypted secret: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("build secret cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("build secret gcm: %w", err)
	}
	if len(payload) <= gcm.NonceSize() {
		return "", errors.New("encrypted secret payload is invalid")
	}
	nonce := payload[:gcm.NonceSize()]
	ciphertext := payload[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, additionalData)
	if err != nil {
		return "", fmt.Errorf("decrypt secret: %w", err)
	}
	return string(plaintext), nil
}

func parseV2(value string) (string, string, error) {
	remainder := strings.TrimPrefix(value, PrefixV2)
	keyID, payload, ok := strings.Cut(remainder, ":")
	if !ok || strings.TrimSpace(keyID) == "" || strings.TrimSpace(payload) == "" {
		return "", "", errors.New("encrypted secret v2 payload is invalid")
	}
	return keyID, payload, nil
}

func NormalizeKey(key string) ([]byte, error) {
	if strings.TrimSpace(key) == "" {
		return nil, ErrKeyRequired
	}
	if decoded, err := base64.StdEncoding.DecodeString(key); err == nil && len(decoded) > 0 {
		return normalizeKeyBytes(decoded), nil
	}
	return normalizeKeyBytes([]byte(key)), nil
}

// EncryptJSON preserves the legacy binary credential envelope used by virtualization records.
func EncryptJSON(key string, value map[string]any) ([]byte, error) {
	normalized, err := NormalizeKey(key)
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(value)
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
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

func DecryptJSON(key string, encrypted []byte) (map[string]any, error) {
	normalized, err := NormalizeKey(key)
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
	plain, err := gcm.Open(nil, encrypted[:gcm.NonceSize()], encrypted[gcm.NonceSize():], nil)
	if err != nil {
		return nil, errors.New("decrypt credential json")
	}
	var value map[string]any
	if err := json.Unmarshal(plain, &value); err != nil {
		return nil, fmt.Errorf("unmarshal credential json: %w", err)
	}
	return value, nil
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
