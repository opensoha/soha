package runner

import (
	"encoding/json"
	"errors"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
	"go.uber.org/zap"
	"net/http"
	"slices"
	"sync"
	"testing"
)

func TestManifestClaimRequiresThisResponseConfirmation(t *testing.T) {
	for _, mode := range []string{"current", "old-core", "wrong-version", "duplicate", "forged-payload", "legacy", "wrong-kind", "old-plan", "old-plan-with-header", "old-plan-observe", "old-initialization", "standalone-sync"} {
		t.Run(mode, func(t *testing.T) {
			task := ExecutionTask{ID: "task", TaskKind: "manifest_preflight", ProviderKind: manifestAdmissionProviderPrefix + resourceruntime.ManifestAgentProviderPrefix + "one"}
			if mode == "legacy" {
				task.ProviderKind = resourceruntime.ManifestAgentProviderPrefix + "one"
			}
			switch mode {
			case "old-plan", "old-plan-with-header":
				task.ProviderKind = resourceruntime.ManifestAgentProviderPrefix + "one"
				task.Payload = map[string]any{"fieldManager": "opensoha-manifest/binding", "action": "apply"}
			case "old-plan-observe":
				task.ProviderKind = resourceruntime.RootCRAgentProviderPrefix + "one"
				task.TaskKind = "manifest_observe"
				task.Payload = map[string]any{"fieldManager": "opensoha-manifest/binding", "action": "observe", "rootResource": map[string]any{"uid": "root"}}
			case "old-initialization":
				task.ProviderKind = resourceruntime.ManifestInitializationProviderPrefix + "one"
				task.Payload = map[string]any{"initializationPlanId": "first", "action": "preflight"}
			case "standalone-sync":
				task.ProviderKind = resourceruntime.ManifestAgentProviderPrefix + "one"
				task.TaskKind = "manifest_sync"
				task.Payload = map[string]any{"fieldManager": "opensoha-delivery/v1", "action": "sync"}
			}
			if mode == "wrong-kind" {
				task.TaskKind = "release"
			}
			if mode == "forged-payload" {
				task.Payload = map[string]any{"manifestAdmissionValidated": true, "admissionVersion": "1"}
			}
			transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.Header.Get("Authorization") != "Bearer cluster-token" {
					t.Fatal("claim credential changed")
				}
				response := jsonResponse(t, http.StatusAccepted, map[string]any{"data": task})
				switch mode {
				case "current", "wrong-kind", "old-plan-with-header":
					response.Header.Set(manifestAdmissionHeader, "1")
				case "wrong-version":
					response.Header.Set(manifestAdmissionHeader, "2")
				case "duplicate":
					response.Header.Add(manifestAdmissionHeader, "1")
					response.Header.Add(manifestAdmissionHeader, "1")
				}
				return response, nil
			})
			client := sohaapi.NewClient("http://core", sohaapi.WithBearerToken("cluster-token"), sohaapi.WithHTTPClient(&http.Client{Transport: transport}))
			result, err := claimExecutionWithManifestAdmission(t.Context(), client, claimRequest{AgentID: "agent"})
			valid := mode == "current" || mode == "legacy" || mode == "standalone-sync"
			if result.ID != task.ID || valid && err != nil || !valid && !errors.Is(err, errManifestAdmission) {
				t.Fatalf("mode=%s task=%q err=%v", mode, result.ID, err)
			}
		})
	}
}

func TestManifestAdmissionDoesNotLeakBetweenConcurrentClaims(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var input claimRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			return nil, err
		}
		task := ExecutionTask{ID: input.AgentID, TaskKind: "manifest_preflight", ProviderKind: manifestAdmissionProviderPrefix + resourceruntime.ManifestAgentProviderPrefix + "one"}
		response := jsonResponse(t, http.StatusAccepted, map[string]any{"data": task})
		if input.AgentID == "confirmed" {
			response.Header.Set(manifestAdmissionHeader, "1")
		}
		return response, nil
	})
	client := sohaapi.NewClient("http://core", sohaapi.WithHTTPClient(&http.Client{Transport: transport}))
	var group sync.WaitGroup
	for range 10 {
		for _, id := range []string{"confirmed", "unconfirmed"} {
			group.Go(func() {
				_, err := claimExecutionWithManifestAdmission(t.Context(), client, claimRequest{AgentID: id})
				if (err == nil) != (id == "confirmed") {
					t.Errorf("claim %s inherited another confirmation", id)
				}
			})
		}
	}
	group.Wait()
	if _, ok := client.HTTPClient.Transport.(roundTripFunc); !ok {
		t.Fatal("shared client transport was mutated")
	}
}

