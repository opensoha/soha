package sohacli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const mfaTestID = "c04ab832-2a23-4da9-bce8-26dd7aab5734"

func TestMFATOTPOnlyExplicitlyExportsEnrollment(t *testing.T) {
	uri := "otpauth://totp/Soha:owned?secret=OWNEDSECRET&issuer=Soha"
	for _, show := range []bool{false, true} {
		t.Run(fmt.Sprint(show), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/api/v1/identity/mfa/totp/enroll" {
					t.Error("wrong MFA path")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"data":{"challengeId":%q,"type":"totp_enrollment","provisioningUri":%q,"expiresAt":"2027-01-01T00:00:00Z"}}`, mfaTestID, uri)
			}))
			defer server.Close()
			args := []string{"auth", "mfa", "totp"}
			if show {
				args = append(args, "--show-enrollment")
			}
			var out, stderr bytes.Buffer
			code := Run(t.Context(), args, Runtime{In: strings.NewReader(""), Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
			var got struct {
				Data map[string]any `json:"data"`
			}
			_ = json.Unmarshal(out.Bytes(), &got)
			exported, _ := got.Data["provisioningUri"].(string)
			if code != 0 || calls.Load() != 1 || strings.Contains(stderr.String(), uri) || (exported == uri) != show || !strings.Contains(out.String(), mfaTestID) {
				t.Fatalf("MFA show=%t code=%d calls=%d out=%s err=%s", show, code, calls.Load(), out.String(), stderr.String())
			}
		})
	}
}

func TestMFAVerificationUsesStdinAndNeverEchoesTheResponse(t *testing.T) {
	const secret = "654321"
	for _, status := range []int{200, 401, 403, 404, 409} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var input map[string]string
				_ = json.NewDecoder(r.Body).Decode(&input)
				if r.Method != http.MethodPost || r.URL.Path != "/api/v1/identity/mfa/challenges/"+mfaTestID+"/verify" || input["response"] != secret {
					t.Error("MFA response binding changed")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				if status == 200 {
					_, _ = fmt.Fprint(w, `{"data":{"verified":true,"authenticationMethods":["mfa","totp"],"authenticatedAt":"2026-10-29T00:00:00Z"}}`)
				} else {
					_, _ = fmt.Fprint(w, `{"error":{"code":"denied","message":"`+secret+`"}}`)
				}
			}))
			defer server.Close()
			var out, stderr bytes.Buffer
			code := Run(t.Context(), []string{"auth", "mfa", "verify", "--challenge-id", mfaTestID, "--response-stdin"}, Runtime{In: strings.NewReader(secret + "\n"), Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
			if calls.Load() != 1 || strings.Contains(out.String()+stderr.String(), secret) || status == 200 && code != 0 || status != 200 && (code == 0 || out.Len() != 0) {
				t.Fatalf("MFA status=%d code=%d calls=%d out=%s err=%s", status, code, calls.Load(), out.String(), stderr.String())
			}
		})
	}
}

func TestMFAUnsafeArgumentsNeverCallServer(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	for _, args := range [][]string{{"verify", "--challenge-id", mfaTestID}, {"verify", "--challenge-id", "../challenge", "--response-stdin"}, {"verify", "123456", "--challenge-id", mfaTestID, "--response-stdin"}, {"status", "--show-enrollment"}, {"totp", "--response-stdin"}, {"verify", "--challenge-id", mfaTestID, "--response-stdin", "--show-enrollment"}} {
		var out, stderr bytes.Buffer
		code := Run(t.Context(), append([]string{"auth", "mfa"}, args...), Runtime{In: strings.NewReader("123456\n"), Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
		if code == 0 || calls.Load() != 0 {
			t.Fatalf("unsafe MFA args=%v code=%d calls=%d", args, code, calls.Load())
		}
	}
	for _, input := range []string{"", strings.Repeat("1", 131), "123\n456"} {
		if _, err := readMFAResponse(strings.NewReader(input)); err == nil {
			t.Fatal("invalid response accepted")
		}
	}
}

func TestNetworkCommandLookupRejectsUnboundReceipt(t *testing.T) {
	for _, body := range []string{`{"id":"other","sessionId":"session:1","status":"pending"}`, `{"id":"command:1","sessionId":"other-session","status":"pending"}`, `{"id":"command:1","sessionId":"session:1","status":"invented"}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"data":`+body+`}`)
		}))
		var out, stderr bytes.Buffer
		code := Run(t.Context(), []string{"network-access", "sessions", "command", "command:1", "--session-id", "session:1"}, Runtime{In: strings.NewReader(""), Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
		server.Close()
		if code == 0 || out.Len() != 0 || !strings.Contains(stderr.String(), "unconfirmed network command") {
			t.Fatalf("unbound code=%d out=%s err=%s", code, out.String(), stderr.String())
		}
	}
}
