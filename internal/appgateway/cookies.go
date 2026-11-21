package appgateway

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	oauthStateTTL     = 5 * time.Minute
	sessionCookieAAD  = "soha-app-gateway:session:v1:"
	oauthStateAAD     = "soha-app-gateway:oauth-state:v1:"
	maxCookieValueLen = 4096
)

type sessionClaims struct {
	SessionID string    `json:"sid"`
	UserID    string    `json:"uid"`
	Host      string    `json:"host"`
	ExpiresAt time.Time `json:"exp"`
}

type oauthStateClaims struct {
	Nonce      string    `json:"n"`
	Host       string    `json:"host"`
	ReturnPath string    `json:"ret"`
	ExpiresAt  time.Time `json:"exp"`
}

func sealSessionCookie(secret []byte, host string, claims sessionClaims, secure bool, now time.Time) (*http.Cookie, error) {
	if claims.SessionID == "" || claims.UserID == "" || host == "" || !claims.ExpiresAt.After(now) {
		return nil, errors.New("invalid session cookie claims")
	}
	claims.Host = host
	claims.ExpiresAt = claims.ExpiresAt.UTC()
	value, err := sealAEAD(secret, sessionCookieAAD+host, claims)
	if err != nil {
		return nil, err
	}
	maxAge := int(claims.ExpiresAt.Sub(now).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	return &http.Cookie{ //nolint:gosec // G124: Secure is true for https (__Host-) and only false when PublicScheme=http for local loopback tests
		Name:     sessionCookieName(secure),
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  claims.ExpiresAt,
		MaxAge:   maxAge,
	}, nil
}

func openSessionCookie(secret []byte, host, rawValue string, now time.Time) (sessionClaims, error) {
	var claims sessionClaims
	if err := openAEAD(secret, sessionCookieAAD+host, rawValue, &claims); err != nil {
		return sessionClaims{}, err
	}
	if claims.Host != host || claims.SessionID == "" || claims.UserID == "" {
		return sessionClaims{}, errors.New("session cookie claims mismatch")
	}
	if !now.Before(claims.ExpiresAt) {
		return sessionClaims{}, errors.New("session cookie expired")
	}
	return claims, nil
}

func mintOAuthStateCookie(secret []byte, host, returnPath string, secure bool, now time.Time) (*http.Cookie, string, error) {
	nonceBytes := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, nonceBytes); err != nil {
		return nil, "", fmt.Errorf("generate oauth state nonce: %w", err)
	}
	expiresAt := now.Add(oauthStateTTL).UTC()
	claims := oauthStateClaims{
		Nonce:      hex.EncodeToString(nonceBytes),
		Host:       host,
		ReturnPath: returnPath,
		ExpiresAt:  expiresAt,
	}
	token, err := sealAEAD(secret, oauthStateAAD+host, claims)
	if err != nil {
		return nil, "", err
	}
	cookie := &http.Cookie{ //nolint:gosec // G124: Secure is true for https (__Host-) and only false when PublicScheme=http for local loopback tests
		Name:     oauthStateCookieName(secure),
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
		MaxAge:   int(oauthStateTTL.Seconds()),
	}
	return cookie, token, nil
}

func verifyOAuthState(secret []byte, host, cookieValue, stateParam string, now time.Time) (oauthStateClaims, error) {
	if cookieValue == "" || stateParam == "" {
		return oauthStateClaims{}, errors.New("missing oauth state")
	}
	if subtle.ConstantTimeCompare([]byte(cookieValue), []byte(stateParam)) != 1 {
		return oauthStateClaims{}, errors.New("oauth state parameter mismatch")
	}
	var claims oauthStateClaims
	if err := openAEAD(secret, oauthStateAAD+host, cookieValue, &claims); err != nil {
		return oauthStateClaims{}, err
	}
	if claims.Host != host || claims.Nonce == "" || !now.Before(claims.ExpiresAt) {
		return oauthStateClaims{}, errors.New("oauth state expired or host mismatch")
	}
	return claims, nil
}

func clearHostOnlyCookie(name string, secure bool) *http.Cookie {
	return &http.Cookie{ //nolint:gosec // G124: Secure matches configured PublicScheme so host-only cookie deletion succeeds on loopback http tests
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
	}
}

func extractSingleCookie(cookies []*http.Cookie, name string) (value string, present bool, ambiguous bool) {
	for _, c := range cookies {
		if c == nil || c.Name != name {
			continue
		}
		if present {
			return "", true, true
		}
		value = c.Value
		present = true
	}
	return value, present, false
}

func sealAEAD(secret []byte, aad string, payload any) (string, error) {
	gcm, err := newAESGCM(secret)
	if err != nil {
		return "", err
	}
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode cookie payload: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate cookie nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, []byte(aad))
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func openAEAD(secret []byte, aad, encoded string, out any) error {
	if encoded == "" || len(encoded) > maxCookieValueLen {
		return errors.New("invalid cookie length")
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("decode cookie: %w", err)
	}
	gcm, err := newAESGCM(secret)
	if err != nil {
		return err
	}
	nonceSize := gcm.NonceSize()
	if len(raw) <= nonceSize+gcm.Overhead() {
		return errors.New("truncated cookie ciphertext")
	}
	plaintext, err := gcm.Open(nil, raw[:nonceSize], raw[nonceSize:], []byte(aad))
	if err != nil {
		return fmt.Errorf("authenticate cookie: %w", err)
	}
	if err := json.Unmarshal(plaintext, out); err != nil {
		return fmt.Errorf("unmarshal cookie claims: %w", err)
	}
	return nil
}

func newAESGCM(secret []byte) (cipher.AEAD, error) {
	if len(secret) < MinCookieSecretBytes {
		return nil, fmt.Errorf("cookie secret must be at least %d bytes", MinCookieSecretBytes)
	}
	key := sha256.Sum256(secret)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	return cipher.NewGCM(block)
}
