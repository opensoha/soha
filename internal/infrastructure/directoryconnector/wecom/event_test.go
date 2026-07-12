package wecom

import (
	"crypto/sha1"
	"encoding/hex"
	"sort"
	"strings"
	"testing"
)

func TestVerifyEventSignature(t *testing.T) {
	parts := []string{"token", "1700000000", "nonce", "cipher"}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	signature := hex.EncodeToString(sum[:])
	if err := VerifyEventSignature("token", "1700000000", "nonce", "cipher", signature); err != nil {
		t.Fatal(err)
	}
	if VerifyEventSignature("token", "1700000000", "nonce", "cipher", "bad") == nil {
		t.Fatal("forged signature accepted")
	}
}
