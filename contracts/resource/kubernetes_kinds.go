package resource

import "strings"

// KubernetesResourceKind describes the fixed resource surface shared by both transports.
// GenericYAML excludes resources whose dedicated lifecycle has stronger authorization.
type KubernetesResourceKind struct {
	Kind        string
	Group       string
	Version     string
	Resource    string
	Namespaced  bool
	GenericYAML bool
}

var kubernetesResourceKinds = [...]KubernetesResourceKind{
	{Kind: "Namespace", Group: "", Version: "v1", Resource: "namespaces", Namespaced: false, GenericYAML: false},
	{Kind: "Pod", Group: "", Version: "v1", Resource: "pods", Namespaced: true, GenericYAML: true},
	{Kind: "Node", Group: "", Version: "v1", Resource: "nodes", Namespaced: false, GenericYAML: true},
	{Kind: "Deployment", Group: "apps", Version: "v1", Resource: "deployments", Namespaced: true, GenericYAML: true},
	{Kind: "StatefulSet", Group: "apps", Version: "v1", Resource: "statefulsets", Namespaced: true, GenericYAML: true},
	{Kind: "DaemonSet", Group: "apps", Version: "v1", Resource: "daemonsets", Namespaced: true, GenericYAML: true},
	{Kind: "ReplicaSet", Group: "apps", Version: "v1", Resource: "replicasets", Namespaced: true, GenericYAML: true},
	{Kind: "Job", Group: "batch", Version: "v1", Resource: "jobs", Namespaced: true, GenericYAML: true},
	{Kind: "CronJob", Group: "batch", Version: "v1", Resource: "cronjobs", Namespaced: true, GenericYAML: true},
	{Kind: "ConfigMap", Group: "", Version: "v1", Resource: "configmaps", Namespaced: true, GenericYAML: true},
	{Kind: "Secret", Group: "", Version: "v1", Resource: "secrets", Namespaced: true, GenericYAML: true},
	{Kind: "ServiceAccount", Group: "", Version: "v1", Resource: "serviceaccounts", Namespaced: true, GenericYAML: true},
	{Kind: "ReplicationController", Group: "", Version: "v1", Resource: "replicationcontrollers", Namespaced: true, GenericYAML: true},
	{Kind: "Service", Group: "", Version: "v1", Resource: "services", Namespaced: true, GenericYAML: true},
	{Kind: "PersistentVolumeClaim", Group: "", Version: "v1", Resource: "persistentvolumeclaims", Namespaced: true, GenericYAML: true},
	{Kind: "PersistentVolume", Group: "", Version: "v1", Resource: "persistentvolumes", Namespaced: false, GenericYAML: true},
	{Kind: "StorageClass", Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses", Namespaced: false, GenericYAML: true},
	{Kind: "Role", Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles", Namespaced: true, GenericYAML: true},
	{Kind: "RoleBinding", Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings", Namespaced: true, GenericYAML: true},
	{Kind: "ResourceQuota", Group: "", Version: "v1", Resource: "resourcequotas", Namespaced: true, GenericYAML: true},
	{Kind: "LimitRange", Group: "", Version: "v1", Resource: "limitranges", Namespaced: true, GenericYAML: true},
	{Kind: "Lease", Group: "coordination.k8s.io", Version: "v1", Resource: "leases", Namespaced: true, GenericYAML: true},
	{Kind: "HorizontalPodAutoscaler", Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers", Namespaced: true, GenericYAML: true},
	{Kind: "PodDisruptionBudget", Group: "policy", Version: "v1", Resource: "poddisruptionbudgets", Namespaced: true, GenericYAML: true},
	{Kind: "Ingress", Group: "networking.k8s.io", Version: "v1", Resource: "ingresses", Namespaced: true, GenericYAML: true},
	{Kind: "EndpointSlice", Group: "discovery.k8s.io", Version: "v1", Resource: "endpointslices", Namespaced: true, GenericYAML: true},
	{Kind: "NetworkPolicy", Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies", Namespaced: true, GenericYAML: true},
	{Kind: "IngressClass", Group: "networking.k8s.io", Version: "v1", Resource: "ingressclasses", Namespaced: false, GenericYAML: true},
	{Kind: "GatewayClass", Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gatewayclasses", Namespaced: false, GenericYAML: true},
	{Kind: "Gateway", Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways", Namespaced: true, GenericYAML: true},
	{Kind: "HTTPRoute", Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes", Namespaced: true, GenericYAML: true},
	{Kind: "BackendTLSPolicy", Group: "gateway.networking.k8s.io", Version: "v1", Resource: "backendtlspolicies", Namespaced: true, GenericYAML: true},
	{Kind: "GRPCRoute", Group: "gateway.networking.k8s.io", Version: "v1", Resource: "grpcroutes", Namespaced: true, GenericYAML: true},
	{Kind: "ReferenceGrant", Group: "gateway.networking.k8s.io", Version: "v1", Resource: "referencegrants", Namespaced: true, GenericYAML: true},
	{Kind: "PriorityClass", Group: "scheduling.k8s.io", Version: "v1", Resource: "priorityclasses", Namespaced: false, GenericYAML: true},
	{Kind: "RuntimeClass", Group: "node.k8s.io", Version: "v1", Resource: "runtimeclasses", Namespaced: false, GenericYAML: true},
	{Kind: "ClusterRole", Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles", Namespaced: false, GenericYAML: true},
	{Kind: "ClusterRoleBinding", Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings", Namespaced: false, GenericYAML: true},
	{Kind: "CustomResourceDefinition", Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions", Namespaced: false, GenericYAML: false},
	{Kind: "WorkloadCronJob", Group: "workloads.soha.io", Version: "v1alpha1", Resource: "workloadcronjobs", Namespaced: true, GenericYAML: false},
	{Kind: "MutatingWebhookConfiguration", Group: "admissionregistration.k8s.io", Version: "v1", Resource: "mutatingwebhookconfigurations", Namespaced: false, GenericYAML: true},
	{Kind: "ValidatingWebhookConfiguration", Group: "admissionregistration.k8s.io", Version: "v1", Resource: "validatingwebhookconfigurations", Namespaced: false, GenericYAML: true},
}

// KubernetesResourceKinds returns a copy so callers cannot alter the shared surface.
func KubernetesResourceKinds() []KubernetesResourceKind {
	return append([]KubernetesResourceKind(nil), kubernetesResourceKinds[:]...)
}

func LookupKubernetesResourceKind(kind string) (KubernetesResourceKind, bool) {
	for _, definition := range kubernetesResourceKinds {
		if strings.EqualFold(strings.TrimSpace(kind), definition.Kind) {
			return definition, true
		}
	}
	return KubernetesResourceKind{}, false
}
