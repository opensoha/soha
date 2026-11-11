package runtime

import (
	"context"
	"fmt"
	"time"

	contractresource "github.com/opensoha/soha-contracts/resource"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// ManifestHealth verifies the supported Operator's references with bounded,
// same-namespace reads. Healthy means its configuration is synchronized, not
// that a scheduled Job has executed successfully.
func ManifestHealth(ctx context.Context, client dynamic.Interface, object *unstructured.Unstructured) (string, error) {
	if object == nil {
		return "unknown", nil
	}
	if object.GetDeletionTimestamp() != nil || object.GetAPIVersion() != "workloads.soha.io/v1alpha1" || object.GetKind() != "WorkloadCronJob" {
		return contractresource.ManifestHealth(object.Object), nil
	}
	var item workloadCronJob
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(object.Object, &item); err != nil {
		return "unknown", fmt.Errorf("decode WorkloadCronJob health: %w", err)
	}
	if health := item.conditionHealth(); health != "healthy" {
		return health, nil
	}
	if client == nil || !item.validReferences() {
		return "unknown", nil
	}
	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	sourceResource := map[string]string{"Deployment": "deployments", "StatefulSet": "statefulsets", "DaemonSet": "daemonsets"}[item.Spec.SourceRef.Kind]
	source, err := client.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: sourceResource}).Namespace(item.Namespace).Get(queryCtx, item.Spec.SourceRef.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "degraded", nil
	}
	if err != nil {
		return "unknown", fmt.Errorf("observe WorkloadCronJob source: %w", err)
	}
	target, err := client.Resource(schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}).Namespace(item.Namespace).Get(queryCtx, item.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "progressing", nil
	}
	if err != nil {
		return "unknown", fmt.Errorf("observe WorkloadCronJob target: %w", err)
	}
	return item.relatedHealth(source, target), nil
}

type workloadCronJob struct {
	metav1.ObjectMeta `json:"metadata"`
	Spec              struct {
		SourceRef struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"sourceRef"`
	} `json:"spec"`
	Status struct {
		ObservedGeneration    int64                  `json:"observedGeneration"`
		SourceUID             string                 `json:"sourceUid"`
		SourceResourceVersion string                 `json:"sourceResourceVersion"`
		CronJobRef            corev1.ObjectReference `json:"cronJobRef"`
		Conditions            []metav1.Condition     `json:"conditions"`
	} `json:"status"`
}

func (item workloadCronJob) conditionHealth() string {
	if item.Generation < 1 || item.Status.ObservedGeneration != item.Generation {
		return "progressing"
	}
	for _, condition := range item.Status.Conditions {
		if condition.Type != "Ready" || condition.ObservedGeneration != item.Generation {
			continue
		}
		if condition.Status == metav1.ConditionFalse {
			return "degraded"
		}
		if condition.Status == metav1.ConditionTrue && condition.Reason == "Reconciled" {
			return "healthy"
		}
	}
	return "unknown"
}

func (item workloadCronJob) validReferences() bool {
	source := item.Spec.SourceRef
	target := item.Status.CronJobRef
	validKind := source.Kind == "Deployment" || source.Kind == "StatefulSet" || source.Kind == "DaemonSet"
	return validKind && item.UID != "" && item.Namespace != "" && item.Name != "" && source.Name != "" &&
		item.Status.SourceUID != "" && item.Status.SourceResourceVersion != "" && target.UID != "" && target.ResourceVersion != "" &&
		target.APIVersion == "batch/v1" && target.Kind == "CronJob" && target.Namespace == item.Namespace && target.Name == item.Name
}

func (item workloadCronJob) relatedHealth(source, target *unstructured.Unstructured) string {
	if source.GetDeletionTimestamp() != nil || target.GetDeletionTimestamp() != nil ||
		string(source.GetUID()) != item.Status.SourceUID || target.GetUID() != item.Status.CronJobRef.UID {
		return "degraded"
	}
	owner := metav1.GetControllerOf(target)
	if owner == nil || owner.UID != item.UID || owner.Name != item.Name || owner.APIVersion != "workloads.soha.io/v1alpha1" || owner.Kind != "WorkloadCronJob" {
		return "degraded"
	}
	if source.GetResourceVersion() != item.Status.SourceResourceVersion || target.GetResourceVersion() != item.Status.CronJobRef.ResourceVersion {
		return "progressing"
	}
	return "healthy"
}
