package runtime

import (
	"encoding/hex"
	"fmt"
	"net/url"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	ArgoOwnerAnnotation     = "delivery.soha.io/argocd-owner"
	ArgoOperationAnnotation = "delivery.soha.io/argocd-operation"
)

var argoApplications = schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "applications"}
var argoImageOverride = regexp.MustCompile(`^(?:([A-Za-z0-9][A-Za-z0-9._:/-]*)=)?([A-Za-z0-9][A-Za-z0-9._:/-]*)@sha256:[a-f0-9]{64}$`)

// ParseArgoImageOverride accepts Kustomize's image@digest and name=image@digest forms.
func ParseArgoImageOverride(value string) (name, image string, err error) {
	parts := argoImageOverride.FindStringSubmatch(value)
	if len(parts) != 3 || strings.LastIndex(parts[2], ":") > strings.LastIndex(parts[2], "/") {
		return "", "", fmt.Errorf("GitOps image overrides require an immutable image digest without a tag")
	}
	name = parts[1]
	if name == "" {
		name = parts[2]
	}
	_, image, found := strings.Cut(value, "=")
	if !found {
		image = value
	}
	return name, image, nil
}

func IsArgoApplication(object *unstructured.Unstructured) bool {
	return object != nil && object.GetAPIVersion() == "argoproj.io/v1alpha1" && object.GetKind() == "Application"
}

// ValidateArgoApplication defines the supported, single-namespace GitOps path.
// AppProject and repository credentials remain administrator-managed in Argo CD.
func ValidateArgoApplication(object *unstructured.Unstructured) error {
	if !IsArgoApplication(object) || object.GetNamespace() == "" || object.GetName() == "" {
		return fmt.Errorf("GitOps requires a namespaced Argo CD Application")
	}
	if object.GetDeletionTimestamp() != nil || len(object.GetOwnerReferences()) != 0 || len(object.GetFinalizers()) != 0 {
		return fmt.Errorf("GitOps Application must be an independent, non-deleting root without cascading deletion")
	}
	for key := range object.GetAnnotations() {
		if strings.HasPrefix(key, "argocd.argoproj.io/") {
			return fmt.Errorf("GitOps Application must not override controller behavior with annotations")
		}
	}
	spec, _, err := unstructured.NestedMap(object.Object, "spec")
	if err != nil {
		return err
	}
	if err := argoAllowedFields(spec, "project", "source", "destination", "syncPolicy"); err != nil {
		return err
	}
	if argoString(object, "spec", "project") == "" || argoString(object, "spec", "project") == "default" {
		return fmt.Errorf("GitOps requires a dedicated restricted AppProject")
	}
	destination, _, err := unstructured.NestedStringMap(object.Object, "spec", "destination")
	if err != nil || len(destination) != 2 || destination["namespace"] != object.GetNamespace() || destination["server"] != "https://kubernetes.default.svc" {
		return fmt.Errorf("GitOps destination must be this cluster and the Application namespace")
	}
	source, _, err := unstructured.NestedMap(object.Object, "spec", "source")
	if err != nil {
		return err
	}
	if err := argoAllowedFields(source, "repoURL", "targetRevision", "path", "kustomize"); err != nil {
		return err
	}
	revision := argoString(object, "spec", "source", "targetRevision")
	if decoded, err := hex.DecodeString(revision); err != nil || len(decoded) != 20 && len(decoded) != 32 {
		return fmt.Errorf("GitOps targetRevision must be a full immutable Git commit")
	}
	repository, err := url.Parse(argoString(object, "spec", "source", "repoURL"))
	if err != nil || repository.Host == "" || repository.User != nil || repository.RawQuery != "" || repository.Fragment != "" || (repository.Scheme != "https" && repository.Scheme != "http" && repository.Scheme != "ssh") {
		return fmt.Errorf("GitOps repository must be an HTTP(S) or SSH URL without embedded credentials")
	}
	directory := argoString(object, "spec", "source", "path")
	if directory == "" || path.IsAbs(directory) || path.Clean(directory) != directory || directory == ".." || strings.HasPrefix(directory, "../") || strings.Contains(directory, "\\") {
		return fmt.Errorf("GitOps path must stay within the pinned repository")
	}
	kustomize, _, err := unstructured.NestedMap(object.Object, "spec", "source", "kustomize")
	if err != nil {
		return err
	}
	if err := argoAllowedFields(kustomize, "images", "namespace"); err != nil {
		return err
	}
	if argoString(object, "spec", "source", "kustomize", "namespace") != object.GetNamespace() {
		return fmt.Errorf("GitOps Kustomize namespace must be explicitly fixed to the delivery namespace")
	}
	images, _, err := unstructured.NestedStringSlice(object.Object, "spec", "source", "kustomize", "images")
	if err != nil || len(images) == 0 || len(images) > 16 {
		return fmt.Errorf("GitOps requires 1 to 16 fixed Kustomize image overrides")
	}
	imageNames := map[string]bool{}
	for _, image := range images {
		name, _, err := ParseArgoImageOverride(image)
		if err != nil {
			return err
		}
		if imageNames[name] {
			return fmt.Errorf("GitOps image overrides must use unique image names")
		}
		imageNames[name] = true
	}
	policy, _, err := unstructured.NestedMap(object.Object, "spec", "syncPolicy")
	if err != nil {
		return err
	}
	if err := argoAllowedFields(policy, "syncOptions"); err != nil {
		return err
	}
	options, _, err := unstructured.NestedStringSlice(object.Object, "spec", "syncPolicy", "syncOptions")
	if err != nil || !reflect.DeepEqual(options, []string{"FailOnSharedResource=true"}) {
		return fmt.Errorf("GitOps requires manual sync and FailOnSharedResource=true")
	}
	return nil
}

func argoAllowedFields(object map[string]any, allowed ...string) error {
	for key := range object {
		if !slices.Contains(allowed, key) {
			return fmt.Errorf("unsupported GitOps field %q", key)
		}
	}
	return nil
}

func argoString(object *unstructured.Unstructured, fields ...string) string {
	value, _, _ := unstructured.NestedString(object.Object, fields...)
	return value
}

func argoOperationID(object *unstructured.Unstructured, fields ...string) string {
	items, _, _ := unstructured.NestedSlice(object.Object, append(fields, "info")...)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["name"] == ArgoOperationAnnotation {
			value, _ := item["value"].(string)
			return value
		}
	}
	return ""
}

var argoResourceTypes = map[string]schema.GroupVersionResource{
	"apps/Deployment":        {Group: "apps", Version: "v1", Resource: "deployments"},
	"apps/StatefulSet":       {Group: "apps", Version: "v1", Resource: "statefulsets"},
	"/Service":               {Version: "v1", Resource: "services"},
	"/ConfigMap":             {Version: "v1", Resource: "configmaps"},
	"/Secret":                {Version: "v1", Resource: "secrets"},
	"/ServiceAccount":        {Version: "v1", Resource: "serviceaccounts"},
	"/PersistentVolumeClaim": {Version: "v1", Resource: "persistentvolumeclaims"},
}
