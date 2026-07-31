package resourcebackend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestDirectLogRuntimeAggregatesAndStreamsTimestampedSources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(directLogRuntimeTestHandler))
	defer server.Close()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatalf("NewForConfig() error = %v", err)
	}
	runtime := &directLogRuntime{clusterID: "cluster-a", typed: client}
	query := domainresource.LogQuery{
		Selector: &domainresource.LogSourceSelector{Namespace: "platform", LabelSelector: "app=api"},
		Tail:     10,
	}
	page, err := runtime.query(context.Background(), query)
	if err != nil {
		t.Fatalf("query() error = %v", err)
	}
	if len(page.Entries) != 2 || page.Entries[0].Message != "second" || page.Entries[1].Message != "first" {
		t.Fatalf("entries = %#v", page.Entries)
	}
	if page.Entries[0].Source.ClusterID != "cluster-a" || page.Entries[0].Source.PodUID != "uid-api-1" || page.Coverage.SuccessfulSources != 2 {
		t.Fatalf("page source/coverage = %#v %#v", page.Entries[0].Source, page.Coverage)
	}

	streamQuery := query
	streamSelector := *query.Selector
	streamSelector.PodNames = []string{"api-0"}
	streamQuery.Selector = &streamSelector
	var events []domainresource.LogStreamEvent
	if err := runtime.stream(context.Background(), streamQuery, func(event domainresource.LogStreamEvent) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatalf("stream() error = %v", err)
	}
	if len(events) != 3 || events[0].Type != "status" || events[1].Type != "entry" || events[1].Entry.Message != "first" || events[2].Type != "end" {
		t.Fatalf("events = %#v", events)
	}
}

func directLogRuntimeTestHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v1/namespaces/platform/pods" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(corev1.PodList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PodList"}, Items: []corev1.Pod{
			testRuntimeLogPod("api-0", "uid-api-0"),
			testRuntimeLogPod("api-1", "uid-api-1"),
		}})
		return
	}
	if strings.HasSuffix(r.URL.Path, "/log") {
		if r.URL.Query().Get("timestamps") != "true" || r.URL.Query().Get("tailLines") != "10" {
			http.Error(w, "missing bounded timestamp options", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		if strings.Contains(r.URL.Path, "api-1") {
			_, _ = w.Write([]byte("2026-07-31T10:00:02Z second\n"))
			return
		}
		_, _ = w.Write([]byte("2026-07-31T10:00:01Z first\n"))
		return
	}
	http.NotFound(w, r)
}

func testRuntimeLogPod(name, uid string) corev1.Pod {
	return corev1.Pod{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "platform", UID: types.UID(uid), Labels: map[string]string{"app": "api"},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
	}
}

func TestDirectPodLogOptionsDoesNotTailExplicitTimeRange(t *testing.T) {
	from := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
	options := directPodLogOptions(domainresource.LogQuery{From: &from}, "app", false)

	if options.TailLines != nil || options.SinceTime == nil || !options.SinceTime.Time.Equal(from) {
		t.Fatalf("options = %#v, want untailed explicit time range", options)
	}
}
