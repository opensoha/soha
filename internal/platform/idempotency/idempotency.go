package idempotency

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
)

const (
	PayloadHashKey    = "idempotencyInputHash"
	ReceiptPayloadKey = "idempotencyReceipts"
)

var ErrInputMismatch = errors.New("Idempotency-Key is already bound to different input")

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

func LookupReceipt(payload map[string]any, scope, actor, key string, input any) (string, string, bool, error) {
	receiptID, inputHash, err := Derive(scope, actor, key, input)
	if err != nil {
		return "", "", false, err
	}
	storedHash := receiptHash(payload, receiptID)
	if storedHash == "" {
		return receiptID, inputHash, false, nil
	}
	if storedHash != inputHash {
		return receiptID, inputHash, false, ErrInputMismatch
	}
	return receiptID, inputHash, true, nil
}

func RecordReceipt(payload map[string]any, receiptID, inputHash string) {
	if payload == nil || strings.TrimSpace(receiptID) == "" || strings.TrimSpace(inputHash) == "" {
		return
	}
	receipts, _ := payload[ReceiptPayloadKey].(map[string]any)
	if receipts == nil {
		receipts = map[string]any{}
		payload[ReceiptPayloadKey] = receipts
	}
	receipts[receiptID] = inputHash
}

func receiptHash(payload map[string]any, receiptID string) string {
	if payload == nil {
		return ""
	}
	switch receipts := payload[ReceiptPayloadKey].(type) {
	case map[string]any:
		value, _ := receipts[receiptID].(string)
		return value
	case map[string]string:
		return receipts[receiptID]
	default:
		return ""
	}
}
