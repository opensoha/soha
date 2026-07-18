package resourcebackend

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"time"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (d *Direct) ListConfigMaps(ctx context.Context, clusterID, namespace string) ([]domainresource.ConfigMapView, error) {
	bundle, queryCtx, cancel, err := d.configurationQuery(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	table, err := listTable(queryCtx, bundle, schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, true, namespace)
	if err != nil {
		return nil, err
	}
	return mapConfigMapTable(table)
}

func (d *Direct) GetConfigMapDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.ConfigMapDetailView, error) {
	bundle, queryCtx, cancel, err := d.configurationQuery(ctx, clusterID)
	if err != nil {
		return domainresource.ConfigMapDetailView{}, err
	}
	defer cancel()
	item, err := bundle.Typed.CoreV1().ConfigMaps(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ConfigMapDetailView{}, err
	}
	return mapConfigMapDetail(*item), nil
}

func (d *Direct) UpdateConfigMapData(ctx context.Context, clusterID, namespace, name string, data, binaryData map[string]string) (domainresource.ConfigMapDetailView, error) {
	bundle, queryCtx, cancel, err := d.configurationQuery(ctx, clusterID)
	if err != nil {
		return domainresource.ConfigMapDetailView{}, err
	}
	defer cancel()
	item, err := bundle.Typed.CoreV1().ConfigMaps(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ConfigMapDetailView{}, err
	}
	decoded := make(map[string][]byte, len(binaryData))
	for key, value := range binaryData {
		raw, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return domainresource.ConfigMapDetailView{}, fmt.Errorf("%w: invalid binaryData.%s: %v", apperrors.ErrInvalidArgument, key, err)
		}
		decoded[key] = raw
	}
	item.Data = data
	item.BinaryData = decoded
	updated, err := bundle.Typed.CoreV1().ConfigMaps(namespace).Update(queryCtx, item, metav1.UpdateOptions{})
	if err != nil {
		return domainresource.ConfigMapDetailView{}, err
	}
	return mapConfigMapDetail(*updated), nil
}

func (d *Direct) GetSecretDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.SecretDetailView, error) {
	bundle, queryCtx, cancel, err := d.configurationQuery(ctx, clusterID)
	if err != nil {
		return domainresource.SecretDetailView{}, err
	}
	defer cancel()
	item, err := bundle.Typed.CoreV1().Secrets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.SecretDetailView{}, err
	}
	return mapSecretDetail(*item), nil
}

func (d *Direct) UpdateSecretData(ctx context.Context, clusterID, namespace, name string, data map[string]string) (domainresource.SecretDetailView, error) {
	bundle, queryCtx, cancel, err := d.configurationQuery(ctx, clusterID)
	if err != nil {
		return domainresource.SecretDetailView{}, err
	}
	defer cancel()
	item, err := bundle.Typed.CoreV1().Secrets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.SecretDetailView{}, err
	}
	item.Data = nil
	item.StringData = data
	updated, err := bundle.Typed.CoreV1().Secrets(namespace).Update(queryCtx, item, metav1.UpdateOptions{})
	if err != nil {
		return domainresource.SecretDetailView{}, err
	}
	return mapSecretDetail(*updated), nil
}

