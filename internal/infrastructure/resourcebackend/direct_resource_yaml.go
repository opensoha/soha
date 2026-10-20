package resourcebackend

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	contractresource "github.com/opensoha/soha-contracts/resource"
	contractruntime "github.com/opensoha/soha-contracts/resource/runtime"
	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

func (d *Direct) CreateResourceYAML(ctx context.Context, clusterID, namespace, kind, content string) (domainresource.ResourceYAMLView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	var object map[string]any
	if err := yaml.Unmarshal([]byte(content), &object); err != nil {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: invalid yaml: %v", apperrors.ErrInvalidArgument, err)
	}
	item := &unstructured.Unstructured{Object: object}
	if item.GetKind() == "" {
		item.SetKind(kind)
	}
	if !strings.EqualFold(item.GetKind(), kind) {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml kind %s does not match target %s", apperrors.ErrInvalidArgument, item.GetKind(), kind)
	}
	if strings.TrimSpace(item.GetName()) == "" {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml metadata.name is required", apperrors.ErrInvalidArgument)
	}
	if err := validateResourceMutation(item); err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	gvr, namespaceScoped, err := resourceGVRForKind(kind)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if namespaceScoped {
		if item.GetNamespace() == "" {
			item.SetNamespace(namespace)
		}
		if strings.TrimSpace(item.GetNamespace()) == "" {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: namespace is required for namespace-scoped resource", apperrors.ErrInvalidArgument)
		}
	} else {
		if strings.TrimSpace(item.GetNamespace()) != "" {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml metadata.namespace must be empty for cluster-scoped resource", apperrors.ErrInvalidArgument)
		}
		item.SetNamespace("")
	}
	item.SetResourceVersion("")
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	created, err := dynamicResource(bundle.Dynamic, gvr, namespaceScoped, item.GetNamespace()).Create(queryCtx, item, metav1.CreateOptions{FieldManager: resourceEditFieldManager})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	unstructured.RemoveNestedField(created.Object, "metadata", "managedFields")
	rendered, err := yaml.Marshal(created.Object)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{
		Kind: kind, Name: created.GetName(), Namespace: created.GetNamespace(), Content: string(rendered),
	}, nil
}

