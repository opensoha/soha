package runtime

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// ValidateArgoResources bounds the controller's writes to the resources rendered
// from the same immutable source used by the delivery plan.
func ValidateArgoResources(application *unstructured.Unstructured, resources []*unstructured.Unstructured) error {
	if err := ValidateArgoApplication(application); err != nil {
		return err
	}
	if len(resources) == 0 || len(resources) > 49 {
		return fmt.Errorf("GitOps requires 1 to 49 frozen child resources")
	}
	images, _, _ := unstructured.NestedStringSlice(application.Object, "spec", "source", "kustomize", "images")
	expectedImages := map[string]bool{}
	for _, image := range images {
		_, image, _ = ParseArgoImageOverride(image)
		expectedImages[image] = false
	}
	seen := map[string]bool{}
	for _, resource := range resources {
		if resource == nil {
			return fmt.Errorf("GitOps child resource is missing")
		}
		gvk := resource.GroupVersionKind()
		gvr, supported := argoResourceTypes[gvk.Group+"/"+gvk.Kind]
		key := gvk.Group + "/" + gvk.Kind + "/" + resource.GetName()
		if !supported || gvk.Version != gvr.Version || resource.GetNamespace() != application.GetNamespace() || resource.GetName() == "" || seen[key] {
			return fmt.Errorf("GitOps child resource is unsupported, repeated or outside the target namespace")
		}
		seen[key] = true
		if resource.GetUID() != "" || resource.GetResourceVersion() != "" || resource.GetGenerateName() != "" || resource.GetDeletionTimestamp() != nil || len(resource.GetOwnerReferences()) != 0 || len(resource.GetFinalizers()) != 0 || resource.Object["status"] != nil {
			return fmt.Errorf("GitOps child resources must be independent desired objects")
		}
		if err := ValidateDirectManifestOwner(resource); err != nil {
			return err
		}
		for key := range resource.GetAnnotations() {
			if strings.HasPrefix(key, "argocd.argoproj.io/") && key != "argocd.argoproj.io/sync-wave" {
				return fmt.Errorf("GitOps child hooks and controller overrides are unsupported")
			}
		}
		if resource.GetKind() == "Secret" && (resource.Object["data"] != nil || resource.Object["stringData"] != nil) {
			return fmt.Errorf("GitOps secret values must use administrator-managed references")
		}
		for _, field := range []string{"containers", "initContainers"} {
			containers, _, err := unstructured.NestedSlice(resource.Object, "spec", "template", "spec", field)
			if err != nil {
				return err
			}
			for _, raw := range containers {
				container, _ := raw.(map[string]any)
				image, _ := container["image"].(string)
				if _, ok := expectedImages[image]; !ok {
					return fmt.Errorf("GitOps workload image is outside the frozen digest set")
				}
				expectedImages[image] = true
			}
		}
	}
	for _, used := range expectedImages {
		if !used {
			return fmt.Errorf("GitOps image override is not used by a workload")
		}
	}
	return nil
}

// CheckArgoResourceOwners refuses implicit adoption, including unlabelled live
// objects. It is read-only and runs again immediately before controller sync.
func CheckArgoResourceOwners(ctx context.Context, client dynamic.Interface, application *unstructured.Unstructured, resources []*unstructured.Unstructured) error {
	if err := ValidateArgoResources(application, resources); err != nil {
		return err
	}
	for _, resource := range resources {
		gvk := resource.GroupVersionKind()
		gvr := argoResourceTypes[gvk.Group+"/"+gvk.Kind]
		live, err := client.Resource(gvr).Namespace(resource.GetNamespace()).Get(ctx, resource.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		if live.GetUID() == "" || live.GetDeletionTimestamp() != nil || !argoTracksResource(application, live) || metav1.GetControllerOf(live) != nil {
			return fmt.Errorf("GitOps child %s/%s belongs to another writer or is being deleted", resource.GetKind(), resource.GetName())
		}
		labels, annotations := live.GetLabels(), live.GetAnnotations()
		if labels["app.kubernetes.io/managed-by"] == "Helm" || annotations["meta.helm.sh/release-name"] != "" || labels["kustomize.toolkit.fluxcd.io/name"] != "" || labels["helm.toolkit.fluxcd.io/name"] != "" {
			return fmt.Errorf("GitOps child has another delivery owner")
		}
		for _, field := range live.GetManagedFields() {
			if strings.HasPrefix(field.Manager, "opensoha-") || field.Manager == "helm" || field.Manager == "kustomize-controller" || field.Manager == "helm-controller" {
				return fmt.Errorf("GitOps child has fields owned by another delivery writer")
			}
		}
	}
	return nil
}

func argoTracksResource(application, resource *unstructured.Unstructured) bool {
	gvk := resource.GroupVersionKind()
	suffix := ":" + gvk.Group + "/" + gvk.Kind + ":" + resource.GetNamespace() + "/" + resource.GetName()
	tracking := resource.GetAnnotations()["argocd.argoproj.io/tracking-id"]
	return tracking == application.GetName()+suffix || tracking == application.GetNamespace()+"_"+application.GetName()+suffix
}