func (d *Direct) ListConfigReferences(ctx context.Context, clusterID, namespace, name string, configMap bool) ([]domainresource.ConfigReferenceView, error) {
	bundle, queryCtx, cancel, err := d.configurationQuery(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	referencesFor := func(kind, itemName string, spec corev1.PodSpec) []domainresource.ConfigReferenceView {
		refs := make([]domainresource.ConfigReferenceView, 0)
		for _, path := range collectPodSpecConfigReferencePaths(spec, name, configMap) {
			refs = append(refs, domainresource.ConfigReferenceView{Kind: kind, Name: itemName, Namespace: namespace, Path: path})
		}
		return refs
	}
	tasks := []func() ([]domainresource.ConfigReferenceView, error){
		func() ([]domainresource.ConfigReferenceView, error) {
			items, err := d.listReferencePods(queryCtx, bundle, clusterID, namespace)
			refs := make([]domainresource.ConfigReferenceView, 0)
			for _, item := range items {
				refs = append(refs, referencesFor("Pod", item.Name, item.Spec)...)
			}
			return refs, err
		},
		func() ([]domainresource.ConfigReferenceView, error) {
			items, err := d.listReferenceDeployments(queryCtx, bundle, clusterID, namespace)
			refs := make([]domainresource.ConfigReferenceView, 0)
			for _, item := range items {
				refs = append(refs, referencesFor("Deployment", item.Name, item.Spec.Template.Spec)...)
			}
			return refs, err
		},
		func() ([]domainresource.ConfigReferenceView, error) {
			items, err := d.listReferenceStatefulSets(queryCtx, bundle, clusterID, namespace)
			refs := make([]domainresource.ConfigReferenceView, 0)
			for _, item := range items {
				refs = append(refs, referencesFor("StatefulSet", item.Name, item.Spec.Template.Spec)...)
			}
			return refs, err
		},
		func() ([]domainresource.ConfigReferenceView, error) {
			items, err := bundle.Typed.AppsV1().DaemonSets(namespace).List(queryCtx, metav1.ListOptions{})
			refs := make([]domainresource.ConfigReferenceView, 0)
			if err == nil {
				for _, item := range items.Items {
					refs = append(refs, referencesFor("DaemonSet", item.Name, item.Spec.Template.Spec)...)
				}
			}
			return refs, err
		},
		func() ([]domainresource.ConfigReferenceView, error) {
			items, err := bundle.Typed.AppsV1().ReplicaSets(namespace).List(queryCtx, metav1.ListOptions{})
			refs := make([]domainresource.ConfigReferenceView, 0)
			if err == nil {
				for _, item := range items.Items {
					refs = append(refs, referencesFor("ReplicaSet", item.Name, item.Spec.Template.Spec)...)
				}
			}
			return refs, err
		},
		func() ([]domainresource.ConfigReferenceView, error) {
			items, err := bundle.Typed.CoreV1().ReplicationControllers(namespace).List(queryCtx, metav1.ListOptions{})
			refs := make([]domainresource.ConfigReferenceView, 0)
			if err == nil {
				for _, item := range items.Items {
					refs = append(refs, referencesFor("ReplicationController", item.Name, item.Spec.Template.Spec)...)
				}
			}
			return refs, err
		},
		func() ([]domainresource.ConfigReferenceView, error) {
			items, err := bundle.Typed.BatchV1().Jobs(namespace).List(queryCtx, metav1.ListOptions{})
			refs := make([]domainresource.ConfigReferenceView, 0)
			if err == nil {
				for _, item := range items.Items {
					refs = append(refs, referencesFor("Job", item.Name, item.Spec.Template.Spec)...)
				}
			}
			return refs, err
		},
		func() ([]domainresource.ConfigReferenceView, error) {
			items, err := bundle.Typed.BatchV1().CronJobs(namespace).List(queryCtx, metav1.ListOptions{})
			refs := make([]domainresource.ConfigReferenceView, 0)
			if err == nil {
				for _, item := range items.Items {
					refs = append(refs, referencesFor("CronJob", item.Name, item.Spec.JobTemplate.Spec.Template.Spec)...)
				}
			}
			return refs, err
		},
	}
	refs, err := collectConfigReferenceTasks(tasks)
	if err != nil {
		return nil, err
	}
	sortConfigReferences(refs)
	return refs, nil
}

type configReferenceResult struct {
	items []domainresource.ConfigReferenceView
	err   error
}

func collectConfigReferenceTasks(tasks []func() ([]domainresource.ConfigReferenceView, error)) ([]domainresource.ConfigReferenceView, error) {
	results := make(chan configReferenceResult, len(tasks))
	for _, task := range tasks {
		go func(task func() ([]domainresource.ConfigReferenceView, error)) {
			items, err := task()
			results <- configReferenceResult{items: items, err: err}
		}(task)
	}
	refs := make([]domainresource.ConfigReferenceView, 0)
	for range tasks {
		result := <-results
		if result.err != nil {
			return nil, result.err
		}
		refs = append(refs, result.items...)
	}
	return refs, nil
}

func (d *Direct) listReferencePods(ctx context.Context, bundle *k8sinfra.Bundle, clusterID, namespace string) ([]corev1.Pod, error) {
	if d.cache != nil {
		if items, err := d.cache.ListPods(clusterID, namespace); err == nil {
			return items, nil
		} else if !d.cache.CacheUnavailable(err) {
			return nil, err
		}
	}
	items, err := bundle.Typed.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	return items.Items, err
}

func (d *Direct) listReferenceDeployments(ctx context.Context, bundle *k8sinfra.Bundle, clusterID, namespace string) ([]appsv1.Deployment, error) {
	if d.cache != nil {
		if items, err := d.cache.ListDeployments(clusterID, namespace); err == nil {
			return items, nil
		} else if !d.cache.CacheUnavailable(err) {
			return nil, err
		}
	}
	items, err := bundle.Typed.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	return items.Items, err
}

func (d *Direct) listReferenceStatefulSets(ctx context.Context, bundle *k8sinfra.Bundle, clusterID, namespace string) ([]appsv1.StatefulSet, error) {
	if d.cache != nil {
		if items, err := d.cache.ListStatefulSets(clusterID, namespace); err == nil {
			return items, nil
		} else if !d.cache.CacheUnavailable(err) {
			return nil, err
		}
	}
	items, err := bundle.Typed.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	return items.Items, err
}

func (d *Direct) configurationQuery(ctx context.Context, clusterID string) (*k8sinfra.Bundle, context.Context, context.CancelFunc, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, nil, nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	return bundle, queryCtx, cancel, nil
}

func listConfigurationNamespace[T any](ctx context.Context, direct *Direct, clusterID, namespace string, list func(context.Context, *k8sinfra.Bundle, string) ([]T, error)) ([]T, error) {
	if strings.TrimSpace(namespace) == "" {
		return listAcrossNamespaces(ctx, direct, clusterID, 4*time.Second, list)
	}
	bundle, err := direct.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	return list(queryCtx, bundle, namespace)
}

func collectPodSpecConfigReferencePaths(spec corev1.PodSpec, name string, configMap bool) []string {
	paths := map[string]struct{}{}
	add := func(path string) {
		if strings.TrimSpace(path) != "" {
			paths[path] = struct{}{}
		}
	}
	collectVolumeConfigReferencePaths(spec.Volumes, name, configMap, add)
	if !configMap {
		collectImagePullSecretReferences(spec.ImagePullSecrets, name, add)
	}
	collectContainerConfigReferencePaths(spec.InitContainers, name, configMap, add)
	collectContainerConfigReferencePaths(spec.Containers, name, configMap, add)
	items := make([]string, 0, len(paths))
	for path := range paths {
		items = append(items, path)
	}
	sort.Strings(items)
	return items
}

func collectVolumeConfigReferencePaths(volumes []corev1.Volume, name string, configMap bool, add func(string)) {
	for _, volume := range volumes {
		if configMap && volume.ConfigMap != nil && volume.ConfigMap.Name == name {
			add(fmt.Sprintf("volume/%s/configMap", volume.Name))
		}
		if !configMap && volume.Secret != nil && volume.Secret.SecretName == name {
			add(fmt.Sprintf("volume/%s/secret", volume.Name))
		}
		if volume.Projected != nil {
			for _, source := range volume.Projected.Sources {
				if configMap && source.ConfigMap != nil && source.ConfigMap.Name == name {
					add(fmt.Sprintf("volume/%s/projected/configMap", volume.Name))
				}
				if !configMap && source.Secret != nil && source.Secret.Name == name {
					add(fmt.Sprintf("volume/%s/projected/secret", volume.Name))
				}
			}
		}
	}
}

func collectImagePullSecretReferences(secrets []corev1.LocalObjectReference, name string, add func(string)) {
	for _, secret := range secrets {
		if secret.Name == name {
			add("imagePullSecrets")
		}
	}
}

func collectContainerConfigReferencePaths(containers []corev1.Container, name string, configMap bool, add func(string)) {
	for _, container := range containers {
		for _, env := range container.Env {
			if env.ValueFrom == nil {
				continue
			}
			if configMap && env.ValueFrom.ConfigMapKeyRef != nil && env.ValueFrom.ConfigMapKeyRef.Name == name {
				add(fmt.Sprintf("container/%s/env/%s/configMapKeyRef", container.Name, env.Name))
			}
			if !configMap && env.ValueFrom.SecretKeyRef != nil && env.ValueFrom.SecretKeyRef.Name == name {
				add(fmt.Sprintf("container/%s/env/%s/secretKeyRef", container.Name, env.Name))
			}
		}
		for _, envFrom := range container.EnvFrom {
			if configMap && envFrom.ConfigMapRef != nil && envFrom.ConfigMapRef.Name == name {
				add(fmt.Sprintf("container/%s/envFrom/configMapRef", container.Name))
			}
			if !configMap && envFrom.SecretRef != nil && envFrom.SecretRef.Name == name {
				add(fmt.Sprintf("container/%s/envFrom/secretRef", container.Name))
			}
		}
	}
}

func sortConfigReferences(refs []domainresource.ConfigReferenceView) {
	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].Namespace != refs[j].Namespace {
			return refs[i].Namespace < refs[j].Namespace
		}
		if refs[i].Kind != refs[j].Kind {
			return refs[i].Kind < refs[j].Kind
		}
		if refs[i].Name != refs[j].Name {
			return refs[i].Name < refs[j].Name
		}
		return refs[i].Path < refs[j].Path
	})
}

