package sohacli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

type kubernetesResourceEndpoint struct {
	kind, path   string
	namespaced   bool
	list, detail func() any
}

// Fixed, public API families only; this is not an arbitrary HTTP passthrough.
var kubernetesResourceEndpoints = []kubernetesResourceEndpoint{
	{"Pod", "workloads/pods", true, func() any { return &sohaapi.KubernetesPodListEnvelope{} }, func() any { return &sohaapi.KubernetesPodDetailEnvelope{} }},
	{"Deployment", "workloads/deployments", true, func() any { return &sohaapi.KubernetesDeploymentListEnvelope{} }, func() any { return &sohaapi.KubernetesDeploymentDetailEnvelope{} }},
	{"StatefulSet", "workloads/statefulsets", true, func() any { return &sohaapi.KubernetesStatefulSetListEnvelope{} }, func() any { return &sohaapi.KubernetesStatefulSetDetailEnvelope{} }},
	{"DaemonSet", "workloads/daemonsets", true, func() any { return &sohaapi.KubernetesDaemonSetListEnvelope{} }, func() any { return &sohaapi.KubernetesDaemonSetDetailEnvelope{} }},
	{"ReplicaSet", "workloads/replicasets", true, func() any { return &sohaapi.KubernetesReplicaSetListEnvelope{} }, func() any { return &sohaapi.KubernetesReplicaSetDetailEnvelope{} }},
	{"ReplicationController", "workloads/replicationcontrollers", true, func() any { return &sohaapi.KubernetesReplicationControllerListEnvelope{} }, func() any { return &sohaapi.KubernetesReplicationControllerDetailEnvelope{} }},
	{"Job", "workloads/jobs", true, func() any { return &sohaapi.KubernetesJobListEnvelope{} }, func() any { return &sohaapi.KubernetesJobDetailEnvelope{} }},
	{"CronJob", "workloads/cronjobs", true, func() any { return &sohaapi.KubernetesCronJobListEnvelope{} }, func() any { return &sohaapi.KubernetesCronJobDetailEnvelope{} }},
	{"Node", "infrastructure/nodes", false, func() any { return &sohaapi.KubernetesNodeListEnvelope{} }, func() any { return &sohaapi.KubernetesNodeDetailEnvelope{} }},
	{"ConfigMap", "configuration/configmaps", true, func() any { return &sohaapi.KubernetesConfigMapListEnvelope{} }, func() any { return &sohaapi.KubernetesConfigMapDetailEnvelope{} }},
	{"Secret", "configuration/secrets", true, func() any { return &sohaapi.KubernetesSecretMetadataListEnvelope{} }, nil},
	{"Service", "network/services", true, func() any { return &sohaapi.KubernetesServiceListEnvelope{} }, func() any { return &sohaapi.KubernetesServiceDetailEnvelope{} }},
	{"Ingress", "network/ingresses", true, func() any { return &sohaapi.KubernetesIngressListEnvelope{} }, func() any { return &sohaapi.KubernetesIngressDetailEnvelope{} }},
	{"PersistentVolumeClaim", "storage/persistentvolumeclaims", true, func() any { return &sohaapi.KubernetesPersistentVolumeClaimListEnvelope{} }, func() any { return &sohaapi.KubernetesPersistentVolumeClaimDetailEnvelope{} }},
	{"PersistentVolume", "storage/persistentvolumes", false, func() any { return &sohaapi.KubernetesPersistentVolumeListEnvelope{} }, func() any { return &sohaapi.KubernetesPersistentVolumeDetailEnvelope{} }},
	{"StorageClass", "storage/storageclasses", false, func() any { return &sohaapi.KubernetesStorageClassListEnvelope{} }, nil},
	{"ServiceAccount", "access-control/serviceaccounts", true, func() any { return &sohaapi.KubernetesServiceAccountListEnvelope{} }, func() any { return &sohaapi.KubernetesServiceAccountDetailEnvelope{} }},
	{"Role", "access-control/roles", true, func() any { return &sohaapi.KubernetesRoleListEnvelope{} }, func() any { return &sohaapi.KubernetesRoleDetailEnvelope{} }},
	{"RoleBinding", "access-control/rolebindings", true, func() any { return &sohaapi.KubernetesRoleBindingListEnvelope{} }, func() any { return &sohaapi.KubernetesRoleBindingDetailEnvelope{} }},
}

func kubernetesEndpoint(kind string) (kubernetesResourceEndpoint, error) {
	for _, endpoint := range kubernetesResourceEndpoints {
		parts := strings.Split(endpoint.path, "/")
		if strings.EqualFold(kind, endpoint.kind) || strings.EqualFold(kind, parts[len(parts)-1]) {
			return endpoint, nil
		}
	}
	return kubernetesResourceEndpoint{}, usageError{message: "unsupported Kubernetes resource kind; see soha k8s --help"}
}

