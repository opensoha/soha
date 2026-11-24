package hostedbuild

import (
	"context"
	"errors"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

const testRevision = "rev_a01b2345600000000000000000000000"

var testDigest = "sha256:" + strings.Repeat("a", 64)

func testConfig() Config {
	return Config{ //nolint:gosec // Secret fields contain synthetic Kubernetes resource names.
		Namespace:          "builds",
		BuildClientImage:   "registry.example/platform/client@" + testDigest,
		NodeBase:           "docker.io/library/node@" + testDigest,
		PythonBase:         "docker.io/library/python@" + testDigest,
		RegistryRepository: "registry.example/platform/hosted",
		DockerConfigSecret: "registry-auth",
		BuildKitAddress:    "tcp://buildkit.builds.svc.cluster.local:1234",
		BuildKitTLSSecret:  "buildkit-auth",
		SourceOrigin:       "https://objects.example:8443",
	}
}

func testBuilder(t *testing.T) (*Builder, *fake.Clientset) {
	t.Helper()
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "jobs", func(action ktesting.Action) (bool, runtime.Object, error) {
		create, ok := action.(ktesting.CreateAction)
		if !ok {
			t.Fatalf("unexpected action type %T", action)
		}
		job, ok := create.GetObject().(*batchv1.Job)
		if !ok {
			t.Fatalf("unexpected object type %T", create.GetObject())
		}
		job.UID = "hosted-build-job"
		return false, nil, nil
	})
	builder, err := New(client, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	return builder, client
}

func TestBuilderRejectsUnpinnedImagesAndUnsafeOrigins(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*Config){
		"mutable image":               func(cfg *Config) { cfg.NodeBase = "docker.io/library/node:22" },
		"shell in image":              func(cfg *Config) { cfg.NodeBase += "\nRUN command" },
		"mutable repository":          func(cfg *Config) { cfg.RegistryRepository += ":mutable" },
		"host socket":                 func(cfg *Config) { cfg.BuildKitAddress = "unix:///var/run/docker.sock" },
		"source origin path":          func(cfg *Config) { cfg.SourceOrigin += "/path" },
		"source credential in origin": func(cfg *Config) { cfg.SourceOrigin = "https://user:secret@objects.example" },
		"secret name collision":       func(cfg *Config) { cfg.BuildKitTLSSecret = cfg.DockerConfigSecret },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig()
			change(&cfg)
			if _, err := New(fake.NewSimpleClientset(), cfg); err == nil {
				t.Fatal("accepted invalid build configuration")
			}
		})
	}
}

func TestPrepareRejectsRequestsBeforeKubernetesAccess(t *testing.T) {
	t.Parallel()
	b, client := testBuilder(t)
	cases := map[string]Spec{
		"incorrect revision": {RevisionID: "rev_../", Template: "node", SourceURL: "https://objects.example:8443/source?token=x"},
		"arbitrary template": {RevisionID: testRevision, Template: "user-dockerfile", SourceURL: "https://objects.example:8443/source?token=x"},
		"another host":       {RevisionID: testRevision, Template: "node", SourceURL: "https://elsewhere.example/source?token=x"},
		"another port":       {RevisionID: testRevision, Template: "node", SourceURL: "https://objects.example/source?token=x"},
		"insecure URL":       {RevisionID: testRevision, Template: "node", SourceURL: "http://objects.example:8443/source?token=x"},
		"URL with userinfo":  {RevisionID: testRevision, Template: "node", SourceURL: "https://user:secret@objects.example:8443/source?token=x"}, //nolint:gosec // Synthetic URL exercises rejection of embedded credentials.
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			if err := b.Prepare(t.Context(), spec); err == nil || strings.Contains(err.Error(), spec.SourceURL) {
				t.Fatalf("unsafe rejection error %v", err)
			}
		})
	}
	if len(client.Actions()) != 0 {
		t.Fatal("invalid build reached Kubernetes")
	}
}

func TestPrepareUsesOwnedSecretAndResumesSubmission(t *testing.T) {
	t.Parallel()
	b, client := testBuilder(t)
	ctx := t.Context()
	url := "https://objects.example:8443/source?token=private-build-input"
	spec := Spec{RevisionID: testRevision, Template: "node", SourceURL: url}
	failCreate := true
	client.PrependReactor("create", "secrets", func(ktesting.Action) (bool, runtime.Object, error) {
		if failCreate {
			failCreate = false
			return true, nil, errors.New("admission denied " + url)
		}
		return false, nil, nil
	})
	err := b.Prepare(ctx, spec)
	if err == nil || strings.Contains(err.Error(), url) {
		t.Fatalf("unsafe rejection error %v", err)
	}
	jobs := client.BatchV1().Jobs(b.cfg.Namespace)
	job, err := jobs.Get(ctx, jobName(testRevision), metav1.GetOptions{})
	if err != nil || (job.Spec.Suspend == nil || !*job.Spec.Suspend) {
		t.Fatalf("failed credential creation must leave Job suspended: %v", err)
	}

	if err := b.Prepare(ctx, spec); err != nil {
		t.Fatal(err)
	}
	job, err = jobs.Get(ctx, job.Name, metav1.GetOptions{})
	if err != nil || (job.Spec.Suspend == nil || *job.Spec.Suspend) {
		t.Fatalf("credential must release Job: %v", err)
	}
	secrets := client.CoreV1().Secrets(b.cfg.Namespace)
	secret, err := secrets.Get(ctx, job.Name+"-source", metav1.GetOptions{})
	if err != nil || !controlledBy(secret, job) || (secret.Immutable == nil || !*secret.Immutable) || string(secret.Data[sourceKey]) != url {
		t.Fatalf("credential has incorrect contents or controller: %v", err)
	}
	spec.SourceURL = "https://objects.example:8443/source?token=new-token"
	if err := b.Prepare(ctx, spec); err != nil {
		t.Fatalf("replay should tolerate a refreshed signature: %v", err)
	}
	spec.SourceURL = "https://objects.example:8443/another-object?token=new-token"
	if err := b.Prepare(ctx, spec); err == nil {
		t.Fatal("replay with different source path was accepted")
	}
}

