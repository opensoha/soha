package virtualization

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func TestPVECapacityReusesScanAuthentication(t *testing.T) {
	var logins, reads atomic.Int32
	ticket := uuid.NewString()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api2/json/access/ticket" && r.Method == http.MethodPost {
			logins.Add(1)
			writePVEAny(w, map[string]any{"ticket": ticket, "CSRFPreventionToken": uuid.NewString()})
			return
		}
		cookie, err := r.Cookie("PVEAuthCookie")
		if r.Method != http.MethodGet || err != nil || cookie.Value != ticket {
			t.Error("capacity inventory must use authenticated reads")
			http.Error(w, "unexpected request", http.StatusForbidden)
			return
		}
		reads.Add(1)
		switch r.URL.Path {
		case "/api2/json/access/permissions":
			writePVEAny(w, map[string]any{"/": map[string]any{"Sys.Audit": 1, "VM.Audit": 1, "Datastore.Audit": 1}})
		case "/api2/json/cluster/resources":
			writePVEData(w, []map[string]any{{"type": "node", "node": "a", "status": "online", "cpu": 0.25, "maxcpu": 8, "maxmem": int64(16 << 30), "mem": int64(3 << 30)}})
		case "/api2/json/nodes/a/storage":
			writePVEData(w, []map[string]any{})
		default:
			t.Error("unexpected inventory read")
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	password := uuid.NewString()
	connection := Connection{Endpoint: server.URL, Credential: map[string]any{"username": "fixture@pam", "password": password}}
	snapshot, err := NewPVEAdapter(server.Client()).ObserveCapacity(context.Background(), connection, CreateVMInput{})
	if err != nil || !snapshot.Complete || len(snapshot.Nodes) != 1 {
		t.Fatalf("capacity inventory failed: %v", err)
	}
	if logins.Load() != 1 || reads.Load() != 3 {
		t.Fatalf("capacity scan logged in %d times for %d reads", logins.Load(), reads.Load())
	}
	if len(connection.Credential) != 2 || connection.Credential["password"] != password {
		t.Fatal("capacity scan changed the saved credential")
	}
}
