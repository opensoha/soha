package agent

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	contractresource "github.com/opensoha/soha/contracts/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestAgentPresenceRejectsMissingProtocolTargetAndAmbiguous404(t *testing.T) {
	for _, scenario := range []string{"absent", "present", "missing-state", "old-protocol", "wrong-gvr", "wrong-name", "missing-version", "text404", "json404", "text405", "json405"} {
		t.Run(scenario, func(t *testing.T) { agentPresenceRejectsMissingProtocolTargetAndAmbiguous404Phase1(t, &scenario) })
	}
}

func agentPresenceRejectsMissingProtocolTargetAndAmbiguous404Phase1(t *testing.T, sharedScenario *string) {

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveResourcePresenceFixture(t, *sharedScenario, &calls, w, r)
	}))
	defer server.Close()
	client := &Client{baseURL: server.URL, token: "fixture", httpClient: server.Client()}
	result, err := client.ObserveResourcePresence(t.Context(), "team-a", "ConfigMap", "app")
	valid := (*sharedScenario) == "absent" || (*sharedScenario) == "present"
	if calls != 1 || (err == nil) != valid || valid && result.State != (*sharedScenario) {
		t.Fatal("unsupported/malformed presence became absence or fell back", err)
	}
	if ((*sharedScenario) == "text404" || (*sharedScenario) == "json404" || (*sharedScenario) == "text405" || (*sharedScenario) == "json405") && !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatal("old Agent was not explicitly unsupported", err)
	}
}

func serveResourcePresenceFixture(t *testing.T, scenario string, calls *int, w http.ResponseWriter, r *http.Request) {
	(*calls)++
	if r.Method != "GET" || r.URL.Path != resourcePresencePath || r.URL.Query().Get("namespace") != "team-a" || r.URL.Query().Get("name") != "app" || r.Header.Get("Authorization") != "Bearer fixture" {
		t.Error("presence requested a legacy, unbound or unauthenticated path")
	}
	if scenario == "text404" || scenario == "text405" {
		status := 404
		if scenario == "text405" {
			status = 405
		}
		http.Error(w, "old Agent", status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if scenario == "json404" || scenario == "json405" {
		status := 404
		if scenario == "json405" {
			status = 405
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found"}}`))
		return
	}
	item := contractresource.ResourcePresence{Protocol: contractresource.ResourcePresenceProtocol, APIVersion: "v1", Resource: "configmaps", Kind: "ConfigMap", Namespace: "team-a", Name: "app", CollectionResourceVersion: "opaque", State: "absent"}
	switch scenario {
	case "present":
		item.State, item.UID, item.ResourceVersion = "present", "uid", "object-rv"
	case "missing-state":
		item.State = ""
	case "old-protocol":
		item.Protocol = "preconditions-v1"
	case "wrong-gvr":
		item.Resource = "secrets"
	case "wrong-name":
		item.Name = "other"
	case "missing-version":
		item.CollectionResourceVersion = ""
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": item})
}
