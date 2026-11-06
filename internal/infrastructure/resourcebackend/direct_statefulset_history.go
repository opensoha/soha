package resourcebackend

import (
	"context"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"time"
)

func (d *Direct) ListStatefulSetRolloutHistory(ctx context.Context, clusterID, namespace, name string) ([]domainresource.RolloutHistoryView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return listStatefulSetHistory(queryCtx, bundle.Typed, namespace, name)
}
func (d *Direct) GetStatefulSetRevisionYAML(ctx context.Context, clusterID, namespace, name, revisionName string) (domainresource.ResourceYAMLView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return statefulSetRevisionYAML(queryCtx, bundle.Typed, namespace, name, revisionName)
}
func listStatefulSetHistory(ctx context.Context, typed kubernetes.Interface, namespace, name string) ([]domainresource.RolloutHistoryView, error) {
	sts, err := typed.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return listControllerHistory(ctx, typed, revisionOwner{kind: "StatefulSet", name: sts.Name, namespace: sts.Namespace, uid: sts.UID})
}
func statefulSetRevisionYAML(ctx context.Context, typed kubernetes.Interface, namespace, name, revisionName string) (domainresource.ResourceYAMLView, error) {
	sts, err := typed.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return controllerRevisionYAML(ctx, typed, revisionOwner{kind: "StatefulSet", name: sts.Name, namespace: sts.Namespace, uid: sts.UID}, revisionName)
}
