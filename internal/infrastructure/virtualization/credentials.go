package virtualization

import "github.com/opensoha/soha/internal/platform/secretcrypto"

var ErrCredentialKeyRequired = secretcrypto.ErrKeyRequired

func EncryptCredentialJSON(key string, credential map[string]any) ([]byte, error) {
	return secretcrypto.EncryptJSON(key, credential)
}

func DecryptCredentialJSON(key string, encrypted []byte) (map[string]any, error) {
	return secretcrypto.DecryptJSON(key, encrypted)
}

func NormalizeCredentialKey(key string) ([]byte, error) {
	return secretcrypto.NormalizeKey(key)
}
