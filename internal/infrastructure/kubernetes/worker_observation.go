package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
)

func (m *Manager) ObserveWorker(ctx context.Context, pool domain.WorkerPool, operationID string) (domain.WorkerObservation, error) {
	result := domain.WorkerObservation{NodeName: domain.WorkerNodeName(operationID), ObservedAt: time.Now().UTC()}
	result.ValidUntil = result.ObservedAt.Add(30 * time.Second)
	bundle, err := m.checkWorkerCluster(ctx, pool)
	if err != nil {
		return result, err
	}
	node, err := bundle.Typed.CoreV1().Nodes().Get(ctx, result.NodeName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		result.Reason = "worker has not registered"
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("%w: worker node observation is unavailable", apperrors.ErrConflict)
	}
	if err := workerNodeIdentity(node, pool, operationID); err != nil {
		return result, err
	}
	result.NodeUID = string(node.UID)
	if reason := workerNodeReadiness(node, pool); reason != "" {
		result.Reason = reason
		return result, nil
	}
	if !workerHeartbeatFresh(ctx, bundle, node) {
		result.Reason = "worker heartbeat is absent, stale or belongs to another node"
		return result, nil
	}
	for _, ref := range pool.Spec.RequiredDaemonSets {
		daemon, err := bundle.Typed.AppsV1().DaemonSets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil || !workerDaemonCurrent(daemon, pool.Identity.DaemonSetUIDs[ref.Namespace+"/"+ref.Name]) {
			result.Reason = "required node daemon identity or rollout is not current"
			return result, nil
		}
		selector, err := metav1.LabelSelectorAsSelector(daemon.Spec.Selector)
		if err != nil {
			return result, err
		}
		revisions, err := bundle.Typed.AppsV1().ControllerRevisions(ref.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String(), Limit: 200})
		if err != nil || revisions.Continue != "" {
			result.Reason = "required node daemon revision is unavailable"
			return result, nil
		}
		hash := workerDaemonRevision(daemon, revisions.Items)
		pods, err := bundle.Typed.CoreV1().Pods(ref.Namespace).List(ctx, metav1.ListOptions{FieldSelector: fields.OneTermEqualSelector("spec.nodeName", node.Name).String(), LabelSelector: selector.String(), Limit: 200})
		if err != nil || pods.Continue != "" || !workerDaemonPodReady(pods.Items, node.Name, string(daemon.UID), hash, daemon.Spec.MinReadySeconds) {
			result.Reason = "required network or storage daemon is not ready on this worker"
			return result, nil
		}
	}
	result.Ready, result.Reason = true, "original worker is Ready, schedulable, recently heartbeating and its required node daemons are ready"
	return result, nil
}

func workerHeartbeatFresh(ctx context.Context, bundle *Bundle, node *corev1.Node) bool {
	lease, err := bundle.Typed.CoordinationV1().Leases("kube-node-lease").Get(ctx, node.Name, metav1.GetOptions{})
	return err == nil && lease.Spec.RenewTime != nil && lease.Spec.HolderIdentity != nil && *lease.Spec.HolderIdentity == node.Name && ownedByUID(lease.OwnerReferences, string(node.UID)) && time.Since(lease.Spec.RenewTime.Time) <= 60*time.Second && time.Until(lease.Spec.RenewTime.Time) <= 5*time.Second
}

func workerNodeIdentity(node *corev1.Node, pool domain.WorkerPool, operationID string) error {
	if node.Name != domain.WorkerNodeName(operationID) || node.UID == "" || node.DeletionTimestamp != nil || !strings.EqualFold(node.Status.NodeInfo.SystemUUID, operationID) {
		return fmt.Errorf("%w: Kubernetes worker does not match the original VM identity", apperrors.ErrConflict)
	}
	if owner := node.Annotations[workerOperationAnnotation]; owner != "" && owner != operationID {
		return fmt.Errorf("%w: worker belongs to another operation", apperrors.ErrConflict)
	}
	if owner := node.Labels["soha.io/worker-pool-id"]; owner != "" && owner != pool.ID.String() {
		return fmt.Errorf("%w: worker belongs to another pool", apperrors.ErrConflict)
	}
	return nil
}

