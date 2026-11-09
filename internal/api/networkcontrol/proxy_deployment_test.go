package networkcontrol

import (
	"context"
	"encoding/json"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type deploymentStub struct {
	calls, acks     int
	instance, epoch string
	control         api.NetworkProxyDeploymentRuntimeControl
}

func (s *deploymentStub) RuntimeControl(_ context.Context, _ networkidentity.Identity, instance, epoch string) (api.NetworkProxyDeploymentRuntimeControl, error) {
	s.calls++
	s.instance = instance
	s.epoch = epoch
	return s.control, nil
}
func (s *deploymentStub) RuntimeACK(_ context.Context, _ networkidentity.Identity, _ api.NetworkProxyDeploymentRuntimeAck) error {
	s.acks++
	return nil
}
func TestProxyDeploymentRuntimeTypedRoutesRequireExactMTLSAndPreserveDistinctMember(t *testing.T) {
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	stub := &deploymentStub{control: api.NetworkProxyDeploymentRuntimeControl{InstanceID: "pool-0", RuntimeID: "p1", ProcessEpoch: "boot", ControlRevision: 2, OwnerEpoch: 1, CredentialID: "cred", CredentialEpoch: 1, IssuedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Second)}}
	router, err := NewRouter(&controlServiceStub{}, controlReadyStoreStub{}, Options{MaxBodyBytes: 262144, RequestsPerMinute: 100, ProxySchemas: schemas, ProxyDeployment: stub})
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/network-control/v1/proxy-runtimes/p1/deployment/control?instanceId=pool-0&processEpoch=boot"
	for _, tc := range []struct {
		name, kind, id, path string
		status               int
	}{{"valid", "proxy", "p1", path, 200}, {"foreign", "proxy", "other", path, 401}, {"endpoint", "endpoint", "p1", path, 403}, {"duplicate query", "proxy", "p1", path + "&instanceId=other", 400}} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, authenticatedRuntimeRequest(t, http.MethodGet, tc.path, "", tc.kind, tc.id))
			if rec.Code != tc.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
			}
			if tc.name == "valid" {
				var control api.NetworkProxyDeploymentRuntimeControl
				if err := json.Unmarshal(rec.Body.Bytes(), &control); err != nil || control.InstanceID != "pool-0" || control.RuntimeID != "p1" || control.ControlRevision != 2 || rec.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("wrong raw DTO: %s", rec.Body)
				}
			}
		})
	}
	if stub.calls != 1 || stub.instance != "pool-0" || stub.epoch != "boot" {
		t.Fatal("untrusted request reached deployment service")
	}
	ack := api.NetworkProxyDeploymentRuntimeAck{InstanceID: "pool-0", RuntimeID: "p1", ProcessEpoch: "boot", ControlRevision: 2, CredentialID: "cred", CredentialEpoch: 1, OwnerEpoch: 1, ExpiresAt: stub.control.ExpiresAt, ObservedAt: time.Now().UTC(), ActiveCountKnown: false}
	raw, _ := json.Marshal(ack)
	for _, trailing := range []bool{false, true} {
		body := string(raw)
		if trailing {
			body += " {}"
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, authenticatedRuntimeRequest(t, http.MethodPost, "/api/network-control/v1/proxy-runtimes/p1/deployment/control/receipt", body, "proxy", "p1"))
		want := 200
		if trailing {
			want = 400
		}
		if rec.Code != want {
			t.Fatalf("typed receipt status=%d body=%s", rec.Code, rec.Body)
		}
	}
	if stub.acks != 1 {
		t.Fatal("trailing JSON reached ACK service")
	}
}