func TestResultAcceptsOnlyOwnedDigest(t *testing.T) {
	t.Parallel()
	b, client := testBuilder(t)
	ctx := t.Context()
	spec := Spec{RevisionID: testRevision, Template: "python", SourceURL: "https://objects.example:8443/source?token=x"}
	if err := b.Prepare(ctx, spec); err != nil {
		t.Fatal(err)
	}
	if image, ready, err := b.Result(ctx, testRevision); image != "" || ready || err != nil {
		t.Fatalf("pending result: %q %t %v", image, ready, err)
	}
	jobs := client.BatchV1().Jobs(b.cfg.Namespace)
	job, err := jobs.Get(ctx, jobName(testRevision), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pods := client.CoreV1().Pods(b.cfg.Namespace)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "build-pod",
			Namespace: b.cfg.Namespace,
			Labels:    map[string]string{batchv1.ControllerUidLabel: string(job.UID)},
			OwnerReferences: []metav1.OwnerReference{
				{Kind: "Job", APIVersion: "batch/v1", Name: "unrelated", UID: job.UID, Controller: new(true)},
			},
		},
		Spec: job.Spec.Template.Spec,
		Status: corev1.PodStatus{
			Phase: corev1.PodSucceeded,
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: buildContainer, State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Message: testDigest}}},
			},
		},
	}
	if _, err := pods.Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	setJobCondition(t, ctx, b, client, job, batchv1.JobComplete)
	if _, ready, err := b.Result(ctx, testRevision); ready || !errors.Is(err, ErrBuildFailed) {
		t.Fatalf("foreign result was accepted: %t %v", ready, err)
	}
	pod.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(job, batchv1.SchemeGroupVersion.WithKind("Job"))}
	pod.Status.ContainerStatuses[0].State.Terminated.Message = "elsewhere.example/app@" + testDigest
	if _, err := pods.Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, ready, err := b.Result(ctx, testRevision); ready || !errors.Is(err, ErrBuildFailed) {
		t.Fatalf("unapproved result was accepted: %t %v", ready, err)
	}
	pod.Status.ContainerStatuses[0].State.Terminated.Message = testDigest
	if _, err := pods.UpdateStatus(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if image, ready, err := b.Result(ctx, testRevision); image != b.cfg.RegistryRepository+"@"+testDigest || !ready || err != nil {
		t.Fatalf("incorrect ready result: %q %t %v", image, ready, err)
	}
	setJobCondition(t, ctx, b, client, job, batchv1.JobFailed)
	if _, ready, err := b.Result(ctx, testRevision); ready || !errors.Is(err, ErrBuildFailed) {
		t.Fatalf("failed Job must reject results: %t %v", ready, err)
	}
}

func setJobCondition(t *testing.T, ctx context.Context, b *Builder, client *fake.Clientset, job *batchv1.Job, condition batchv1.JobConditionType) {
	t.Helper()
	job.Status.Conditions = []batchv1.JobCondition{{Type: condition, Status: corev1.ConditionTrue}}
	if _, err := client.BatchV1().Jobs(b.cfg.Namespace).UpdateStatus(ctx, job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteRequiresOwnership(t *testing.T) {
	t.Parallel()
	b, client := testBuilder(t)
	ctx := t.Context()
	jobs := client.BatchV1().Jobs(b.cfg.Namespace)
	job, err := jobs.Create(ctx, &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: jobName(testRevision), Namespace: b.cfg.Namespace}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Delete(ctx, testRevision); err == nil {
		t.Fatal("foreign Job deletion was accepted")
	}
	job.Labels = map[string]string{managedByLabel: managedBy, revisionLabel: testRevision}
	if _, err := jobs.Update(ctx, job, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := b.Delete(ctx, testRevision); err != nil {
		t.Fatal(err)
	}
	if _, err := jobs.Get(ctx, job.Name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("managed Job should be deleted: %v", err)
	}
	for _, action := range client.Actions() {
		deletion, ok := action.(ktesting.DeleteAction)
		if !ok {
			continue
		}
		opts := deletion.GetDeleteOptions()
		if opts.Preconditions == nil || (opts.Preconditions.UID == nil || *opts.Preconditions.UID != job.UID) ||
			(opts.PropagationPolicy == nil || *opts.PropagationPolicy != metav1.DeletePropagationForeground) {
			t.Fatalf("unexpected deletion options: %#v", opts)
		}
	}
}
