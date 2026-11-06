package kubernetes

import (
	"context"
	domainresource "github.com/opensoha/soha-contracts/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"time"
)

func (c *Client) ListStatefulSetRolloutHistory(ctx context.Context, namespace, name string) ([]domainresource.RolloutHistoryView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return listStatefulSetHistory(queryCtx, c.typed, namespace, name)
}
func (c *Client) GetStatefulSetRevisionYAML(ctx context.Context, namespace, name, revisionName string) (domainresource.ResourceYAMLView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return statefulSetRevisionYAML(queryCtx, c.typed, namespace, name, revisionName)
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
