package storagedriver

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"regexp"
	"strconv"
	"strings"
)

const LaunchSecretBytes = 32

var (
	pluginIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*\.[a-z0-9][a-z0-9._-]*$`)
	versionPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_-]*$`)
	tokenEncoding   = base64.RawURLEncoding.Strict()
)

// Identity describes a running driver. The supervisor obtains its expected identity
// from the verified release, and the server obtains its actual identity from the driver.
type Identity struct {
	PluginID      string       `json:"pluginId"`
	Version       string       `json:"version"`
	ProtocolMajor int          `json:"protocolMajor"`
	Capabilities  Capabilities `json:"capabilities"`
}

func (i Identity) Validate() error {
	if len(i.PluginID) > 255 || !pluginIDPattern.MatchString(i.PluginID) ||
		len(i.Version) > 128 || !versionPattern.MatchString(i.Version) ||
		i.ProtocolMajor != ProtocolMajor || i.Capabilities != V1Capabilities {
		return RPCError{Code: CodeHandshakeMismatch}
	}
	return nil
}

type HandshakeRequest struct {
	Expected Identity `json:"expected"`
	Nonce    string   `json:"nonce"`
	Proof    string   `json:"proof"`
}

type HandshakeResponse struct {
	Actual Identity `json:"actual"`
	Proof  string   `json:"proof"`
}

func NewHandshakeRequest(launchSecret []byte, expected Identity) (HandshakeRequest, error) {
	if len(launchSecret) != LaunchSecretBytes {
		return HandshakeRequest{}, RPCError{Code: CodeUnauthenticated}
	}
	if err := expected.Validate(); err != nil {
		return HandshakeRequest{}, err
	}
	var nonce [LaunchSecretBytes]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return HandshakeRequest{}, err
	}
	nonceValue := tokenEncoding.EncodeToString(nonce[:])
	return HandshakeRequest{
		Expected: expected,
		Nonce:    nonceValue,
		Proof:    encodeProof(launchSecret, "host", expected, nonceValue),
	}, nil
}

// Accept checks the host proof and the actual driver identity, then returns the driver
// proof and the locally derived bearer token. The server must enforce one acceptance
// per process launch and keep the token out of the response.
func (r HandshakeRequest) Accept(launchSecret []byte, actual Identity) (HandshakeResponse, string, error) {
	if len(launchSecret) != LaunchSecretBytes || !validToken(r.Nonce) ||
		!verifyProof(launchSecret, "host", r.Expected, r.Nonce, r.Proof) {
		return HandshakeResponse{}, "", RPCError{Code: CodeUnauthenticated}
	}
	if r.Expected != actual || actual.Validate() != nil {
		return HandshakeResponse{}, "", RPCError{Code: CodeHandshakeMismatch}
	}
	response := HandshakeResponse{Actual: actual, Proof: encodeProof(launchSecret, "driver", actual, r.Nonce)}
	return response, encodeProof(launchSecret, "session", actual, r.Nonce), nil
}

// Verify authenticates the driver for the outstanding request and returns the
// locally derived bearer token. It must run before any binding is sent.
func (r HandshakeResponse) Verify(launchSecret []byte, request HandshakeRequest) (string, error) {
	if len(launchSecret) != LaunchSecretBytes || !validToken(request.Nonce) ||
		!verifyProof(launchSecret, "driver", r.Actual, request.Nonce, r.Proof) {
		return "", RPCError{Code: CodeUnauthenticated}
	}
	if r.Actual != request.Expected || r.Actual.Validate() != nil {
		return "", RPCError{Code: CodeHandshakeMismatch}
	}
	return encodeProof(launchSecret, "session", r.Actual, request.Nonce), nil
}

func validToken(token string) bool {
	if len(token) != tokenEncoding.EncodedLen(sha256.Size) {
		return false
	}
	raw, err := tokenEncoding.DecodeString(token)
	return err == nil && len(raw) == sha256.Size
}

func verifyProof(secret []byte, role string, identity Identity, nonce, proof string) bool {
	return validToken(proof) && hmac.Equal([]byte(proof), []byte(encodeProof(secret, role, identity, nonce)))
}

func encodeProof(secret []byte, role string, identity Identity, nonce string) string {
	input := strings.Join([]string{
		"soha-storage-driver/v1", role, identity.PluginID, identity.Version,
		strconv.Itoa(identity.ProtocolMajor), strconv.FormatUint(uint64(identity.Capabilities), 10), nonce,
	}, "\n")
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(input))
	return tokenEncoding.EncodeToString(mac.Sum(nil))
}
