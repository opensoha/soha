package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/yaml"
)

func assessmentHTTPInput(t *testing.T, entry, healthPath string) sohaapi.DeliveryBatchAssessmentInput {
	t.Helper()
	encoded, _ := json.Marshal(map[string]any{"batchId": "batch", "targetId": "target", "maxAgeSeconds": 120, "http": map[string]any{"url": entry, "healthPath": healthPath}})
	var input sohaapi.DeliveryBatchAssessmentInput
	if err := json.Unmarshal(encoded, &input); err != nil {
		t.Fatal(err)
	}
	return input
}

func TestDeliveryAccessPinsResourceAddressAndDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer other.Close()
	var host string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host = r.Host
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/auth":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			http.Redirect(w, r, other.URL, http.StatusFound)
		}
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	// The candidate's hostname intentionally has no DNS record. Only the
	// authorized resource address is dialed, while HTTP Host is preserved.
	entry := "http://release.invalid:" + parsed.Port() + "/"
	candidates := []domaindelivery.AccessCandidate{{URL: entry, DialAddress: "127.0.0.1"}}
	for _, tc := range []struct{ path, verdict, auth string }{{"/health", "satisfied", "unknown"}, {"/auth", "unsatisfied", "required"}, {"/redirect", "unsatisfied", "unknown"}} {
		result := sohaapi.DeliveryBatchAssessment{ApplicationID: "app", ServiceID: "svc", Verdict: "satisfied"}
		if err := assessTargetAccess(context.Background(), assessmentHTTPInput(t, entry, tc.path), candidates, &result); err != nil {
			t.Fatal(err)
		}
		access := result.Access[0]
		if string(result.Verdict) != tc.verdict || string(access.Authentication) != tc.auth || access.ProbeLocation != "soha_control_plane" || access.NetworkRequirement != "private_network" || access.ValidUntil == nil || len(result.Evidence) != 1 || host != "release.invalid:"+parsed.Port() {
			t.Fatalf("unexpected access evidence: %+v", result)
		}
	}
	if redirected.Load() != 0 {
		t.Fatal("probe followed an external redirect")
	}
	for _, healthPath := range []string{"//external.test", "/../admin", "/health?token=secret", "/%2e%2e/admin", "/health#fragment"} {
		result := sohaapi.DeliveryBatchAssessment{}
		if err := assessTargetAccess(context.Background(), assessmentHTTPInput(t, entry, healthPath), candidates, &result); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("unsafe path accepted: %q: %v", healthPath, err)
		}
	}
	result := sohaapi.DeliveryBatchAssessment{}
	if err := assessTargetAccess(context.Background(), assessmentHTTPInput(t, other.URL, "/health"), candidates, &result); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatal("arbitrary endpoint was accepted")
	}
}

func TestDeliveryAccessDoesNotTreatTLSFailureOrBlockedAddressesAsSuccess(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	for _, tc := range []struct{ entry, address string }{{server.URL + "/", "127.0.0.1"}, {"http://169.254.169.254/", "169.254.169.254"}, {"http://[fe80::1]/", "fe80::1"}} {
		result := sohaapi.DeliveryBatchAssessment{Verdict: "satisfied"}
		if err := assessTargetAccess(context.Background(), assessmentHTTPInput(t, tc.entry, "/health"), []domaindelivery.AccessCandidate{{URL: tc.entry, DialAddress: tc.address}}, &result); err != nil {
			t.Fatal(err)
		}
		if result.Verdict != "inconclusive" || !result.Evidence[0].Incomplete {
			t.Fatalf("missing trustworthy response marked healthy: %+v", result)
		}
	}
	result := sohaapi.DeliveryBatchAssessment{Verdict: "satisfied"}
	if err := assessTargetAccess(context.Background(), sohaapi.DeliveryBatchAssessmentInput{}, []domaindelivery.AccessCandidate{{URL: "http://10.0.0.10/", DialAddress: "10.0.0.10"}}, &result); err != nil || result.Access[0].Reachability != "unverified" || len(result.Evidence) != 0 {
		t.Fatal("unrequested probe or false reachability claim")
	}
}

