package releasebackend

import (
	"context"
	"errors"
	"testing"
	"time"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type fakeReleaseClusterManager struct {
	bundle   *k8sinfra.Bundle
	bundleFn func(context.Context, string) (*k8sinfra.Bundle, error)
}

func (m fakeReleaseClusterManager) Metadata(clusterID string) (domaincluster.Summary, error) {
	return domaincluster.Summary{ID: clusterID}, nil
}

func (m fakeReleaseClusterManager) Bundle(ctx context.Context, clusterID string) (*k8sinfra.Bundle, error) {
	if m.bundleFn != nil {
		return m.bundleFn(ctx, clusterID)
	}
	return m.bundle, nil
}

func TestDirectRuntimeUpdateDeploymentImage(t *testing.T) {
	for _, test := range []struct {
		name          string
		containerName string
		wantName      string
		wantPrevious  string
	}{
		{name: "default first container", wantName: "app", wantPrevious: "app:v1"},
		{name: "named container", containerName: "sidecar", wantName: "sidecar", wantPrevious: "sidecar:v1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(testDeployment())
			runtime := NewDirectRuntime(fakeReleaseClusterManager{bundle: &k8sinfra.Bundle{Typed: client}})

			name, previous, err := runtime.UpdateDeploymentImage(context.Background(), "cluster-a", "apps", "api", test.containerName, "release:v2")
			if err != nil {
				t.Fatalf("UpdateDeploymentImage() error = %v", err)
			}
			if name != test.wantName || previous != test.wantPrevious {
				t.Fatalf("UpdateDeploymentImage() = %q/%q, want %q/%q", name, previous, test.wantName, test.wantPrevious)
			}
			deployment, err := client.AppsV1().Deployments("apps").Get(context.Background(), "api", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			for _, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == test.wantName && container.Image != "release:v2" {
					t.Fatalf("container image = %q, want release:v2", container.Image)
				}
			}
		})
	}
}

func TestDirectRuntimeUpdateDeploymentImageReturnsNotFound(t *testing.T) {
	runtime := NewDirectRuntime(fakeReleaseClusterManager{bundle: &k8sinfra.Bundle{Typed: fake.NewSimpleClientset()}})
	_, _, err := runtime.UpdateDeploymentImage(context.Background(), "cluster-a", "apps", "missing", "", "release:v2")
	if !k8serrors.IsNotFound(err) {
		t.Fatalf("UpdateDeploymentImage() error = %v, want Kubernetes NotFound", err)
	}
}

func TestDirectRuntimeUpdateDeploymentImagePropagatesDeadline(t *testing.T) {
	runtime := NewDirectRuntime(fakeReleaseClusterManager{bundleFn: func(ctx context.Context, _ string) (*k8sinfra.Bundle, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	_, _, err := runtime.UpdateDeploymentImage(ctx, "cluster-a", "apps", "api", "", "release:v2")
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, apperrors.ErrClusterUnready) {
		t.Fatalf("UpdateDeploymentImage() error = %v, want deadline and ErrClusterUnready", err)
	}
}

func testDeployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "apps"},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{
			{Name: "app", Image: "app:v1"},
			{Name: "sidecar", Image: "sidecar:v1"},
		}}}},
	}
}
