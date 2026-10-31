package runtime

import (
	"context"
	"fmt"
	"time"

	contractresource "github.com/opensoha/soha-contracts/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
)

func ApplyConditionalWorkloadAction(ctx context.Context, client dynamic.Interface, namespace, kind, name, action string, input contractresource.ConditionalWorkloadActionInput, now time.Time) (contractresource.ConditionalWorkloadActionReceipt, error) {
	definition, ok := contractresource.LookupKubernetesResourceKind(kind)
	if !ok {
		return contractresource.ConditionalWorkloadActionReceipt{}, apierrors.NewBadRequest("unsupported workload kind")
	}
	kind = definition.Kind
	if err := input.Validate(kind, action); err != nil {
		return contractresource.ConditionalWorkloadActionReceipt{}, apierrors.NewBadRequest(err.Error())
	}
	if client == nil || len(validation.IsDNS1123Label(namespace)) > 0 || len(validation.IsDNS1123Subdomain(name)) > 0 {
		return contractresource.ConditionalWorkloadActionReceipt{}, apierrors.NewBadRequest("a valid namespace and workload name are required")
	}
	resource := client.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: definition.Resource}).Namespace(namespace)
	current, err := resource.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return contractresource.ConditionalWorkloadActionReceipt{}, err
	}
	if current.GetName() != name || current.GetNamespace() != namespace || current.GetKind() != kind || current.GetAPIVersion() != "apps/v1" {
		return contractresource.ConditionalWorkloadActionReceipt{}, apierrors.NewBadRequest("workload response target does not match the request")
	}
	if err := ValidateResourceMutationPreconditions(current, input.Preconditions); err != nil {
		return contractresource.ConditionalWorkloadActionReceipt{}, err
	}
	if err := ValidateDirectManifestOwner(current); err != nil {
		return contractresource.ConditionalWorkloadActionReceipt{}, err
	}
	desired := current.DeepCopy()
	if err := prepareConditionalWorkloadAction(ctx, client, current, desired, action, input, now); err != nil {
		return contractresource.ConditionalWorkloadActionReceipt{}, err
	}
	options := metav1.UpdateOptions{}
	if input.DryRun {
		options.DryRun = []string{metav1.DryRunAll}
	}
	updated, err := resource.Update(ctx, desired, options)
	if err != nil {
		return contractresource.ConditionalWorkloadActionReceipt{}, err
	}
	if updated == nil || updated.GetUID() != current.GetUID() || updated.GetResourceVersion() == "" || updated.GetName() != name || updated.GetNamespace() != namespace {
		return contractresource.ConditionalWorkloadActionReceipt{}, apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: definition.Resource}, name, fmt.Errorf("update response did not confirm the original workload identity; inspect before retrying"))
	}
	return contractresource.ConditionalWorkloadActionReceipt{Applied: !input.DryRun, DryRun: input.DryRun, Kind: kind, Name: name, Namespace: namespace, Action: action, UID: string(updated.GetUID()), ResourceVersion: updated.GetResourceVersion(), Preconditions: input.Preconditions}, nil
}

func prepareConditionalWorkloadAction(ctx context.Context, client dynamic.Interface, current, desired *unstructured.Unstructured, action string, input contractresource.ConditionalWorkloadActionInput, now time.Time) error {
	switch action {
	case "restart":
		annotations, _, err := unstructured.NestedStringMap(desired.Object, "spec", "template", "metadata", "annotations")
		if err != nil {
			return apierrors.NewBadRequest("workload template annotations are invalid")
		}
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations["kubectl.kubernetes.io/restartedAt"] = now.UTC().Format(time.RFC3339Nano)
		return unstructured.SetNestedStringMap(desired.Object, annotations, "spec", "template", "metadata", "annotations")
	case "scale":
		return unstructured.SetNestedField(desired.Object, int64(*input.Replicas), "spec", "replicas")
	case "rollback":
		template, err := conditionalRollbackTemplate(ctx, client, current, input.Revision)
		if err != nil {
			return err
		}
		return unstructured.SetNestedMap(desired.Object, template, "spec", "template")
	default:
		return apierrors.NewBadRequest("unsupported workload action")
	}
}

func conditionalRollbackTemplate(ctx context.Context, client dynamic.Interface, current *unstructured.Unstructured, revision string) (map[string]any, error) {
	selectorMap, found, err := unstructured.NestedMap(current.Object, "spec", "selector")
	if err != nil || !found {
		return nil, apierrors.NewBadRequest("deployment selector is unavailable")
	}
	var selector metav1.LabelSelector
	if err := k8sruntime.DefaultUnstructuredConverter.FromUnstructured(selectorMap, &selector); err != nil {
		return nil, apierrors.NewBadRequest("deployment selector is invalid")
	}
	match, err := metav1.LabelSelectorAsSelector(&selector)
	if err != nil || match.Empty() {
		return nil, apierrors.NewBadRequest("deployment selector is empty or invalid")
	}
	resource := client.Resource(schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}).Namespace(current.GetNamespace())
	var selected map[string]any
	continuation := ""
	for page := 0; page < 100; page++ {
		items, err := resource.List(ctx, metav1.ListOptions{LabelSelector: match.String(), Limit: 200, Continue: continuation})
		if err != nil {
			return nil, err
		}
		for index := range items.Items {
			item := &items.Items[index]
			if item.GetAnnotations()["deployment.kubernetes.io/revision"] != revision || !controlledByOriginalDeployment(item, current) {
				continue
			}
			if selected != nil {
				return nil, apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, current.GetName(), fmt.Errorf("revision matches more than one owned ReplicaSet"))
			}
			template, found, err := unstructured.NestedMap(item.Object, "spec", "template")
			if err != nil || !found {
				return nil, apierrors.NewBadRequest("revision template is unavailable")
			}
			unstructured.RemoveNestedField(template, "metadata", "labels", "pod-template-hash")
			selected = template
		}
		continuation = items.GetContinue()
		if continuation == "" {
			if selected == nil {
				return nil, apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "replicasets"}, "requested-revision")
			}
			return selected, nil
		}
	}
	return nil, apierrors.NewBadRequest("revision history exceeds the bounded lookup; narrow or repair the workload history")
}

func controlledByOriginalDeployment(item, current *unstructured.Unstructured) bool {
	for _, owner := range item.GetOwnerReferences() {
		if owner.Controller != nil && *owner.Controller && owner.Kind == "Deployment" && owner.APIVersion == "apps/v1" && owner.UID == current.GetUID() {
			return true
		}
	}
	return false
}