func (d *Direct) ListIngressClasses(ctx context.Context, clusterID string) ([]domainresource.IngressClassView, error) {
	bundle, queryCtx, cancel, err := d.configurationQuery(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.NetworkingV1().IngressClasses().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.IngressClassView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapIngressClass(item))
	}
	return views, nil
}

func (d *Direct) GetIngressClassDetail(ctx context.Context, clusterID, name string) (domainresource.IngressClassDetailView, error) {
	bundle, queryCtx, cancel, err := d.configurationQuery(ctx, clusterID)
	if err != nil {
		return domainresource.IngressClassDetailView{}, err
	}
	defer cancel()
	item, err := bundle.Typed.NetworkingV1().IngressClasses().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.IngressClassDetailView{}, err
	}
	// ponytail: IngressClass has no server-side reverse index; cap related rows until an informer index owns it.
	ingressItems, err := bundle.Typed.NetworkingV1().Ingresses("").List(queryCtx, metav1.ListOptions{Limit: 500})
	if err != nil {
		return domainresource.IngressClassDetailView{}, err
	}
	matched := make([]domainresource.IngressView, 0)
	for _, ingressItem := range ingressItems.Items {
		ingress := mapIngress(ingressItem)
		if ingress.ClassName == name {
			matched = append(matched, ingress)
		}
	}
	return domainresource.IngressClassDetailView{IngressClassView: mapIngressClass(*item), Labels: item.Labels, Annotations: item.Annotations, Ingresses: matched}, nil
}