func TestProtectedManifestClaimUsesClusterTokenAndFailsWithoutAdmission(t *testing.T) {
	r := New(cfgpkg.ControlPlaneConfig{BaseURL: "http://core", BearerToken: "general-token", CallbackRetry: cfgpkg.CallbackRetryConfig{MaxAttempts: 1}}, zap.NewNop())
	r.SetManifestExecutor(blockingManifestExecutor{}, "one")
	r.SetHelmExecutor(nil, "one", "cluster-token")
	// SetHelmExecutor's executor is only a gate here; no Helm operation executes.
	r.helmExecutor = heldHelmExecutor{}
	statuses := []string{}
	claims := 0
	r.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/delivery/execution-tasks/claim" || req.URL.Path == "/api/v1/delivery/execution-tasks/claim" {
			var input claimRequest
			if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
				return nil, err
			}
			claims++
			for _, provider := range protectedManifestProviders("one") {
				if !slices.Contains(input.ProviderKinds, provider) {
					t.Fatalf("protected provider missing: %s", provider)
				}
			}
			if req.Header.Get("Authorization") != "Bearer cluster-token" {
				t.Fatal("protected claim did not use cluster credential")
			}
			return jsonResponse(t, http.StatusAccepted, map[string]any{"data": ExecutionTask{ID: "task", CallbackToken: "callback", TaskKind: "manifest_preflight", ProviderKind: manifestAdmissionProviderPrefix + resourceruntime.ManifestAgentProviderPrefix + "one"}}), nil
		}
		var callback callbackRequest
		if err := json.NewDecoder(req.Body).Decode(&callback); err != nil {
			return nil, err
		}
		statuses = append(statuses, callback.Status)
		return jsonResponse(t, http.StatusOK, map[string]any{"data": ExecutionTask{ID: "task", Status: callback.Status}}), nil
	})}
	if _, ok := r.claimHelmTask(t.Context()); ok {
		t.Fatal("old Core admission was accepted")
	}
	if claims != 1 || !slices.Equal(statuses, []string{"failed"}) {
		t.Fatalf("claims=%d statuses=%v", claims, statuses)
	}
}

func TestProtectedManifestProviderRetainsRootAndInitializationChecks(t *testing.T) {
	for _, mode := range []string{"native", "root", "root-missing", "root-downgraded", "initialize", "initialize-missing", "wrong-cluster", "unknown", "double-wrapper"} {
		t.Run(mode, func(t *testing.T) {
			provider := resourceruntime.ManifestAgentProviderPrefix + "one"
			payload := sohaapi.ManifestExecutionTaskPayload{Action: "apply", ClusterID: "one"}
			switch mode {
			case "root":
				provider = resourceruntime.RootCRAgentProviderPrefix + "one"
				payload.RootResource = &sohaapi.ManifestRootResource{UID: "root"}
			case "root-missing":
				provider = resourceruntime.RootCRAgentProviderPrefix + "one"
			case "root-downgraded":
				payload.RootResource = &sohaapi.ManifestRootResource{UID: "root"}
			case "initialize":
				provider = resourceruntime.ManifestInitializationProviderPrefix + "one"
				payload.InitializationPlanID = "first"
			case "initialize-missing":
				provider = resourceruntime.ManifestInitializationProviderPrefix + "one"
			case "wrong-cluster":
				provider = resourceruntime.ManifestAgentProviderPrefix + "two"
			case "unknown":
				provider = "other.one"
			case "double-wrapper":
				provider = manifestAdmissionProviderPrefix + provider
			}
			err := validateManifestProtocol(manifestAdmissionProviderPrefix+provider, "one", payload)
			valid := mode == "native" || mode == "root" || mode == "initialize"
			if (err == nil) != valid {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
		})
	}
}
