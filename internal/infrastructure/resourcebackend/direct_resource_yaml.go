package resourcebackend

import (
	"context"
	"fmt"
	"strings"
	"time"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
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
	created, err := dynamicResource(bundle.Dynamic, gvr, namespaceScoped, item.GetNamespace()).Create(queryCtx, item, metav1.CreateOptions{})
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
	return dynamicResource(bundle.Dynamic, gvr, namespaceScoped, namespace).Delete(queryCtx, name, metav1.DeleteOptions{})
}

func (d *Direct) ApplyResourceYAML(ctx context.Context, clusterID, namespace, kind, name, content string) (domainresource.ResourceYAMLView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	var object map[string]any
	if err := yaml.Unmarshal([]byte(content), &object); err != nil {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: invalid yaml: %v", apperrors.ErrInvalidArgument, err)
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
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml metadata.name does not match target resource", apperrors.ErrInvalidArgument)
	}
	gvr, namespaceScoped, err := resourceGVRForKind(kind)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if namespaceScoped {
		if item.GetNamespace() != namespace {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml metadata.namespace does not match target resource", apperrors.ErrInvalidArgument)
		}
	} else {
		if strings.TrimSpace(item.GetNamespace()) != "" {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml metadata.namespace must be empty for cluster-scoped resource", apperrors.ErrInvalidArgument)
		}
		item.SetNamespace("")
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resource := dynamicResource(bundle.Dynamic, gvr, namespaceScoped, namespace)
	if item.GetResourceVersion() == "" {
		current, err := resource.Get(queryCtx, name, metav1.GetOptions{})
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		item.SetResourceVersion(current.GetResourceVersion())
	}
	updated, err := resource.Update(queryCtx, item, metav1.UpdateOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	rendered, err := yaml.Marshal(updated.Object)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{
		Kind: kind, Name: name, Namespace: item.GetNamespace(), Content: string(rendered),
	}, nil
}

func dynamicResource(client dynamic.Interface, gvr schema.GroupVersionResource, namespaced bool, namespace string) dynamic.ResourceInterface {
	if namespaced {
		return client.Resource(gvr).Namespace(namespace)
	}
	return client.Resource(gvr)
}

type resourceKindDefinition struct {
	gvr        schema.GroupVersionResource
	namespaced bool
}

var resourceKindDefinitions = map[string]resourceKindDefinition{
	"pod":                            {gvr: schema.GroupVersionResource{Version: "v1", Resource: "pods"}, namespaced: true},
	"node":                           {gvr: schema.GroupVersionResource{Version: "v1", Resource: "nodes"}},
	"deployment":                     {gvr: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, namespaced: true},
	"statefulset":                    {gvr: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, namespaced: true},
	"daemonset":                      {gvr: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, namespaced: true},
	"replicaset":                     {gvr: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}, namespaced: true},
	"job":                            {gvr: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, namespaced: true},
	"cronjob":                        {gvr: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}, namespaced: true},
	"configmap":                      {gvr: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, namespaced: true},
	"secret":                         {gvr: schema.GroupVersionResource{Version: "v1", Resource: "secrets"}, namespaced: true},
	"serviceaccount":                 {gvr: schema.GroupVersionResource{Version: "v1", Resource: "serviceaccounts"}, namespaced: true},
	"replicationcontroller":          {gvr: schema.GroupVersionResource{Version: "v1", Resource: "replicationcontrollers"}, namespaced: true},
	"service":                        {gvr: schema.GroupVersionResource{Version: "v1", Resource: "services"}, namespaced: true},
	"persistentvolumeclaim":          {gvr: schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}, namespaced: true},
	"persistentvolume":               {gvr: schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumes"}},
	"storageclass":                   {gvr: schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"}},
	"role":                           {gvr: schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}, namespaced: true},
	"rolebinding":                    {gvr: schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"}, namespaced: true},
	"resourcequota":                  {gvr: schema.GroupVersionResource{Version: "v1", Resource: "resourcequotas"}, namespaced: true},
	"limitrange":                     {gvr: schema.GroupVersionResource{Version: "v1", Resource: "limitranges"}, namespaced: true},
	"lease":                          {gvr: schema.GroupVersionResource{Group: "coordination.k8s.io", Version: "v1", Resource: "leases"}, namespaced: true},
	"horizontalpodautoscaler":        {gvr: schema.GroupVersionResource{Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers"}, namespaced: true},
	"poddisruptionbudget":            {gvr: schema.GroupVersionResource{Group: "policy", Version: "v1", Resource: "poddisruptionbudgets"}, namespaced: true},
	"ingress":                        {gvr: schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, namespaced: true},
	"endpointslice":                  {gvr: schema.GroupVersionResource{Group: "discovery.k8s.io", Version: "v1", Resource: "endpointslices"}, namespaced: true},
	"networkpolicy":                  {gvr: schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}, namespaced: true},
	"ingressclass":                   {gvr: schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingressclasses"}},
	"gatewayclass":                   {gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gatewayclasses"}},
	"gateway":                        {gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}, namespaced: true},
	"httproute":                      {gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}, namespaced: true},
	"backendtlspolicy":               {gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "backendtlspolicies"}, namespaced: true},
	"grpcroute":                      {gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "grpcroutes"}, namespaced: true},
	"referencegrant":                 {gvr: schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "referencegrants"}, namespaced: true},
	"priorityclass":                  {gvr: schema.GroupVersionResource{Group: "scheduling.k8s.io", Version: "v1", Resource: "priorityclasses"}},
	"runtimeclass":                   {gvr: schema.GroupVersionResource{Group: "node.k8s.io", Version: "v1", Resource: "runtimeclasses"}},
	"clusterrole":                    {gvr: schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}},
	"clusterrolebinding":             {gvr: schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings"}},
	"mutatingwebhookconfiguration":   {gvr: schema.GroupVersionResource{Group: "admissionregistration.k8s.io", Version: "v1", Resource: "mutatingwebhookconfigurations"}},
	"validatingwebhookconfiguration": {gvr: schema.GroupVersionResource{Group: "admissionregistration.k8s.io", Version: "v1", Resource: "validatingwebhookconfigurations"}},
}

func resourceGVRForKind(kind string) (schema.GroupVersionResource, bool, error) {
	definition, ok := resourceKindDefinitions[strings.ToLower(strings.TrimSpace(kind))]
	if !ok {
		return schema.GroupVersionResource{}, false, fmt.Errorf("%w: yaml apply does not support kind %s", apperrors.ErrInvalidArgument, kind)
	}
	return definition.gvr, definition.namespaced, nil
}

var _ appresource.DirectGenericResource = (*Direct)(nil)
