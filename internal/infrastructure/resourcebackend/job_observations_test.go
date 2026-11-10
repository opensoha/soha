package resourcebackend

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

func TestJobNativeObservationsPreserveTerminalAndZeroConfiguration(t *testing.T) {
	zero, one, no := int32(0), int32(1), false
	start := metav1.NewTime(time.Unix(100, 0).UTC())
	job := batchv1.Job{Spec: batchv1.JobSpec{Completions: &one, BackoffLimit: &zero, Suspend: &no}, Status: batchv1.JobStatus{StartTime: &start, Failed: 1, Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded", LastTransitionTime: start}}}}
	detail, list := mapJobDetail(job), mapJob(job)
	if detail.BackoffLimit == nil || *detail.BackoffLimit != 0 || list.Suspend == nil || *list.Suspend || list.CompletionTarget == nil || *list.CompletionTarget != 1 {
		t.Fatalf("native zero or pointer lost: %#v / %#v", detail, list)
	}
	if len(detail.Conditions) != 1 || len(list.Conditions) != 1 || detail.Conditions[0].Reason != "BackoffLimitExceeded" || list.StartTime != "1970-01-01T00:01:40Z" {
		t.Fatalf("native execution observation lost: %#v", detail)
	}
	job.Spec.Completions = nil
	job.Status.Conditions = nil
	list = mapJob(job)
	if list.CompletionTarget != nil || len(list.Conditions) != 0 || list.Failed != 1 {
		t.Fatalf("invented completion target or terminal condition: %#v", list)
	}
}

func TestJobYAMLRetainsManifestIdentityAfterTypedDecode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/apis/batch/v1/namespaces/team/jobs/report" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"apiVersion":"batch/v1","kind":"Job","metadata":{"name":"report","namespace":"team"},"spec":{"template":{"spec":{"containers":[{"name":"app","image":"busybox:1.38"}]}}}}`))
	}))
	defer server.Close()
	kubeconfig := fmt.Sprintf("apiVersion: v1\nkind: Config\ncurrent-context: test\nclusters:\n- name: test\n  cluster:\n    server: %s\ncontexts:\n- name: test\n  context:\n    cluster: test\n    user: test\nusers:\n- name: test\n  user: {}\n", server.URL)
	direct := NewDirect(NewClusters(k8sinfra.NewManager([]cfgpkg.ClusterConfig{{ID: "test", KubeconfigData: kubeconfig}})), nil)
	view, err := direct.GetJobYAML(context.Background(), "test", "team", "report")
	if err != nil {
		t.Fatal(err)
	}
	var job batchv1.Job
	if err := yaml.Unmarshal([]byte(view.Content), &job); err != nil {
		t.Fatal(err)
	}
	if job.APIVersion != "batch/v1" || job.Kind != "Job" || job.Name != "report" || job.Namespace != "team" || len(job.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("Job YAML lost identity or template: %+v", job)
	}
}

func TestCronJobYAMLRetainsManifestIdentityAfterTypedDecode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/apis/batch/v1/namespaces/team/cronjobs/report" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"apiVersion":"batch/v1","kind":"CronJob","metadata":{"name":"report","namespace":"team"},"spec":{"schedule":"0 2 * * *","jobTemplate":{"spec":{"template":{"spec":{"containers":[{"name":"app","image":"busybox:1.38"}]}}}}}}`))
	}))
	defer server.Close()
	kubeconfig := fmt.Sprintf("apiVersion: v1\nkind: Config\ncurrent-context: test\nclusters:\n- name: test\n  cluster:\n    server: %s\ncontexts:\n- name: test\n  context:\n    cluster: test\n    user: test\nusers:\n- name: test\n  user: {}\n", server.URL)
	direct := NewDirect(NewClusters(k8sinfra.NewManager([]cfgpkg.ClusterConfig{{ID: "test", KubeconfigData: kubeconfig}})), nil)
	view, err := direct.GetCronJobYAML(context.Background(), "test", "team", "report")
	if err != nil {
		t.Fatal(err)
	}
	var job batchv1.CronJob
	if err := yaml.Unmarshal([]byte(view.Content), &job); err != nil {
		t.Fatal(err)
	}
	if job.APIVersion != "batch/v1" || job.Kind != "CronJob" || job.Name != "report" || job.Namespace != "team" || len(job.Spec.JobTemplate.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("CronJob YAML lost identity or template: %+v", job)
	}
}
