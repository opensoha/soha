package hostedbuild

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestManagedImageRegistryDeletion(t *testing.T) {
	t.Parallel()
	for _, item := range []struct {
		name   string
		status int
		body   string
		ok     bool
	}{
		{name: "accepted", status: http.StatusAccepted, ok: true},
		{name: "already-deleted", status: http.StatusNotFound, body: `{"errors":[{"code":"MANIFEST_UNKNOWN"}]}`, ok: true},
		{name: "missing-repository", status: http.StatusNotFound, body: `{"errors":[{"code":"NAME_UNKNOWN"}]}`, ok: true},
		{name: "unverified-not-found", status: http.StatusNotFound, body: "not a registry"},
		{name: "method-disabled", status: http.StatusMethodNotAllowed},
		{name: "unauthorized", status: http.StatusUnauthorized},
		{name: "transient-error", status: http.StatusBadGateway},
		{name: "redirect", status: http.StatusTemporaryRedirect},
	} {
		t.Run(item.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				calls.Add(1)
				if req.Method != http.MethodDelete || req.URL.Path != "/v2/platform/hosted/"+jobName(testRevision)+"/manifests/"+testDigest ||
					req.Header.Get("Authorization") != "Bearer provisioned-credential" {
					t.Errorf("unexpected registry call %s %s", req.Method, req.URL.Path)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.Header().Set("Location", "/never-follow")
				w.WriteHeader(item.status)
				_, _ = fmt.Fprint(w, item.body)
			}))
			defer server.Close()
			cfg := testConfig()
			cfg.RegistryRepository = strings.TrimPrefix(server.URL, "https://") + "/platform/hosted"
			builder, err := New(fake.NewSimpleClientset(), cfg, WithRegistryImageCleanup("Bearer provisioned-credential"))
			require.NoError(t, err)
			builder.registryCleanup.client.Transport = server.Client().Transport
			managed := builder.managedImageRepository(testRevision) + "@" + testDigest
			for _, ref := range []string{cfg.RegistryRepository + "@" + testDigest, managed + "/traversal", "other.example/" + managed, strings.Replace(managed, "@", ":tag@", 1)} {
				require.Error(t, builder.DeleteManagedImage(t.Context(), testRevision, ref))
			}
			err = builder.DeleteManagedImage(t.Context(), testRevision, managed)
			if item.ok {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, fmt.Sprint(item.status))
				require.NotContains(t, err.Error(), "provisioned-credential")
			}
			require.Equal(t, int32(1), calls.Load())
		})
	}
}

func TestManagedBuildResultRequiresOwnedPodAndDestination(t *testing.T) {
	t.Parallel()
	b, client := testBuilder(t)
	ctx := t.Context()
	spec := Spec{RevisionID: testRevision, Template: "node", SourceURL: "https://objects.example:8443/source?token=x"}
	require.NoError(t, WithRegistryImageCleanup("Basic dXNlcjpwYXNz")(b))
	require.NoError(t, b.Prepare(ctx, spec))
	jobs := client.BatchV1().Jobs(b.cfg.Namespace)
	job, err := jobs.Get(ctx, jobName(testRevision), metav1.GetOptions{})
	require.NoError(t, err)
	repository := b.managedImageRepository(testRevision)
	require.Equal(t, repository, job.Annotations[repoAnnotation])
	require.Equal(t, repository+":"+job.Name, job.Spec.Template.Spec.Containers[0].Args[4])
	image := repository + "@" + testDigest
	before, err := b.ManagedImageForCleanup(ctx, testRevision)
	require.NoError(t, err)
	require.Empty(t, before)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "build-pod", Namespace: b.cfg.Namespace,
			Labels:          map[string]string{batchv1.ControllerUidLabel: string(job.UID)},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(job, batchv1.SchemeGroupVersion.WithKind("Job"))},
		},
		Spec: *job.Spec.Template.Spec.DeepCopy(),
		Status: corev1.PodStatus{
			Phase: corev1.PodSucceeded,
			ContainerStatuses: []corev1.ContainerStatus{
				{Name: buildContainer, State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Message: testDigest}}},
			},
		},
	}
	verifiedDestination := pod.Spec.Containers[0].Args[4]
	pod.Spec.Containers[0].Args[4] = b.cfg.RegistryRepository + ":external"
	pods := client.CoreV1().Pods(b.cfg.Namespace)
	_, err = pods.Create(ctx, pod, metav1.CreateOptions{})
	require.NoError(t, err)
	unverified, err := b.ManagedImageForCleanup(ctx, testRevision)
	require.NoError(t, err)
	require.Empty(t, unverified)
	pod.Spec.Containers[0].Args[4] = verifiedDestination
	_, err = pods.Update(ctx, pod, metav1.UpdateOptions{})
	require.NoError(t, err)
	verified, err := b.ManagedImageForCleanup(ctx, testRevision)
	require.NoError(t, err)
	require.Equal(t, image, verified)
	setJobCondition(t, ctx, b, client, job, batchv1.JobComplete)
	result, ready, err := b.Result(ctx, testRevision)
	require.NoError(t, err)
	require.True(t, ready)
	require.Equal(t, image, result)
}