func (d *Direct) ListPriorityClasses(ctx context.Context, clusterID string) ([]domainresource.PriorityClassView, error) {
	bundle, queryCtx, cancel, err := d.configurationQuery(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.SchedulingV1().PriorityClasses().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.PriorityClassView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapPriorityClass(item))
	}
	return views, nil
}

func (d *Direct) ListRuntimeClasses(ctx context.Context, clusterID string) ([]domainresource.RuntimeClassView, error) {
	bundle, queryCtx, cancel, err := d.configurationQuery(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.NodeV1().RuntimeClasses().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.RuntimeClassView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapRuntimeClass(item))
	}
	return views, nil
}

func (d *Direct) ListMutatingWebhookConfigurations(ctx context.Context, clusterID string) ([]domainresource.MutatingWebhookConfigurationView, error) {
	bundle, queryCtx, cancel, err := d.configurationQuery(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	table, err := listTable(queryCtx, bundle, schema.GroupVersionResource{Group: "admissionregistration.k8s.io", Version: "v1", Resource: "mutatingwebhookconfigurations"}, false, "")
	if err != nil {
		return nil, err
	}
	return mapMutatingWebhookConfigurationTable(table)
}

func (d *Direct) GetMutatingWebhookConfigurationDetail(ctx context.Context, clusterID, name string) (domainresource.AdmissionWebhookConfigurationDetailView, error) {
	bundle, queryCtx, cancel, err := d.configurationQuery(ctx, clusterID)
	if err != nil {
		return domainresource.AdmissionWebhookConfigurationDetailView{}, err
	}
	defer cancel()
	item, err := bundle.Typed.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.AdmissionWebhookConfigurationDetailView{}, err
	}
	webhooks := make([]domainresource.AdmissionWebhookView, 0, len(item.Webhooks))
	for _, webhook := range item.Webhooks {
		webhooks = append(webhooks, mapAdmissionWebhook(webhook.Name, webhook.ClientConfig, webhook.Rules, webhook.FailurePolicy, webhook.MatchPolicy, webhook.SideEffects, webhook.TimeoutSeconds, webhook.AdmissionReviewVersions, webhook.NamespaceSelector, webhook.ObjectSelector))
	}
	return mapAdmissionWebhookConfigurationDetail(item.ObjectMeta, webhooks), nil
}

