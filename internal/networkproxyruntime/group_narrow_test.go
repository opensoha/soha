package networkproxyruntime

import (
	"context"
	"encoding/json"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"
)

func TestNarrowedGroupReadbackAllowsExplicitSurvivorSelection(t *testing.T) {
	selected := "node-a"
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPut {
			var input map[string]string
			if json.NewDecoder(request.Body).Decode(&input) != nil {
				t.Error("invalid selection")
				return
			}
			selected = input["name"]
			writes++
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"now": selected, "all": []string{"node-a", "node-b"}})
	}))
	defer server.Close()
	original := api.NetworkProxyGroup{GroupID: "choice", Type: "select", NodeIDs: []string{"node-a", "node-b"}}
	current := original
	current.NodeIDs = []string{"node-b"}
	engine := newEngine(Config{Engine: domain.EngineMihomo, ControllerURL: server.URL}, "controller-secret")
	engine.desired.Publication = &api.NetworkProxyServerPublication{Groups: []api.NetworkProxyGroup{current}}
	runner := &Runner{engine: engine, ingress: &publicationIngress{publication: api.NetworkProxyServerPublication{Groups: []api.NetworkProxyGroup{original}}}}
	groups, err := runner.readGroups(context.Background())
	if err != nil || len(groups) != 1 || groups[0].SelectedNodeID != "" || !slices.Equal(groups[0].NodeIDs, []string{"node-b"}) || selected != "node-a" || writes != 0 {
		t.Fatalf("readback changed native selection or hid survivor: %v", err)
	}
	command := api.NetworkProxyRuntimeCommandInput{GroupID: "choice", NodeID: "node-a", ExpiresAt: time.Now().Add(time.Minute)}
	if _, status, _ := runner.selectNode(context.Background(), command); status != "unsupported" || writes != 0 {
		t.Fatal("revoked selection reached native controller")
	}
	command.NodeID = "node-b"
	result, status, reason := runner.selectNode(context.Background(), command)
	if status != "succeeded" || result.SelectedNodeID != "node-b" || writes != 1 {
		t.Fatalf("survivor selection failed: %s %s", status, reason)
	}
}
func TestNarrowedGroupReadbackRejectsNativeDriftAndWidening(t *testing.T) {
	original := api.NetworkProxyGroup{GroupID: "choice", Type: "select", NodeIDs: []string{"a", "b"}}
	cases := []struct {
		name          string
		nodes, native []string
		selected      string
	}{
		{"added node", []string{"b", "c"}, []string{"a", "b"}, "a"},
		{"native removed node", []string{"b"}, []string{"b"}, "b"},
		{"native added node", []string{"b"}, []string{"a", "b", "c"}, "b"},
		{"unknown selection", []string{"b"}, []string{"a", "b"}, "c"},
		{"duplicate", []string{"b", "b"}, []string{"a", "b"}, "b"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			current := original
			current.NodeIDs = tt.nodes
			if _, err := narrowedGroupReadback(current, original, tt.selected, tt.native); err == nil {
				t.Fatal("unsafe readback accepted")
			}
		})
	}
	runner := &Runner{engine: &engine{desired: domain.Configuration{Publication: &api.NetworkProxyServerPublication{}}}}
	if _, err := runner.readGroups(context.Background()); err == nil {
		t.Fatal("missing installed inventory accepted")
	}
}
