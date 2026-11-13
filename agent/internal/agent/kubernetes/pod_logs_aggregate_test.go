package kubernetes

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func TestAggregatePodLogOptionsDoesNotTailExplicitTimeRange(t *testing.T) {
	from := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
	options := aggregatePodLogOptions(domainresource.LogQuery{From: &from}, "app", false)

	if options.TailLines != nil || options.SinceTime == nil || !options.SinceTime.Time.Equal(from) {
		t.Fatalf("options = %#v, want untailed explicit time range", options)
	}
}

func TestValidateAggregateLogSelectorRequiresNamespace(t *testing.T) {
	_, err := validateAggregateLogSelector(domainresource.LogSourceSelector{})
	if !errors.Is(err, ErrInvalidLogQuery) {
		t.Fatalf("validateAggregateLogSelector() error = %v, want invalid query", err)
	}
}

func TestQueryPodLogsOrdersEntriesAndKeepsPartialResults(t *testing.T) {
	client := newAggregateLogTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/log") {
			if r.URL.Query().Get("timestamps") != "true" || r.URL.Query().Get("tailLines") != "2" || r.URL.Query().Get("container") != "app" {
				t.Errorf("log query = %s, want timestamps/tail/container", r.URL.RawQuery)
			}
			if strings.Contains(r.URL.Path, "/pod-b/") {
				http.Error(w, "unavailable", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte("2026-07-31T12:00:02Z later\n2026-07-31T12:00:01Z earlier\n"))
			return
		}
		writeAggregateLogTestPod(t, w, r)
	})

	page, err := client.QueryPodLogs(t.Context(), domainresource.LogQuery{
		SourceMode: sohaapi.LogSourceModeRuntime,
		Direction:  sohaapi.LogDirectionForward,
		Tail:       2,
		Selector: &domainresource.LogSourceSelector{
			Namespace:  "demo",
			PodNames:   []string{"pod-a", "pod-b"},
			Containers: []string{"app"},
		},
	})
	if err != nil {
		t.Fatalf("QueryPodLogs() error = %v", err)
	}
	if !page.Partial || page.Coverage == nil || page.Coverage.ResolvedSources != 2 || page.Coverage.SuccessfulSources != 1 || page.Coverage.FailedSources != 1 {
		t.Fatalf("coverage = %#v partial=%v, want 2/1/1 partial", page.Coverage, page.Partial)
	}
	if len(page.Entries) != 2 || page.Entries[0].Message != "earlier" || page.Entries[1].Message != "later" {
		t.Fatalf("entries = %#v, want ascending successful source logs", page.Entries)
	}
	if page.Entries[0].Source.ClusterID != "cluster-a" || page.Entries[0].Source.PodName != "pod-a" || page.Entries[0].Source.ContainerName != "app" {
		t.Fatalf("source = %#v, want normalized identity", page.Entries[0].Source)
	}
}

func TestStreamPodLogEventsEmitsLifecycle(t *testing.T) {
	client := newAggregateLogTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/log") {
			if r.URL.Query().Get("follow") != "true" || r.URL.Query().Get("timestamps") != "true" {
				t.Errorf("stream query = %s, want follow and timestamps", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte("2026-07-31T12:00:00Z ready\n"))
			return
		}
		writeAggregateLogTestPod(t, w, r)
	})

	events := make([]domainresource.LogStreamEvent, 0, 3)
	err := client.StreamPodLogEvents(t.Context(), domainresource.LogQuery{Selector: &domainresource.LogSourceSelector{
		Namespace: "demo",
		PodNames:  []string{"pod-a"},
	}}, func(event domainresource.LogStreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamPodLogEvents() error = %v", err)
	}
	if len(events) != 3 || events[0].Type != "status" || events[1].Type != "entry" || events[2].Type != "end" {
		t.Fatalf("event types = %#v, want status/entry/end", events)
	}
	if events[1].Entry == nil || events[1].Entry.Message != "ready" {
		t.Fatalf("entry event = %#v, want parsed log entry", events[1])
	}
}

