package kubernetes

import (
	"context"
	domainresource "github.com/opensoha/soha/contracts/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"time"
)

func (c *Client) ListDaemonSetRolloutHistory(ctx context.Context, namespace, name string) ([]domainresource.RolloutHistoryView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return listDaemonSetHistory(queryCtx, c.typed, namespace, name)
}
func (c *Client) GetDaemonSetRevisionYAML(ctx context.Context, namespace, name, revisionName string) (domainresource.ResourceYAMLView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return daemonSetRevisionYAML(queryCtx, c.typed, namespace, name, revisionName)
}
func listDaemonSetHistory(ctx context.Context, typed kubernetes.Interface, namespace, name string) ([]domainresource.RolloutHistoryView, error) {
	ds, err := typed.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	patch, err := daemonSetTemplatePatch(ds.Spec.Template)
	if err != nil {
		return nil, err
	}
	return listControllerHistory(ctx, typed, revisionOwner{kind: "DaemonSet", name: ds.Name, namespace: ds.Namespace, uid: ds.UID, templatePatch: patch})
}
func daemonSetRevisionYAML(ctx context.Context, typed kubernetes.Interface, namespace, name, revisionName string) (domainresource.ResourceYAMLView, error) {
	ds, err := typed.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return controllerRevisionYAML(ctx, typed, revisionOwner{kind: "DaemonSet", name: ds.Name, namespace: ds.Namespace, uid: ds.UID}, revisionName)
}