func kubernetesSegment(value string) bool {
	return strings.TrimSpace(value) != "" && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "/\\?#%\x00\r\n\t") && value != "." && value != ".."
}
func kubernetesClusterPath(cluster string) (string, error) {
	if !kubernetesSegment(cluster) {
		return "", usageError{message: "--cluster-id must name one Soha cluster"}
	}
	return "/api/v1/clusters/" + url.PathEscape(cluster), nil
}
func kubernetesQuery(path, namespace string) string {
	if namespace == "" {
		return path
	}
	return path + "?" + url.Values{"namespace": {namespace}}.Encode()
}

func (c APIClient) ReadKubernetesResource(ctx context.Context, cluster, namespace, kind, name, action string) (any, error) {
	base, err := kubernetesClusterPath(cluster)
	if err != nil {
		return nil, err
	}
	endpoint, err := kubernetesEndpoint(kind)
	if err != nil {
		return nil, err
	}
	if !endpoint.namespaced {
		namespace = ""
	}
	path := base + "/" + endpoint.path
	var out any
	if action == "list" {
		out = endpoint.list()
	} else {
		if !kubernetesSegment(name) {
			return nil, usageError{message: "one valid Kubernetes resource name is required"}
		}
		if endpoint.namespaced && namespace == "" {
			return nil, usageError{message: "a single --namespace is required for a named resource"}
		}
		switch action {
		case "get":
			if endpoint.detail != nil {
				path += "/" + url.PathEscape(name) + "/detail"
				out = endpoint.detail()
				break
			}
			fallthrough
		case "identity":
			path = base + "/resources/" + endpoint.kind + "/" + url.PathEscape(name) + "/identity"
			out = &sohaapi.KubernetesResourceIdentityEnvelope{}
		case "yaml":
			path += "/" + url.PathEscape(name) + "/yaml"
			out = &sohaapi.KubernetesResourceYamlEnvelope{}
		default:
			return nil, usageError{message: "unsupported Kubernetes read action"}
		}
	}
	err = c.doJSON(ctx, http.MethodGet, kubernetesQuery(path, namespace), c.Token, nil, nil, out)
	return out, err
}

func (c APIClient) ConditionalKubernetesResource(ctx context.Context, cluster, namespace, kind, name, action string, input sohaapi.KubernetesConditionalResourceYamlInput) (any, error) {
	base, err := kubernetesClusterPath(cluster)
	if err != nil {
		return nil, err
	}
	endpoint, err := kubernetesEndpoint(kind)
	if err != nil {
		return nil, err
	}
	if !kubernetesSegment(name) || input.Preconditions.ExpectedUID == "" || input.Preconditions.ExpectedResourceVersion == "" {
		return nil, usageError{message: "a name, --expected-uid and --expected-resource-version are required"}
	}
	if endpoint.namespaced && namespace == "" {
		return nil, usageError{message: "a single --namespace is required for mutation"}
	}
	if !endpoint.namespaced {
		namespace = ""
	}
	path := base + "/resources/" + endpoint.kind + "/" + url.PathEscape(name) + "/conditional"
	method := http.MethodPut
	var body any = input
	var out any = &sohaapi.KubernetesResourceYamlEnvelope{}
	switch action {
	case "preflight":
		method = http.MethodPost
		path += "/yaml/preflight"
		out = &sohaapi.OperationalPlanEnvelope{}
	case "apply":
		path += "/yaml"
	case "delete":
		method = http.MethodDelete
		body = nil
		out = &sohaapi.KubernetesResourceDeletionReceiptEnvelope{}
		query := url.Values{"expectedUid": {input.Preconditions.ExpectedUID}, "expectedResourceVersion": {input.Preconditions.ExpectedResourceVersion}}
		if namespace != "" {
			query.Set("namespace", namespace)
		}
		path += "?" + query.Encode()
	default:
		return nil, fmt.Errorf("unsupported conditional Kubernetes action")
	}
	if action != "delete" {
		path = kubernetesQuery(path, namespace)
	}
	err = c.doJSON(ctx, method, path, c.Token, nil, body, out)
	if err == nil && action == "delete" {
		envelope, ok := out.(*sohaapi.KubernetesResourceDeletionReceiptEnvelope)
		if !ok || envelope == nil {
			return nil, fmt.Errorf("invalid conditional deletion receipt; inspect the original resource identity before retrying")
		}
		receipt := envelope.Data
		if !receipt.Accepted || receipt.State != "deletion_requested" || receipt.Kind != endpoint.kind || receipt.Name != name || receipt.Namespace != namespace || receipt.Preconditions != input.Preconditions {
			return nil, fmt.Errorf("invalid conditional deletion receipt; inspect the original resource identity before retrying")
		}
	}
	return out, err
}
