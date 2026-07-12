package resourcebackend

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/yaml"
)

func (d *Direct) ListDeployments(ctx context.Context, clusterID, namespace string) ([]domainresource.DeploymentView, string, error) {
	items, source, err := listCachedResources(ctx, clusterID, namespace, strings.TrimSpace(namespace) != "" && d.cache != nil, d.listCachedDeployments, d.cacheUnavailable, d.listDeploymentsDirect)
	if err != nil {
		return nil, source, err
	}
	return mapResourceItems(items, mapDeployment), source, nil
}

func (d *Direct) GetDeploymentDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.DeploymentDetailView, error) {
	item, err := d.getDeployment(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.DeploymentDetailView{}, err
	}
	return mapDeploymentDetail(*item), nil
}

func (d *Direct) GetDeploymentYAML(ctx context.Context, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	item, err := d.getDeployment(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	return marshalWorkloadYAML("Deployment", name, namespace, copyItem)
}

func (d *Direct) GetDeploymentRolloutStatus(ctx context.Context, clusterID, namespace, name string) (domainresource.DeploymentRolloutStatusView, error) {
	item, err := d.getDeployment(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.DeploymentRolloutStatusView{}, err
	}
	return mapDeploymentRolloutStatus(*item), nil
}

func (d *Direct) ListDeploymentRolloutHistory(ctx context.Context, clusterID, namespace, name string) ([]domainresource.RolloutHistoryView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	deployment, err := bundle.Typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	replicaSets, err := bundle.Typed.AppsV1().ReplicaSets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	items := make([]domainresource.RolloutHistoryView, 0)
	for _, item := range replicaSets.Items {
		if !ownedByDeployment(item.OwnerReferences, deployment.UID) {
			continue
		}
		images := make([]string, 0, len(item.Spec.Template.Spec.Containers))
		for _, container := range item.Spec.Template.Spec.Containers {
			images = append(images, fmt.Sprintf("%s=%s", container.Name, container.Image))
		}
		replicas := int32(0)
		if item.Spec.Replicas != nil {
			replicas = *item.Spec.Replicas
		}
		items = append(items, domainresource.RolloutHistoryView{
			Name: item.Name, Namespace: item.Namespace,
			Revision: item.Annotations["deployment.kubernetes.io/revision"], Images: images,
			Replicas: replicas, ReadyReplicas: item.Status.ReadyReplicas,
			CreatedAt: item.CreationTimestamp.Format(time.RFC3339),
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].CreatedAt > items[j].CreatedAt })
	return items, nil
}

func (d *Direct) RollbackDeployment(ctx context.Context, clusterID, namespace, name, revision string) error {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	deployment, err := bundle.Typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	replicaSets, err := bundle.Typed.AppsV1().ReplicaSets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	var target *appsv1.ReplicaSet
	for index := range replicaSets.Items {
		item := &replicaSets.Items[index]
		if ownedByDeployment(item.OwnerReferences, deployment.UID) && item.Annotations["deployment.kubernetes.io/revision"] == revision {
			target = item
			break
		}
	}
	if target == nil {
		return fmt.Errorf("%w: target revision %s not found", apperrors.ErrNotFound, revision)
	}
	deployment.Spec.Template = *target.Spec.Template.DeepCopy()
	delete(deployment.Spec.Template.Labels, "pod-template-hash")
	updateCtx, updateCancel := context.WithTimeout(ctx, 5*time.Second)
	defer updateCancel()
	_, err = bundle.Typed.AppsV1().Deployments(namespace).Update(updateCtx, deployment, metav1.UpdateOptions{})
	return err
}

func (d *Direct) ListStatefulSets(ctx context.Context, clusterID, namespace string) ([]domainresource.StatefulSetView, string, error) {
	items, source, err := listCachedResources(ctx, clusterID, namespace, strings.TrimSpace(namespace) != "" && d.cache != nil, d.listCachedStatefulSets, d.cacheUnavailable, d.listStatefulSetsDirect)
	if err != nil {
		return nil, source, err
	}
	return mapResourceItems(items, mapStatefulSet), source, nil
}

func (d *Direct) listDeploymentsDirect(ctx context.Context, clusterID, namespace string) ([]appsv1.Deployment, error) {
	return directNamespacedList(ctx, d, clusterID, namespace, func(queryCtx context.Context, namespace string) ([]appsv1.Deployment, error) {
		bundle, err := d.directClients(queryCtx, clusterID)
		if err != nil {
			return nil, err
		}
		return listDeploymentsLive(queryCtx, bundle, namespace)
	})
}

func (d *Direct) listCachedDeployments(clusterID, namespace string) ([]appsv1.Deployment, error) {
	return d.cache.ListDeployments(clusterID, namespace)
}

func (d *Direct) listStatefulSetsDirect(ctx context.Context, clusterID, namespace string) ([]appsv1.StatefulSet, error) {
	return directNamespacedList(ctx, d, clusterID, namespace, func(queryCtx context.Context, namespace string) ([]appsv1.StatefulSet, error) {
		bundle, err := d.directClients(queryCtx, clusterID)
		if err != nil {
			return nil, err
		}
		return listStatefulSetsLive(queryCtx, bundle, namespace)
	})
}

func (d *Direct) listCachedStatefulSets(clusterID, namespace string) ([]appsv1.StatefulSet, error) {
	return d.cache.ListStatefulSets(clusterID, namespace)
}

func (d *Direct) GetStatefulSetDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.StatefulSetDetailView, error) {
	item, err := d.getStatefulSet(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.StatefulSetDetailView{}, err
	}
	return mapStatefulSetDetail(*item), nil
}

