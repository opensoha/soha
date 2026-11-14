package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha/agent/internal/agent/config"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/contracts/helmrelease"
	resourceruntime "github.com/opensoha/soha/contracts/resource/runtime"
)

type recordingHelmExecutor struct{ calls int }

func (e *recordingHelmExecutor) ExecuteHelmDelivery(context.Context, sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskResult, error) {
	e.calls++
	return sohaapi.HelmExecutionTaskResult{Stopped: true, Ready: true}, nil
}

type recordingManifestExecutor struct{ calls int }

func (e *recordingManifestExecutor) ExecuteManifestTask(context.Context, sohaapi.ManifestExecutionTaskPayload) (sohaapi.ManifestExecutionTaskResult, error) {
	e.calls++
	return sohaapi.ManifestExecutionTaskResult{}, nil
}

func TestExecutionClaimAndDispatchMatchExecutorConfiguration(t *testing.T) {
	const cluster = "one"
	const token = "cluster-token"
	manifestProvider := "manifest_delivery_v1.manifest_agent_v3.one"
	manifestTask := ExecutionTask{
		ID: "manifest", TaskKind: "manifest_preflight", ProviderKind: manifestProvider, CallbackToken: "manifest-attempt",
		Payload: map[string]any{"action": "preflight", "clusterId": cluster, "packageId": "pkg", "generation": 1, "idempotencyKey": "key"},
	}
	helmTask := ExecutionTask{
		ID: "helm", TaskKind: "helm_observe", ProviderKind: "helm_agent.one", CallbackToken: "helm-attempt",
		Payload: map[string]any{"helm": sohaapi.HelmExecutionTaskPayload{
			Action: sohaapi.Observe,
			Snapshot: sohaapi.HelmDeliverySnapshot{
				ClusterID: cluster, Namespace: "dev", ReleaseName: "app", DeliveryPlanID: "plan",
				ApplicationID: "app", ApplicationEnvironmentID: "env", ServiceID: "svc", TargetID: "target",
			},
		}},
	}
	manifestProviders := []string{
		"manifest_initialize_v1.one", manifestProvider, "manifest_delivery_v1.manifest_root_cr_v1.one", "manifest_delivery_v1.manifest_initialize_v1.one",
	}
	helmProviders := []string{"helm_agent.one", "helm_initialize_v1.one"}
	manifestCapabilities := []string{resourceruntime.RootCRAgentCapability, "manifest.delivery-admission.v1", resourceruntime.ManifestInitializationCapability}
	helmCapabilities := []string{helmrelease.InitializationCapability}
	for _, tc := range []struct {
		name         string
		helm         bool
		manifest     bool
		providers    []string
		capabilities []string
		tasks        []ExecutionTask
	}{
		{name: "ManifestOnly", manifest: true, providers: manifestProviders, capabilities: manifestCapabilities, tasks: []ExecutionTask{manifestTask}},
		{name: "HelmOnly", helm: true, providers: helmProviders, capabilities: helmCapabilities, tasks: []ExecutionTask{helmTask}},
		{
			name: "Combined", helm: true, manifest: true,
			providers:    append(slices.Clone(helmProviders), manifestProviders...),
			capabilities: append(slices.Clone(manifestCapabilities), helmCapabilities...),
			tasks:        []ExecutionTask{helmTask, manifestTask},
		},
		{name: "Disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newClaimTestRunner()
			helm, manifest := &recordingHelmExecutor{}, &recordingManifestExecutor{}
			if tc.helm {
				r.SetHelmExecutor(helm, cluster, token, nil)
			}
			if tc.manifest {
				r.SetManifestExecutor(manifest, cluster, token)
			}
			if got := r.ExecutionClaimCapabilities(); !slices.Equal(got, tc.capabilities) {
				t.Fatalf("advertised capabilities = %v, want %v", got, tc.capabilities)
			}
			if got := r.executionClaimEnabled(); got != (tc.helm || tc.manifest) {
				t.Fatalf("execution claim enabled = %v", got)
			}
			claimCount := 0
			tasks := slices.Clone(tc.tasks)
			var statuses []string
			var task ExecutionTask
			r.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if strings.HasSuffix(req.URL.Path, "/execution-tasks/claim") {
					claimCount++
					var input claimRequest
					if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
						return nil, err
					}
					if req.Header.Get("Authorization") == "Bearer general-token" && tc.manifest && len(tasks) == 0 {
						want := []string{"manifest_agent.one", "manifest_agent_v3.one", "manifest_root_cr_v1.one"}
						if !slices.Equal(input.ProviderKinds, want) {
							t.Errorf("standalone Manifest providers = %v, want %v", input.ProviderKinds, want)
						}
						return jsonResponse(t, http.StatusNoContent, nil), nil
					}
					if req.Header.Get("Authorization") != "Bearer "+token || !slices.Equal(input.ProviderKinds, tc.providers) {
						t.Errorf("cluster providers = %v, want %v with the cluster credential", input.ProviderKinds, tc.providers)
					}
					if len(tasks) == 0 {
						return jsonResponse(t, http.StatusNoContent, nil), nil
					}
					task, tasks = tasks[0], tasks[1:]
					response := jsonResponse(t, http.StatusAccepted, map[string]any{"data": task})
					if task.ID == manifestTask.ID {
						response.Header.Set(manifestAdmissionHeader, "1")
					}
					return response, nil
				}
				var callback callbackRequest
				if err := json.NewDecoder(req.Body).Decode(&callback); err != nil {
					return nil, err
				}
				if callback.CallbackToken != task.CallbackToken {
					t.Error("callback lost the current attempt")
				}
				statuses = append(statuses, callback.Status)
				next := task
				next.Status = callback.Status
				return jsonResponse(t, http.StatusAccepted, map[string]any{"data": next}), nil
			})}
			for _, want := range tc.tasks {
				claimed, ok := r.claim(t.Context())
				if !ok || claimed.ID != want.ID {
					t.Fatalf("claim = %q, %v, want %q", claimed.ID, ok, want.ID)
				}
				r.executeClaimedTask(t.Context(), claimed)
			}
			if _, ok := r.claim(t.Context()); ok {
				t.Fatal("empty claim returned a task")
			}
			wantClaims := len(tc.tasks)
			if tc.helm || tc.manifest {
				wantClaims++
			}
			if tc.manifest {
				wantClaims++
			}
			if claimCount != wantClaims {
				t.Fatalf("HTTP claims = %d, want %d", claimCount, wantClaims)
			}
			if (helm.calls == 1) != tc.helm || (manifest.calls == 1) != tc.manifest {
				t.Fatalf("executor calls Helm=%d Manifest=%d", helm.calls, manifest.calls)
			}
			for i, status := range statuses {
				if want := []string{"running", "completed"}[i%2]; status != want {
					t.Fatalf("callbacks = %v, expected running/completed for each task", statuses)
				}
			}
			if len(statuses) != len(tc.tasks)*2 {
				t.Fatalf("callbacks = %v", statuses)
			}
		})
	}
}

