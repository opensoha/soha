package idempotency

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

const PayloadHashKey = "idempotencyInputHash"

func Derive(scope, actor, key string, input any) (string, string, error) {
	payload, err := json.Marshal(input)
	if err != nil {
		return "", "", err
	}
	hash := sha256.Sum256(payload)
	id := uuid.NewSHA1(uuid.NameSpaceURL, []byte(strings.TrimSpace(scope)+"\x00"+strings.TrimSpace(actor)+"\x00"+strings.TrimSpace(key)))
	return id.String(), hex.EncodeToString(hash[:]), nil
}

func Matches(payload map[string]any, inputHash string) bool {
	value, _ := payload[PayloadHashKey].(string)
	return value != "" && value == inputHash
}