func (d *Direct) GetStatefulSetYAML(ctx context.Context, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	item, err := d.getStatefulSet(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	return marshalWorkloadYAML("StatefulSet", name, namespace, copyItem)
}

func (d *Direct) ListDaemonSets(ctx context.Context, clusterID, namespace string) ([]domainresource.DaemonSetView, error) {
	items, err := directNamespacedList(ctx, d, clusterID, namespace, func(queryCtx context.Context, namespace string) ([]appsv1.DaemonSet, error) {
		bundle, err := d.directClients(queryCtx, clusterID)
		if err != nil {
			return nil, err
		}
		return listDaemonSetsLive(queryCtx, bundle, namespace)
	})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.DaemonSetView, 0, len(items))
	for _, item := range items {
		views = append(views, mapDaemonSet(item))
	}
	return views, nil
}

func (d *Direct) GetDaemonSetDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.DaemonSetDetailView, error) {
	item, err := d.getDaemonSet(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.DaemonSetDetailView{}, err
	}
	return mapDaemonSetDetail(*item), nil
}

func (d *Direct) GetDaemonSetYAML(ctx context.Context, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	item, err := d.getDaemonSet(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	return marshalWorkloadYAML("DaemonSet", name, namespace, copyItem)
}

func (d *Direct) ListJobs(ctx context.Context, clusterID, namespace string) ([]domainresource.JobView, error) {
	items, err := directNamespacedList(ctx, d, clusterID, namespace, func(queryCtx context.Context, namespace string) ([]batchv1.Job, error) {
		bundle, err := d.directClients(queryCtx, clusterID)
		if err != nil {
			return nil, err
		}
		return listJobsLive(queryCtx, bundle, namespace)
	})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.JobView, 0, len(items))
	for _, item := range items {
		views = append(views, mapJob(item))
	}
	return views, nil
}

func (d *Direct) GetJobDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.JobDetailView, error) {
	item, err := d.getJob(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.JobDetailView{}, err
	}
	return mapJobDetail(*item), nil
}

