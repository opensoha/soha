package config

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strings"
)

// TrustedKeys returns only locally provisioned anchors. Payloads never add keys.
func (c OutpostConfig) TrustedKeys() (map[string]ed25519.PublicKey, error) {
	keys := make(map[string]ed25519.PublicKey, len(c.TrustPublicKeys)+1)
	if c.TrustKeyID != "" || c.TrustPublicKey != "" {
		key, err := decodeOutpostAnchor(c.TrustKeyID, c.TrustPublicKey)
		if err != nil {
			return nil, err
		}
		keys[c.TrustKeyID] = key
	}
	for id, value := range c.TrustPublicKeys {
		key, err := decodeOutpostAnchor(id, value)
		if err != nil {
			return nil, err
		}
		if old, exists := keys[id]; exists && !bytes.Equal(old, key) {
			return nil, fmt.Errorf("control_plane.outpost has conflicting trust keys")
		}
		keys[id] = key
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("control_plane.outpost requires trust_key_id and trust_public_key or trust_public_keys")
	}
	return keys, nil
}

func decodeOutpostAnchor(id, value string) (ed25519.PublicKey, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if strings.TrimSpace(id) == "" || id != strings.TrimSpace(id) || err != nil || len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("control_plane.outpost trust_key_id must be nonempty and trust_public_key must be a base64-encoded Ed25519 public key")
	}
	return ed25519.PublicKey(key), nil
}
