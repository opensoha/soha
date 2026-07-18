package resourcebackend

import (
	"context"
	"sort"
	"strings"
	"time"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
)

func buildWorkloadAssociations(ctx context.Context, bundle *k8sinfra.Bundle, namespace string, selector labels.Selector, ownerUID types.UID, ownerKind string, template corev1.PodTemplateSpec, owners []metav1.OwnerReference) ([]domainresource.PodView, []domainresource.WorkloadRelationView, error) {
	if selector.Empty() {
		related, err := listWorkloadRelations(ctx, bundle, namespace, template, owners)
		return nil, related, err
	}
	podList, err := bundle.Typed.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, nil, err
	}
	pods := make([]domainresource.PodView, 0, len(podList.Items))
	refs := buildPodSourceRefs(corev1.Pod{Spec: template.Spec})
	for _, pod := range podList.Items {
		if ownerUID == "" || ownedByUID(pod.OwnerReferences, ownerUID, ownerKind) {
			pods = append(pods, mapPodView(pod))
			mergeWorkloadSourceRefs(&refs, buildPodSourceRefs(pod))
		}
	}
	sort.SliceStable(pods, func(i, j int) bool { return pods[i].Name < pods[j].Name })
	related, err := listWorkloadRelations(ctx, bundle, namespace, template, owners, refs)
	if err != nil {
		return nil, nil, err
	}
	return pods, related, nil
}

func mergeWorkloadSourceRefs(target *podSourceRefs, source podSourceRefs) {
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

func (d *Direct) workloadPodRelations(ctx context.Context, clusterID, namespace string, selector *metav1.LabelSelector, template corev1.PodTemplateSpec, owners []metav1.OwnerReference, jobLabel, jobName string, ownerUID types.UID, ownerKind string) ([]domainresource.PodView, []domainresource.WorkloadRelationView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	labelSelector := labels.Everything()
	if selector != nil {
		labelSelector, err = metav1.LabelSelectorAsSelector(selector)
		if err != nil {
			return nil, nil, err
		}
	} else if jobName != "" {
		labelSelector = labels.Set{jobLabel: jobName}.AsSelector()
	}
	pods, related, err := buildWorkloadAssociations(queryCtx, bundle, namespace, labelSelector, ownerUID, ownerKind, template, owners)
	if err != nil {
		return nil, nil, err
	}
	return pods, d.appendGatewayRouteRelations(queryCtx, clusterID, namespace, related), nil
}

func (d *Direct) cronJobAssociations(ctx context.Context, clusterID string, item batchv1.CronJob) ([]domainresource.JobView, []domainresource.WorkloadRelationView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	jobs, related, err := buildCronJobAssociations(queryCtx, bundle, item)
	if err != nil {
		return nil, nil, err
	}
	return jobs, d.appendGatewayRouteRelations(queryCtx, clusterID, item.Namespace, related), nil
}

func buildCronJobAssociations(ctx context.Context, bundle *k8sinfra.Bundle, item batchv1.CronJob) ([]domainresource.JobView, []domainresource.WorkloadRelationView, error) {
	list, err := bundle.Typed.BatchV1().Jobs(item.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, err
	}
	jobs := make([]domainresource.JobView, 0)
	for _, job := range list.Items {
		if ownedByUID(job.OwnerReferences, item.UID, "CronJob") {
			jobs = append(jobs, mapJob(job))
		}
	}
	sort.SliceStable(jobs, func(i, j int) bool { return jobs[i].Name > jobs[j].Name })
	related, err := listWorkloadRelations(ctx, bundle, item.Namespace, item.Spec.JobTemplate.Spec.Template, item.OwnerReferences)
	return jobs, related, err
}

func listWorkloadRelations(ctx context.Context, bundle *k8sinfra.Bundle, namespace string, template corev1.PodTemplateSpec, owners []metav1.OwnerReference, sourceRefs ...podSourceRefs) ([]domainresource.WorkloadRelationView, error) {
	relations := make(map[string]domainresource.WorkloadRelationView)
	add := func(kind, name, relation string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		item := domainresource.WorkloadRelationView{Kind: kind, Name: name, Namespace: namespace, Relation: relation}
		relations[kind+"/"+namespace+"/"+name+"/"+relation] = item
	}
	for _, owner := range owners {
		add(owner.Kind, owner.Name, "owner")
	}
	refs := buildPodSourceRefs(corev1.Pod{Spec: template.Spec})
	for _, source := range sourceRefs {
		mergeWorkloadSourceRefs(&refs, source)
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
	serviceNames := make(map[string]struct{})
	if services, err := bundle.Typed.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{}); err == nil {
		for _, service := range services.Items {
			if selectorMatchesLabels(service.Spec.Selector, template.Labels) {
				add("Service", service.Name, "selected-by-service")
				serviceNames[service.Name] = struct{}{}
			}
		}
	}
	if len(serviceNames) > 0 {
		if ingresses, err := bundle.Typed.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{}); err == nil {
			for _, ingress := range ingresses.Items {
				for _, serviceName := range extractIngressBackendServices(ingress) {
					if _, ok := serviceNames[serviceName]; ok {
						add("Ingress", ingress.Name, "routes-service")
						break
					}
				}
			}
		}
	}
	items := make([]domainresource.WorkloadRelationView, 0, len(relations))
	for _, item := range relations {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].Relation < items[j].Relation
	})
	return items, nil
}

func (d *Direct) appendGatewayRouteRelations(ctx context.Context, clusterID, namespace string, related []domainresource.WorkloadRelationView) []domainresource.WorkloadRelationView {
	services := make(map[string]struct{})
	seen := make(map[string]struct{}, len(related))
	for _, item := range related {
		seen[item.Kind+"/"+item.Name+"/"+item.Relation] = struct{}{}
		if item.Kind == "Service" && item.Relation == "selected-by-service" {
			services[item.Name] = struct{}{}
		}
	}
	add := func(kind, name string, backends []string) {
		for _, backend := range backends {
			if _, ok := services[backend]; !ok {
				continue
			}
			key := kind + "/" + name + "/routes-service"
			if _, ok := seen[key]; !ok {
				related = append(related, domainresource.WorkloadRelationView{Kind: kind, Namespace: namespace, Name: name, Relation: "routes-service"})
				seen[key] = struct{}{}
			}
			return
		}
	}
	if routes, err := d.ListHTTPRoutes(ctx, clusterID, namespace); err == nil {
		for _, route := range routes {
			add("HTTPRoute", route.Name, route.BackendServices)
		}
	}
	if routes, err := d.ListGRPCRoutes(ctx, clusterID, namespace); err == nil {
		for _, route := range routes {
			add("GRPCRoute", route.Name, route.BackendServices)
		}
	}
	sort.SliceStable(related, func(i, j int) bool {
		if related[i].Kind != related[j].Kind {
			return related[i].Kind < related[j].Kind
		}
		if related[i].Name != related[j].Name {
			return related[i].Name < related[j].Name
		}
		return related[i].Relation < related[j].Relation
	})
	return related
}

func ownedByUID(owners []metav1.OwnerReference, uid types.UID, kind string) bool {
	for _, owner := range owners {
		if owner.UID == uid && owner.Kind == kind {
			return true
		}
	}
	return false
}
