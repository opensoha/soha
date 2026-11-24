package resource

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestIdentityPodMetricsKeepOtherIncarnationsOutOfRangeSelectors(t *testing.T) {
	uid := "new-uid"
	definitions := buildIdentityPodMetrics("team", "same-pod", uid, "main", "cluster-1", "cluster")
	if len(definitions) != 2 {
		t.Fatalf("unexpected metric list: %+v", definitions)
	}
	extractID := regexp.MustCompile(`id=~("(?:\\.|[^"\\])*")`)
	for _, definition := range definitions {
		query := definition.Query
		for _, matcher := range []string{`namespace="team"`, `pod="same-pod"`, `container="main"`, `cluster="cluster-1"`, `uid="new-uid"`, `uid=""`} {
			if strings.Count(query, matcher) < 1 {
				t.Fatalf("missing scoped matcher %s in %s", matcher, definition.Key)
			}
		}
		if definition.Key == "cpu" && strings.Count(query, "}[5m]") != 2 {
			t.Fatal("CPU rates must use identity-scoped input vectors")
		}
		match := extractID.FindStringSubmatch(query)
		if len(match) != 2 {
			t.Fatalf("no cgroup matcher for %s", definition.Key)
		}
		pattern, err := strconv.Unquote(match[1])
		if err != nil {
			t.Fatal(err)
		}
		compiled := regexp.MustCompile("^(?:" + pattern + ")$")
		for _, test := range []struct {
			path  string
			match bool
		}{
			{"/kubepods/burstable/podnew-uid/abc", true},
			{"/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-podnew_uid.slice/cri-containerd-id.scope", true},
			{"/kubepods/burstable/podold-uid/abc", false},
			{"/kubepods/burstable/podnew-uid-prefix/abc", false},
			{"/kubepods.slice/kubepods-podnew_uid_suffix.slice/abc", false},
			{"", false},
		} {
			if compiled.MatchString(test.path) != test.match {
				t.Errorf("unexpected UID match for %s in %s", test.path, definition.Key)
			}
		}
		t.Logf("%s query: %s", definition.Key, query)
	}
}

func TestPodMetricsIdentityFilterRejectsNonliteralInput(t *testing.T) {
	for _, test := range []struct{ uid, container string }{
		{"", "main"},
		{`bad"uid`, "main"},
		{"uid", `main"} or up`},
		{"uid", strings.Repeat("a", 64)},
	} {
		if validPodMetricSelection(test.uid, test.container) {
			t.Errorf("invalid identity filter accepted: %+v", test)
		}
	}
	if !validPodMetricSelection("uid", "api-sidecar") || !validPodMetricSelection("", "") {
		t.Fatal("valid scoped and legacy requests must both remain supported")
	}
}

type metricIdentityPodReader struct {
	DirectPods
	reads int
	uids  []string
}

func (d *metricIdentityPodReader) GetPodDetail(_ context.Context, _, namespace, name string) (domainresource.PodDetailView, error) {
	uid := d.uids[d.reads]
	d.reads++
	return domainresource.PodDetailView{
		UID: uid, Namespace: namespace, Name: name,
		Containers: []domainresource.WorkloadContainerView{{Name: "main"}},
	}, nil
}

func TestIdentityPodMetricsChecksCurrentPodBeforeAndAfterReadingSeries(t *testing.T) {
	for _, tc := range []struct {
		name         string
		uids         []string
		unconfigured bool
		wantQueries  int
		wantErr      error
	}{
		{name: "matches", uids: []string{"reviewed", "reviewed"}, wantQueries: 2},
		{name: "already replaced", uids: []string{"replacement"}, wantErr: apperrors.ErrConflict},
		{name: "replaced during query", uids: []string{"reviewed", "replacement"}, wantQueries: 2, wantErr: apperrors.ErrConflict},
		{name: "unconfigured source and replaced UID", uids: []string{"replacement"}, unconfigured: true, wantErr: apperrors.ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var queries atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				queries.Add(1)
				query := r.URL.Query().Get("query")
				if r.URL.Path != "/api/v1/query_range" || !strings.Contains(query, `uid="reviewed"`) || !strings.Contains(query, `container="main"`) {
					t.Errorf("unexpected metrics query: %s", query)
				}
				_, _ = fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[[1000,"1"]]}]}}`)
			}))
			defer server.Close()
			connection := domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-1"}}
			if !tc.unconfigured {
				connection.Metadata = map[string]any{"prometheus_url": server.URL}
			}
			resolver := stubConnectionResolver{connection: connection}
			access := &resourceAccess{resolver: resolver, authorizer: &recordingCreateAuthorizer{allow: true}, audit: discardAuditRecorder{}}
			pods := &metricIdentityPodReader{uids: tc.uids}
			workloads := &Workloads{
				resourceAccess: access,
				metricsSupport: &metricsSupport{resourceAccess: access, resolver: resolver, httpClient: server.Client()},
				directPods:     pods,
			}

			view, err := workloads.GetPodMetricsForIdentity(t.Context(), domainidentity.Principal{}, "cluster-1", "team", "same-pod", "reviewed", "main", 60, 60)
			wantSeries := 0
			if tc.wantErr == nil {
				wantSeries = 2
			}
			if !errors.Is(err, tc.wantErr) || len(view.Series) != wantSeries || int(queries.Load()) != tc.wantQueries || pods.reads != len(tc.uids) {
				t.Fatalf("series=%d, Prometheus queries=%d, identity reads=%d, error=%v", len(view.Series), queries.Load(), pods.reads, err)
			}
		})
	}
}
