package proxytransport

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

// MaxUsernameBytes bounds HTTP Basic usernames, including opaque mapped IDs.
// Two maximum-length identifiers encode to fewer than 360 bytes.
const MaxUsernameBytes = 512

var mappedIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// EncodeMappedUsername encodes exactly [credentialID,nodeID] as canonical JSON
// with unpadded URL-safe base64. Unlike the IDs, the result never contains ':',
// which is reserved as HTTP Basic's username/password separator.
func EncodeMappedUsername(credentialID, nodeID string) (string, error) {
	if !mappedIdentifier.MatchString(credentialID) || !mappedIdentifier.MatchString(nodeID) {
		return "", errors.New("invalid mapped proxy identifier")
	}
	data, err := json.Marshal([2]string{credentialID, nodeID})
	if err != nil {
		return "", err
	}
	return "v1." + base64.RawURLEncoding.EncodeToString(data), nil
}

// DecodeMappedUsername accepts only the canonical v1 encoding. It rejects
// alternate JSON/base64 spellings so callers share one unambiguous identity key.
func DecodeMappedUsername(username string) (credentialID, nodeID string, err error) {
	invalid := errors.New("invalid mapped proxy username")
	if len(username) > MaxUsernameBytes || !strings.HasPrefix(username, "v1.") {
		return "", "", invalid
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(username[len("v1."):])
	if err != nil {
		return "", "", invalid
	}
	var identifiers []string
	if json.Unmarshal(data, &identifiers) != nil || len(identifiers) != 2 {
		return "", "", invalid
	}
	canonical, err := EncodeMappedUsername(identifiers[0], identifiers[1])
	if err != nil || canonical != username {
		return "", "", invalid
	}
	return identifiers[0], identifiers[1], nil
}
