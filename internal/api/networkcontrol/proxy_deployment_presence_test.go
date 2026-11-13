package networkcontrol

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/networkprotocol"
)

func TestProxyDeploymentStopProofRequiresExplicitKnownZero(t *testing.T) {
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		edit   func(map[string]any)
		status int
	}{
		{"stopped", func(map[string]any) {}, 200},
		{"missing count", func(v map[string]any) { delete(v, "activeConnections") }, 400},
		{"null count", func(v map[string]any) { v["activeConnections"] = nil }, 400},
		{"unknown count", func(v map[string]any) { v["activeCountKnown"] = false }, 400},
		{"still active", func(v map[string]any) { v["activeConnections"] = 1 }, 400},
		{"accepting", func(v map[string]any) { v["acceptingRequests"] = true }, 400},
		{"missing accepting", func(v map[string]any) { delete(v, "acceptingRequests") }, 400},
		{"null observed", func(v map[string]any) { v["observedAt"] = nil }, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			ack := api.NetworkProxyDeploymentRuntimeAck{RuntimeID: "p1", InstanceID: "pool-0", ProcessEpoch: "boot", CredentialID: "cred", CredentialEpoch: 1, OwnerEpoch: 1, ControlRevision: 3,
				ObservedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Second), ProcessStopped: true, ActiveCountKnown: true, ActiveConnections: 0}
			body, err := json.Marshal(ack)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(body, &fields); err != nil {
				t.Fatal(err)
			}
			test.edit(fields)
			body, _ = json.Marshal(fields)
			stub := &deploymentStub{}
			router, err := NewRouter(&controlServiceStub{}, controlReadyStoreStub{}, Options{MaxBodyBytes: 262144, RequestsPerMinute: 100, ProxySchemas: schemas, ProxyDeployment: stub})
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, authenticatedRuntimeRequest(t, http.MethodPost, "/api/network-control/v1/proxy-runtimes/p1/deployment/control/receipt", string(body), "proxy", "p1"))
			if rec.Code != test.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
			}
			if (stub.acks > 0) != (test.status == 200) {
				t.Fatal("invalid stop proof reached application")
			}
		})
	}
}