func (d *Direct) ListValidatingWebhookConfigurations(ctx context.Context, clusterID string) ([]domainresource.ValidatingWebhookConfigurationView, error) {
	bundle, queryCtx, cancel, err := d.configurationQuery(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	table, err := listTable(queryCtx, bundle, schema.GroupVersionResource{Group: "admissionregistration.k8s.io", Version: "v1", Resource: "validatingwebhookconfigurations"}, false, "")
	if err != nil {
		return nil, err
	}
	return mapValidatingWebhookConfigurationTable(table)
}

func (d *Direct) GetValidatingWebhookConfigurationDetail(ctx context.Context, clusterID, name string) (domainresource.AdmissionWebhookConfigurationDetailView, error) {
	bundle, queryCtx, cancel, err := d.configurationQuery(ctx, clusterID)
	if err != nil {
		return domainresource.AdmissionWebhookConfigurationDetailView{}, err
	}
	defer cancel()
	item, err := bundle.Typed.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.AdmissionWebhookConfigurationDetailView{}, err
	}
	webhooks := make([]domainresource.AdmissionWebhookView, 0, len(item.Webhooks))
	for _, webhook := range item.Webhooks {
		webhooks = append(webhooks, mapAdmissionWebhook(webhook.Name, webhook.ClientConfig, webhook.Rules, webhook.FailurePolicy, webhook.MatchPolicy, webhook.SideEffects, webhook.TimeoutSeconds, webhook.AdmissionReviewVersions, webhook.NamespaceSelector, webhook.ObjectSelector))
	}
	return mapAdmissionWebhookConfigurationDetail(item.ObjectMeta, webhooks), nil
}

func (d *Direct) ListResourceQuotas(ctx context.Context, clusterID, namespace string) ([]domainresource.ResourceQuotaView, error) {
	items, err := listConfigurationNamespace(ctx, d, clusterID, namespace, func(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]corev1.ResourceQuota, error) {
		list, err := bundle.Typed.CoreV1().ResourceQuotas(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return list.Items, nil
	})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ResourceQuotaView, 0, len(items))
	for _, item := range items {
		views = append(views, mapResourceQuota(item))
	}
	return views, nil
}

func (d *Direct) GetResourceQuotaDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.ResourceQuotaDetailView, error) {
	bundle, queryCtx, cancel, err := d.configurationQuery(ctx, clusterID)
	if err != nil {
		return domainresource.ResourceQuotaDetailView{}, err
	}
	defer cancel()
	item, err := bundle.Typed.CoreV1().ResourceQuotas(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ResourceQuotaDetailView{}, err
	}
	return mapResourceQuotaDetail(*item), nil
}

func (d *Direct) ListLimitRanges(ctx context.Context, clusterID, namespace string) ([]domainresource.LimitRangeView, error) {
	items, err := listConfigurationNamespace(ctx, d, clusterID, namespace, func(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]corev1.LimitRange, error) {
		list, err := bundle.Typed.CoreV1().LimitRanges(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return list.Items, nil
	})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.LimitRangeView, 0, len(items))
	for _, item := range items {
		views = append(views, mapLimitRange(item))
	}
	return views, nil
}

func (d *Direct) GetLimitRangeDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.LimitRangeDetailView, error) {
	bundle, queryCtx, cancel, err := d.configurationQuery(ctx, clusterID)
	if err != nil {
		return domainresource.LimitRangeDetailView{}, err
	}
	defer cancel()
	item, err := bundle.Typed.CoreV1().LimitRanges(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.LimitRangeDetailView{}, err
	}
	return mapLimitRangeDetail(*item), nil
}

func (d *Direct) ListLeases(ctx context.Context, clusterID, namespace string) ([]domainresource.LeaseView, error) {
	items, err := listConfigurationNamespace(ctx, d, clusterID, namespace, func(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]coordinationv1.Lease, error) {
		list, err := bundle.Typed.CoordinationV1().Leases(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return list.Items, nil
	})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.LeaseView, 0, len(items))
	for _, item := range items {
		views = append(views, mapLease(item))
	}
	return views, nil
}