func TestExecutionClaimKeepsDifferentClusterIdentitiesSeparate(t *testing.T) {
	for _, tc := range []struct{ name, cluster, token string }{
		{name: "same cluster different credentials", cluster: "one", token: "manifest-token"},
		{name: "different clusters same credential", cluster: "two", token: "helm-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newClaimTestRunner()
			r.SetHelmExecutor(&recordingHelmExecutor{}, "one", "helm-token", nil)
			r.SetManifestExecutor(&recordingManifestExecutor{}, tc.cluster, tc.token)
			claimCount := 0
			r.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				claimCount++
				var input claimRequest
				if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
					return nil, err
				}
				if claimCount == 1 {
					if req.Header.Get("Authorization") != "Bearer helm-token" || !slices.Equal(input.ProviderKinds, []string{"helm_agent.one", "helm_initialize_v1.one"}) {
						t.Errorf("Helm claim mixed in another executor identity: %v", input.ProviderKinds)
					}
					return jsonResponse(t, http.StatusNoContent, nil), nil
				}
				want := append([]string{resourceruntime.ManifestInitializationProviderPrefix + tc.cluster}, protectedManifestProviders(tc.cluster)...)
				if req.Header.Get("Authorization") != "Bearer "+tc.token || !slices.Equal(input.ProviderKinds, want) {
					t.Errorf("Manifest claim did not use its own identity: %v", input.ProviderKinds)
				}
				task := ExecutionTask{ID: "manifest", TaskKind: "manifest_preflight", ProviderKind: want[1]}
				response := jsonResponse(t, http.StatusAccepted, map[string]any{"data": task})
				response.Header.Set(manifestAdmissionHeader, "1")
				return response, nil
			})}
			if task, ok := r.claim(t.Context()); !ok || task.ID != "manifest" || claimCount != 2 {
				t.Fatalf("separate Manifest claim = %q, %v, HTTP claims = %d", task.ID, ok, claimCount)
			}
		})
	}
}

