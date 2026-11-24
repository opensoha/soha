package resourcebackend

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"

	cr "github.com/opensoha/soha/contracts/resource"
	dr "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

type deliveryController struct {
	metav1.Object
	selector *metav1.LabelSelector
}

type deliveryPodListsKey struct{}

type deliveryPodListKey struct {
	client              *kubernetes.Clientset
	namespace, selector string
}

// WithDeliveryPodLists shares identical selector reads within one observation.
// Controller identities are still read live for every target.
func (*Direct) WithDeliveryPodLists(ctx context.Context) context.Context {
	return context.WithValue(ctx, deliveryPodListsKey{}, &sync.Map{})
}

func (d *Direct) ListDeliveryControllerPods(ctx context.Context, ref dr.ResourceRef) ([]dr.PodView, error) {
	bundle, err := d.directClients(ctx, ref.ClusterID)
	if err != nil {
		return nil, err
	}
	return listDeliveryControllerPods(ctx, bundle.Typed, ref)
}

func listDeliveryControllerPods(ctx context.Context, client kubernetes.Interface, ref dr.ResourceRef) ([]dr.PodView, error) {
	root, err := getDeliveryController(ctx, client, ref)
	if err != nil {
		return nil, deliveryKubeReadError(err)
	}
	if root.GetName() != ref.Name || root.GetNamespace() != ref.Namespace || root.GetUID() == "" || (ref.UID != "" && string(root.GetUID()) != ref.UID) {
		return nil, apperrors.ErrConflict
	}
	ref.UID = string(root.GetUID())
	var pods []dr.PodView
	if ref.Kind == "CronJob" {
		pods, err = listDeliveryCronJobPods(ctx, client, ref)
	} else {
		pods, err = listDeliverySelectedPods(ctx, client, ref, root.selector)
	}
	current, verifyErr := getDeliveryController(ctx, client, ref)
	if verifyErr != nil {
		return nil, deliveryKubeReadError(verifyErr)
	}
	if current.GetUID() != root.GetUID() || current.GetName() != ref.Name || current.GetNamespace() != ref.Namespace {
		return nil, apperrors.ErrConflict
	}
	return pods, deliveryKubeReadError(err)
}

func getDeliveryController(ctx context.Context, client kubernetes.Interface, ref dr.ResourceRef) (deliveryController, error) {
	ns, name := ref.Namespace, ref.Name
	options := metav1.GetOptions{}
	switch ref.Kind {
	case "Deployment":
		obj, err := client.AppsV1().Deployments(ns).Get(ctx, name, options)
		return controllerWithSelector(obj, err, func() *metav1.LabelSelector { return obj.Spec.Selector })
	case "StatefulSet":
		obj, err := client.AppsV1().StatefulSets(ns).Get(ctx, name, options)
		return controllerWithSelector(obj, err, func() *metav1.LabelSelector { return obj.Spec.Selector })
	case "DaemonSet":
		obj, err := client.AppsV1().DaemonSets(ns).Get(ctx, name, options)
		return controllerWithSelector(obj, err, func() *metav1.LabelSelector { return obj.Spec.Selector })
	case "Job":
		obj, err := client.BatchV1().Jobs(ns).Get(ctx, name, options)
		return controllerWithSelector(obj, err, func() *metav1.LabelSelector { return deliveryJobSelector(obj) })
	case "CronJob":
		obj, err := client.BatchV1().CronJobs(ns).Get(ctx, name, options)
		return deliveryController{Object: obj}, err
	default:
		return deliveryController{}, apperrors.ErrUnsupportedOperation
	}
}

func controllerWithSelector(obj metav1.Object, err error, selector func() *metav1.LabelSelector) (deliveryController, error) {
	if err != nil {
		return deliveryController{}, err
	}
	return deliveryController{Object: obj, selector: selector()}, nil
}

func deliveryJobSelector(job *batchv1.Job) *metav1.LabelSelector {
	if job.Spec.Selector != nil {
		return job.Spec.Selector
	}
	return &metav1.LabelSelector{MatchLabels: map[string]string{"batch.kubernetes.io/job-name": job.Name}}
}