func (d *Direct) ListReplicationControllers(ctx context.Context, clusterID, namespace string) ([]domainresource.ReplicationControllerView, error) {
	items, err := listConfigurationNamespace(ctx, d, clusterID, namespace, func(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]corev1.ReplicationController, error) {
		list, err := bundle.Typed.CoreV1().ReplicationControllers(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return list.Items, nil
	})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ReplicationControllerView, 0, len(items))
	for _, item := range items {
		views = append(views, mapReplicationController(item))
	}
	return views, nil
}

func mapConfigMap(item corev1.ConfigMap) domainresource.ConfigMapView {
	return domainresource.ConfigMapView{Name: item.Name, Namespace: item.Namespace, DataEntries: len(item.Data), AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
}

func mapConfigMapTable(table metav1.Table) ([]domainresource.ConfigMapView, error) {
	dataColumn := tableColumnIndex(table.ColumnDefinitions, "Data")
	if dataColumn < 0 {
		return nil, fmt.Errorf("configmap table is missing Data column")
	}
	return tableViews(table, func(row metav1.TableRow, metadata metav1.Object) (domainresource.ConfigMapView, error) {
		entries, err := tableIntCell(row.Cells, dataColumn)
		if err != nil {
			return domainresource.ConfigMapView{}, err
		}
		return domainresource.ConfigMapView{Name: metadata.GetName(), Namespace: metadata.GetNamespace(), DataEntries: entries, AgeSeconds: secondsSince(metadata.GetCreationTimestamp().Time)}, nil
	})
}

func mapSecret(item corev1.Secret) domainresource.SecretView {
	immutable := item.Immutable != nil && *item.Immutable
	return domainresource.SecretView{Name: item.Name, Namespace: item.Namespace, Type: string(item.Type), DataEntries: len(item.Data), Immutable: &immutable, AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
}

func mapConfigMapDetail(item corev1.ConfigMap) domainresource.ConfigMapDetailView {
	binaryData := make(map[string]string, len(item.BinaryData))
	for key, value := range item.BinaryData {
		binaryData[key] = base64.StdEncoding.EncodeToString(value)
	}
	return domainresource.ConfigMapDetailView{Name: item.Name, Namespace: item.Namespace, Labels: item.Labels, Annotations: item.Annotations, Data: item.Data, BinaryData: binaryData, Immutable: item.Immutable != nil && *item.Immutable, CreatedAt: item.CreationTimestamp.Format(time.RFC3339), AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
}

func mapSecretDetail(item corev1.Secret) domainresource.SecretDetailView {
	data := make(map[string]string, len(item.Data))
	for key, value := range item.Data {
		data[key] = base64.StdEncoding.EncodeToString(value)
	}
	return domainresource.SecretDetailView{Name: item.Name, Namespace: item.Namespace, Type: string(item.Type), Labels: item.Labels, Annotations: item.Annotations, Data: data, Immutable: item.Immutable != nil && *item.Immutable, CreatedAt: item.CreationTimestamp.Format(time.RFC3339), AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
}

func mapIngressClass(item networkingv1.IngressClass) domainresource.IngressClassView {
	isDefault := strings.EqualFold(strings.TrimSpace(item.Annotations["ingressclass.kubernetes.io/is-default-class"]), "true")
	parameters := ""
	if item.Spec.Parameters != nil {
		parameters = fmt.Sprintf("%s/%s", item.Spec.Parameters.Kind, item.Spec.Parameters.Name)
	}
	return domainresource.IngressClassView{Name: item.Name, Controller: item.Spec.Controller, IsDefault: isDefault, Parameters: parameters, AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
}

func mapPriorityClass(item schedulingv1.PriorityClass) domainresource.PriorityClassView {
	preemptionPolicy := ""
	if item.PreemptionPolicy != nil {
		preemptionPolicy = string(*item.PreemptionPolicy)
	}
	return domainresource.PriorityClassView{Name: item.Name, Value: item.Value, GlobalDefault: item.GlobalDefault, PreemptionPolicy: preemptionPolicy, Description: item.Description, AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
}

func mapRuntimeClass(item nodev1.RuntimeClass) domainresource.RuntimeClassView {
	return domainresource.RuntimeClassView{Name: item.Name, Handler: item.Handler, AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
}

func mapMutatingWebhookConfigurationTable(table metav1.Table) ([]domainresource.MutatingWebhookConfigurationView, error) {
	webhooksColumn := tableColumnIndex(table.ColumnDefinitions, "Webhooks")
	if webhooksColumn < 0 {
		return nil, fmt.Errorf("mutating webhook table is missing Webhooks column")
	}
	return tableViews(table, func(row metav1.TableRow, metadata metav1.Object) (domainresource.MutatingWebhookConfigurationView, error) {
		webhooks, err := tableIntCell(row.Cells, webhooksColumn)
		if err != nil {
			return domainresource.MutatingWebhookConfigurationView{}, err
		}
		return domainresource.MutatingWebhookConfigurationView{Name: metadata.GetName(), Webhooks: webhooks, AgeSeconds: secondsSince(metadata.GetCreationTimestamp().Time)}, nil
	})
}

func mapValidatingWebhookConfigurationTable(table metav1.Table) ([]domainresource.ValidatingWebhookConfigurationView, error) {
	webhooksColumn := tableColumnIndex(table.ColumnDefinitions, "Webhooks")
	if webhooksColumn < 0 {
		return nil, fmt.Errorf("validating webhook table is missing Webhooks column")
	}
	return tableViews(table, func(row metav1.TableRow, metadata metav1.Object) (domainresource.ValidatingWebhookConfigurationView, error) {
		webhooks, err := tableIntCell(row.Cells, webhooksColumn)
		if err != nil {
			return domainresource.ValidatingWebhookConfigurationView{}, err
		}
		return domainresource.ValidatingWebhookConfigurationView{Name: metadata.GetName(), Webhooks: webhooks, AgeSeconds: secondsSince(metadata.GetCreationTimestamp().Time)}, nil
	})
}

func mapAdmissionWebhookConfigurationDetail(metadata metav1.ObjectMeta, webhooks []domainresource.AdmissionWebhookView) domainresource.AdmissionWebhookConfigurationDetailView {
	return domainresource.AdmissionWebhookConfigurationDetailView{
		Name: metadata.Name, Labels: cloneMap(metadata.Labels), Annotations: cloneMap(metadata.Annotations),
		CreatedAt: metadata.CreationTimestamp.Format(time.RFC3339), AgeSeconds: secondsSince(metadata.CreationTimestamp.Time), Webhooks: webhooks,
	}
}

func mapAdmissionWebhook(name string, client admissionregistrationv1.WebhookClientConfig, rules []admissionregistrationv1.RuleWithOperations, failurePolicy *admissionregistrationv1.FailurePolicyType, matchPolicy *admissionregistrationv1.MatchPolicyType, sideEffects *admissionregistrationv1.SideEffectClass, timeout *int32, versions []string, namespaceSelector, objectSelector *metav1.LabelSelector) domainresource.AdmissionWebhookView {
	view := domainresource.AdmissionWebhookView{
		Name: name, CABundleConfigured: len(client.CABundle) > 0, AdmissionReviewVersions: append([]string(nil), versions...),
		NamespaceSelector: labelSelectorString(namespaceSelector), ObjectSelector: labelSelectorString(objectSelector),
	}
	if client.URL != nil {
		view.URL, view.ClientTarget = *client.URL, *client.URL
	}
	if service := client.Service; service != nil {
		view.ServiceName, view.ServiceNamespace = service.Name, service.Namespace
		if service.Path != nil {
			view.ServicePath = *service.Path
		}
		if service.Port != nil {
			view.ServicePort = *service.Port
		}
		view.ClientTarget = service.Namespace + "/" + service.Name
	}
	if failurePolicy != nil {
		view.FailurePolicy = string(*failurePolicy)
	}
	if matchPolicy != nil {
		view.MatchPolicy = string(*matchPolicy)
	}
	if sideEffects != nil {
		view.SideEffects = string(*sideEffects)
	}
	if timeout != nil {
		view.TimeoutSeconds = *timeout
	}
	view.Rules = make([]domainresource.AdmissionWebhookRuleView, 0, len(rules))
	for _, rule := range rules {
		mapped := domainresource.AdmissionWebhookRuleView{APIGroups: append([]string(nil), rule.APIGroups...), APIVersions: append([]string(nil), rule.APIVersions...), Resources: append([]string(nil), rule.Resources...)}
		for _, operation := range rule.Operations {
			mapped.Operations = append(mapped.Operations, string(operation))
		}
		if rule.Scope != nil {
			mapped.Scope = string(*rule.Scope)
		}
		view.Rules = append(view.Rules, mapped)
	}
	return view
}

func labelSelectorString(selector *metav1.LabelSelector) string {
	if selector == nil {
		return ""
	}
	parsed, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return ""
	}
	return parsed.String()
}

func mapResourceQuota(item corev1.ResourceQuota) domainresource.ResourceQuotaView {
	scopes := make([]string, 0, len(item.Spec.Scopes))
	for _, scope := range item.Spec.Scopes {
		scopes = append(scopes, string(scope))
	}
	hard := make(map[string]string, len(item.Status.Hard))
	for key, value := range item.Status.Hard {
		hard[string(key)] = value.String()
	}
	used := make(map[string]string, len(item.Status.Used))
	for key, value := range item.Status.Used {
		used[string(key)] = value.String()
	}
	return domainresource.ResourceQuotaView{Name: item.Name, Namespace: item.Namespace, Scopes: scopes, Hard: hard, Used: used, AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
}

func mapResourceQuotaDetail(item corev1.ResourceQuota) domainresource.ResourceQuotaDetailView {
	return domainresource.ResourceQuotaDetailView{ResourceQuotaView: mapResourceQuota(item), Labels: cloneMap(item.Labels), Annotations: cloneMap(item.Annotations), CreatedAt: item.CreationTimestamp.Format(time.RFC3339)}
}

func mapLimitRange(item corev1.LimitRange) domainresource.LimitRangeView {
	return domainresource.LimitRangeView{Name: item.Name, Namespace: item.Namespace, Limits: len(item.Spec.Limits), AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
}

func mapLimitRangeDetail(item corev1.LimitRange) domainresource.LimitRangeDetailView {
	rules := make([]domainresource.LimitRangeRuleView, 0, len(item.Spec.Limits))
	for _, limit := range item.Spec.Limits {
		rules = append(rules, domainresource.LimitRangeRuleView{Type: string(limit.Type), Min: quantityStrings(limit.Min), Max: quantityStrings(limit.Max), Default: quantityStrings(limit.Default), DefaultRequest: quantityStrings(limit.DefaultRequest), MaxLimitRequestRatio: quantityStrings(limit.MaxLimitRequestRatio)})
	}
	return domainresource.LimitRangeDetailView{LimitRangeView: mapLimitRange(item), Labels: cloneMap(item.Labels), Annotations: cloneMap(item.Annotations), CreatedAt: item.CreationTimestamp.Format(time.RFC3339), Rules: rules}
}

func quantityStrings(items corev1.ResourceList) map[string]string {
	if len(items) == 0 {
		return nil
	}
	result := make(map[string]string, len(items))
	for name, quantity := range items {
		result[string(name)] = quantity.String()
	}
	return result
}

func mapLease(item coordinationv1.Lease) domainresource.LeaseView {
	holder := ""
	if item.Spec.HolderIdentity != nil {
		holder = *item.Spec.HolderIdentity
	}
	duration := int32(0)
	if item.Spec.LeaseDurationSeconds != nil {
		duration = *item.Spec.LeaseDurationSeconds
	}
	acquire, renew := "", ""
	if item.Spec.AcquireTime != nil {
		acquire = item.Spec.AcquireTime.UTC().Format(time.RFC3339)
	}
	if item.Spec.RenewTime != nil {
		renew = item.Spec.RenewTime.UTC().Format(time.RFC3339)
	}
	return domainresource.LeaseView{Name: item.Name, Namespace: item.Namespace, HolderIdentity: holder, LeaseDurationSeconds: duration, AcquireTime: acquire, RenewTime: renew, AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
}

func mapReplicationController(item corev1.ReplicationController) domainresource.ReplicationControllerView {
	desired := int32(0)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	return domainresource.ReplicationControllerView{Name: item.Name, Namespace: item.Namespace, DesiredReplicas: desired, CurrentReplicas: item.Status.Replicas, ReadyReplicas: item.Status.ReadyReplicas, AvailableReplicas: item.Status.AvailableReplicas, AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
}

var _ appresource.DirectConfiguration = (*Direct)(nil)
