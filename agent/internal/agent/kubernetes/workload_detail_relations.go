package kubernetes

import (
	"context"
	"sort"
	"strings"

	domainresource "github.com/opensoha/soha/agent/internal/domain/resource"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

func (c *Client) loadWorkloadDetailRelations(
	ctx context.Context,
	namespace string,
	selector *metav1.LabelSelector,
	owners []metav1.OwnerReference,
	template corev1.PodTemplateSpec,
	podOwnerUID types.UID,
	ownerKind ...string,
) ([]domainresource.PodView, []domainresource.WorkloadRelationView, error) {
	pods, refs, err := c.listWorkloadPods(ctx, namespace, selector, podOwnerUID, ownerKind...)
	if err != nil {
		return nil, nil, err
	}
	relations, err := c.listWorkloadRelations(ctx, namespace, owners, template, refs)
	if err != nil {
		return nil, nil, err
	}
	return pods, relations, nil
}

func (c *Client) listWorkloadPods(ctx context.Context, namespace string, selector *metav1.LabelSelector, ownerUID types.UID, ownerKind ...string) ([]domainresource.PodView, podVolumeSourceRefSet, error) {
	refs := emptyPodVolumeSourceRefs()
	labelSelector := labels.Nothing()
	var err error
	if selector != nil {
		labelSelector, err = metav1.LabelSelectorAsSelector(selector)
		if err != nil {
			return nil, refs, err
		}
	} else if ownerUID != "" {
		labelSelector = labels.Set{batchv1.ControllerUidLabel: string(ownerUID)}.AsSelector()
	}
	if labelSelector.Empty() {
		return []domainresource.PodView{}, refs, nil
	}

	items, err := c.typed.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector.String()})
	if err != nil {
		return nil, refs, err
	}
	pods := make([]domainresource.PodView, 0, len(items.Items))
	for _, item := range items.Items {
		if ownerUID != "" && !ownedByUID(item.OwnerReferences, ownerUID) {
			continue
		}
		if len(ownerKind) > 0 && ownerKind[0] == "DaemonSet" && !daemonSetControlsPod(item.OwnerReferences, ownerUID) {
			continue
		}
		pods = append(pods, mapPod(item))
		mergePodVolumeSourceRefs(&refs, buildPodVolumeSourceRefs(item))
	}
	sort.SliceStable(pods, func(i, j int) bool { return pods[i].Name < pods[j].Name })
	return pods, refs, nil
}

func (c *Client) listWorkloadRelations(ctx context.Context, namespace string, owners []metav1.OwnerReference, template corev1.PodTemplateSpec, podRefs ...podVolumeSourceRefSet) ([]domainresource.WorkloadRelationView, error) {
	relations := map[string]domainresource.WorkloadRelationView{}
	add := func(kind, name, relation string) {
		kind, name = strings.TrimSpace(kind), strings.TrimSpace(name)
		if kind == "" || name == "" {
			return
		}
		item := domainresource.WorkloadRelationView{Kind: kind, Name: name, Namespace: namespace, Relation: relation}
		relations[kind+"\x00"+name+"\x00"+relation] = item
	}
	for _, owner := range owners {
		add(owner.Kind, owner.Name, "owner")
	}

	refs := buildPodVolumeSourceRefs(corev1.Pod{Spec: template.Spec})
	for _, podRef := range podRefs {
		mergePodVolumeSourceRefs(&refs, podRef)
	}
	for name := range refs.pvcs {
		add("PersistentVolumeClaim", name, "volume")
	}
	for name := range refs.configMaps {
		add("ConfigMap", name, "config")
	}
	for name := range refs.secrets {
		add("Secret", name, "secret")
	}

	services, err := c.typed.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	serviceNames := map[string]struct{}{}
	for _, service := range services.Items {
		if selectorMatchesPodLabels(service.Spec.Selector, template.Labels) {
			add("Service", service.Name, "selected-by-service")
			serviceNames[service.Name] = struct{}{}
		}
	}
	if len(serviceNames) > 0 {
		ingresses, err := c.typed.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, ingress := range ingresses.Items {
			for _, serviceName := range extractIngressBackendServices(ingress) {
				if _, ok := serviceNames[serviceName]; ok {
					add("Ingress", ingress.Name, "routes-service")
					break
				}
			}
		}
		if c.dynamic != nil {
			if routes, routeErr := c.ListHTTPRoutes(ctx, namespace); routeErr == nil {
				for _, route := range routes {
					if referencesWorkloadService(route.BackendServices, serviceNames) {
						add("HTTPRoute", route.Name, "routes-service")
					}
				}
			}
			if routes, routeErr := c.ListGRPCRoutes(ctx, namespace); routeErr == nil {
				for _, route := range routes {
					if referencesWorkloadService(route.BackendServices, serviceNames) {
						add("GRPCRoute", route.Name, "routes-service")
					}
				}
			}
		}
	}

	result := make([]domainresource.WorkloadRelationView, 0, len(relations))
	for _, relation := range relations {
		result = append(result, relation)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].Relation < result[j].Relation
	})
	return result, nil
}

func emptyPodVolumeSourceRefs() podVolumeSourceRefSet {
	return buildPodVolumeSourceRefs(corev1.Pod{})
}

func mergePodVolumeSourceRefs(target *podVolumeSourceRefSet, source podVolumeSourceRefSet) {
	for name := range source.configMaps {
		target.configMaps[name] = struct{}{}
	}
	for name := range source.secrets {
		target.secrets[name] = struct{}{}
	}
	for name := range source.pvcs {
		target.pvcs[name] = struct{}{}
	}
}

func referencesWorkloadService(backends []string, services map[string]struct{}) bool {
	for _, backend := range backends {
		if _, ok := services[backend]; ok {
			return true
		}
	}
	return false
}

func (c *Client) listOwnedJobs(ctx context.Context, namespace string, ownerUID types.UID) ([]domainresource.JobView, error) {
	items, err := c.typed.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	jobs := make([]domainresource.JobView, 0, len(items.Items))
	for _, item := range items.Items {
		if ownedByUIDAndKind(item.OwnerReferences, ownerUID, "CronJob") {
			jobs = append(jobs, mapJob(item))
		}
	}
	sort.SliceStable(jobs, func(i, j int) bool { return jobs[i].Name > jobs[j].Name })
	return jobs, nil
}

func ownedByUID(owners []metav1.OwnerReference, uid types.UID) bool {
	for _, owner := range owners {
		if owner.UID == uid {
			return true
		}
	}
	return false
}

func ownedByUIDAndKind(owners []metav1.OwnerReference, uid types.UID, kind string) bool {
	for _, owner := range owners {
		if owner.UID == uid && owner.Kind == kind {
			return true
		}
	}
	return false
}
