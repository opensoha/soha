package networkcontrol

import (
	"context"
	"encoding/json"
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type radiusControlStub struct{ calls, acks int }

func (s *radiusControlStub) RuntimeControl(_ context.Context, id networkidentity.Identity, member, epoch string) (api.NetworkRADIUSRuntimeControl, error) {
	s.calls++
	return api.NetworkRADIUSRuntimeControl{RuntimeID: id.ID, MemberID: member, ProcessEpoch: epoch}, nil
}
func (s *radiusControlStub) RuntimeACK(context.Context, networkidentity.Identity, api.NetworkRADIUSRuntimeACK) error {
	s.acks++
	return nil
}
func TestRadiusRuntimeTransportIdentityAndExplicitKnownZero(t *testing.T) {
	schemas, e := networkprotocol.CompileSchemas()
	if e != nil {
		t.Fatal(e)
	}
	stub := &radiusControlStub{}
	r, e := NewRouter(&controlServiceStub{}, controlReadyStoreStub{}, Options{MaxBodyBytes: 262144, RequestsPerMinute: 100, ProxySchemas: schemas, RadiusDeployment: stub})
	if e != nil {
		t.Fatal(e)
	}
	path := "/api/network-control/v1/radius-runtimes/radius-member:a/control?memberId=radius-0&processEpoch=epoch"
	for _, tc := range []struct {
		kind, id, path string
		want           int
	}{{"nas", "radius-member:a", path, 200}, {"nas", "other", path, 401}, {"endpoint", "radius-member:a", path, 401}, {"nas", "radius-member:a", path + "&memberId=other", 400}} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, authenticatedRuntimeRequest(t, http.MethodGet, tc.path, "", tc.kind, tc.id))
		if rec.Code != tc.want {
			t.Fatalf("kind=%s status=%d body=%s", tc.kind, rec.Code, rec.Body)
		}
	}
	zero := 0
	ack := api.NetworkRADIUSRuntimeACK{PoolID: "pool", MemberID: "radius-0", RuntimeID: "radius-member:a", CredentialID: "credential", ProcessEpoch: "epoch", OwnerEpoch: 1, ControlRevision: 1, ConfigurationRevision: 1, ConfigurationHash: "sha256:" + strings.Repeat("a", 64), StateCountKnown: true, ActiveStates: &zero, ProcessStopped: true, RuntimeVersion: "test", ObservedAt: time.Now().UTC()}
	raw, _ := json.Marshal(ack)
	for _, body := range []string{string(raw), strings.Replace(string(raw), `"activeStates":0,`, "", 1), strings.Replace(string(raw), `"activeStates":0`, `"activeStates":null`, 1), string(raw) + " {}"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, authenticatedRuntimeRequest(t, http.MethodPost, "/api/network-control/v1/radius-runtimes/radius-member:a/control/receipt", body, "nas", "radius-member:a"))
		want := 400
		if body == string(raw) {
			want = 200
		}
		if rec.Code != want {
			t.Fatalf("receipt status=%d want=%d body=%s", rec.Code, want, rec.Body)
		}
	}
	if stub.calls != 1 || stub.acks != 1 {
		t.Fatalf("untrusted or malformed input reached service: %d %d", stub.calls, stub.acks)
	}
}