func listDeliverySelectedPods(ctx context.Context, client kubernetes.Interface, ref dr.ResourceRef, spec *metav1.LabelSelector) ([]dr.PodView, error) {
	selector, err := metav1.LabelSelectorAsSelector(spec)
	if err != nil || spec == nil || selector.Empty() {
		return nil, apperrors.ErrUnsupportedOperation
	}
	pods, err := readDeliverySelectedPods(ctx, client, ref.Namespace, selector.String())
	if err != nil {
		return nil, err
	}
	var coverageErr error
	if pods.Continue != "" || len(pods.Items) > cr.RelationMaxItems {
		coverageErr = dr.ErrDeliveryPodObservationLimit
	}
	replicaSets := make(map[metav1.OwnerReference]deliveryReplicaSetCheck)
	var result []dr.PodView
	for _, pod := range pods.Items[:min(len(pods.Items), cr.RelationMaxItems)] {
		if ctx.Err() != nil {
			return result, errors.Join(coverageErr, ctx.Err())
		}
		// Match again before trusting provider output and always require controller ownership.
		if pod.Namespace != ref.Namespace || !selector.Matches(labels.Set(pod.Labels)) {
			continue
		}
		owner := metav1.GetControllerOf(&pod)
		owned := ref.Kind != "Deployment" && deliveryOwnerMatches(owner, ref)
		if ref.Kind == "Deployment" && owner != nil && owner.Kind == "ReplicaSet" && owner.APIVersion == "apps/v1" {
			check := checkDeliveryReplicaSet(ctx, client, ref, *owner, replicaSets)
			owned = check.owned
			coverageErr = errors.Join(coverageErr, check.err)
		}
		if owned && pod.UID != "" {
			result = append(result, mapPodView(pod))
		} else if owned {
			coverageErr = errors.Join(coverageErr, dr.ErrDeliveryPodOwnershipUnverified)
		}
	}
	return result, coverageErr
}

func readDeliverySelectedPods(ctx context.Context, client kubernetes.Interface, namespace, selector string) (*corev1.PodList, error) {
	read := func() (*corev1.PodList, error) {
		return client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector, Limit: cr.RelationMaxItems})
	}
	cache, cached := ctx.Value(deliveryPodListsKey{}).(*sync.Map)
	typed, stableClient := client.(*kubernetes.Clientset)
	if !cached || !stableClient {
		return read()
	}
	key := deliveryPodListKey{client: typed, namespace: namespace, selector: selector}
	result, _ := cache.LoadOrStore(key, sync.OnceValues(read))
	load, valid := result.(func() (*corev1.PodList, error))
	if !valid {
		return read()
	}
	pods, err := load()
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	return pods, err
}

type deliveryReplicaSetCheck struct {
	owned bool
	err   error
}

func checkDeliveryReplicaSet(ctx context.Context, client kubernetes.Interface, root dr.ResourceRef, owner metav1.OwnerReference, cache map[metav1.OwnerReference]deliveryReplicaSetCheck) deliveryReplicaSetCheck {
	owner.Controller, owner.BlockOwnerDeletion = nil, nil
	if check, ok := cache[owner]; ok {
		return check
	}
	var check deliveryReplicaSetCheck
	rs, err := client.AppsV1().ReplicaSets(root.Namespace).Get(ctx, owner.Name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		check.err = dr.ErrDeliveryPodOwnershipUnverified
	case err != nil:
		check.err = err
	case rs.UID != owner.UID || rs.UID == "" || rs.Name != owner.Name || rs.Namespace != root.Namespace:
		check.err = dr.ErrDeliveryPodOwnershipUnverified
	default:
		check.owned = deliveryOwnerMatches(metav1.GetControllerOf(rs), root)
	}
	cache[owner] = check
	return check
}

