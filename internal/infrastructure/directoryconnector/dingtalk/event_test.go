package dingtalk

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"testing"
	"time"
)

func TestVerifyEventSignature(t *testing.T) {
	now := time.Now().UTC()
	timestamp := strconv.FormatInt(now.UnixMilli(), 10)
	mac := hmac.New(sha256.New, []byte("secret"))
	_, _ = mac.Write([]byte(timestamp))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if err := VerifyEventSignature(timestamp, "secret", signature, now, time.Minute); err != nil {
		t.Fatal(err)
	}
	if VerifyEventSignature(timestamp, "secret", "bad", now, time.Minute) == nil {
		t.Fatal("forged signature accepted")
	}
}
