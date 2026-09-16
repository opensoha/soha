// Package networkprobe authenticates short-lived entrance probes, not network access.
package networkprobe

import (
	"bytes"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/opensoha/soha/internal/platform/keyring"
)

type Claims struct {
	Version           int    `json:"version"`
	GatewayRuntimeID  string `json:"gatewayRuntimeId"`
	EndpointRuntimeID string `json:"endpointRuntimeId"`
	IntentID          string `json:"intentId"`
	IssuedAt          int64  `json:"issuedAt"`
	ExpiresAt         int64  `json:"expiresAt"`
}

type Signer struct{ key ed25519.PrivateKey }

// Domain-separated derivation keeps probe signing under the existing runtime keyring.
// Only the derived public key is sent to gateways; no additional secret store is needed.
func NewSigner(keys keyring.Ring) (*Signer, error) {
	active := keys.Active()
	if active.ID() == "" || active.Secret() == "" {
		return nil, errors.New("VPN probe signing key is unavailable")
	}
	mac := hmac.New(sha256.New, []byte(active.Secret()))
	_, _ = mac.Write([]byte("OpenSoha/network-vpn-probe/ed25519/v1"))
	return &Signer{key: ed25519.NewKeyFromSeed(mac.Sum(nil))}, nil
}

func (s *Signer) PublicKey() string {
	return base64.StdEncoding.EncodeToString(s.key[ed25519.SeedSize:])
}

func (s *Signer) Sign(claims Claims) (string, error) {
	if !validClaims(claims) {
		return "", errors.New("invalid VPN probe claims")
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signature := ed25519.Sign(s.key, payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func Verify(publicKey, token, gatewayRuntimeID, endpointRuntimeID string, now time.Time) (Claims, error) {
	fail := errors.New("VPN probe token is invalid or expired")
	if len(token) > 4096 {
		return Claims{}, fail
	}
	encoded, signature, ok := strings.Cut(token, ".")
	if !ok || strings.Contains(signature, ".") {
		return Claims{}, fail
	}
	key, err := base64.StdEncoding.DecodeString(publicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return Claims{}, fail
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(payload) > 2048 {
		return Claims{}, fail
	}
	sig, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(key), payload, sig) {
		return Claims{}, fail
	}
	var claims Claims
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&claims) != nil || decoder.Decode(new(any)) != io.EOF || !validClaims(claims) || claims.GatewayRuntimeID != gatewayRuntimeID || claims.EndpointRuntimeID != endpointRuntimeID || claims.ExpiresAt <= now.Unix() || claims.IssuedAt > now.Add(5*time.Second).Unix() {
		return Claims{}, fail
	}
	return claims, nil
}

func validClaims(c Claims) bool {
	return c.Version == 1 && c.GatewayRuntimeID != "" && len(c.GatewayRuntimeID) <= 128 && c.EndpointRuntimeID != "" && len(c.EndpointRuntimeID) <= 128 && c.IntentID != "" && len(c.IntentID) <= 128 && c.ExpiresAt > c.IssuedAt && c.ExpiresAt-c.IssuedAt <= 120
}
