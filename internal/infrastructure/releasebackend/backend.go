package releasebackend

import (
	"context"
	"fmt"
	"strings"
	"time"

	apprelease "github.com/opensoha/soha/internal/application/release"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DirectRuntime struct {
	manager clusterManager
}

type clusterManager interface {
	Metadata(string) (domaincluster.Summary, error)
	Bundle(context.Context, string) (*k8sinfra.Bundle, error)
}

func NewDirectRuntime(manager clusterManager) *DirectRuntime {
	return &DirectRuntime{manager: manager}
}

func (r *DirectRuntime) Metadata(clusterID string) (domaincluster.Summary, error) {
	return r.manager.Metadata(clusterID)
}

func (r *DirectRuntime) UpdateDeploymentImage(ctx context.Context, clusterID, namespace, name, containerName, image string) (string, string, error) {
	bundle, err := r.manager.Bundle(ctx, clusterID)
	if err != nil {
		return "", "", fmt.Errorf("%w: get Kubernetes bundle: %w", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	deployment, err := bundle.Typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return "", "", err
	}
	resolvedContainer, previousImage, err := mutateDeploymentImage(deployment, containerName, image)
	if err != nil {
		return "", "", err
	}
	if _, err := bundle.Typed.AppsV1().Deployments(namespace).Update(queryCtx, deployment, metav1.UpdateOptions{}); err != nil {
		return "", "", err
	}
	return resolvedContainer, previousImage, nil
}

func mutateDeploymentImage(deployment *appsv1.Deployment, containerName, image string) (string, string, error) {
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return "", "", fmt.Errorf("deployment has no containers")
	}
	if strings.TrimSpace(containerName) == "" {
		previous := deployment.Spec.Template.Spec.Containers[0].Image
		deployment.Spec.Template.Spec.Containers[0].Image = image
		return deployment.Spec.Template.Spec.Containers[0].Name, previous, nil
	}
	for index := range deployment.Spec.Template.Spec.Containers {
		if deployment.Spec.Template.Spec.Containers[index].Name == containerName {
			previous := deployment.Spec.Template.Spec.Containers[index].Image
			deployment.Spec.Template.Spec.Containers[index].Image = image
			return deployment.Spec.Template.Spec.Containers[index].Name, previous, nil
		}
	}
	return "", "", fmt.Errorf("container %s not found in deployment", containerName)
}

var _ apprelease.DirectRuntime = (*DirectRuntime)(nil)
