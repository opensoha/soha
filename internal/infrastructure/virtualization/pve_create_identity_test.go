package virtualization

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPVEPreparationSkipsClaimedAndOccupiedIDsWithoutWrites(t *testing.T) {
	for _, test := range []struct {
		name      string
		rejected  []string
		fixed     string
		suggested string
		want      string
		readError bool
	}{
		{name: "first advisory identity", suggested: "701", want: "701"},
		{name: "claimed and provider occupied", suggested: "701", rejected: []string{"701"}, want: "703"},
		{name: "several claims", suggested: "701", rejected: []string{"701", "703"}, want: "704"},
		{name: "fixed identity stays frozen", fixed: "701", rejected: []string{"701"}, want: "701"},
		{name: "invalid suggestion", suggested: "invalid", rejected: []string{"701"}},
		{name: "bounded identity range", suggested: "999999999", rejected: []string{"999999999"}},
		{name: "inventory failure is not empty", suggested: "701", rejected: []string{"701"}, readError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reads := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("preparation wrote to provider: %s", r.Method)
				}
				reads++
				switch r.URL.Path {
				case "/api2/json/cluster/nextid":
					writePVEAny(w, test.suggested)
				case "/api2/json/cluster/resources":
					if r.URL.Query().Get("type") != "vm" {
						t.Error("inventory was not scoped to VMs")
					}
					if test.readError {
						w.WriteHeader(http.StatusForbidden)
						return
					}
					writePVEData(w, []map[string]any{{"vmid": 702}})
				default:
					t.Errorf("unexpected provider read: %s", r.URL.Path)
				}
			}))
			defer server.Close()
			params := map[string]any{}
			if test.fixed != "" {
				params["vmid"] = test.fixed
			}
			input, err := NewPVEAdapter(server.Client()).PrepareVMCreate(context.Background(), Connection{Endpoint: server.URL}, CreateVMInput{
				Name: "disposable", Node: "node", ProviderParams: params, RejectedProviderIDs: test.rejected,
			})
			if test.want == "" {
				if err == nil {
					t.Fatalf("unconfirmed identity accepted: %+v", input)
				}
				return
			}
			if err != nil || input.ProviderParams["vmid"] != test.want {
				t.Fatalf("identity=%v err=%v; want %s", input.ProviderParams["vmid"], err, test.want)
			}
			if test.fixed != "" && reads != 0 {
				t.Fatalf("fixed identity triggered allocation: %d reads", reads)
			}
		})
	}
}