func (d *Direct) GetJobYAML(ctx context.Context, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	item, err := d.getJob(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	return marshalWorkloadYAML("Job", name, namespace, copyItem)
}

func (d *Direct) ListCronJobs(ctx context.Context, clusterID, namespace string) ([]domainresource.CronJobView, error) {
	items, err := directNamespacedList(ctx, d, clusterID, namespace, func(queryCtx context.Context, namespace string) ([]batchv1.CronJob, error) {
		bundle, err := d.directClients(queryCtx, clusterID)
		if err != nil {
			return nil, err
		}
		return listCronJobsLive(queryCtx, bundle, namespace)
	})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.CronJobView, 0, len(items))
	for _, item := range items {
		views = append(views, mapCronJob(item))
	}
	return views, nil
}

func (d *Direct) GetCronJobDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.CronJobDetailView, error) {
	item, err := d.getCronJob(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.CronJobDetailView{}, err
	}
	return mapCronJobDetail(*item), nil
}

func (d *Direct) GetCronJobYAML(ctx context.Context, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	item, err := d.getCronJob(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	return marshalWorkloadYAML("CronJob", name, namespace, copyItem)
}

func (d *Direct) SetCronJobSuspend(ctx context.Context, clusterID, namespace, name string, suspend bool) (domainresource.CronJobDetailView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.CronJobDetailView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.BatchV1().CronJobs(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.CronJobDetailView{}, err
	}
	item.Spec.Suspend = &suspend
	updated, err := bundle.Typed.BatchV1().CronJobs(namespace).Update(queryCtx, item, metav1.UpdateOptions{})
	if err != nil {
		return domainresource.CronJobDetailView{}, err
	}
	return mapCronJobDetail(*updated), nil
}

func (d *Direct) ListReplicaSets(ctx context.Context, clusterID, namespace string) ([]domainresource.ReplicaSetView, error) {
	items, err := directNamespacedList(ctx, d, clusterID, namespace, func(queryCtx context.Context, namespace string) ([]appsv1.ReplicaSet, error) {
		bundle, err := d.directClients(queryCtx, clusterID)
		if err != nil {
			return nil, err
		}
		result, err := bundle.Typed.AppsV1().ReplicaSets(namespace).List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return result.Items, nil
	})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ReplicaSetView, 0, len(items))
	for _, item := range items {
		views = append(views, mapReplicaSet(item))
	}
	return views, nil
}

func (d *Direct) ListHorizontalPodAutoscalers(ctx context.Context, clusterID, namespace string) ([]domainresource.HorizontalPodAutoscalerView, error) {
	items, err := directNamespacedList(ctx, d, clusterID, namespace, func(queryCtx context.Context, namespace string) ([]autoscalingv2.HorizontalPodAutoscaler, error) {
		bundle, err := d.directClients(queryCtx, clusterID)
		if err != nil {
			return nil, err
		}
		result, err := bundle.Typed.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return result.Items, nil
	})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.HorizontalPodAutoscalerView, 0, len(items))
	for _, item := range items {
		views = append(views, mapHorizontalPodAutoscaler(item))
	}
	return views, nil
}

func (d *Direct) ListPodDisruptionBudgets(ctx context.Context, clusterID, namespace string) ([]domainresource.PodDisruptionBudgetView, error) {
	items, err := directNamespacedList(ctx, d, clusterID, namespace, func(queryCtx context.Context, namespace string) ([]policyv1.PodDisruptionBudget, error) {
		bundle, err := d.directClients(queryCtx, clusterID)
		if err != nil {
			return nil, err
		}
		result, err := bundle.Typed.PolicyV1().PodDisruptionBudgets(namespace).List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return result.Items, nil
	})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.PodDisruptionBudgetView, 0, len(items))
	for _, item := range items {
		views = append(views, mapPodDisruptionBudget(item))
	}
	return views, nil
}

