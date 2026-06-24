package resource

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// ApplyResourceYAMLByKind applies updated YAML for any kind registered in
// resourceGVRForKind via the dynamic client.
func (s *Service) ApplyResourceYAMLByKind(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name, content string) (domainresource.ResourceYAMLView, error) {
	return s.applyResourceYAML(ctx, principal, clusterID, namespace, kind, name, content)
}
func (s *Service) applyResourceYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name, content string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionUpdate)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if strings.TrimSpace(content) == "" {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml content is required", apperrors.ErrInvalidArgument)
	}
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		item, err := client.ApplyResourceYAML(ctx, namespace, kind, name, content)
		if err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, kind, name, string(domainaccess.ActionUpdate), "failure", err.Error())
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, name, string(domainaccess.ActionUpdate), "success", "applied resource yaml via agent")
		s.recordOperation(ctx, principal, "platform.resource.apply", connection.Summary.ID, namespace, kind, name, "applied resource yaml via agent", nil)
		return item, nil
	default:
		item, err := s.applyDirectResourceYAML(ctx, clusterID, namespace, kind, name, content)
		if err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, kind, name, string(domainaccess.ActionUpdate), "failure", err.Error())
			return domainresource.ResourceYAMLView{}, err
		}
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, name, string(domainaccess.ActionUpdate), "success", "applied resource yaml")
		s.recordOperation(ctx, principal, "platform.resource.apply", connection.Summary.ID, namespace, kind, name, "applied resource yaml", nil)
		return item, nil
	}
}

// GetResourceYAML fetches the YAML representation of any kind registered in
// resourceGVRForKind via the dynamic client. Namespace may be empty for
// cluster-scoped kinds.
func (s *Service) GetResourceYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		item, err := client.GetResourceYAML(ctx, namespace, kind, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, name, string(domainaccess.ActionView), "success", "viewed resource yaml via agent")
		return item, nil
	default:
		item, err := s.getDirectResourceYAMLByKind(ctx, clusterID, namespace, kind, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, name, string(domainaccess.ActionView), "success", "viewed resource yaml")
		return item, nil
	}
}

