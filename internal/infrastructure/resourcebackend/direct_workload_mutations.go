package resourcebackend

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (d *Direct) RestartDeployment(ctx context.Context, clusterID, namespace, name string) error {
	bundle, queryCtx, cancel, err := d.workloadQueryContext(ctx, clusterID)
	if err != nil {
		return err
	}
	defer cancel()
	item, err := bundle.Typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	setRestartAnnotation(&item.Spec.Template.Annotations)
	_, err = bundle.Typed.AppsV1().Deployments(namespace).Update(queryCtx, item, metav1.UpdateOptions{})
	return err
}

func (d *Direct) ScaleDeployment(ctx context.Context, clusterID, namespace, name string, replicas int32) error {
	bundle, queryCtx, cancel, err := d.workloadQueryContext(ctx, clusterID)
	if err != nil {
		return err
	}
	defer cancel()
	item, err := bundle.Typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	item.Spec.Replicas = &replicas
	_, err = bundle.Typed.AppsV1().Deployments(namespace).Update(queryCtx, item, metav1.UpdateOptions{})
	return err
}

func (d *Direct) RestartStatefulSet(ctx context.Context, clusterID, namespace, name string) error {
	bundle, queryCtx, cancel, err := d.workloadQueryContext(ctx, clusterID)
	if err != nil {
		return err
	}
	defer cancel()
	item, err := bundle.Typed.AppsV1().StatefulSets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	setRestartAnnotation(&item.Spec.Template.Annotations)
	_, err = bundle.Typed.AppsV1().StatefulSets(namespace).Update(queryCtx, item, metav1.UpdateOptions{})
	return err
}

func (d *Direct) ScaleStatefulSet(ctx context.Context, clusterID, namespace, name string, replicas int32) error {
	bundle, queryCtx, cancel, err := d.workloadQueryContext(ctx, clusterID)
	if err != nil {
		return err
	}
	defer cancel()
	item, err := bundle.Typed.AppsV1().StatefulSets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	item.Spec.Replicas = &replicas
	_, err = bundle.Typed.AppsV1().StatefulSets(namespace).Update(queryCtx, item, metav1.UpdateOptions{})
	return err
}

func (d *Direct) RestartDaemonSet(ctx context.Context, clusterID, namespace, name string) error {
	bundle, queryCtx, cancel, err := d.workloadQueryContext(ctx, clusterID)
	if err != nil {
		return err
	}
	defer cancel()
	item, err := bundle.Typed.AppsV1().DaemonSets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	setRestartAnnotation(&item.Spec.Template.Annotations)
	_, err = bundle.Typed.AppsV1().DaemonSets(namespace).Update(queryCtx, item, metav1.UpdateOptions{})
	return err
}

func setRestartAnnotation(annotations *map[string]string) {
	if *annotations == nil {
		*annotations = make(map[string]string)
	}
	(*annotations)["kubectl.kubernetes.io/restartedAt"] = time.Now().UTC().Format(time.RFC3339)
}