func listDeliveryCronJobPods(ctx context.Context, client kubernetes.Interface, root dr.ResourceRef) ([]dr.PodView, error) {
	jobs, err := client.BatchV1().Jobs(root.Namespace).List(ctx, metav1.ListOptions{Limit: cr.RelationMaxItems})
	if err != nil {
		return nil, err
	}
	var coverageErr error
	if jobs.Continue != "" || len(jobs.Items) > cr.RelationMaxItems {
		coverageErr = dr.ErrDeliveryPodObservationLimit
	}
	var result []dr.PodView
	for _, job := range jobs.Items[:min(len(jobs.Items), cr.RelationMaxItems)] {
		if ctx.Err() != nil {
			return result, errors.Join(coverageErr, ctx.Err())
		}
		if job.UID == "" || job.Namespace != root.Namespace || !deliveryOwnerMatches(metav1.GetControllerOf(&job), root) {
			continue
		}
		ref := dr.ResourceRef{APIVersion: "batch/v1", Kind: "Job", Namespace: root.Namespace, Name: job.Name, UID: string(job.UID)}
		pods, err := listDeliverySelectedPods(ctx, client, ref, deliveryJobSelector(&job))
		coverageErr = errors.Join(coverageErr, err)
		space := cr.RelationMaxItems - len(result)
		result = append(result, pods[:min(space, len(pods))]...)
		if len(pods) > space || space == 0 {
			return result, errors.Join(coverageErr, dr.ErrDeliveryPodObservationLimit)
		}
	}
	return result, coverageErr
}

func deliveryOwnerMatches(owner *metav1.OwnerReference, ref dr.ResourceRef) bool {
	return owner != nil && ref.UID != "" && string(owner.UID) == ref.UID && owner.APIVersion == ref.APIVersion && owner.Kind == ref.Kind && owner.Name == ref.Name
}

func (d *Direct) GetDeliveryPodStatus(ctx context.Context, ref, controller dr.ResourceRef) (dr.PodView, error) {
	pod, err := d.getPod(ctx, ref.ClusterID, ref.Namespace, ref.Name)
	if err != nil {
		return dr.PodView{}, deliveryKubeReadError(err)
	}
	return deliveryPodStatus(*pod, ref, controller)
}

func (*Direct) DecodeDeliveryPodStatus(content string, ref, controller dr.ResourceRef) (dr.PodView, error) {
	var pod corev1.Pod
	if len(content) > 2<<20 || yaml.Unmarshal([]byte(content), &pod) != nil || pod.Kind != "Pod" || pod.APIVersion != "v1" {
		return dr.PodView{}, apperrors.ErrConflict
	}
	return deliveryPodStatus(pod, ref, controller)
}

func deliveryPodStatus(pod corev1.Pod, ref, controller dr.ResourceRef) (dr.PodView, error) {
	if ref.UID == "" || string(pod.UID) != ref.UID || pod.Namespace != ref.Namespace || pod.Name != ref.Name {
		return dr.PodView{}, apperrors.ErrConflict
	}
	owner := metav1.GetControllerOf(&pod)
	if owner == nil || deliveryAPIGroup(owner.APIVersion) != deliveryAPIGroup(controller.APIVersion) {
		return dr.PodView{}, dr.ErrDeliveryPodOwnershipUnverified
	}
	controller.APIVersion = owner.APIVersion
	if !deliveryOwnerMatches(owner, controller) {
		return dr.PodView{}, dr.ErrDeliveryPodOwnershipUnverified
	}
	return mapPodView(pod), nil
}

func deliveryAPIGroup(version string) string {
	if group, _, found := strings.Cut(version, "/"); found {
		return group
	}
	return ""
}

func deliveryKubeReadError(err error) error {
	var networkError *net.OpError
	var timeout net.Error
	switch {
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return errors.Join(apperrors.ErrAccessDenied, err)
	case apierrors.IsNotFound(err):
		return errors.Join(apperrors.ErrNotFound, err)
	case errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.As(err, &timeout) && timeout.Timeout():
		return errors.Join(context.DeadlineExceeded, err)
	case errors.As(err, &networkError) && networkError.Op == "dial":
		return errors.Join(apperrors.ErrClusterUnready, err)
	default:
		return err
	}
}