// DeleteResourceByKind deletes any kind registered in resourceGVRForKind.
func (s *Service) DeleteResourceByKind(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name string) error {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, kind, domainaccess.ActionDelete)
	if err != nil {
		return err
	}
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return err
		}
		if err := client.DeleteResource(ctx, namespace, kind, name); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, kind, name, string(domainaccess.ActionDelete), "failure", err.Error())
			return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, name, string(domainaccess.ActionDelete), "success", "deleted resource via agent")
		s.recordOperation(ctx, principal, "platform.resource.delete", connection.Summary.ID, namespace, kind, name, "deleted resource via agent", nil)
		return nil
	default:
		if err := s.deleteDirectResourceByKind(ctx, clusterID, namespace, kind, name); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, kind, name, string(domainaccess.ActionDelete), "failure", err.Error())
			return err
		}
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, name, string(domainaccess.ActionDelete), "success", "deleted resource")
		s.recordOperation(ctx, principal, "platform.resource.delete", connection.Summary.ID, namespace, kind, name, "deleted resource", nil)
		return nil
	}
}
func (s *Service) getDirectResourceYAMLByKind(ctx context.Context, clusterID, namespace, kind, name string) (domainresource.ResourceYAMLView, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	gvr, namespaceScoped, err := resourceGVRForKind(kind)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var resource dynamic.ResourceInterface
	if namespaceScoped {
		resource = bundle.Dynamic.Resource(gvr).Namespace(namespace)
	} else {
		resource = bundle.Dynamic.Resource(gvr)
	}
	item, err := resource.Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	unstructured.RemoveNestedField(item.Object, "metadata", "managedFields")
	content, err := yaml.Marshal(item.Object)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{
		Kind:      kind,
		Name:      name,
		Namespace: item.GetNamespace(),
		Content:   string(content),
	}, nil
}
func (s *Service) deleteDirectResourceByKind(ctx context.Context, clusterID, namespace, kind, name string) error {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	gvr, namespaceScoped, err := resourceGVRForKind(kind)
	if err != nil {
		return err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var resource dynamic.ResourceInterface
	if namespaceScoped {
		resource = bundle.Dynamic.Resource(gvr).Namespace(namespace)
	} else {
		resource = bundle.Dynamic.Resource(gvr)
	}
	return resource.Delete(queryCtx, name, metav1.DeleteOptions{})
}
func (s *Service) applyDirectResourceYAML(ctx context.Context, clusterID, namespace, kind, name, content string) (domainresource.ResourceYAMLView, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
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
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var resource dynamic.ResourceInterface
	if namespaceScoped {
		if item.GetNamespace() == "" {
			item.SetNamespace(namespace)
		}
		if item.GetNamespace() != namespace {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml metadata.namespace does not match target resource", apperrors.ErrInvalidArgument)
		}
		resource = bundle.Dynamic.Resource(gvr).Namespace(namespace)
	} else {
		if strings.TrimSpace(item.GetNamespace()) != "" {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: yaml metadata.namespace must be empty for cluster-scoped resource", apperrors.ErrInvalidArgument)
		}
		item.SetNamespace("")
		resource = bundle.Dynamic.Resource(gvr)
	}
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
		Kind:      kind,
		Name:      name,
		Namespace: item.GetNamespace(),
		Content:   string(rendered),
	}, nil
}
func resourceGVRForKind(kind string) (schema.GroupVersionResource, bool, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "pod":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}, true, nil
	case "node":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "nodes"}, false, nil
	case "deployment":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, true, nil
	case "statefulset":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, true, nil
	case "daemonset":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, true, nil
	case "replicaset":
		return schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}, true, nil
	case "job":
		return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, true, nil
	case "cronjob":
		return schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}, true, nil
	case "configmap":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}, true, nil
	case "secret":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}, true, nil
	case "serviceaccount":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "serviceaccounts"}, true, nil
	case "replicationcontroller":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "replicationcontrollers"}, true, nil
	case "service":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "services"}, true, nil
	case "role":
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles"}, true, nil
	case "rolebinding":
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings"}, true, nil
	case "resourcequota":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "resourcequotas"}, true, nil
	case "limitrange":
		return schema.GroupVersionResource{Group: "", Version: "v1", Resource: "limitranges"}, true, nil
	case "lease":
		return schema.GroupVersionResource{Group: "coordination.k8s.io", Version: "v1", Resource: "leases"}, true, nil
	case "ingress":
		return schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, true, nil
	case "endpointslice":
		return schema.GroupVersionResource{Group: "discovery.k8s.io", Version: "v1", Resource: "endpointslices"}, true, nil
	case "networkpolicy":
		return schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}, true, nil
	case "ingressclass":
		return schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingressclasses"}, false, nil
	case "gatewayclass":
		return schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gatewayclasses"}, false, nil
	case "gateway":
		return schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways"}, true, nil
	case "httproute":
		return schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}, true, nil
	case "backendtlspolicy":
		return schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "backendtlspolicies"}, true, nil
	case "grpcroute":
		return schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "grpcroutes"}, true, nil
	case "referencegrant":
		return schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "referencegrants"}, true, nil
	case "priorityclass":
		return schema.GroupVersionResource{Group: "scheduling.k8s.io", Version: "v1", Resource: "priorityclasses"}, false, nil
	case "runtimeclass":
		return schema.GroupVersionResource{Group: "node.k8s.io", Version: "v1", Resource: "runtimeclasses"}, false, nil
	case "clusterrole":
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}, false, nil
	case "clusterrolebinding":
		return schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings"}, false, nil
	case "mutatingwebhookconfiguration":
		return schema.GroupVersionResource{Group: "admissionregistration.k8s.io", Version: "v1", Resource: "mutatingwebhookconfigurations"}, false, nil
	case "validatingwebhookconfiguration":
		return schema.GroupVersionResource{Group: "admissionregistration.k8s.io", Version: "v1", Resource: "validatingwebhookconfigurations"}, false, nil
	default:
		return schema.GroupVersionResource{}, false, fmt.Errorf("%w: yaml apply does not support kind %s", apperrors.ErrInvalidArgument, kind)
	}
}
