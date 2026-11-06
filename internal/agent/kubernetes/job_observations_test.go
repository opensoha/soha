package kubernetes

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
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
	client := &Client{typed: fake.NewClientset(&batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "report", Namespace: "team"}, Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "busybox:1.38"}}}}}})}
	view, err := client.GetJobYAML(context.Background(), "team", "report")
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
	client := &Client{typed: fake.NewClientset(&batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "report", Namespace: "team"}, Spec: batchv1.CronJobSpec{Schedule: "0 2 * * *", JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "busybox:1.38"}}}}}}}})}
	view, err := client.GetCronJobYAML(context.Background(), "team", "report")
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
