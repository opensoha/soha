package networkcontrol

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type radiusPasswordStub struct {
	calls  int
	err    error
	source string
}

func (s *radiusPasswordStub) VerifyNASPassword(_ context.Context, _ networkidentity.Identity, source string, _ []byte) (networkprotocol.RadiusPasswordResult, error) {
	s.calls++
	s.source = source
	return networkprotocol.RadiusPasswordResult{SubjectID: "user-1", AuthenticationReceipt: strings.Repeat("A", 43)}, s.err
}

func TestRadiusPasswordWireAndRejections(t *testing.T) {
	for _, scenario := range []string{"success", "wrong-password", "limited", "unavailable", "no-certificate", "wrong-kind", "wrong-path", "wrong-source", "forwarded-source", "oversized", "compressed"} {
		t.Run(scenario, func(t *testing.T) {
			verifier := &radiusPasswordStub{}
			expected := http.StatusOK
			switch scenario {
			case "wrong-password":
				verifier.err = apperrors.ErrUnauthorized
				expected = 401
			case "limited":
				verifier.err = apperrors.ErrConflict
				expected = 429
			case "unavailable":
				verifier.err = apperrors.ErrServiceUnavailable
				expected = 503
			case "no-certificate", "wrong-kind", "wrong-path", "wrong-source", "forwarded-source", "oversized", "compressed":
				expected = 401
			}
			router, err := NewRouter(&controlServiceStub{}, controlReadyStoreStub{}, Options{MaxBodyBytes: 8192, RequestsPerMinute: 100, PasswordRuntime: verifier, PasswordAllowedPeers: []string{"192.0.2.0/24"}})
			if err != nil {
				t.Fatal(err)
			}
			kind, pathID := "nas", "radius-1"
			if scenario == "wrong-kind" {
				kind = "endpoint"
			}
			if scenario == "wrong-path" {
				pathID = "other"
			}
			body := `{"passwordBase64":"c3ludGhldGljLXNlY3JldA=="}`
			if scenario == "oversized" {
				body = strings.Repeat("x", 4097)
			}
			request := authenticatedRuntimeRequest(t, "POST", "/api/network-control/v1/runtimes/"+pathID+"/radius:verify-password", body, kind, "radius-1")
			request.RemoteAddr = "192.0.2.2:12345"
			if scenario == "no-certificate" {
				request.TLS = nil
			}
			if scenario == "wrong-source" || scenario == "forwarded-source" {
				request.RemoteAddr = "198.51.100.1:12345"
				request.Header.Set("X-Forwarded-For", "192.0.2.2")
			}
			if scenario == "compressed" {
				request.Header.Set("Content-Encoding", "gzip")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != expected {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if expected != 200 {
				if response.Body.Len() != 0 {
					t.Fatal("failure body must be empty")
				}
				return
			}
			assertRadiusPasswordAttributes(t, response, verifier)
		})
	}
}

func assertRadiusPasswordAttributes(t *testing.T, response *httptest.ResponseRecorder, verifier *radiusPasswordStub) {
	t.Helper()
	var attributes map[string]radiusReplyAttribute
	if err := json.Unmarshal(response.Body.Bytes(), &attributes); err != nil {
		t.Fatal(err)
	}
	if len(attributes) != 2 || attributes["control:Tmp-String-4"].DoXLAT || attributes["control:Tmp-String-5"].Value[0] != "user-1" || verifier.source != "192.0.2.2" {
		t.Fatalf("unsafe RADIUS response: %+v", attributes)
	}
}