func (d *Direct) GetResourceYAML(ctx context.Context, clusterID, namespace, kind, name string) (domainresource.ResourceYAMLView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	gvr, namespaceScoped, err := resourceGVRForKind(kind)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := dynamicResource(bundle.Dynamic, gvr, namespaceScoped, namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	unstructured.RemoveNestedField(item.Object, "metadata", "managedFields")
	content, err := yaml.Marshal(item.Object)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{
		Kind: kind, Name: name, Namespace: item.GetNamespace(), Content: string(content),
	}, nil
}

func (d *Direct) DeleteResource(ctx context.Context, clusterID, namespace, kind, name string) error {
	if strings.EqualFold(strings.TrimSpace(kind), "Pod") {
		return d.DeletePod(ctx, clusterID, namespace, name)
	}
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return err
	}
	gvr, namespaceScoped, err := resourceGVRForKind(kind)
	if err != nil {
		return err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return resourceMutationError(contractruntime.DeleteManifest(queryCtx, dynamicResource(bundle.Dynamic, gvr, namespaceScoped, namespace), name, ""))
}

func (d *Direct) ApplyResourceYAML(ctx context.Context, clusterID, namespace, kind, name, content string) (domainresource.ResourceYAMLView, error) {
	view, _, err := d.applyResourceYAML(ctx, clusterID, namespace, kind, name, content, false)
	return view, err
}

func (d *Direct) DryRunResourceYAML(ctx context.Context, clusterID, namespace, kind, name, content string) (domainresource.ResourceUpdateAnalysis, error) {
	_, analysis, err := d.applyResourceYAML(ctx, clusterID, namespace, kind, name, content, true)
	return analysis, err
}

func (d *Direct) applyResourceYAML(ctx context.Context, clusterID, namespace, kind, name, content string, dryRun bool) (domainresource.ResourceYAMLView, domainresource.ResourceUpdateAnalysis, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceYAMLView{}, domainresource.ResourceUpdateAnalysis{}, err
	}
	var object map[string]any
	if err := yaml.Unmarshal([]byte(content), &object); err != nil {
		return domainresource.ResourceYAMLView{}, domainresource.ResourceUpdateAnalysis{}, fmt.Errorf("%w: invalid yaml: %v", apperrors.ErrInvalidArgument, err)
	}
	item := &unstructured.Unstructured{Object: object}
	item.SetKind(kind)
	if item.GetName() == "" {
		item.SetName(name)
	}
	if item.GetNamespace() == "" {
		item.SetNamespace(namespace)
	}
	if item.GetName() != name {
		return domainresource.ResourceYAMLView{}, domainresource.ResourceUpdateAnalysis{}, fmt.Errorf("%w: yaml metadata.name does not match target resource", apperrors.ErrInvalidArgument)
	}
	gvr, namespaceScoped, err := resourceGVRForKind(kind)
	if err != nil {
		return domainresource.ResourceYAMLView{}, domainresource.ResourceUpdateAnalysis{}, err
	}
	if namespaceScoped {
		if item.GetNamespace() != namespace {
			return domainresource.ResourceYAMLView{}, domainresource.ResourceUpdateAnalysis{}, fmt.Errorf("%w: yaml metadata.namespace does not match target resource", apperrors.ErrInvalidArgument)
		}
	} else {
		if strings.TrimSpace(item.GetNamespace()) != "" {
			return domainresource.ResourceYAMLView{}, domainresource.ResourceUpdateAnalysis{}, fmt.Errorf("%w: yaml metadata.namespace must be empty for cluster-scoped resource", apperrors.ErrInvalidArgument)
		}
		item.SetNamespace("")
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resource := dynamicResource(bundle.Dynamic, gvr, namespaceScoped, item.GetNamespace())
	current, err := resource.Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, domainresource.ResourceUpdateAnalysis{}, err
	}
	if err := validateResourceMutation(current); err != nil {
		return domainresource.ResourceYAMLView{}, domainresource.ResourceUpdateAnalysis{}, err
	}
	item.SetAPIVersion(gvr.GroupVersion().String())
	item.SetResourceVersion(current.GetResourceVersion())
	item.SetUID(current.GetUID())
	unstructured.RemoveNestedField(item.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(item.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(item.Object, "metadata", "generation")
	unstructured.RemoveNestedField(item.Object, "status")
	analysis := analyzeResourceUpdate(current, item)
	updated, err := contractruntime.ApplyResourceEdit(queryCtx, resource, current, item, resourceEditFieldManager, dryRun)
	if err != nil {
		if dryRun && apierrors.IsConflict(err) {
			analysis.Conflicts = conflictsFromError(err.Error(), analysis)
			return domainresource.ResourceYAMLView{}, analysis, nil
		}
		if apierrors.IsConflict(err) {
			err = fmt.Errorf("%w: %v", apperrors.ErrConflict, err)
		}
		return domainresource.ResourceYAMLView{}, analysis, err
	}
	unstructured.RemoveNestedField(updated.Object, "metadata", "managedFields")
	rendered, err := yaml.Marshal(updated.Object)
	if err != nil {
		return domainresource.ResourceYAMLView{}, analysis, err
	}
	return domainresource.ResourceYAMLView{
		Kind: kind, Name: name, Namespace: item.GetNamespace(), Content: string(rendered),
	}, analysis, nil
}

func validateResourceMutation(object metav1.Object) error {
	if err := contractruntime.ValidateDirectManifestOwner(object); err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrConflict, err)
	}
	return nil
}

func dynamicResource(client dynamic.Interface, gvr schema.GroupVersionResource, namespaced bool, namespace string) dynamic.ResourceInterface {
	if namespaced {
		return client.Resource(gvr).Namespace(namespace)
	}
	return client.Resource(gvr)
}

func resourceGVRForKind(kind string) (schema.GroupVersionResource, bool, error) {
	definition, ok := contractresource.LookupKubernetesResourceKind(kind)
	if !ok || !definition.GenericYAML {
		return schema.GroupVersionResource{}, false, fmt.Errorf("%w: yaml apply does not support kind %s", apperrors.ErrInvalidArgument, kind)
	}
	return knownResourceGVRForKind(kind)
}

// Creation has its own per-kind authorization and may include dedicated lifecycle kinds.
func knownResourceGVRForKind(kind string) (schema.GroupVersionResource, bool, error) {
	definition, ok := contractresource.LookupKubernetesResourceKind(kind)
	if !ok {
		return schema.GroupVersionResource{}, false, fmt.Errorf("%w: unsupported kind %s", apperrors.ErrInvalidArgument, kind)
	}
	return schema.GroupVersionResource{Group: definition.Group, Version: definition.Version, Resource: definition.Resource}, definition.Namespaced, nil
}

var _ appresource.DirectGenericResource = (*Direct)(nil)

func resourceMutationError(err error) error {
	if errors.Is(err, contractruntime.ErrResourceOwnership) || apierrors.IsConflict(err) {
		return fmt.Errorf("%w: %v", apperrors.ErrConflict, err)
	}
	return err
}
