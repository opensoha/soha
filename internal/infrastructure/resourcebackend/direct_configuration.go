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
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (d *Direct) ListConfigMaps(ctx context.Context, clusterID, namespace string) ([]domainresource.ConfigMapView, error) {
	items, err := listConfigurationNamespace(ctx, d, clusterID, namespace, func(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]corev1.ConfigMap, error) {
		list, err := bundle.Typed.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return list.Items, nil
	})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ConfigMapView, 0, len(items))
	for _, item := range items {
		views = append(views, mapConfigMap(item))
	}
	return views, nil
}

func (d *Direct) ListSecrets(ctx context.Context, clusterID, namespace string) ([]domainresource.SecretView, error) {
	items, err := listConfigurationNamespace(ctx, d, clusterID, namespace, func(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]corev1.Secret, error) {
		list, err := bundle.Typed.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return list.Items, nil
	})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.SecretView, 0, len(items))
	for _, item := range items {
		views = append(views, mapSecret(item))
	}
	return views, nil
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
	var refs []domainresource.ConfigReferenceView
	appendIfUsed := func(kind, itemName string, spec corev1.PodSpec) {
		for _, path := range collectPodSpecConfigReferencePaths(spec, name, configMap) {
			refs = append(refs, domainresource.ConfigReferenceView{Kind: kind, Name: itemName, Namespace: namespace, Path: path})
		}
	}
	pods, err := bundle.Typed.CoreV1().Pods(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, item := range pods.Items {
		appendIfUsed("Pod", item.Name, item.Spec)
	}
	deployments, err := bundle.Typed.AppsV1().Deployments(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, item := range deployments.Items {
		appendIfUsed("Deployment", item.Name, item.Spec.Template.Spec)
	}
	statefulSets, err := bundle.Typed.AppsV1().StatefulSets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, item := range statefulSets.Items {
		appendIfUsed("StatefulSet", item.Name, item.Spec.Template.Spec)
	}
	daemonSets, err := bundle.Typed.AppsV1().DaemonSets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, item := range daemonSets.Items {
		appendIfUsed("DaemonSet", item.Name, item.Spec.Template.Spec)
	}
	replicaSets, err := bundle.Typed.AppsV1().ReplicaSets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, item := range replicaSets.Items {
		appendIfUsed("ReplicaSet", item.Name, item.Spec.Template.Spec)
	}
	replicationControllers, err := bundle.Typed.CoreV1().ReplicationControllers(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, item := range replicationControllers.Items {
		appendIfUsed("ReplicationController", item.Name, item.Spec.Template.Spec)
	}
	jobs, err := bundle.Typed.BatchV1().Jobs(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, item := range jobs.Items {
		appendIfUsed("Job", item.Name, item.Spec.Template.Spec)
	}
	cronJobs, err := bundle.Typed.BatchV1().CronJobs(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, item := range cronJobs.Items {
		appendIfUsed("CronJob", item.Name, item.Spec.JobTemplate.Spec.Template.Spec)
	}
	sortConfigReferences(refs)
	return refs, nil
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
	items, err := bundle.Typed.AdmissionregistrationV1().MutatingWebhookConfigurations().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.MutatingWebhookConfigurationView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapMutatingWebhookConfiguration(item))
	}
	return views, nil
}

func (d *Direct) ListValidatingWebhookConfigurations(ctx context.Context, clusterID string) ([]domainresource.ValidatingWebhookConfigurationView, error) {
	bundle, queryCtx, cancel, err := d.configurationQuery(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ValidatingWebhookConfigurationView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapValidatingWebhookConfiguration(item))
	}
	return views, nil
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
	return domainresource.ConfigMapView{Name: item.Name, Namespace: item.Namespace, DataEntries: len(item.Data), BinaryEntries: len(item.BinaryData), Immutable: item.Immutable != nil && *item.Immutable, AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
}

func mapSecret(item corev1.Secret) domainresource.SecretView {
	return domainresource.SecretView{Name: item.Name, Namespace: item.Namespace, Type: string(item.Type), DataEntries: len(item.Data), Immutable: item.Immutable != nil && *item.Immutable, AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
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

func mapMutatingWebhookConfiguration(item admissionregistrationv1.MutatingWebhookConfiguration) domainresource.MutatingWebhookConfigurationView {
	return domainresource.MutatingWebhookConfigurationView{Name: item.Name, Webhooks: len(item.Webhooks), AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
}

func mapValidatingWebhookConfiguration(item admissionregistrationv1.ValidatingWebhookConfiguration) domainresource.ValidatingWebhookConfigurationView {
	return domainresource.ValidatingWebhookConfigurationView{Name: item.Name, Webhooks: len(item.Webhooks), AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
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

func mapLimitRange(item corev1.LimitRange) domainresource.LimitRangeView {
	return domainresource.LimitRangeView{Name: item.Name, Namespace: item.Namespace, Limits: len(item.Spec.Limits), AgeSeconds: secondsSince(item.CreationTimestamp.Time)}
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