func TestManifestAssessmentRequiresCurrentCompleteFreshInventory(t *testing.T) {
	now := time.Now().UTC()
	document := domainmanifest.RenderedDocument{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "app", Name: "web", ContentDigest: "digest"}
	for _, mode := range []string{"healthy", "stale", "future", "missing", "drifted", "generation", "other-plan", "missing-child", "no-uid"} {
		t.Run(mode, func(t *testing.T) {
			snapshot := domainmanifest.DeliverySnapshot{DeliveryPlanID: "plan", RenderedDigest: "render", Documents: []domainmanifest.RenderedDocument{document}}
			observed := now
			deployment := domainmanifest.Deployment{ID: "deployment", Generation: 3, Spec: domainmanifest.DeploymentSpec{DeliverySnapshot: &snapshot}, Status: domainmanifest.DeploymentStatus{ObservedGeneration: 3, AppliedDigest: "render", Phase: domainmanifest.DeploymentPhaseConverged, LastReconciledAt: &observed, Conditions: []domainmanifest.Condition{{Type: "Healthy", Status: "true", ObservedGeneration: 3}}, Inventory: []domainmanifest.ResourceInventory{{UID: "uid", Generation: 3, APIVersion: document.APIVersion, Kind: document.Kind, Namespace: document.Namespace, Name: document.Name, DesiredObjectDigest: "digest", ObservedObjectDigest: "digest", Health: "healthy", LastObservedAt: now}}}}
			want := sohaapi.DeliveryBatchAssessmentVerdict("inconclusive")
			switch mode {
			case "healthy":
				want = "satisfied"
			case "stale":
				deployment.Status.Inventory[0].LastObservedAt = now.Add(-3 * time.Minute)
			case "future":
				observed = now.Add(time.Minute)
			case "missing":
				deployment.Status.Inventory = nil
			case "drifted":
				deployment.Status.Inventory[0].ObservedObjectDigest = "other"
				want = "unsatisfied"
			case "generation":
				deployment.Status.ObservedGeneration = 2
				want = "unsatisfied"
			case "other-plan":
				selected := snapshot
				selected.DeliveryPlanID = "other"
				deployment.Spec.DeliverySnapshot = &selected
				want = "unsatisfied"
			case "missing-child":
				snapshot.GitOpsDocuments = []domainmanifest.RenderedDocument{{Kind: "Service", Name: "child"}}
			case "no-uid":
				deployment.Status.Inventory[0].UID = ""
			}
			verdict, evidence := assessManifestDeployment(deployment, snapshot, 120, now)
			if verdict != want || len(evidence) != 1 || verdict == "satisfied" && evidence[0].Incomplete {
				t.Fatalf("%s: %s %+v", mode, verdict, evidence)
			}
		})
	}
}

func TestIngressCandidatesMatchFrozenRoutesAndCurrentTarget(t *testing.T) {
	content := "apiVersion: networking.k8s.io/v1\nkind: Ingress\nmetadata:\n  name: app\nspec:\n  tls:\n    - hosts: [app.example]\n  rules:\n    - host: app.example\n      http:\n        paths:\n          - path: /app\n            pathType: Prefix\n            backend:\n              service:\n                name: api\n                port:\n                  number: 80\n"
	var desired networkingv1.Ingress
	if err := yaml.Unmarshal([]byte(content), &desired); err != nil {
		t.Fatal(err)
	}
	live := domainresource.IngressView{Name: "app", Namespace: "dev", Hosts: []string{"app.example"}, Address: "10.0.0.10", BackendServices: []string{"api"}}
	candidates := ingressAccessCandidates(desired, "dev", []domainresource.IngressView{live})
	if len(candidates) != 1 || candidates[0].URL != "https://app.example/app" || candidates[0].DialAddress != "10.0.0.10" {
		t.Fatalf("incorrect candidates: %+v", candidates)
	}
	for _, change := range []string{"namespace", "host", "backend", "loopback"} {
		other := live
		switch change {
		case "namespace":
			other.Namespace = "prod"
		case "host":
			other.Hosts = []string{"other.example"}
		case "backend":
			other.BackendServices = []string{"other"}
		case "loopback":
			other.Address = "127.0.0.1"
		}
		if got := ingressAccessCandidates(desired, "dev", []domainresource.IngressView{other}); len(got) != 0 {
			t.Fatalf("wrong %s accepted: %+v", change, got)
		}
	}
	result := sohaapi.DeliveryBatchAssessment{}
	if err := assessTargetAccess(context.Background(), assessmentHTTPInput(t, candidates[0].URL, "/other"), candidates, &result); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatal("probe escaped the frozen ingress path")
	}
	documents, err := accessManifestDocuments(content + "---\nkind: Secret\nstringData:\n  password: hidden\n")
	if err != nil || len(documents) != 1 || strings.Contains(documents[0].Content, "hidden") {
		t.Fatalf("access parsing retained secrets: %+v %v", documents, err)
	}
}

func TestIngressProbeKeepsTLSAndRejectsLocalLoadBalancerAddresses(t *testing.T) {
	if got := resolveIngressAddresses(context.Background(), "localhost,169.254.169.254,::1,10.0.0.10"); len(got) != 1 || got[0] != "10.0.0.10" {
		t.Fatalf("unsafe load balancer addresses: %v", got)
	}
	secure, _ := url.Parse("https://app.example:443/")
	clear, _ := url.Parse("http://app.example:443/")
	if accessCandidateMatches(clear, secure, false) {
		t.Fatal("TLS entry accepted a plaintext probe")
	}
	docker, _ := url.Parse("http://10.0.0.10:8443/")
	tlsDocker, _ := url.Parse("https://10.0.0.10:8443/")
	if !accessCandidateMatches(tlsDocker, docker, true) || accessCandidateMatches(tlsDocker, docker, false) {
		t.Fatal("explicit TLS handling lost the candidate protocol boundary")
	}
}