func TestExecutionClaimRejectsProtectedTaskOutsideRequestedIdentity(t *testing.T) {
	for _, tc := range []struct{ name, cluster, token, responseCluster string }{
		{name: "missing cluster credential", cluster: "one", responseCluster: "one"},
		{name: "wrong cluster response", cluster: "one", token: "cluster-token", responseCluster: "two"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newClaimTestRunner()
			r.SetManifestExecutor(&recordingManifestExecutor{}, tc.cluster, tc.token)
			if tc.token == "" && slices.Contains(r.ExecutionClaimCapabilities(), manifestAdmissionCapability) {
				t.Fatal("missing identity still advertised Manifest admission")
			}
			task := ExecutionTask{ID: "manifest", TaskKind: "manifest_preflight", ProviderKind: protectedManifestProviders(tc.responseCluster)[0], CallbackToken: "attempt"}
			callbacks := 0
			issued := false
			r.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if strings.HasSuffix(req.URL.Path, "/execution-tasks/claim") {
					var input claimRequest
					if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
						return nil, err
					}
					if slices.Contains(input.ProviderKinds, task.ProviderKind) {
						t.Error("protected provider was requested outside its cluster credential")
					}
					if issued {
						return jsonResponse(t, http.StatusNoContent, nil), nil
					}
					issued = true
					response := jsonResponse(t, http.StatusAccepted, map[string]any{"data": task})
					response.Header.Set(manifestAdmissionHeader, "1")
					return response, nil
				}
				var callback callbackRequest
				if err := json.NewDecoder(req.Body).Decode(&callback); err != nil {
					return nil, err
				}
				if callback.Status != "failed" || callback.CallbackToken != task.CallbackToken {
					t.Errorf("unexpected rejection callback: %+v", callback)
				}
				callbacks++
				return jsonResponse(t, http.StatusAccepted, map[string]any{"data": task}), nil
			})}
			if _, ok := r.claim(t.Context()); ok || callbacks != 1 {
				t.Fatalf("protected claim succeeded = %v, rejection callbacks = %d", ok, callbacks)
			}
		})
	}
}

func TestExecutionClaimRejectsTypedNilExecutors(t *testing.T) {
	r := newClaimTestRunner()
	r.cfg.ProviderKinds = []string{" manifest_delivery_v1.manifest_agent_v3.one ", "helm_agent.one"}
	var helm *recordingHelmExecutor
	var manifest *recordingManifestExecutor
	r.SetHelmExecutor(helm, "one", "token", nil)
	r.SetManifestExecutor(manifest, "one", "token")
	if r.executionClaimEnabled() || len(r.ExecutionClaimCapabilities()) != 0 {
		t.Fatal("unavailable executors enabled or advertised a claim")
	}
}

func TestManifestOnlyStartsClaimLoopWithoutGeneralProviders(t *testing.T) {
	r := newClaimTestRunner()
	r.cfg.PollInterval = time.Millisecond
	r.SetManifestExecutor(&recordingManifestExecutor{}, "one", "cluster-token")
	claims := make(chan string, 1)
	r.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		select {
		case claims <- req.Header.Get("Authorization"):
		default:
		}
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody}, nil
	})}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	r.Start(ctx)
	select {
	case token := <-claims:
		if token != "Bearer cluster-token" {
			t.Fatal("first Manifest claim did not use its cluster credential")
		}
	case <-time.After(time.Second):
		t.Fatal("Manifest-only runner did not start claiming")
	}
}

func newClaimTestRunner() *Runner {
	return New(cfgpkg.ControlPlaneConfig{
		Enabled: true, BaseURL: "http://core", AgentID: "agent", BearerToken: "general-token",
		CallbackRetry: cfgpkg.CallbackRetryConfig{MaxAttempts: 1},
	}, nil)
}