func workerNodeReadiness(node *corev1.Node, pool domain.WorkerPool) string {
	if node.Spec.Unschedulable {
		return "worker is cordoned"
	}
	for _, taint := range node.Spec.Taints {
		if taint.Effect == corev1.TaintEffectNoSchedule || taint.Effect == corev1.TaintEffectNoExecute {
			return "worker has a scheduling-blocking taint"
		}
	}
	ready := false
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			ready = condition.Status == corev1.ConditionTrue
		}
		if (condition.Type == corev1.NodeNetworkUnavailable || condition.Type == corev1.NodeMemoryPressure || condition.Type == corev1.NodeDiskPressure || condition.Type == corev1.NodePIDPressure) && condition.Status != corev1.ConditionFalse {
			return "worker network or pressure conditions are not healthy"
		}
	}
	if !ready {
		return "worker Ready condition is not true"
	}
	if !workerRuntimeMatches(node.Status.NodeInfo, pool.Spec.KubernetesVersion) {
		return "worker runtime does not match its registered image profile"
	}
	if !workerHasAllocatableResources(node.Status.Allocatable) {
		return "worker has no verified allocatable CPU, memory or pod slots"
	}
	if node.Annotations[workerOperationAnnotation] == "" || node.Labels["soha.io/worker-pool-id"] != pool.ID.String() {
		return "worker ownership has not been recorded"
	}
	for key, value := range pool.Spec.Labels {
		if node.Labels[key] != value {
			return "worker labels do not match pool constraints"
		}
	}
	return ""
}

func workerHasAllocatableResources(resources corev1.ResourceList) bool {
	return resources.Cpu().Sign() > 0 && resources.Memory().Sign() > 0 && resources.Pods().Sign() > 0
}

func workerRuntimeMatches(info corev1.NodeSystemInfo, kubernetesVersion string) bool {
	return info.Architecture == "amd64" && info.OperatingSystem == "linux" && strings.HasPrefix(info.OSImage, "Ubuntu 24.04") && info.KubeletVersion == kubernetesVersion && strings.HasPrefix(info.ContainerRuntimeVersion, "containerd://")
}

func ownedByUID(owners []metav1.OwnerReference, uid string) bool {
	for _, owner := range owners {
		if uid != "" && string(owner.UID) == uid {
			return true
		}
	}
	return false
}

func workerDaemonCurrent(daemon *appsv1.DaemonSet, uid string) bool {
	return daemon != nil && uid != "" && string(daemon.UID) == uid && daemon.DeletionTimestamp == nil && daemon.Status.ObservedGeneration >= daemon.Generation && daemon.Status.DesiredNumberScheduled > 0 && daemon.Status.UpdatedNumberScheduled == daemon.Status.DesiredNumberScheduled
}

func workerDaemonRevision(daemon *appsv1.DaemonSet, revisions []appsv1.ControllerRevision) string {
	var latest int64
	hash := ""
	for _, revision := range revisions {
		owner := metav1.GetControllerOf(&revision)
		if owner == nil || owner.UID != daemon.UID || revision.Revision < latest {
			continue
		}
		var data struct {
			Spec struct{ Template corev1.PodTemplateSpec }
		}
		if json.Unmarshal(revision.Data.Raw, &data) != nil || !equality.Semantic.DeepEqual(data.Spec.Template, daemon.Spec.Template) {
			continue
		}
		latest, hash = revision.Revision, revision.Labels[appsv1.DefaultDaemonSetUniqueLabelKey]
	}
	return hash
}

func workerDaemonPodReady(pods []corev1.Pod, node, daemonUID, hash string, minReadySeconds int32) bool {
	if hash == "" {
		return false
	}
	for _, pod := range pods {
		owner := metav1.GetControllerOf(&pod)
		if pod.Spec.NodeName != node || pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning || owner == nil || string(owner.UID) != daemonUID || pod.Labels[appsv1.DefaultDaemonSetUniqueLabelKey] != hash {
			continue
		}
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue && !condition.LastTransitionTime.IsZero() && time.Since(condition.LastTransitionTime.Time) >= time.Duration(minReadySeconds)*time.Second {
				return true
			}
		}
	}
	return false
}

// Called only by the authorized VM worker after the provider has returned the
// owned VM. The separate assessment path never adds labels or modifies nodes.
func (m *Manager) ClaimWorkerNode(ctx context.Context, pool domain.WorkerPool, operationID string) error {
	bundle, err := m.checkWorkerCluster(ctx, pool)
	if err != nil {
		return err
	}
	nodes := bundle.Typed.CoreV1().Nodes()
	node, err := nodes.Get(ctx, domain.WorkerNodeName(operationID), metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err := workerNodeIdentity(node, pool, operationID); err != nil {
		return err
	}
	if node.Annotations == nil {
		node.Annotations = map[string]string{}
	}
	if node.Labels == nil {
		node.Labels = map[string]string{}
	}
	changed := node.Annotations[workerOperationAnnotation] != operationID || node.Labels["soha.io/worker-pool-id"] != pool.ID.String()
	node.Annotations[workerOperationAnnotation], node.Labels["soha.io/worker-pool-id"] = operationID, pool.ID.String()
	for key, value := range pool.Spec.Labels {
		changed = changed || node.Labels[key] != value
		node.Labels[key] = value
	}
	if !changed {
		return nil
	}
	_, err = nodes.Update(ctx, node, metav1.UpdateOptions{}) // Kubernetes resourceVersion rejects concurrent changes.
	return err
}