func (d *Direct) getDeployment(ctx context.Context, clusterID, namespace, name string) (*appsv1.Deployment, error) {
	bundle, queryCtx, cancel, err := d.workloadQueryContext(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return bundle.Typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
}

func (d *Direct) getStatefulSet(ctx context.Context, clusterID, namespace, name string) (*appsv1.StatefulSet, error) {
	bundle, queryCtx, cancel, err := d.workloadQueryContext(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return bundle.Typed.AppsV1().StatefulSets(namespace).Get(queryCtx, name, metav1.GetOptions{})
}

func (d *Direct) getDaemonSet(ctx context.Context, clusterID, namespace, name string) (*appsv1.DaemonSet, error) {
	bundle, queryCtx, cancel, err := d.workloadQueryContext(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return bundle.Typed.AppsV1().DaemonSets(namespace).Get(queryCtx, name, metav1.GetOptions{})
}

func (d *Direct) getJob(ctx context.Context, clusterID, namespace, name string) (*batchv1.Job, error) {
	bundle, queryCtx, cancel, err := d.workloadQueryContext(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return bundle.Typed.BatchV1().Jobs(namespace).Get(queryCtx, name, metav1.GetOptions{})
}

func (d *Direct) getCronJob(ctx context.Context, clusterID, namespace, name string) (*batchv1.CronJob, error) {
	bundle, queryCtx, cancel, err := d.workloadQueryContext(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return bundle.Typed.BatchV1().CronJobs(namespace).Get(queryCtx, name, metav1.GetOptions{})
}

func (d *Direct) workloadQueryContext(ctx context.Context, clusterID string) (*k8sinfra.Bundle, context.Context, context.CancelFunc, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, nil, nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	return bundle, queryCtx, cancel, nil
}

func listDeploymentsLive(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]appsv1.Deployment, error) {
	var list appsv1.DeploymentList
	if err := bundle.Typed.AppsV1().RESTClient().Get().Namespace(namespace).Resource("deployments").
		VersionedParams(&metav1.ListOptions{}, scheme.ParameterCodec).Do(ctx).Into(&list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func listStatefulSetsLive(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]appsv1.StatefulSet, error) {
	var list appsv1.StatefulSetList
	if err := bundle.Typed.AppsV1().RESTClient().Get().Namespace(namespace).Resource("statefulsets").
		VersionedParams(&metav1.ListOptions{}, scheme.ParameterCodec).Do(ctx).Into(&list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func listDaemonSetsLive(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]appsv1.DaemonSet, error) {
	var list appsv1.DaemonSetList
	if err := bundle.Typed.AppsV1().RESTClient().Get().Namespace(namespace).Resource("daemonsets").
		VersionedParams(&metav1.ListOptions{}, scheme.ParameterCodec).Do(ctx).Into(&list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func listJobsLive(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]batchv1.Job, error) {
	var list batchv1.JobList
	if err := bundle.Typed.BatchV1().RESTClient().Get().Namespace(namespace).Resource("jobs").
		VersionedParams(&metav1.ListOptions{}, scheme.ParameterCodec).Do(ctx).Into(&list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func listCronJobsLive(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]batchv1.CronJob, error) {
	var list batchv1.CronJobList
	if err := bundle.Typed.BatchV1().RESTClient().Get().Namespace(namespace).Resource("cronjobs").
		VersionedParams(&metav1.ListOptions{}, scheme.ParameterCodec).Do(ctx).Into(&list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func marshalWorkloadYAML(kind, name, namespace string, item any) (domainresource.ResourceYAMLView, error) {
	content, err := yaml.Marshal(item)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{Kind: kind, Name: name, Namespace: namespace, Content: string(content)}, nil
}

func ownedByDeployment(owners []metav1.OwnerReference, uid types.UID) bool {
	for _, owner := range owners {
		if owner.UID == uid && owner.Kind == "Deployment" {
			return true
		}
	}
	return false
}

var _ appresource.DirectWorkloads = (*Direct)(nil)