func TestStreamPodLogEventsReportsResolutionWarnings(t *testing.T) {
	client := newAggregateLogTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeAggregateLogTestPod(t, w, r)
	})
	events := make([]domainresource.LogStreamEvent, 0, 3)
	err := client.StreamPodLogEvents(t.Context(), domainresource.LogQuery{Selector: &domainresource.LogSourceSelector{
		Namespace:  "demo",
		PodNames:   []string{"pod-a"},
		Containers: []string{"missing"},
	}}, func(event domainresource.LogStreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamPodLogEvents() error = %v", err)
	}
	if len(events) != 3 || events[0].Status == nil || events[0].Status.State != "degraded" || events[1].Type != "source_error" || events[2].Type != "end" {
		t.Fatalf("events = %#v, want degraded/source_error/end", events)
	}
}

func TestQueryPodLogsKeepsAvailableNamedPodsWhenOneDisappears(t *testing.T) {
	client := newAggregateLogTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "pod-b") {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/log") {
			_, _ = w.Write([]byte("2026-07-31T12:00:00Z available\n"))
			return
		}
		writeAggregateLogTestPod(t, w, r)
	})
	page, err := client.QueryPodLogs(t.Context(), domainresource.LogQuery{Selector: &domainresource.LogSourceSelector{
		Namespace: "demo",
		PodNames:  []string{"pod-a", "pod-b"},
	}})
	if err != nil {
		t.Fatalf("QueryPodLogs() error = %v", err)
	}
	if !page.Partial || len(page.Entries) != 1 || page.Entries[0].Message != "available" || len(page.Warnings) != 1 || page.Warnings[0].Code != "pod_unavailable" {
		t.Fatalf("page = %#v, want available entry and missing-pod warning", page)
	}
}

func TestParseAggregateLogContentSkipsEmptyResponse(t *testing.T) {
	if entries := parseAggregateLogContent("\n", domainresource.LogQuery{}, domainresource.LogSource{}); len(entries) != 0 {
		t.Fatalf("entries = %#v, want empty response", entries)
	}
}

func TestResolveAggregateWorkloadSelector(t *testing.T) {
	client := &Client{typed: kubernetesfake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "demo"},
		Spec:       appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}}},
	})}
	selector, err := client.resolveAggregateWorkloadSelector(t.Context(), domainresource.LogSourceSelector{
		Namespace:     "demo",
		WorkloadKind:  "Deployment",
		WorkloadName:  "web",
		LabelSelector: "tier=frontend",
	})
	if err != nil {
		t.Fatalf("resolveAggregateWorkloadSelector() error = %v", err)
	}
	if selector.LabelSelector != "app=web,tier=frontend" {
		t.Fatalf("label selector = %q, want workload and requested selectors", selector.LabelSelector)
	}
}

func newAggregateLogTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	typed, err := kubernetes.NewForConfig(&rest.Config{
		Host:    server.URL,
		APIPath: "/api",
		ContentConfig: rest.ContentConfig{
			GroupVersion:         &schema.GroupVersion{Version: "v1"},
			NegotiatedSerializer: scheme.Codecs.WithoutConversion(),
		},
	})
	if err != nil {
		t.Fatalf("NewForConfig() error = %v", err)
	}
	return &Client{cfg: cfgpkg.KubernetesConfig{ID: "cluster-a"}, typed: typed}
}

func writeAggregateLogTestPod(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	name := strings.TrimPrefix(r.URL.Path, "/api/v1/namespaces/demo/pods/")
	if name != "pod-a" && name != "pod-b" {
		t.Errorf("pod path = %s, want pod-a or pod-b", r.URL.Path)
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(corev1.Pod{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "demo", UID: types.UID(name + "-uid")},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
	}); err != nil {
		t.Errorf("encode pod: %v", err)
	}
}
