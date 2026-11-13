package kubernetes

import (
	"context"
	"io"
	"strings"
	"time"

	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
	contractruntime "github.com/opensoha/soha-contracts/resource/runtime"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubectl/pkg/drain"
)

func namespaceView(item corev1.Namespace) domainresource.NamespaceView {
	return domainresource.NamespaceView{Name: item.Name, Status: string(item.Status.Phase), Labels: item.Labels, Annotations: item.Annotations, AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
}
func (c *Client) CreateNamespace(ctx context.Context, input domainresource.NamespaceUpsertInput) (domainresource.NamespaceView, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return domainresource.NamespaceView{}, apierrors.NewBadRequest("namespace name is required")
	}
	item := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: input.Name, Labels: input.Labels, Annotations: input.Annotations}}
	if err := contractruntime.ValidateDirectManifestOwner(item); err != nil {
		return domainresource.NamespaceView{}, err
	}
	created, err := c.typed.CoreV1().Namespaces().Create(ctx, item, metav1.CreateOptions{})
	if err != nil {
		return domainresource.NamespaceView{}, err
	}
	return namespaceView(*created), nil
}
func (c *Client) UpdateNamespace(ctx context.Context, name string, input domainresource.NamespaceUpsertInput) (domainresource.NamespaceView, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if input.Name != "" && input.Name != name {
		return domainresource.NamespaceView{}, apierrors.NewBadRequest("namespace name cannot be changed")
	}
	item, err := c.typed.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.NamespaceView{}, err
	}
	if err := contractruntime.ValidateDirectManifestOwner(item); err != nil {
		return domainresource.NamespaceView{}, err
	}
	item.Labels, item.Annotations = input.Labels, input.Annotations
	if err := contractruntime.ValidateDirectManifestOwner(item); err != nil {
		return domainresource.NamespaceView{}, err
	}
	updated, err := c.typed.CoreV1().Namespaces().Update(ctx, item, metav1.UpdateOptions{})
	if err != nil {
		return domainresource.NamespaceView{}, err
	}
	return namespaceView(*updated), nil
}
func (c *Client) DeleteNamespace(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err := contractruntime.ValidateDirectManifestOwner(item); err != nil {
		return err
	}
	return c.typed.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &item.UID, ResourceVersion: &item.ResourceVersion}})
}
func (c *Client) UpdateNode(ctx context.Context, name string, input domainresource.NodeUpdateInput) (domainresource.NodeDetailView, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.NodeDetailView{}, err
	}
	if err := contractruntime.ValidateDirectManifestOwner(item); err != nil {
		return domainresource.NodeDetailView{}, err
	}
	item.Labels = input.Labels
	item.Spec.Taints = make([]corev1.Taint, 0, len(input.Taints))
	for _, taint := range input.Taints {
		if strings.TrimSpace(taint.Key) == "" {
			return domainresource.NodeDetailView{}, apierrors.NewBadRequest("taint key is required")
		}
		switch corev1.TaintEffect(taint.Effect) {
		case corev1.TaintEffectNoSchedule, corev1.TaintEffectPreferNoSchedule, corev1.TaintEffectNoExecute:
		default:
			return domainresource.NodeDetailView{}, apierrors.NewBadRequest("invalid taint effect")
		}
		item.Spec.Taints = append(item.Spec.Taints, corev1.Taint{Key: taint.Key, Value: taint.Value, Effect: corev1.TaintEffect(taint.Effect)})
	}
	if err := contractruntime.ValidateDirectManifestOwner(item); err != nil {
		return domainresource.NodeDetailView{}, err
	}
	if _, err := c.typed.CoreV1().Nodes().Update(ctx, item, metav1.UpdateOptions{}); err != nil {
		return domainresource.NodeDetailView{}, err
	}
	return c.GetNodeDetail(ctx, name)
}
func (c *Client) SetNodeUnschedulable(ctx context.Context, name string, unschedulable bool) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err := contractruntime.ValidateDirectManifestOwner(item); err != nil {
		return err
	}
	item.Spec.Unschedulable = unschedulable
	_, err = c.typed.CoreV1().Nodes().Update(ctx, item, metav1.UpdateOptions{})
	return err
}
func (c *Client) DrainNode(ctx context.Context, name string, input domainresource.NodeDrainInput) error {
	if input.TimeoutSeconds == 0 {
		input.TimeoutSeconds = 300
	}
	if input.TimeoutSeconds < 30 || input.TimeoutSeconds > 1800 {
		return apierrors.NewBadRequest("drain timeoutSeconds must be between 30 and 1800")
	}
	timeout := time.Duration(input.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	item, err := c.typed.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err := contractruntime.ValidateDirectManifestOwner(item); err != nil {
		return err
	}
	observed := contractruntime.NewDrainClient(c.typed)
	helper := &drain.Helper{Ctx: ctx, Client: observed, Force: input.Force, GracePeriodSeconds: -1, IgnoreAllDaemonSets: true, DeleteEmptyDirData: input.DeleteEmptyDirData, Timeout: timeout, EvictErrorRetryDelay: time.Second, Out: io.Discard, ErrOut: io.Discard}
	if !item.Spec.Unschedulable {
		item.Spec.Unschedulable = true
		if _, err := c.typed.CoreV1().Nodes().Update(ctx, item, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	pods, errs := helper.GetPodsForDeletion(name)
	if len(errs) > 0 {
		return observed.PodSelectionFailure(ctx, errs)
	}
	// Eviction deliberately handles controller-managed Pods; it is not a manifest edit.
	return observed.Failure(ctx, helper.DeleteOrEvictPods(pods.Pods()))
}
func (c *Client) SetCronJobSuspend(ctx context.Context, namespace, name string, suspend bool) (domainresource.CronJobDetailView, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.BatchV1().CronJobs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.CronJobDetailView{}, err
	}
	if err := contractruntime.ValidateDirectManifestOwner(item); err != nil {
		return domainresource.CronJobDetailView{}, err
	}
	item.Spec.Suspend = &suspend
	updated, err := c.typed.BatchV1().CronJobs(namespace).Update(ctx, item, metav1.UpdateOptions{})
	if err != nil {
		return domainresource.CronJobDetailView{}, err
	}
	return mapCronJobDetail(*updated), nil
}
