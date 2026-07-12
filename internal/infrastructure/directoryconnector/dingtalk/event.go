package dingtalk

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strconv"
	"time"
)

func VerifyEventSignature(timestamp, secret, signature string, now time.Time, replayWindow time.Duration) error {
	milliseconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errors.New("invalid DingTalk timestamp")
	}
	if replayWindow > 0 {
		delta := now.Sub(time.UnixMilli(milliseconds))
		if delta < 0 {
			delta = -delta
		}
		if delta > replayWindow {
			return errors.New("DingTalk event outside replay window")
		}
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) != 1 {
		return errors.New("invalid DingTalk event signature")
	}
	return nil
}
