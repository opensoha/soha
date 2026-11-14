package radiusruntime

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManagedHookProofIsCurrentControlOnlyAndBootstrapDeniesNew(t *testing.T) {
	tracker, _ := fixture(t)
	hook, err := NewHTTP(tracker, 101, func(net.Conn) (uint32, error) { return 101, nil }, 4)
	if err != nil {
		t.Fatal(err)
	}
	supervisor := &Supervisor{tracker: tracker, leaseState: Lease{NASID: "nas-1", AcceptNew: false, RuntimeProof: `,"runtimeProof":{"controlRevision":1}`}}
	if err = supervisor.configureHook(hook); err != nil {
		t.Fatal(err)
	}
	handler := hook
	invoke := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/admit", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), peerKey{}, true))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	body := `{"epoch":"child-1","nasId":"nas-1","stationId":"02:00:00:00:00:01","state":""}`
	if response := invoke(body); response.Code != 403 {
		t.Fatal("bootstrap accepted new auth", response.Code)
	}
	supervisor.leaseState.AcceptNew = true
	supervisor.leaseState.RuntimeProof = `,"runtimeProof":{"controlRevision":2}`
	response := invoke(body)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var attrs map[string]struct {
		Value []string `json:"value"`
		Xlat  bool     `json:"do_xlat"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &attrs); err != nil {
		t.Fatal(err)
	}
	if len(attrs) != 2 || len(attrs["control:Tmp-String-9"].Value) != 1 || attrs["control:Tmp-String-9"].Value[0] != `,"runtimeProof":{"controlRevision":2}` || attrs["control:Tmp-String-9"].Xlat {
		t.Fatal("unsafe/stale proof", response.Body.String())
	}
	if !supervisor.observedNASUse {
		t.Fatal("NAS request not observed")
	}
	if _, exists := attrs["reply:Tmp-String-9"]; exists {
		t.Fatal("proof exposed to NAS")
	}
	if response = invoke(strings.Replace(body, `"nas-1"`, `"foreign"`, 1)); response.Code != http.StatusForbidden {
		t.Fatal("foreign NAS accepted")
	}
}
