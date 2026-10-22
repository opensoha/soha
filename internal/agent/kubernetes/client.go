package kubernetes

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/yaml"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	domaincluster "github.com/opensoha/soha-agent/internal/domain/cluster"
	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
	helmrelease "github.com/opensoha/soha-contracts/helmrelease"
	contractresource "github.com/opensoha/soha-contracts/resource"
	contractruntime "github.com/opensoha/soha-contracts/resource/runtime"
	"github.com/opensoha/soha-contracts/streamlimit"
)

type Client struct {
	cfg            cfgpkg.KubernetesConfig
	typed          kubernetes.Interface
	dynamic        dynamic.Interface
	discovery      discovery.DiscoveryInterface
	metadata       metadata.Interface
	restConfig     *rest.Config
	resourceEvents *resourceEventStream
}

func New(cfg cfgpkg.KubernetesConfig) (*Client, error) {
	restConfig, err := buildRESTConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build kubeconfig: %w", err)
	}
	typedClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("build typed client: %w", err)
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("build dynamic client: %w", err)
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("build discovery client: %w", err)
	}
	metadataClient, err := metadata.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("build metadata client: %w", err)
	}
	client := &Client{cfg: cfg, typed: typedClient, dynamic: dynamicClient, discovery: discoveryClient, metadata: metadataClient, restConfig: restConfig}
	client.resourceEvents = newResourceEventStream(cfg.ID, typedClient)
	return client, nil
}

func (c *Client) Summary(_ context.Context) domaincluster.Summary {
	summary := domaincluster.Summary{
		Capabilities:   []string{contractresource.RelationSnapshotFeature, contractresource.NetworkTopologySnapshotFeature, contractresource.NetworkTopologyGatewayL4Feature, contractresource.ResourceMonitoringSnapshotFeature},
		ID:             c.cfg.ID,
		Name:           c.cfg.Name,
		Region:         c.cfg.Region,
		Environment:    c.cfg.Environment,
		Labels:         c.cfg.Labels,
		ConnectionMode: domaincluster.ConnectionModeAgent,
		Health:         domaincluster.Health{Status: "unknown", LastChecked: time.Now().UTC()},
	}

	serverVersion, err := c.discovery.ServerVersion()
	if err != nil {
		summary.Health = domaincluster.Health{Status: "degraded", Message: err.Error(), LastChecked: time.Now().UTC()}
		return summary
	}
	groups, err := c.discovery.ServerGroups()
	if err != nil {
		summary.Version = serverVersion.GitVersion
		summary.Health = domaincluster.Health{Status: "degraded", Message: err.Error(), LastChecked: time.Now().UTC()}
		return summary
	}

	capabilities := []string{
		contractresource.RelationSnapshotFeature, contractresource.NetworkTopologySnapshotFeature, contractresource.NetworkTopologyGatewayL4Feature, contractresource.ResourceMonitoringSnapshotFeature, "manifest.preflight", "manifest.ssa", "manifest.observe", contractruntime.ManifestAgentCapability, contractruntime.RootCRAgentCapability,
		"logs.runtime.snapshot", "logs.runtime.stream", "logs.runtime.aggregate",
	}
	for _, group := range groups.Groups {
		if strings.TrimSpace(group.Name) == "" {
			continue
		}
		capabilities = append(capabilities, group.Name)
		if len(capabilities) >= 9 {
			break
		}
	}

	summary.Version = serverVersion.GitVersion
	summary.Capabilities = capabilities
	summary.Health = domaincluster.Health{Status: "healthy", Message: "ok", LastChecked: time.Now().UTC()}
	return summary
}

func (c *Client) ListNamespaces(ctx context.Context) ([]domainresource.NamespaceView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, cached := cachedResourceItems[corev1.Namespace](queryCtx, c, "Namespace", "")
	if !cached {
		live, err := c.typed.CoreV1().Namespaces().List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		items = live.Items
	}
	views := make([]domainresource.NamespaceView, 0, len(items))
	for _, item := range items {
		views = append(views, domainresource.NamespaceView{
			Name:       item.Name,
			Status:     string(item.Status.Phase),
			Labels:     item.Labels,
			AgeSeconds: secondsSince(item.CreationTimestamp.Time),
		})
	}
	return views, nil
}

func (c *Client) ListNodes(ctx context.Context) ([]domainresource.NodeView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, cached := cachedResourceItems[corev1.Node](queryCtx, c, "Node", "")
	if !cached {
		live, err := c.typed.CoreV1().Nodes().List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		items = live.Items
	}
	pods, cached := cachedResourceItems[corev1.Pod](queryCtx, c, "Pod", metav1.NamespaceAll)
	if !cached {
		live, err := c.typed.CoreV1().Pods(metav1.NamespaceAll).List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		pods = live.Items
	}
	return buildNodeViews(items, pods), nil
}

func (c *Client) GetNodeDetail(ctx context.Context, name string) (domainresource.NodeDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.CoreV1().Nodes().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.NodeDetailView{}, err
	}
	pods, err := c.typed.CoreV1().Pods(metav1.NamespaceAll).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return domainresource.NodeDetailView{}, err
	}
	return buildNodeDetail(*item, pods.Items), nil
}

func (c *Client) ListPods(ctx context.Context, namespace string) ([]domainresource.PodView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, cached := cachedResourceItems[corev1.Pod](queryCtx, c, "Pod", namespace)
	if !cached {
		live, err := c.typed.CoreV1().Pods(namespace).List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		items = live.Items
	}
	views := make([]domainresource.PodView, 0, len(items))
	for _, item := range items {
		views = append(views, mapPod(item))
	}
	return views, nil
}

func (c *Client) GetPodDetail(ctx context.Context, namespace, name string) (domainresource.PodDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.CoreV1().Pods(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.PodDetailView{}, err
	}
	return c.buildPodDetail(queryCtx, *item), nil
}

func (c *Client) GetPodLogs(ctx context.Context, namespace, name, container string, tailLines, sinceSeconds int64, previous bool, expectedUID string) (domainresource.PodLogsView, error) {
	if err := c.CheckPodUID(ctx, namespace, name, expectedUID); err != nil {
		return domainresource.PodLogsView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	options := &corev1.PodLogOptions{Container: container, Previous: previous}
	if tailLines > 0 {
		options.TailLines = &tailLines
	}
	if sinceSeconds > 0 {
		options.SinceSeconds = &sinceSeconds
	}
	stream, err := c.typed.CoreV1().Pods(namespace).GetLogs(name, options).Stream(queryCtx)
	if err != nil {
		return domainresource.PodLogsView{}, err
	}
	defer stream.Close()
	content, totalBytes, contentTruncated, err := streamlimit.ReadString(stream, domainresource.PodLogsMaxContentBytes)
	if err != nil {
		return domainresource.PodLogsView{}, err
	}
	return domainresource.PodLogsView{
		PodName:      name,
		Namespace:    namespace,
		Container:    container,
		Content:      content,
		ContentBytes: totalBytes,
		MaxBytes:     domainresource.PodLogsMaxContentBytes,
		TailLines:    tailLines,
		Previous:     previous,
		Truncated:    tailLines > 0 || contentTruncated,
	}, nil
}

func (c *Client) ExecPod(ctx context.Context, namespace, name, container, command string, timeoutSeconds int64) (domainresource.PodExecView, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 10
	}
	queryCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	request := c.typed.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(name).
		Namespace(namespace).
		SubResource("exec")
	request.VersionedParams(&corev1.PodExecOptions{
		Container: container,
		Command:   []string{"/bin/sh", "-lc", command},
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(c.restConfig, http.MethodPost, request.URL())
	if err != nil {
		return domainresource.PodExecView{}, err
	}
	stdout := streamlimit.NewLimitedBuffer(domainresource.PodExecMaxOutputBytes)
	stderr := streamlimit.NewLimitedBuffer(domainresource.PodExecMaxOutputBytes)
	execErr := executor.StreamWithContext(queryCtx, remotecommand.StreamOptions{
		Stdout: stdout,
		Stderr: stderr,
		Tty:    false,
	})
	exitMessage := ""
	if execErr != nil {
		exitMessage = execErr.Error()
	}
	return domainresource.PodExecView{
		PodName:         name,
		Namespace:       namespace,
		Container:       container,
		Command:         command,
		Stdout:          stdout.String(),
		Stderr:          stderr.String(),
		StdoutBytes:     stdout.TotalBytes(),
		StderrBytes:     stderr.TotalBytes(),
		MaxBytes:        domainresource.PodExecMaxOutputBytes,
		StdoutTruncated: stdout.Truncated(),
		StderrTruncated: stderr.Truncated(),
		Success:         execErr == nil,
		ExitMessage:     exitMessage,
		ExecutedAt:      time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (c *Client) GetPodYAML(ctx context.Context, namespace, name string) (domainresource.ResourceYAMLView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.CoreV1().Pods(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.TypeMeta = metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"}
	copyItem.ManagedFields = nil
	content, err := yaml.Marshal(copyItem)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{
		Kind:      "Pod",
		Name:      name,
		Namespace: namespace,
		Content:   string(content),
	}, nil
}

func (c *Client) ListDeployments(ctx context.Context, namespace string) ([]domainresource.DeploymentView, error) {
	return c.listDeploymentSummaries(ctx, namespace)
}

func (c *Client) GetDeploymentDetail(ctx context.Context, namespace, name string) (domainresource.DeploymentDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.DeploymentDetailView{}, err
	}
	pods, relatedResources, err := c.loadWorkloadDetailRelations(queryCtx, namespace, item.Spec.Selector, item.OwnerReferences, item.Spec.Template, "")
	if err != nil {
		return domainresource.DeploymentDetailView{}, err
	}
	detail := mapDeploymentDetail(*item)
	detail.Pods = pods
	detail.RelatedResources = relatedResources
	return detail, nil
}

func (c *Client) GetDeploymentYAML(ctx context.Context, namespace, name string) (domainresource.ResourceYAMLView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	content, err := yaml.Marshal(copyItem)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{
		Kind:      "Deployment",
		Name:      name,
		Namespace: namespace,
		Content:   string(content),
	}, nil
}

func (c *Client) GetDeploymentRolloutStatus(ctx context.Context, namespace, name string) (domainresource.DeploymentRolloutStatusView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.DeploymentRolloutStatusView{}, err
	}
	return mapDeploymentRolloutStatus(*item), nil
}

func (c *Client) ListDeploymentRolloutHistory(ctx context.Context, namespace, name string) ([]domainresource.RolloutHistoryView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	deployment, err := c.typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	replicaSets, err := c.typed.AppsV1().ReplicaSets(namespace).List(queryCtx, metav1.ListOptions{})
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
			Name:          item.Name,
			Namespace:     item.Namespace,
			Revision:      item.Annotations["deployment.kubernetes.io/revision"],
			Images:        images,
			Replicas:      replicas,
			ReadyReplicas: item.Status.ReadyReplicas,
			CreatedAt:     item.CreationTimestamp.Time.Format(time.RFC3339),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt > items[j].CreatedAt
	})
	return items, nil
}

func (c *Client) RollbackDeployment(ctx context.Context, namespace, name, revision string) error {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	deployment, err := c.typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err := contractruntime.ValidateDirectManifestOwner(deployment); err != nil {
		return err
	}
	replicaSets, err := c.typed.AppsV1().ReplicaSets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	var target *appsv1.ReplicaSet
	for index := range replicaSets.Items {
		item := &replicaSets.Items[index]
		if !ownedByDeployment(item.OwnerReferences, deployment.UID) {
			continue
		}
		if item.Annotations["deployment.kubernetes.io/revision"] == revision {
			target = item
			break
		}
	}
	if target == nil {
		return fmt.Errorf("target revision %s not found", revision)
	}
	deployment.Spec.Template = *target.Spec.Template.DeepCopy()
	if deployment.Spec.Template.Labels != nil {
		delete(deployment.Spec.Template.Labels, "pod-template-hash")
	}
	_, err = c.typed.AppsV1().Deployments(namespace).Update(queryCtx, deployment, metav1.UpdateOptions{})
	return err
}

func (c *Client) ListStatefulSets(ctx context.Context, namespace string) ([]domainresource.StatefulSetView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, cached := cachedResourceItems[appsv1.StatefulSet](queryCtx, c, "StatefulSet", namespace)
	if !cached {
		live, err := c.typed.AppsV1().StatefulSets(namespace).List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		items = live.Items
	}
	views := make([]domainresource.StatefulSetView, 0, len(items))
	for _, item := range items {
		views = append(views, mapStatefulSet(item))
	}
	return views, nil
}

func (c *Client) GetStatefulSetDetail(ctx context.Context, namespace, name string) (domainresource.StatefulSetDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.AppsV1().StatefulSets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.StatefulSetDetailView{}, err
	}
	pods, relatedResources, err := c.loadWorkloadDetailRelations(queryCtx, namespace, item.Spec.Selector, item.OwnerReferences, item.Spec.Template, "")
	if err != nil {
		return domainresource.StatefulSetDetailView{}, err
	}
	detail := mapStatefulSetDetail(*item)
	detail.Pods = pods
	detail.RelatedResources = relatedResources
	return detail, nil
}

func (c *Client) GetStatefulSetYAML(ctx context.Context, namespace, name string) (domainresource.ResourceYAMLView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.AppsV1().StatefulSets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	content, err := yaml.Marshal(copyItem)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{Kind: "StatefulSet", Name: name, Namespace: namespace, Content: string(content)}, nil
}

func (c *Client) ListDaemonSets(ctx context.Context, namespace string) ([]domainresource.DaemonSetView, error) {
	return c.listDaemonSetSummaries(ctx, namespace)
}

func (c *Client) GetDaemonSetDetail(ctx context.Context, namespace, name string) (domainresource.DaemonSetDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.AppsV1().DaemonSets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.DaemonSetDetailView{}, err
	}
	pods, relatedResources, err := c.loadWorkloadDetailRelations(queryCtx, namespace, item.Spec.Selector, item.OwnerReferences, item.Spec.Template, "")
	if err != nil {
		return domainresource.DaemonSetDetailView{}, err
	}
	detail := mapDaemonSetDetail(*item)
	detail.Pods = pods
	detail.RelatedResources = relatedResources
	return detail, nil
}

func (c *Client) GetDaemonSetYAML(ctx context.Context, namespace, name string) (domainresource.ResourceYAMLView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.AppsV1().DaemonSets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	content, err := yaml.Marshal(copyItem)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{Kind: "DaemonSet", Name: name, Namespace: namespace, Content: string(content)}, nil
}

func (c *Client) ListJobs(ctx context.Context, namespace string) ([]domainresource.JobView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, cached := cachedResourceItems[batchv1.Job](queryCtx, c, "Job", namespace)
	if !cached {
		live, err := c.typed.BatchV1().Jobs(namespace).List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		items = live.Items
	}
	views := make([]domainresource.JobView, 0, len(items))
	for _, item := range items {
		views = append(views, mapJob(item))
	}
	return views, nil
}

func (c *Client) GetJobDetail(ctx context.Context, namespace, name string) (domainresource.JobDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.BatchV1().Jobs(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.JobDetailView{}, err
	}
	pods, relatedResources, err := c.loadWorkloadDetailRelations(queryCtx, namespace, item.Spec.Selector, item.OwnerReferences, item.Spec.Template, item.UID)
	if err != nil {
		return domainresource.JobDetailView{}, err
	}
	detail := mapJobDetail(*item)
	detail.Pods = pods
	detail.RelatedResources = relatedResources
	return detail, nil
}

func (c *Client) GetJobYAML(ctx context.Context, namespace, name string) (domainresource.ResourceYAMLView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.BatchV1().Jobs(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	content, err := yaml.Marshal(copyItem)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{Kind: "Job", Name: name, Namespace: namespace, Content: string(content)}, nil
}

func (c *Client) ListCronJobs(ctx context.Context, namespace string) ([]domainresource.CronJobView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, cached := cachedResourceItems[batchv1.CronJob](queryCtx, c, "CronJob", namespace)
	if !cached {
		live, err := c.typed.BatchV1().CronJobs(namespace).List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		items = live.Items
	}
	views := make([]domainresource.CronJobView, 0, len(items))
	for _, item := range items {
		views = append(views, mapCronJob(item))
	}
	return views, nil
}

func (c *Client) ListReplicaSets(ctx context.Context, namespace string) ([]domainresource.ReplicaSetView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, cached := cachedResourceItems[appsv1.ReplicaSet](queryCtx, c, "ReplicaSet", namespace)
	if !cached {
		live, err := c.typed.AppsV1().ReplicaSets(namespace).List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		items = live.Items
	}
	views := make([]domainresource.ReplicaSetView, 0, len(items))
	for _, item := range items {
		views = append(views, mapReplicaSet(item))
	}
	return views, nil
}

func (c *Client) ListConfigMaps(ctx context.Context, namespace string) ([]domainresource.ConfigMapView, error) {
	return c.listConfigMapSummaries(ctx, namespace)
}

func (c *Client) ListSecrets(ctx context.Context, namespace string) ([]domainresource.SecretView, error) {
	return c.listSecretSummaries(ctx, namespace)
}

func (c *Client) ListServiceAccounts(ctx context.Context, namespace string) ([]domainresource.ServiceAccountView, error) {
	return c.listServiceAccountSummaries(ctx, namespace)
}

func (c *Client) GetServiceAccountDetail(ctx context.Context, namespace, name string) (domainresource.ServiceAccountDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.CoreV1().ServiceAccounts(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ServiceAccountDetailView{}, err
	}
	return mapServiceAccountDetail(*item), nil
}

func (c *Client) ListRoles(ctx context.Context, namespace string) ([]domainresource.RoleView, error) {
	return c.listRoleSummaries(ctx, namespace)
}

func (c *Client) GetRoleDetail(ctx context.Context, namespace, name string) (domainresource.RoleDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.RbacV1().Roles(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.RoleDetailView{}, err
	}
	return mapRoleDetail(*item), nil
}

func (c *Client) ListRoleBindings(ctx context.Context, namespace string) ([]domainresource.RoleBindingView, error) {
	return c.listRoleBindingSummaries(ctx, namespace)
}

func (c *Client) ListRoleBindingsForSubject(ctx context.Context, namespace, kind, name, subjectNamespace string) ([]domainresource.RoleBindingView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	views := []domainresource.RoleBindingView{}
	continueToken := ""
	for {
		items, err := c.typed.RbacV1().RoleBindings(namespace).List(queryCtx, metav1.ListOptions{
			Limit: int64(agentTablePageSize), Continue: continueToken,
		})
		if err != nil {
			return nil, err
		}
		for _, item := range items.Items {
			if bindingHasSubject(item.Subjects, kind, name, subjectNamespace, item.Namespace) {
				views = append(views, mapRoleBinding(item))
			}
		}
		if items.Continue == "" {
			return views, nil
		}
		if items.Continue == continueToken {
			return nil, fmt.Errorf("rolebinding listing returned a repeated continue token")
		}
		continueToken = items.Continue
	}
}

func (c *Client) GetRoleBindingDetail(ctx context.Context, namespace, name string) (domainresource.RoleBindingDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.RbacV1().RoleBindings(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.RoleBindingDetailView{}, err
	}
	return mapRoleBindingDetail(*item), nil
}

func (c *Client) ListHorizontalPodAutoscalers(ctx context.Context, namespace string) ([]domainresource.HorizontalPodAutoscalerView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.HorizontalPodAutoscalerView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapHorizontalPodAutoscaler(item))
	}
	return views, nil
}

func (c *Client) GetHorizontalPodAutoscalerDetail(ctx context.Context, namespace, name string) (domainresource.HorizontalPodAutoscalerDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.AutoscalingV2().HorizontalPodAutoscalers(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.HorizontalPodAutoscalerDetailView{}, err
	}
	return mapHorizontalPodAutoscalerDetail(*item), nil
}

func (c *Client) ListPodDisruptionBudgets(ctx context.Context, namespace string) ([]domainresource.PodDisruptionBudgetView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.PolicyV1().PodDisruptionBudgets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.PodDisruptionBudgetView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapPodDisruptionBudget(item))
	}
	return views, nil
}

func (c *Client) GetPodDisruptionBudgetDetail(ctx context.Context, namespace, name string) (domainresource.PodDisruptionBudgetDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.PolicyV1().PodDisruptionBudgets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.PodDisruptionBudgetDetailView{}, err
	}
	selector, err := metav1.LabelSelectorAsSelector(item.Spec.Selector)
	if err != nil {
		return domainresource.PodDisruptionBudgetDetailView{}, err
	}
	podList, err := c.typed.CoreV1().Pods(namespace).List(queryCtx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return domainresource.PodDisruptionBudgetDetailView{}, err
	}
	workload, err := c.commonPodWorkload(queryCtx, namespace, podList.Items)
	if err != nil {
		return domainresource.PodDisruptionBudgetDetailView{}, err
	}
	return mapPodDisruptionBudgetDetail(*item, selector.String(), podList.Items, workload), nil
}

func (c *Client) GetCronJobDetail(ctx context.Context, namespace, name string) (domainresource.CronJobDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.BatchV1().CronJobs(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.CronJobDetailView{}, err
	}
	jobs, err := c.listOwnedJobs(queryCtx, namespace, item.UID)
	if err != nil {
		return domainresource.CronJobDetailView{}, err
	}
	relatedResources, err := c.listWorkloadRelations(queryCtx, namespace, item.OwnerReferences, item.Spec.JobTemplate.Spec.Template)
	if err != nil {
		return domainresource.CronJobDetailView{}, err
	}
	detail := mapCronJobDetail(*item)
	detail.Jobs = jobs
	detail.RelatedResources = relatedResources
	return detail, nil
}

func (c *Client) GetCronJobYAML(ctx context.Context, namespace, name string) (domainresource.ResourceYAMLView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.BatchV1().CronJobs(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyItem := item.DeepCopy()
	copyItem.ManagedFields = nil
	content, err := yaml.Marshal(copyItem)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{Kind: "CronJob", Name: name, Namespace: namespace, Content: string(content)}, nil
}

func (c *Client) GetResourceYAML(ctx context.Context, namespace, kind, name string) (domainresource.ResourceYAMLView, error) {
	gvr, namespaceScoped, canonicalKind, err := resourceGVRForKind(kind)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resource, effectiveNamespace, err := c.dynamicResource(gvr, namespaceScoped, namespace, nil)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
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
		Kind:      canonicalKind,
		Name:      item.GetName(),
		Namespace: effectiveNamespace,
		Content:   string(content),
	}, nil
}

func (c *Client) ApplyResourceYAML(ctx context.Context, namespace, kind, name, content string) (domainresource.ResourceYAMLView, error) {
	view, _, err := c.applyResourceYAML(ctx, namespace, kind, name, content, false)
	return view, err
}

func (c *Client) DryRunResourceYAML(ctx context.Context, namespace, kind, name, content string) (domainresource.ResourceUpdateAnalysis, error) {
	_, analysis, err := c.applyResourceYAML(ctx, namespace, kind, name, content, true)
	return analysis, err
}

func (c *Client) applyResourceYAML(ctx context.Context, namespace, kind, name, content string, dryRun bool) (domainresource.ResourceYAMLView, domainresource.ResourceUpdateAnalysis, error) {
	if strings.TrimSpace(content) == "" {
		return domainresource.ResourceYAMLView{}, domainresource.ResourceUpdateAnalysis{}, fmt.Errorf("yaml content is required")
	}
	gvr, namespaceScoped, canonicalKind, err := resourceGVRForKind(kind)
	if err != nil {
		return domainresource.ResourceYAMLView{}, domainresource.ResourceUpdateAnalysis{}, err
	}
	var object map[string]any
	if err := yaml.Unmarshal([]byte(content), &object); err != nil {
		return domainresource.ResourceYAMLView{}, domainresource.ResourceUpdateAnalysis{}, fmt.Errorf("invalid yaml: %w", err)
	}
	item := &unstructured.Unstructured{Object: object}
	item.SetKind(canonicalKind)
	if item.GetName() == "" {
		item.SetName(name)
	}
	if item.GetName() != name {
		return domainresource.ResourceYAMLView{}, domainresource.ResourceUpdateAnalysis{}, fmt.Errorf("yaml metadata.name does not match target resource")
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resource, effectiveNamespace, err := c.dynamicResource(gvr, namespaceScoped, namespace, item)
	if err != nil {
		return domainresource.ResourceYAMLView{}, domainresource.ResourceUpdateAnalysis{}, err
	}
	current, err := resource.Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, domainresource.ResourceUpdateAnalysis{}, err
	}
	if err := contractruntime.ValidateDirectManifestOwner(current); err != nil {
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
		return domainresource.ResourceYAMLView{}, analysis, err
	}
	unstructured.RemoveNestedField(updated.Object, "metadata", "managedFields")
	rendered, err := yaml.Marshal(updated.Object)
	if err != nil {
		return domainresource.ResourceYAMLView{}, analysis, err
	}
	return domainresource.ResourceYAMLView{
		Kind:      canonicalKind,
		Name:      updated.GetName(),
		Namespace: effectiveNamespace,
		Content:   string(rendered),
	}, analysis, nil
}

func (c *Client) DeleteResource(ctx context.Context, namespace, kind, name string) error {
	gvr, namespaceScoped, _, err := resourceGVRForKind(kind)
	if err != nil {
		return err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resource, _, err := c.dynamicResource(gvr, namespaceScoped, namespace, nil)
	if err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(kind), "Pod") {
		item, err := resource.Get(queryCtx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		uid, version := item.GetUID(), item.GetResourceVersion()
		return resource.Delete(queryCtx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid, ResourceVersion: &version}})
	}
	return contractruntime.DeleteManifest(queryCtx, resource, name, "")
}

func (c *Client) dynamicResource(gvr schema.GroupVersionResource, namespaceScoped bool, namespace string, item *unstructured.Unstructured) (dynamic.ResourceInterface, string, error) {
	if !namespaceScoped {
		if item != nil && strings.TrimSpace(item.GetNamespace()) != "" {
			return nil, "", fmt.Errorf("yaml metadata.namespace must be empty for cluster-scoped resource")
		}
		if item != nil {
			item.SetNamespace("")
		}
		return c.dynamic.Resource(gvr), "", nil
	}
	effectiveNamespace := strings.TrimSpace(namespace)
	if item != nil {
		if strings.TrimSpace(item.GetNamespace()) == "" {
			item.SetNamespace(effectiveNamespace)
		}
		if effectiveNamespace == "" {
			effectiveNamespace = item.GetNamespace()
		}
		if item.GetNamespace() != effectiveNamespace {
			return nil, "", fmt.Errorf("yaml metadata.namespace does not match target resource")
		}
	}
	if effectiveNamespace == "" {
		return nil, "", fmt.Errorf("namespace is required for namespaced resource")
	}
	return c.dynamic.Resource(gvr).Namespace(effectiveNamespace), effectiveNamespace, nil
}

func resourceGVRForKind(kind string) (schema.GroupVersionResource, bool, string, error) {
	definition, ok := contractresource.LookupKubernetesResourceKind(kind)
	if !ok || !definition.GenericYAML {
		return schema.GroupVersionResource{}, false, "", fmt.Errorf("yaml apply does not support kind %s", kind)
	}
	return schema.GroupVersionResource{Group: definition.Group, Version: definition.Version, Resource: definition.Resource}, definition.Namespaced, definition.Kind, nil
}

func (c *Client) ListCRDs(ctx context.Context) ([]domainresource.CRDView, error) {
	return c.listCRDSummaries(ctx)
}

func (c *Client) ListHelmReleases(ctx context.Context, namespace string) ([]domainresource.HelmReleaseView, error) {
	return c.listHelmReleaseSummaries(ctx, namespace)
}

func (c *Client) GetHelmReleaseDetail(ctx context.Context, namespace, name string) (domainresource.HelmReleaseDetailView, error) {
	record, err := c.getHelmReleaseRecord(ctx, namespace, name, "")
	if err != nil {
		return domainresource.HelmReleaseDetailView{}, err
	}
	return mapHelmReleaseDetailRecord(record), nil
}

func (c *Client) ListHelmReleaseHistory(ctx context.Context, namespace, name string) ([]domainresource.HelmReleaseHistoryView, error) {
	records, err := c.listHelmReleaseRecords(ctx, namespace)
	if err != nil {
		return nil, err
	}
	items := make([]domainresource.HelmReleaseHistoryView, 0)
	for _, record := range records {
		if record.release == nil || record.release.Name != name {
			continue
		}
		items = append(items, mapHelmReleaseHistoryRecord(record))
	}
	sort.SliceStable(items, func(i, j int) bool {
		leftRevision, _ := strconv.Atoi(items[i].Revision)
		rightRevision, _ := strconv.Atoi(items[j].Revision)
		return leftRevision > rightRevision
	})
	if len(items) == 0 {
		return nil, fmt.Errorf("helm release %s not found", name)
	}
	return items, nil
}

func (c *Client) GetHelmReleaseValues(ctx context.Context, namespace, name, revision string) (domainresource.HelmValuesView, error) {
	record, err := c.getHelmReleaseRecord(ctx, namespace, name, revision)
	if err != nil {
		return domainresource.HelmValuesView{}, err
	}
	content, err := helmrelease.ValuesYAML(record.release)
	if err != nil {
		return domainresource.HelmValuesView{}, err
	}
	return domainresource.HelmValuesView{
		Name:           record.release.Name,
		Namespace:      record.release.Namespace,
		Revision:       strconv.Itoa(record.release.Version),
		Content:        content,
		Original:       content,
		Editable:       false,
		DiffEnabled:    true,
		AllowedActions: helmrelease.LegacyAllowedActions(record.labels),
	}, nil
}

func (c *Client) GetHelmReleaseManifest(ctx context.Context, namespace, name, revision string) (domainresource.HelmReleaseManifestView, error) {
	record, err := c.getHelmReleaseRecord(ctx, namespace, name, revision)
	if err != nil {
		return domainresource.HelmReleaseManifestView{}, err
	}
	return domainresource.HelmReleaseManifestView{
		Name: record.release.Name, Namespace: record.release.Namespace,
		Revision: strconv.Itoa(record.release.Version), Content: record.release.Manifest,
		Digest: helmrelease.Digest(record.release.Manifest),
	}, nil
}

func (c *Client) ListServices(ctx context.Context, namespace string) ([]domainresource.ServiceView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, cached := cachedResourceItems[corev1.Service](queryCtx, c, "Service", namespace)
	if !cached {
		live, err := c.typed.CoreV1().Services(namespace).List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		items = live.Items
	}
	views := make([]domainresource.ServiceView, 0, len(items))
	for _, item := range items {
		views = append(views, mapService(item))
	}
	return views, nil
}

func (c *Client) GetServiceDetail(ctx context.Context, namespace, name string) (domainresource.ServiceDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	service, err := c.typed.CoreV1().Services(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ServiceDetailView{}, err
	}
	endpointSlices, err := c.typed.DiscoveryV1().EndpointSlices(namespace).List(queryCtx, metav1.ListOptions{
		LabelSelector: labels.Set{discoveryv1.LabelServiceName: name}.AsSelector().String(),
	})
	if err != nil {
		endpointSlices = &discoveryv1.EndpointSliceList{}
	}
	backendPods := []domainresource.PodView{}
	if len(service.Spec.Selector) > 0 {
		pods, listErr := c.typed.CoreV1().Pods(namespace).List(queryCtx, metav1.ListOptions{
			LabelSelector: labels.Set(service.Spec.Selector).AsSelector().String(),
		})
		if listErr != nil {
			backendPods = nil
		} else {
			for _, pod := range pods.Items {
				backendPods = append(backendPods, mapPod(pod))
			}
		}
	}
	return buildServiceDetail(*service, endpointSlices.Items, backendPods), nil
}

func (c *Client) ListIngresses(ctx context.Context, namespace string) ([]domainresource.IngressView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, cached := cachedResourceItems[networkingv1.Ingress](queryCtx, c, "Ingress", namespace)
	if !cached {
		live, err := c.typed.NetworkingV1().Ingresses(namespace).List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		items = live.Items
	}
	views := make([]domainresource.IngressView, 0, len(items))
	for _, item := range items {
		views = append(views, mapIngress(item))
	}
	return views, nil
}

func (c *Client) GetIngressDetail(ctx context.Context, namespace, name string) (domainresource.IngressDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ingress, err := c.typed.NetworkingV1().Ingresses(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.IngressDetailView{}, err
	}
	backends := make([]domainresource.IngressBackendView, 0)
	for _, serviceName := range extractIngressBackendServices(*ingress) {
		service, getErr := c.typed.CoreV1().Services(namespace).Get(queryCtx, serviceName, metav1.GetOptions{})
		if getErr != nil {
			backends = append(backends, domainresource.IngressBackendView{ServiceName: serviceName})
			continue
		}
		slices, listErr := c.typed.DiscoveryV1().EndpointSlices(namespace).List(queryCtx, metav1.ListOptions{LabelSelector: labels.Set{discoveryv1.LabelServiceName: serviceName}.AsSelector().String()})
		if listErr != nil {
			slices = &discoveryv1.EndpointSliceList{}
		}
		pods := []corev1.Pod{}
		if len(service.Spec.Selector) > 0 {
			items, podErr := c.typed.CoreV1().Pods(namespace).List(queryCtx, metav1.ListOptions{LabelSelector: labels.Set(service.Spec.Selector).AsSelector().String()})
			if podErr != nil {
				pods = nil
			} else {
				pods = items.Items
			}
		}
		backends = append(backends, domainresource.IngressBackendView{
			ServiceName: serviceName,
			Endpoints:   mapEndpointSliceEndpoints(slices.Items),
			Pods:        buildNetworkRelatedPods(queryCtx, c.typed, pods),
		})
	}
	return buildIngressDetail(*ingress, backends), nil
}

func (c *Client) ListEndpointSlices(ctx context.Context, namespace string) ([]domainresource.EndpointSliceView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, cached := cachedResourceItems[discoveryv1.EndpointSlice](queryCtx, c, "EndpointSlice", namespace)
	if !cached {
		live, err := c.typed.DiscoveryV1().EndpointSlices(namespace).List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		items = live.Items
	}
	views := make([]domainresource.EndpointSliceView, 0, len(items))
	for _, item := range items {
		views = append(views, mapEndpointSlice(item))
	}
	return views, nil
}

func (c *Client) GetEndpointSliceDetail(ctx context.Context, namespace, name string) (domainresource.EndpointSliceDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.DiscoveryV1().EndpointSlices(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.EndpointSliceDetailView{}, err
	}
	return buildEndpointSliceDetail(*item), nil
}

func (c *Client) ListNetworkPolicies(ctx context.Context, namespace string) ([]domainresource.NetworkPolicyView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, cached := cachedResourceItems[networkingv1.NetworkPolicy](queryCtx, c, "NetworkPolicy", namespace)
	if !cached {
		live, err := c.typed.NetworkingV1().NetworkPolicies(namespace).List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		items = live.Items
	}
	views := make([]domainresource.NetworkPolicyView, 0, len(items))
	for _, item := range items {
		views = append(views, mapNetworkPolicy(item))
	}
	return views, nil
}

func (c *Client) GetNetworkPolicyDetail(ctx context.Context, namespace, name string) (domainresource.NetworkPolicyDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.NetworkingV1().NetworkPolicies(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.NetworkPolicyDetailView{}, err
	}
	selector, err := metav1.LabelSelectorAsSelector(&item.Spec.PodSelector)
	if err != nil {
		return domainresource.NetworkPolicyDetailView{}, err
	}
	pods, err := c.typed.CoreV1().Pods(namespace).List(queryCtx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return domainresource.NetworkPolicyDetailView{}, err
	}
	views := make([]domainresource.PodView, 0, len(pods.Items))
	for _, pod := range pods.Items {
		views = append(views, mapPod(pod))
	}
	return buildNetworkPolicyDetail(*item, views), nil
}

func (c *Client) ListPersistentVolumeClaims(ctx context.Context, namespace string) ([]domainresource.PersistentVolumeClaimView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.CoreV1().PersistentVolumeClaims(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.PersistentVolumeClaimView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapPersistentVolumeClaim(item))
	}
	return views, nil
}

func (c *Client) ListPersistentVolumes(ctx context.Context) ([]domainresource.PersistentVolumeView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.CoreV1().PersistentVolumes().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.PersistentVolumeView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapPersistentVolume(item))
	}
	return views, nil
}

func (c *Client) ListStorageClasses(ctx context.Context) ([]domainresource.StorageClassView, error) {
	return c.listStorageClassSummaries(ctx)
}

func (c *Client) ListIngressClasses(ctx context.Context) ([]domainresource.IngressClassView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.NetworkingV1().IngressClasses().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.IngressClassView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapIngressClass(item))
	}
	return views, nil
}

func (c *Client) ListPriorityClasses(ctx context.Context) ([]domainresource.PriorityClassView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.SchedulingV1().PriorityClasses().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.PriorityClassView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapPriorityClass(item))
	}
	return views, nil
}

func (c *Client) ListRuntimeClasses(ctx context.Context) ([]domainresource.RuntimeClassView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.NodeV1().RuntimeClasses().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.RuntimeClassView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapRuntimeClass(item))
	}
	return views, nil
}

func (c *Client) ListClusterRoles(ctx context.Context) ([]domainresource.ClusterRoleView, error) {
	return c.listClusterRoleSummaries(ctx)
}

func (c *Client) GetClusterRoleDetail(ctx context.Context, name string) (domainresource.ClusterRoleDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.RbacV1().ClusterRoles().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ClusterRoleDetailView{}, err
	}
	return mapClusterRoleDetail(*item), nil
}

func (c *Client) ListClusterRoleBindings(ctx context.Context) ([]domainresource.ClusterRoleBindingView, error) {
	return c.listClusterRoleBindingSummaries(ctx)
}

func (c *Client) ListClusterRoleBindingsForSubject(ctx context.Context, kind, name, namespace string) ([]domainresource.ClusterRoleBindingView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	views := []domainresource.ClusterRoleBindingView{}
	continueToken := ""
	for {
		items, err := c.typed.RbacV1().ClusterRoleBindings().List(queryCtx, metav1.ListOptions{
			Limit: int64(agentTablePageSize), Continue: continueToken,
		})
		if err != nil {
			return nil, err
		}
		for _, item := range items.Items {
			if bindingHasSubject(item.Subjects, kind, name, namespace, "") {
				views = append(views, mapClusterRoleBinding(item))
			}
		}
		if items.Continue == "" {
			return views, nil
		}
		if items.Continue == continueToken {
			return nil, fmt.Errorf("clusterrolebinding listing returned a repeated continue token")
		}
		continueToken = items.Continue
	}
}

func bindingHasSubject(subjects []rbacv1.Subject, kind, name, namespace, defaultNamespace string) bool {
	for _, subject := range subjects {
		subjectNamespace := subject.Namespace
		if subjectNamespace == "" {
			subjectNamespace = defaultNamespace
		}
		if subject.Kind == kind && subject.Name == name && subjectNamespace == namespace {
			return true
		}
	}
	return false
}

func (c *Client) GetClusterRoleBindingDetail(ctx context.Context, name string) (domainresource.ClusterRoleBindingDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.RbacV1().ClusterRoleBindings().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ClusterRoleBindingDetailView{}, err
	}
	return mapClusterRoleBindingDetail(*item), nil
}

func (c *Client) ListMutatingWebhookConfigurations(ctx context.Context) ([]domainresource.MutatingWebhookConfigurationView, error) {
	return c.listWebhookSummaries(ctx, "mutatingwebhookconfigurations")
}

func (c *Client) GetMutatingWebhookConfigurationDetail(ctx context.Context, name string) (domainresource.AdmissionWebhookConfigurationDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.AdmissionWebhookConfigurationDetailView{}, err
	}
	return mapMutatingWebhookConfigurationDetail(*item), nil
}

func (c *Client) ListValidatingWebhookConfigurations(ctx context.Context) ([]domainresource.ValidatingWebhookConfigurationView, error) {
	items, err := c.listWebhookSummaries(ctx, "validatingwebhookconfigurations")
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ValidatingWebhookConfigurationView, 0, len(items))
	for _, item := range items {
		views = append(views, domainresource.ValidatingWebhookConfigurationView{
			Name: item.Name, Webhooks: item.Webhooks, AgeSeconds: item.AgeSeconds,
		})
	}
	return views, nil
}

func (c *Client) GetValidatingWebhookConfigurationDetail(ctx context.Context, name string) (domainresource.AdmissionWebhookConfigurationDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.AdmissionWebhookConfigurationDetailView{}, err
	}
	return mapValidatingWebhookConfigurationDetail(*item), nil
}

func (c *Client) ListResourceQuotas(ctx context.Context, namespace string) ([]domainresource.ResourceQuotaView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.CoreV1().ResourceQuotas(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ResourceQuotaView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapResourceQuota(item))
	}
	return views, nil
}

func (c *Client) GetResourceQuotaDetail(ctx context.Context, namespace, name string) (domainresource.ResourceQuotaDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.CoreV1().ResourceQuotas(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ResourceQuotaDetailView{}, err
	}
	return mapResourceQuotaDetail(*item), nil
}

func (c *Client) ListLimitRanges(ctx context.Context, namespace string) ([]domainresource.LimitRangeView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.CoreV1().LimitRanges(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.LimitRangeView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapLimitRange(item))
	}
	return views, nil
}

func (c *Client) GetLimitRangeDetail(ctx context.Context, namespace, name string) (domainresource.LimitRangeDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.CoreV1().LimitRanges(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.LimitRangeDetailView{}, err
	}
	return mapLimitRangeDetail(*item), nil
}

func (c *Client) ListLeases(ctx context.Context, namespace string) ([]domainresource.LeaseView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.CoordinationV1().Leases(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.LeaseView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapLease(item))
	}
	return views, nil
}

func (c *Client) ListReplicationControllers(ctx context.Context, namespace string) ([]domainresource.ReplicationControllerView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.CoreV1().ReplicationControllers(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ReplicationControllerView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapReplicationController(item))
	}
	return views, nil
}

func (c *Client) ListClusterEvents(ctx context.Context, namespace string, limit int) ([]domainresource.ClusterEventView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, cached := cachedResourceItems[corev1.Event](queryCtx, c, "Event", namespace)
	if !cached {
		live, err := c.typed.CoreV1().Events(namespace).List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		items = live.Items
	}
	views := make([]domainresource.ClusterEventView, 0, len(items))
	for _, item := range items {
		views = append(views, mapClusterEvent(item))
	}
	sort.SliceStable(views, func(i, j int) bool {
		return views[i].LastTimestamp > views[j].LastTimestamp
	})
	if limit > 0 && len(views) > limit {
		views = views[:limit]
	}
	return views, nil
}

func (c *Client) RestartDeployment(ctx context.Context, namespace, name string) error {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	deployment, err := c.typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err := contractruntime.ValidateDirectManifestOwner(deployment); err != nil {
		return err
	}
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().UTC().Format(time.RFC3339)
	_, err = c.typed.AppsV1().Deployments(namespace).Update(queryCtx, deployment, metav1.UpdateOptions{})
	return err
}

func (c *Client) ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) error {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	deployment, err := c.typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err := contractruntime.ValidateDirectManifestOwner(deployment); err != nil {
		return err
	}
	deployment.Spec.Replicas = &replicas
	_, err = c.typed.AppsV1().Deployments(namespace).Update(queryCtx, deployment, metav1.UpdateOptions{})
	return err
}

func (c *Client) RestartStatefulSet(ctx context.Context, namespace, name string) error {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	statefulSet, err := c.typed.AppsV1().StatefulSets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err := contractruntime.ValidateDirectManifestOwner(statefulSet); err != nil {
		return err
	}
	if statefulSet.Spec.Template.Annotations == nil {
		statefulSet.Spec.Template.Annotations = map[string]string{}
	}
	statefulSet.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().UTC().Format(time.RFC3339)
	_, err = c.typed.AppsV1().StatefulSets(namespace).Update(queryCtx, statefulSet, metav1.UpdateOptions{})
	return err
}

func (c *Client) ScaleStatefulSet(ctx context.Context, namespace, name string, replicas int32) error {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	statefulSet, err := c.typed.AppsV1().StatefulSets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err := contractruntime.ValidateDirectManifestOwner(statefulSet); err != nil {
		return err
	}
	statefulSet.Spec.Replicas = &replicas
	_, err = c.typed.AppsV1().StatefulSets(namespace).Update(queryCtx, statefulSet, metav1.UpdateOptions{})
	return err
}

func (c *Client) RestartDaemonSet(ctx context.Context, namespace, name string) error {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	daemonSet, err := c.typed.AppsV1().DaemonSets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if err := contractruntime.ValidateDirectManifestOwner(daemonSet); err != nil {
		return err
	}
	if daemonSet.Spec.Template.Annotations == nil {
		daemonSet.Spec.Template.Annotations = map[string]string{}
	}
	daemonSet.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().UTC().Format(time.RFC3339)
	_, err = c.typed.AppsV1().DaemonSets(namespace).Update(queryCtx, daemonSet, metav1.UpdateOptions{})
	return err
}

func (c *Client) UpdateDeploymentImage(ctx context.Context, namespace, name, containerName, image string) (string, string, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	deployment, err := c.typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return "", "", err
	}
	if err := contractruntime.ValidateDirectManifestOwner(deployment); err != nil {
		return "", "", err
	}
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return "", "", fmt.Errorf("deployment has no containers")
	}
	if containerName == "" {
		previous := deployment.Spec.Template.Spec.Containers[0].Image
		deployment.Spec.Template.Spec.Containers[0].Image = image
		_, err = c.typed.AppsV1().Deployments(namespace).Update(queryCtx, deployment, metav1.UpdateOptions{})
		return deployment.Spec.Template.Spec.Containers[0].Name, previous, err
	}
	for index := range deployment.Spec.Template.Spec.Containers {
		if deployment.Spec.Template.Spec.Containers[index].Name == containerName {
			previous := deployment.Spec.Template.Spec.Containers[index].Image
			deployment.Spec.Template.Spec.Containers[index].Image = image
			_, err = c.typed.AppsV1().Deployments(namespace).Update(queryCtx, deployment, metav1.UpdateOptions{})
			return deployment.Spec.Template.Spec.Containers[index].Name, previous, err
		}
	}
	return "", "", fmt.Errorf("container %s not found in deployment", containerName)
}

func buildRESTConfig(cfg cfgpkg.KubernetesConfig) (*rest.Config, error) {
	if cfg.KubeconfigData != "" {
		clientConfig, err := clientcmd.NewClientConfigFromBytes([]byte(cfg.KubeconfigData))
		if err != nil {
			return nil, err
		}
		restConfig, err := clientConfig.ClientConfig()
		if err != nil {
			return nil, err
		}
		restConfig.QPS = 20
		restConfig.Burst = 40
		restConfig.Timeout = 5 * time.Second
		return restConfig, nil
	}

	if strings.TrimSpace(cfg.Kubeconfig) == "" {
		restConfig, err := rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
		restConfig.QPS = 20
		restConfig.Burst = 40
		restConfig.Timeout = 5 * time.Second
		return restConfig, nil
	}

	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: cfg.Kubeconfig}
	overrides := &clientcmd.ConfigOverrides{}
	if cfg.Context != "" {
		overrides.CurrentContext = cfg.Context
	}
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	restConfig.QPS = 20
	restConfig.Burst = 40
	restConfig.Timeout = 5 * time.Second
	return restConfig, nil
}

func mapPod(item corev1.Pod) domainresource.PodView {
	ready := 0
	restarts := int32(0)
	claims := make([]string, 0)
	requests, limits := podResourceTotals(item)
	for _, status := range item.Status.ContainerStatuses {
		if status.Ready {
			ready++
		}
		restarts += status.RestartCount
	}
	for _, volume := range item.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil && strings.TrimSpace(volume.PersistentVolumeClaim.ClaimName) != "" {
			claims = append(claims, volume.PersistentVolumeClaim.ClaimName)
		}
	}
	return domainresource.PodView{
		UID:                    string(item.UID),
		Name:                   item.Name,
		Namespace:              item.Namespace,
		Phase:                  string(item.Status.Phase),
		NodeName:               item.Spec.NodeName,
		PodIP:                  item.Status.PodIP,
		CreatedAt:              item.CreationTimestamp.Time.Format(time.RFC3339),
		Requests:               formatNodeResourceTotals(requests),
		Limits:                 formatNodeResourceTotals(limits),
		Labels:                 item.Labels,
		PersistentVolumeClaims: claims,
		ReadyContainers:        fmt.Sprintf("%d/%d", ready, len(item.Status.ContainerStatuses)),
		Restarts:               restarts,
		AgeSeconds:             secondsSince(item.CreationTimestamp.Time),
	}
}

func mapPodDetail(item corev1.Pod) domainresource.PodDetailView {
	containers := make([]domainresource.WorkloadContainerView, 0, len(item.Spec.Containers))
	statusMap := make(map[string]corev1.ContainerStatus, len(item.Status.ContainerStatuses))
	for _, status := range item.Status.ContainerStatuses {
		statusMap[status.Name] = status
	}
	for _, container := range item.Spec.Containers {
		containerStatus := statusMap[container.Name]
		containers = append(containers, domainresource.WorkloadContainerView{
			Name:         container.Name,
			Image:        container.Image,
			Ready:        containerStatus.Ready,
			RestartCount: containerStatus.RestartCount,
			State:        containerState(containerStatus.State),
			LastState:    containerState(containerStatus.LastTerminationState),
		})
	}
	conditions := make([]domainresource.WorkloadConditionView, 0, len(item.Status.Conditions))
	for _, condition := range item.Status.Conditions {
		conditions = append(conditions, domainresource.WorkloadConditionView{
			Type:               string(condition.Type),
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastTransitionTime: condition.LastTransitionTime.Time.Format(time.RFC3339),
		})
	}
	startTime := ""
	if item.Status.StartTime != nil {
		startTime = item.Status.StartTime.Time.Format(time.RFC3339)
	}
	return domainresource.PodDetailView{
		UID:                string(item.UID),
		Name:               item.Name,
		Namespace:          item.Namespace,
		Phase:              string(item.Status.Phase),
		PodIP:              item.Status.PodIP,
		HostIP:             item.Status.HostIP,
		NodeName:           item.Spec.NodeName,
		ServiceAccountName: item.Spec.ServiceAccountName,
		QOSClass:           string(item.Status.QOSClass),
		CreatedAt:          item.CreationTimestamp.Time.Format(time.RFC3339),
		StartTime:          startTime,
		Labels:             item.Labels,
		Annotations:        item.Annotations,
		Containers:         containers,
		Conditions:         conditions,
	}
}

func (c *Client) buildPodDetail(ctx context.Context, item corev1.Pod) domainresource.PodDetailView {
	view := mapPodDetail(item)
	refs := buildPodVolumeSourceRefs(item)
	view.Containers = buildDetailedPodContainers(item)
	view.Volumes = buildPodVolumes(item, refs)
	view.RelatedResources = c.buildPodRelatedResources(ctx, item, refs)
	return view
}

type podVolumeSourceRefSet struct {
	configMaps      map[string]struct{}
	secrets         map[string]struct{}
	serviceAccounts map[string]struct{}
	pvcs            map[string]struct{}
}

type podRelatedResourceAccumulator struct {
	kind      string
	name      string
	namespace string
	relations map[string]struct{}
	details   map[string]struct{}
}

func buildDetailedPodContainers(item corev1.Pod) []domainresource.WorkloadContainerView {
	build := func(specs []corev1.Container, statuses []corev1.ContainerStatus, role string) []domainresource.WorkloadContainerView {
		containers := make([]domainresource.WorkloadContainerView, 0, len(specs))
		statusMap := make(map[string]corev1.ContainerStatus, len(statuses))
		for _, status := range statuses {
			statusMap[status.Name] = status
		}
		for index, container := range specs {
			containerStatus := statusMap[container.Name]
			state := containerState(containerStatus.State)
			lastState := containerState(containerStatus.LastTerminationState)
			startedAt := ""
			reason := ""
			message := ""
			if containerStatus.State.Running != nil && !containerStatus.State.Running.StartedAt.IsZero() {
				startedAt = containerStatus.State.Running.StartedAt.Time.UTC().Format(time.RFC3339)
			}
			if containerStatus.State.Waiting != nil {
				reason = containerStatus.State.Waiting.Reason
				message = containerStatus.State.Waiting.Message
			}
			if containerStatus.State.Terminated != nil {
				if reason == "" {
					reason = containerStatus.State.Terminated.Reason
				}
				if message == "" {
					message = containerStatus.State.Terminated.Message
				}
				if startedAt == "" && !containerStatus.State.Terminated.StartedAt.IsZero() {
					startedAt = containerStatus.State.Terminated.StartedAt.Time.UTC().Format(time.RFC3339)
				}
			}
			containerRole := role
			if role == "init" && container.RestartPolicy != nil && *container.RestartPolicy == corev1.ContainerRestartPolicyAlways {
				containerRole = "sidecar"
			}
			if containerRole == "" {
				containerRole = "sidecar"
				if index == 0 {
					containerRole = "main"
				}
			}
			containers = append(containers, domainresource.WorkloadContainerView{
				Name:         container.Name,
				Image:        container.Image,
				Role:         containerRole,
				Ready:        containerStatus.Ready,
				RestartCount: containerStatus.RestartCount,
				State:        state,
				LastState:    lastState,
				ContainerID:  strings.TrimSpace(containerStatus.ContainerID),
				StartedAt:    startedAt,
				Reason:       strings.TrimSpace(reason),
				Message:      strings.TrimSpace(message),
			})
		}
		return containers
	}
	containers := build(item.Spec.InitContainers, item.Status.InitContainerStatuses, "init")
	return append(containers, build(item.Spec.Containers, item.Status.ContainerStatuses, "")...)
}

func buildPodVolumeSourceRefs(item corev1.Pod) podVolumeSourceRefSet {
	refs := podVolumeSourceRefSet{
		configMaps:      map[string]struct{}{},
		secrets:         map[string]struct{}{},
		serviceAccounts: map[string]struct{}{},
		pvcs:            map[string]struct{}{},
	}
	if sa := strings.TrimSpace(item.Spec.ServiceAccountName); sa != "" {
		refs.serviceAccounts[sa] = struct{}{}
	}
	for _, volume := range item.Spec.Volumes {
		if volume.ConfigMap != nil && strings.TrimSpace(volume.ConfigMap.Name) != "" {
			refs.configMaps[volume.ConfigMap.Name] = struct{}{}
		}
		if volume.Secret != nil && strings.TrimSpace(volume.Secret.SecretName) != "" {
			refs.secrets[volume.Secret.SecretName] = struct{}{}
		}
		if volume.PersistentVolumeClaim != nil && strings.TrimSpace(volume.PersistentVolumeClaim.ClaimName) != "" {
			refs.pvcs[volume.PersistentVolumeClaim.ClaimName] = struct{}{}
		}
		if volume.Projected != nil {
			for _, source := range volume.Projected.Sources {
				if source.ConfigMap != nil && strings.TrimSpace(source.ConfigMap.Name) != "" {
					refs.configMaps[source.ConfigMap.Name] = struct{}{}
				}
				if source.Secret != nil && strings.TrimSpace(source.Secret.Name) != "" {
					refs.secrets[source.Secret.Name] = struct{}{}
				}
				if source.ServiceAccountToken != nil && strings.TrimSpace(item.Spec.ServiceAccountName) != "" {
					refs.serviceAccounts[item.Spec.ServiceAccountName] = struct{}{}
				}
			}
		}
	}
	for _, container := range item.Spec.Containers {
		collectContainerEnvRefs(container, &refs)
	}
	for _, container := range item.Spec.InitContainers {
		collectContainerEnvRefs(container, &refs)
	}
	return refs
}

func collectContainerEnvRefs(container corev1.Container, refs *podVolumeSourceRefSet) {
	for _, env := range container.Env {
		if env.ValueFrom == nil {
			continue
		}
		if env.ValueFrom.ConfigMapKeyRef != nil && strings.TrimSpace(env.ValueFrom.ConfigMapKeyRef.Name) != "" {
			refs.configMaps[env.ValueFrom.ConfigMapKeyRef.Name] = struct{}{}
		}
		if env.ValueFrom.SecretKeyRef != nil && strings.TrimSpace(env.ValueFrom.SecretKeyRef.Name) != "" {
			refs.secrets[env.ValueFrom.SecretKeyRef.Name] = struct{}{}
		}
	}
	for _, envFrom := range container.EnvFrom {
		if envFrom.ConfigMapRef != nil && strings.TrimSpace(envFrom.ConfigMapRef.Name) != "" {
			refs.configMaps[envFrom.ConfigMapRef.Name] = struct{}{}
		}
		if envFrom.SecretRef != nil && strings.TrimSpace(envFrom.SecretRef.Name) != "" {
			refs.secrets[envFrom.SecretRef.Name] = struct{}{}
		}
	}
}

func buildPodVolumes(item corev1.Pod, refs podVolumeSourceRefSet) []domainresource.PodVolumeView {
	mountsByVolume := map[string][]domainresource.PodVolumeMountView{}
	appendMounts := func(containerName string, mounts []corev1.VolumeMount) {
		for _, mount := range mounts {
			if strings.TrimSpace(mount.Name) == "" {
				continue
			}
			mountsByVolume[mount.Name] = append(mountsByVolume[mount.Name], domainresource.PodVolumeMountView{
				Name:        containerName,
				MountPath:   mount.MountPath,
				SubPath:     mount.SubPath,
				ReadOnly:    mount.ReadOnly,
				Description: containerName,
			})
		}
	}
	for _, container := range item.Spec.InitContainers {
		appendMounts(container.Name, container.VolumeMounts)
	}
	for _, container := range item.Spec.Containers {
		appendMounts(container.Name, container.VolumeMounts)
	}

	volumes := make([]domainresource.PodVolumeView, 0, len(item.Spec.Volumes))
	for _, volume := range item.Spec.Volumes {
		volumeType, sourceName, readOnly, details := describePodVolume(volume)
		referencedConfigMaps := referencedConfigMapsForVolume(volume)
		volumeMounts := append([]domainresource.PodVolumeMountView(nil), mountsByVolume[volume.Name]...)
		for index := range volumeMounts {
			volumeMounts[index].VolumeType = volumeType
			volumeMounts[index].SourceName = sourceName
		}
		sort.SliceStable(volumeMounts, func(i, j int) bool {
			if volumeMounts[i].Name != volumeMounts[j].Name {
				return volumeMounts[i].Name < volumeMounts[j].Name
			}
			return volumeMounts[i].MountPath < volumeMounts[j].MountPath
		})
		sort.Strings(referencedConfigMaps)
		volumes = append(volumes, domainresource.PodVolumeView{
			Name:                 volume.Name,
			Type:                 volumeType,
			SourceName:           sourceName,
			ReadOnly:             readOnly,
			Details:              details,
			VolumeMounts:         volumeMounts,
			ReferencedConfigMaps: referencedConfigMaps,
		})
	}
	sort.SliceStable(volumes, func(i, j int) bool { return volumes[i].Name < volumes[j].Name })
	return volumes
}

func describePodVolume(volume corev1.Volume) (string, string, bool, []string) {
	switch {
	case volume.ConfigMap != nil:
		details := []string{fmt.Sprintf("ConfigMap: %s", volume.ConfigMap.Name)}
		if volume.ConfigMap.Optional != nil {
			details = append(details, fmt.Sprintf("Optional: %t", *volume.ConfigMap.Optional))
		}
		if len(volume.ConfigMap.Items) > 0 {
			details = append(details, fmt.Sprintf("Items: %d", len(volume.ConfigMap.Items)))
		}
		return "ConfigMap", volume.ConfigMap.Name, false, details
	case volume.Secret != nil:
		details := []string{fmt.Sprintf("Secret: %s", volume.Secret.SecretName)}
		if volume.Secret.Optional != nil {
			details = append(details, fmt.Sprintf("Optional: %t", *volume.Secret.Optional))
		}
		if volume.Secret.DefaultMode != nil {
			details = append(details, fmt.Sprintf("DefaultMode: %04o", *volume.Secret.DefaultMode))
		}
		return "Secret", volume.Secret.SecretName, false, details
	case volume.PersistentVolumeClaim != nil:
		details := []string{fmt.Sprintf("PVC: %s", volume.PersistentVolumeClaim.ClaimName)}
		if volume.PersistentVolumeClaim.ReadOnly {
			details = append(details, "ReadOnly: true")
		}
		return "PersistentVolumeClaim", volume.PersistentVolumeClaim.ClaimName, volume.PersistentVolumeClaim.ReadOnly, details
	case volume.Projected != nil:
		details := []string{fmt.Sprintf("Sources: %d", len(volume.Projected.Sources))}
		if volume.Projected.DefaultMode != nil {
			details = append(details, fmt.Sprintf("DefaultMode: %04o", *volume.Projected.DefaultMode))
		}
		return "Projected", summarizeProjectedSourceNames(volume.Projected.Sources), false, details
	case volume.EmptyDir != nil:
		details := []string{}
		if volume.EmptyDir.Medium != "" {
			details = append(details, fmt.Sprintf("Medium: %s", volume.EmptyDir.Medium))
		}
		if volume.EmptyDir.SizeLimit != nil {
			details = append(details, fmt.Sprintf("SizeLimit: %s", volume.EmptyDir.SizeLimit.String()))
		}
		return "EmptyDir", "", false, details
	case volume.HostPath != nil:
		details := []string{fmt.Sprintf("Path: %s", volume.HostPath.Path)}
		if volume.HostPath.Type != nil {
			details = append(details, fmt.Sprintf("HostPathType: %s", string(*volume.HostPath.Type)))
		}
		return "HostPath", volume.HostPath.Path, false, details
	case volume.DownwardAPI != nil:
		details := []string{fmt.Sprintf("Items: %d", len(volume.DownwardAPI.Items))}
		if volume.DownwardAPI.DefaultMode != nil {
			details = append(details, fmt.Sprintf("DefaultMode: %04o", *volume.DownwardAPI.DefaultMode))
		}
		return "DownwardAPI", "", false, details
	default:
		return detectGenericPodVolumeType(volume), "", false, nil
	}
}

func detectGenericPodVolumeType(volume corev1.Volume) string {
	switch {
	case volume.CSI != nil:
		return "CSI"
	case volume.NFS != nil:
		return "NFS"
	case volume.AzureDisk != nil:
		return "AzureDisk"
	case volume.AzureFile != nil:
		return "AzureFile"
	case volume.CephFS != nil:
		return "CephFS"
	case volume.GCEPersistentDisk != nil:
		return "GCEPersistentDisk"
	case volume.ISCSI != nil:
		return "ISCSI"
	case volume.Ephemeral != nil:
		return "Ephemeral"
	default:
		return "Other"
	}
}

func summarizeProjectedSourceNames(sources []corev1.VolumeProjection) string {
	names := make([]string, 0, len(sources))
	for _, source := range sources {
		switch {
		case source.ConfigMap != nil && strings.TrimSpace(source.ConfigMap.Name) != "":
			names = append(names, source.ConfigMap.Name)
		case source.Secret != nil && strings.TrimSpace(source.Secret.Name) != "":
			names = append(names, source.Secret.Name)
		case source.ServiceAccountToken != nil:
			names = append(names, "serviceAccountToken")
		case source.DownwardAPI != nil:
			names = append(names, "downwardAPI")
		case source.ClusterTrustBundle != nil && source.ClusterTrustBundle.Name != nil && strings.TrimSpace(*source.ClusterTrustBundle.Name) != "":
			names = append(names, *source.ClusterTrustBundle.Name)
		}
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func referencedConfigMapsForVolume(volume corev1.Volume) []string {
	names := make([]string, 0, 2)
	if volume.ConfigMap != nil && strings.TrimSpace(volume.ConfigMap.Name) != "" {
		names = append(names, volume.ConfigMap.Name)
	}
	if volume.Projected != nil {
		for _, source := range volume.Projected.Sources {
			if source.ConfigMap != nil && strings.TrimSpace(source.ConfigMap.Name) != "" {
				names = append(names, source.ConfigMap.Name)
			}
		}
	}
	return uniqueSortedStrings(names)
}

func (c *Client) buildPodRelatedResources(ctx context.Context, item corev1.Pod, refs podVolumeSourceRefSet) []domainresource.PodRelatedResourceView {
	resources := map[string]*podRelatedResourceAccumulator{}
	add := func(kind, namespace, name, relation string, details ...string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		key := fmt.Sprintf("%s/%s/%s", kind, namespace, name)
		entry, ok := resources[key]
		if !ok {
			entry = &podRelatedResourceAccumulator{
				kind:      kind,
				name:      name,
				namespace: namespace,
				relations: map[string]struct{}{},
				details:   map[string]struct{}{},
			}
			resources[key] = entry
		}
		if strings.TrimSpace(relation) != "" {
			entry.relations[relation] = struct{}{}
		}
		for _, detail := range details {
			if strings.TrimSpace(detail) != "" {
				entry.details[detail] = struct{}{}
			}
		}
	}

	if sa := strings.TrimSpace(item.Spec.ServiceAccountName); sa != "" {
		add("ServiceAccount", item.Namespace, sa, "service-account")
	}
	for name := range refs.configMaps {
		add("ConfigMap", item.Namespace, name, "config")
	}
	for name := range refs.secrets {
		add("Secret", item.Namespace, name, "secret")
	}
	for name := range refs.pvcs {
		add("PersistentVolumeClaim", item.Namespace, name, "volume")
	}
	for _, owner := range item.OwnerReferences {
		switch owner.Kind {
		case "ReplicaSet":
			add("ReplicaSet", item.Namespace, owner.Name, "owner")
		case "StatefulSet", "DaemonSet", "Job", "CronJob":
			add(owner.Kind, item.Namespace, owner.Name, "owner")
		}
	}

	if services, err := c.ListServices(ctx, item.Namespace); err == nil {
		serviceNames := map[string]struct{}{}
		for _, svc := range services {
			if selectorMatchesPodLabels(svc.Selector, item.Labels) {
				add("Service", svc.Namespace, svc.Name, "selected-by-service", fmt.Sprintf("Type: %s", svc.Type))
				serviceNames[svc.Name] = struct{}{}
			}
		}
		if ingresses, err := c.ListIngresses(ctx, item.Namespace); err == nil {
			for _, ingress := range ingresses {
				for _, serviceName := range ingress.BackendServices {
					if _, ok := serviceNames[serviceName]; ok {
						add("Ingress", ingress.Namespace, ingress.Name, "routes-service", fmt.Sprintf("Service: %s", serviceName))
					}
				}
			}
		}
	}
	if replicaSets, err := c.typed.AppsV1().ReplicaSets(item.Namespace).List(ctx, metav1.ListOptions{}); err == nil {
		for _, rs := range replicaSets.Items {
			if selectorMatchesPodLabels(rs.Spec.Selector.MatchLabels, item.Labels) {
				add("ReplicaSet", rs.Namespace, rs.Name, "selector-match")
				for _, owner := range rs.OwnerReferences {
					if owner.Kind == "Deployment" {
						add("Deployment", rs.Namespace, owner.Name, "managed-by-replicaset", fmt.Sprintf("ReplicaSet: %s", rs.Name))
					}
				}
			}
		}
	}
	if deployments, err := c.typed.AppsV1().Deployments(item.Namespace).List(ctx, metav1.ListOptions{}); err == nil {
		for _, deployment := range deployments.Items {
			if selectorMatchesPodLabels(deployment.Spec.Selector.MatchLabels, item.Labels) {
				add("Deployment", deployment.Namespace, deployment.Name, "selector-match")
			}
		}
	}

	result := make([]domainresource.PodRelatedResourceView, 0, len(resources))
	for _, entry := range resources {
		result = append(result, domainresource.PodRelatedResourceView{
			Kind:      entry.kind,
			Name:      entry.name,
			Namespace: entry.namespace,
			Relations: mapKeysSorted(entry.relations),
			Details:   mapKeysSorted(entry.details),
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind < result[j].Kind
		}
		if result[i].Namespace != result[j].Namespace {
			return result[i].Namespace < result[j].Namespace
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func selectorMatchesPodLabels(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}

func uniqueSortedStrings(items []string) []string {
	set := make(map[string]struct{}, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			set[item] = struct{}{}
		}
	}
	return mapKeysSorted(set)
}

func mapKeysSorted(items map[string]struct{}) []string {
	values := make([]string, 0, len(items))
	for item := range items {
		values = append(values, item)
	}
	sort.Strings(values)
	return values
}

func mapDeploymentDetail(item appsv1.Deployment) domainresource.DeploymentDetailView {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	containers := make([]domainresource.WorkloadContainerView, 0, len(item.Spec.Template.Spec.Containers))
	for _, container := range item.Spec.Template.Spec.Containers {
		containers = append(containers, domainresource.WorkloadContainerView{Name: container.Name, Image: container.Image})
	}
	conditions := make([]domainresource.WorkloadConditionView, 0, len(item.Status.Conditions))
	for _, condition := range item.Status.Conditions {
		conditions = append(conditions, domainresource.WorkloadConditionView{
			Type:               string(condition.Type),
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastTransitionTime: condition.LastTransitionTime.Time.Format(time.RFC3339),
		})
	}
	return domainresource.DeploymentDetailView{
		Name:               item.Name,
		Namespace:          item.Namespace,
		DesiredReplicas:    desired,
		ReadyReplicas:      item.Status.ReadyReplicas,
		UpdatedReplicas:    item.Status.UpdatedReplicas,
		AvailableReplicas:  item.Status.AvailableReplicas,
		ObservedGeneration: item.Status.ObservedGeneration,
		Strategy:           string(item.Spec.Strategy.Type),
		CreatedAt:          item.CreationTimestamp.Time.Format(time.RFC3339),
		Labels:             item.Labels,
		Annotations:        item.Annotations,
		Selector:           item.Spec.Selector.MatchLabels,
		Containers:         containers,
		Conditions:         conditions,
	}
}

func mapDeploymentRolloutStatus(item appsv1.Deployment) domainresource.DeploymentRolloutStatusView {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	status := "progressing"
	message := "rollout is progressing"
	for _, condition := range item.Status.Conditions {
		if condition.Type == appsv1.DeploymentAvailable && condition.Status == corev1.ConditionTrue && item.Status.UpdatedReplicas == desired && item.Status.AvailableReplicas == desired {
			status = "healthy"
			message = "deployment is fully available"
		}
		if condition.Type == appsv1.DeploymentReplicaFailure && condition.Status == corev1.ConditionTrue {
			status = "degraded"
			message = condition.Message
		}
	}
	conditions := make([]domainresource.WorkloadConditionView, 0, len(item.Status.Conditions))
	for _, condition := range item.Status.Conditions {
		conditions = append(conditions, domainresource.WorkloadConditionView{
			Type:               string(condition.Type),
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastTransitionTime: condition.LastTransitionTime.Time.Format(time.RFC3339),
		})
	}
	return domainresource.DeploymentRolloutStatusView{
		Name:               item.Name,
		Namespace:          item.Namespace,
		Revision:           item.Annotations["deployment.kubernetes.io/revision"],
		Status:             status,
		Message:            message,
		DesiredReplicas:    desired,
		UpdatedReplicas:    item.Status.UpdatedReplicas,
		ReadyReplicas:      item.Status.ReadyReplicas,
		AvailableReplicas:  item.Status.AvailableReplicas,
		ObservedGeneration: item.Status.ObservedGeneration,
		Conditions:         conditions,
	}
}

func mapStatefulSet(item appsv1.StatefulSet) domainresource.StatefulSetView {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	return domainresource.StatefulSetView{
		Name:            item.Name,
		Namespace:       item.Namespace,
		ServiceName:     item.Spec.ServiceName,
		DesiredReplicas: desired,
		ReadyReplicas:   item.Status.ReadyReplicas,
		CurrentReplicas: item.Status.CurrentReplicas,
		AgeSeconds:      secondsSince(item.CreationTimestamp.Time),
	}
}

func mapStatefulSetDetail(item appsv1.StatefulSet) domainresource.StatefulSetDetailView {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	return domainresource.StatefulSetDetailView{
		Name:            item.Name,
		Namespace:       item.Namespace,
		ServiceName:     item.Spec.ServiceName,
		DesiredReplicas: desired,
		ReadyReplicas:   item.Status.ReadyReplicas,
		CurrentReplicas: item.Status.CurrentReplicas,
		UpdateStrategy:  string(item.Spec.UpdateStrategy.Type),
		CurrentRevision: item.Status.CurrentRevision,
		UpdateRevision:  item.Status.UpdateRevision,
		CreatedAt:       item.CreationTimestamp.Time.Format(time.RFC3339),
		Labels:          item.Labels,
		Annotations:     item.Annotations,
		Selector:        item.Spec.Selector.MatchLabels,
	}
}

func mapDaemonSetDetail(item appsv1.DaemonSet) domainresource.DaemonSetDetailView {
	selector := map[string]string{}
	if item.Spec.Selector != nil {
		selector = item.Spec.Selector.MatchLabels
	}
	return domainresource.DaemonSetDetailView{
		Name:            item.Name,
		Namespace:       item.Namespace,
		DesiredNumber:   item.Status.DesiredNumberScheduled,
		CurrentNumber:   item.Status.CurrentNumberScheduled,
		ReadyNumber:     item.Status.NumberReady,
		AvailableNumber: item.Status.NumberAvailable,
		UpdatedNumber:   item.Status.UpdatedNumberScheduled,
		UpdateStrategy:  string(item.Spec.UpdateStrategy.Type),
		CreatedAt:       item.CreationTimestamp.Time.Format(time.RFC3339),
		Labels:          item.Labels,
		Annotations:     item.Annotations,
		Selector:        selector,
	}
}

func mapJob(item batchv1.Job) domainresource.JobView {
	completions := int32(0)
	if item.Spec.Completions != nil {
		completions = *item.Spec.Completions
	}
	completionMode := ""
	if item.Spec.CompletionMode != nil {
		completionMode = string(*item.Spec.CompletionMode)
	}
	return domainresource.JobView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		Completions:    completions,
		Succeeded:      item.Status.Succeeded,
		Failed:         item.Status.Failed,
		Active:         item.Status.Active,
		CompletionMode: completionMode,
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
	}
}

func mapJobDetail(item batchv1.Job) domainresource.JobDetailView {
	completions := int32(0)
	if item.Spec.Completions != nil {
		completions = *item.Spec.Completions
	}
	parallelism := int32(1)
	if item.Spec.Parallelism != nil {
		parallelism = *item.Spec.Parallelism
	}
	completionMode := ""
	if item.Spec.CompletionMode != nil {
		completionMode = string(*item.Spec.CompletionMode)
	}
	startTime := ""
	if item.Status.StartTime != nil {
		startTime = item.Status.StartTime.Time.Format(time.RFC3339)
	}
	completionTime := ""
	if item.Status.CompletionTime != nil {
		completionTime = item.Status.CompletionTime.Time.Format(time.RFC3339)
	}
	return domainresource.JobDetailView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		Completions:    completions,
		Parallelism:    parallelism,
		Succeeded:      item.Status.Succeeded,
		Failed:         item.Status.Failed,
		Active:         item.Status.Active,
		CompletionMode: completionMode,
		CreatedAt:      item.CreationTimestamp.Time.Format(time.RFC3339),
		StartTime:      startTime,
		CompletionTime: completionTime,
		Labels:         item.Labels,
		Annotations:    item.Annotations,
	}
}

func mapCronJob(item batchv1.CronJob) domainresource.CronJobView {
	lastScheduleTime := ""
	if item.Status.LastScheduleTime != nil {
		lastScheduleTime = item.Status.LastScheduleTime.Time.Format(time.RFC3339)
	}
	return domainresource.CronJobView{
		Name:             item.Name,
		Namespace:        item.Namespace,
		Schedule:         item.Spec.Schedule,
		Suspend:          item.Spec.Suspend != nil && *item.Spec.Suspend,
		ActiveJobs:       int32(len(item.Status.Active)),
		LastScheduleTime: lastScheduleTime,
		AgeSeconds:       secondsSince(item.CreationTimestamp.Time),
	}
}

func mapCronJobDetail(item batchv1.CronJob) domainresource.CronJobDetailView {
	lastScheduleTime := ""
	if item.Status.LastScheduleTime != nil {
		lastScheduleTime = item.Status.LastScheduleTime.Time.Format(time.RFC3339)
	}
	timeZone := ""
	if item.Spec.TimeZone != nil {
		timeZone = *item.Spec.TimeZone
	}
	return domainresource.CronJobDetailView{
		Name:              item.Name,
		Namespace:         item.Namespace,
		Schedule:          item.Spec.Schedule,
		Suspend:           item.Spec.Suspend != nil && *item.Spec.Suspend,
		ActiveJobs:        int32(len(item.Status.Active)),
		LastScheduleTime:  lastScheduleTime,
		ConcurrencyPolicy: string(item.Spec.ConcurrencyPolicy),
		TimeZone:          timeZone,
		CreatedAt:         item.CreationTimestamp.Time.Format(time.RFC3339),
		Labels:            item.Labels,
		Annotations:       item.Annotations,
	}
}

func mapReplicaSet(item appsv1.ReplicaSet) domainresource.ReplicaSetView {
	desired := int32(0)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	return domainresource.ReplicaSetView{
		Name:              item.Name,
		Namespace:         item.Namespace,
		DesiredReplicas:   desired,
		ReadyReplicas:     item.Status.ReadyReplicas,
		AvailableReplicas: item.Status.AvailableReplicas,
		AgeSeconds:        secondsSince(item.CreationTimestamp.Time),
	}
}

func mapServiceAccountDetail(item corev1.ServiceAccount) domainresource.ServiceAccountDetailView {
	secrets := make([]string, 0, len(item.Secrets))
	for _, secret := range item.Secrets {
		if strings.TrimSpace(secret.Name) != "" {
			secrets = append(secrets, secret.Name)
		}
	}
	imagePullSecrets := make([]string, 0, len(item.ImagePullSecrets))
	for _, secret := range item.ImagePullSecrets {
		if strings.TrimSpace(secret.Name) != "" {
			imagePullSecrets = append(imagePullSecrets, secret.Name)
		}
	}
	sort.Strings(secrets)
	sort.Strings(imagePullSecrets)
	return domainresource.ServiceAccountDetailView{
		Name:             item.Name,
		Namespace:        item.Namespace,
		Labels:           item.Labels,
		Annotations:      item.Annotations,
		Secrets:          secrets,
		ImagePullSecrets: imagePullSecrets,
		AutomountSAToken: item.AutomountServiceAccountToken != nil && *item.AutomountServiceAccountToken,
		CreatedAt:        item.CreationTimestamp.Time.Format(time.RFC3339),
		AgeSeconds:       secondsSince(item.CreationTimestamp.Time),
	}
}

func summarizeRBACPolicyRules(rules []rbacv1.PolicyRule) []string {
	summaries := make([]string, 0, len(rules))
	for _, rule := range rules {
		verbs := append([]string(nil), rule.Verbs...)
		sort.Strings(verbs)
		left := strings.Join(verbs, ", ")
		switch {
		case len(rule.NonResourceURLs) > 0:
			urls := append([]string(nil), rule.NonResourceURLs...)
			sort.Strings(urls)
			summaries = append(summaries, fmt.Sprintf("%s -> %s", left, strings.Join(urls, ", ")))
		default:
			resources := append([]string(nil), rule.Resources...)
			sort.Strings(resources)
			right := strings.Join(resources, ", ")
			if len(rule.APIGroups) > 0 {
				groups := append([]string(nil), rule.APIGroups...)
				sort.Strings(groups)
				groupSummary := strings.Join(groups, ", ")
				if strings.TrimSpace(groupSummary) != "" {
					right = fmt.Sprintf("%s (%s)", right, groupSummary)
				}
			}
			if len(rule.ResourceNames) > 0 {
				names := append([]string(nil), rule.ResourceNames...)
				sort.Strings(names)
				right = fmt.Sprintf("%s [%s]", right, strings.Join(names, ", "))
			}
			summaries = append(summaries, fmt.Sprintf("%s -> %s", left, right))
		}
	}
	return summaries
}

func mapRoleDetail(item rbacv1.Role) domainresource.RoleDetailView {
	return domainresource.RoleDetailView{
		Name:          item.Name,
		Namespace:     item.Namespace,
		Labels:        item.Labels,
		Annotations:   item.Annotations,
		Rules:         len(item.Rules),
		RuleSummaries: summarizeRBACPolicyRules(item.Rules),
		CreatedAt:     item.CreationTimestamp.Time.Format(time.RFC3339),
		AgeSeconds:    secondsSince(item.CreationTimestamp.Time),
	}
}

func mapRoleBinding(item rbacv1.RoleBinding) domainresource.RoleBindingView {
	return domainresource.RoleBindingView{
		Name:       item.Name,
		Namespace:  item.Namespace,
		RoleRef:    fmt.Sprintf("%s/%s", item.RoleRef.Kind, item.RoleRef.Name),
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapRoleBindingDetail(item rbacv1.RoleBinding) domainresource.RoleBindingDetailView {
	subjects := make([]string, 0, len(item.Subjects))
	for _, subject := range item.Subjects {
		if strings.TrimSpace(subject.Namespace) != "" {
			subjects = append(subjects, fmt.Sprintf("%s:%s/%s", subject.Kind, subject.Namespace, subject.Name))
			continue
		}
		subjects = append(subjects, fmt.Sprintf("%s:%s", subject.Kind, subject.Name))
	}
	sort.Strings(subjects)
	return domainresource.RoleBindingDetailView{
		Name:        item.Name,
		Namespace:   item.Namespace,
		Labels:      item.Labels,
		Annotations: item.Annotations,
		RoleRef:     fmt.Sprintf("%s/%s", item.RoleRef.Kind, item.RoleRef.Name),
		Subjects:    subjects,
		CreatedAt:   item.CreationTimestamp.Time.Format(time.RFC3339),
		AgeSeconds:  secondsSince(item.CreationTimestamp.Time),
	}
}

func mapHorizontalPodAutoscaler(item autoscalingv2.HorizontalPodAutoscaler) domainresource.HorizontalPodAutoscalerView {
	minReplicas := int32(1)
	if item.Spec.MinReplicas != nil {
		minReplicas = *item.Spec.MinReplicas
	}
	return domainresource.HorizontalPodAutoscalerView{
		Name:            item.Name,
		Namespace:       item.Namespace,
		TargetRef:       fmt.Sprintf("%s/%s", item.Spec.ScaleTargetRef.Kind, item.Spec.ScaleTargetRef.Name),
		MinReplicas:     minReplicas,
		MaxReplicas:     item.Spec.MaxReplicas,
		CurrentReplicas: item.Status.CurrentReplicas,
		DesiredReplicas: item.Status.DesiredReplicas,
		AgeSeconds:      secondsSince(item.CreationTimestamp.Time),
	}
}

func mapHorizontalPodAutoscalerDetail(item autoscalingv2.HorizontalPodAutoscaler) domainresource.HorizontalPodAutoscalerDetailView {
	metrics := make([]domainresource.HorizontalPodAutoscalerMetricView, 0, len(item.Spec.Metrics))
	for index, metric := range item.Spec.Metrics {
		current := ""
		if index < len(item.Status.CurrentMetrics) {
			current = horizontalPodAutoscalerMetricCurrent(item.Status.CurrentMetrics[index])
		}
		metrics = append(metrics, domainresource.HorizontalPodAutoscalerMetricView{
			Type:    string(metric.Type),
			Name:    horizontalPodAutoscalerMetricName(metric),
			Target:  horizontalPodAutoscalerMetricTarget(metric),
			Current: current,
		})
	}
	conditions := make([]domainresource.WorkloadConditionView, 0, len(item.Status.Conditions))
	for _, condition := range item.Status.Conditions {
		conditions = append(conditions, domainresource.WorkloadConditionView{
			Type:               string(condition.Type),
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastTransitionTime: condition.LastTransitionTime.Time.Format(time.RFC3339),
		})
	}
	return domainresource.HorizontalPodAutoscalerDetailView{
		HorizontalPodAutoscalerView: mapHorizontalPodAutoscaler(item),
		Labels:                      item.Labels,
		Annotations:                 item.Annotations,
		CreatedAt:                   item.CreationTimestamp.Time.Format(time.RFC3339),
		Metrics:                     metrics,
		Conditions:                  conditions,
	}
}

func horizontalPodAutoscalerMetricName(metric autoscalingv2.MetricSpec) string {
	switch metric.Type {
	case autoscalingv2.ResourceMetricSourceType:
		return string(metric.Resource.Name)
	case autoscalingv2.ContainerResourceMetricSourceType:
		return metric.ContainerResource.Container + "/" + string(metric.ContainerResource.Name)
	case autoscalingv2.PodsMetricSourceType:
		return metric.Pods.Metric.Name
	case autoscalingv2.ObjectMetricSourceType:
		return metric.Object.Metric.Name
	case autoscalingv2.ExternalMetricSourceType:
		return metric.External.Metric.Name
	default:
		return ""
	}
}

func horizontalPodAutoscalerMetricTarget(metric autoscalingv2.MetricSpec) string {
	switch metric.Type {
	case autoscalingv2.ResourceMetricSourceType:
		return formatMetricTarget(metric.Resource.Target)
	case autoscalingv2.ContainerResourceMetricSourceType:
		return formatMetricTarget(metric.ContainerResource.Target)
	case autoscalingv2.PodsMetricSourceType:
		return formatMetricTarget(metric.Pods.Target)
	case autoscalingv2.ObjectMetricSourceType:
		return formatMetricTarget(metric.Object.Target)
	case autoscalingv2.ExternalMetricSourceType:
		return formatMetricTarget(metric.External.Target)
	default:
		return ""
	}
}

func formatMetricTarget(target autoscalingv2.MetricTarget) string {
	if target.AverageUtilization != nil {
		return strconv.FormatInt(int64(*target.AverageUtilization), 10) + "%"
	}
	if target.AverageValue != nil {
		return target.AverageValue.String()
	}
	if target.Value != nil {
		return target.Value.String()
	}
	return ""
}

func horizontalPodAutoscalerMetricCurrent(metric autoscalingv2.MetricStatus) string {
	switch metric.Type {
	case autoscalingv2.ResourceMetricSourceType:
		return formatMetricValue(metric.Resource.Current)
	case autoscalingv2.ContainerResourceMetricSourceType:
		return formatMetricValue(metric.ContainerResource.Current)
	case autoscalingv2.PodsMetricSourceType:
		return formatMetricValue(metric.Pods.Current)
	case autoscalingv2.ObjectMetricSourceType:
		return formatMetricValue(metric.Object.Current)
	case autoscalingv2.ExternalMetricSourceType:
		return formatMetricValue(metric.External.Current)
	default:
		return ""
	}
}

func formatMetricValue(value autoscalingv2.MetricValueStatus) string {
	if value.AverageUtilization != nil {
		return strconv.FormatInt(int64(*value.AverageUtilization), 10) + "%"
	}
	if value.AverageValue != nil {
		return value.AverageValue.String()
	}
	if value.Value != nil {
		return value.Value.String()
	}
	return ""
}

func mapPodDisruptionBudget(item policyv1.PodDisruptionBudget) domainresource.PodDisruptionBudgetView {
	minAvailable := ""
	if item.Spec.MinAvailable != nil {
		minAvailable = item.Spec.MinAvailable.String()
	}
	maxUnavailable := ""
	if item.Spec.MaxUnavailable != nil {
		maxUnavailable = item.Spec.MaxUnavailable.String()
	}
	return domainresource.PodDisruptionBudgetView{
		Name:               item.Name,
		Namespace:          item.Namespace,
		MinAvailable:       minAvailable,
		MaxUnavailable:     maxUnavailable,
		CurrentHealthy:     item.Status.CurrentHealthy,
		DesiredHealthy:     item.Status.DesiredHealthy,
		DisruptionsAllowed: item.Status.DisruptionsAllowed,
		AgeSeconds:         secondsSince(item.CreationTimestamp.Time),
	}
}

func mapPodDisruptionBudgetDetail(
	item policyv1.PodDisruptionBudget,
	selector string,
	pods []corev1.Pod,
	workload *domainresource.PodRelatedResourceView,
) domainresource.PodDisruptionBudgetDetailView {
	podViews := make([]domainresource.PodView, 0, len(pods))
	for _, pod := range pods {
		podViews = append(podViews, mapPod(pod))
	}
	sort.SliceStable(podViews, func(i, j int) bool { return podViews[i].Name < podViews[j].Name })
	conditions := make([]domainresource.WorkloadConditionView, 0, len(item.Status.Conditions))
	for _, condition := range item.Status.Conditions {
		conditions = append(conditions, domainresource.WorkloadConditionView{
			Type:               string(condition.Type),
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastTransitionTime: condition.LastTransitionTime.Time.Format(time.RFC3339),
		})
	}
	return domainresource.PodDisruptionBudgetDetailView{
		PodDisruptionBudgetView: mapPodDisruptionBudget(item),
		Labels:                  item.Labels,
		Annotations:             item.Annotations,
		CreatedAt:               item.CreationTimestamp.Time.Format(time.RFC3339),
		Selector:                selector,
		Pods:                    podViews,
		Workload:                workload,
		Conditions:              conditions,
	}
}

func (c *Client) commonPodWorkload(
	ctx context.Context,
	namespace string,
	pods []corev1.Pod,
) (*domainresource.PodRelatedResourceView, error) {
	if len(pods) == 0 {
		return nil, nil
	}
	resolvedOwners := map[string]metav1.OwnerReference{}
	common, ok, err := c.resolvePodWorkloadOwner(ctx, namespace, pods[0], resolvedOwners)
	if err != nil || !ok {
		return nil, err
	}
	for _, pod := range pods[1:] {
		owner, ownerOK, ownerErr := c.resolvePodWorkloadOwner(ctx, namespace, pod, resolvedOwners)
		if ownerErr != nil {
			return nil, ownerErr
		}
		if !ownerOK || !sameOwnerReference(common, owner) {
			return nil, nil
		}
	}
	return &domainresource.PodRelatedResourceView{
		Kind:      common.Kind,
		Name:      common.Name,
		Namespace: namespace,
		Relations: []string{"owner"},
	}, nil
}

func (c *Client) resolvePodWorkloadOwner(
	ctx context.Context,
	namespace string,
	pod corev1.Pod,
	resolvedOwners map[string]metav1.OwnerReference,
) (metav1.OwnerReference, bool, error) {
	owner := metav1.GetControllerOf(&pod)
	if owner == nil {
		return metav1.OwnerReference{}, false, nil
	}
	if owner.Kind != "ReplicaSet" {
		return *owner, true, nil
	}
	key := owner.APIVersion + "/" + owner.Kind + "/" + owner.Name + "/" + string(owner.UID)
	if resolved, ok := resolvedOwners[key]; ok {
		return resolved, true, nil
	}
	replicaSet, err := c.typed.AppsV1().ReplicaSets(namespace).Get(ctx, owner.Name, metav1.GetOptions{})
	if err != nil {
		resolvedOwners[key] = *owner
		return *owner, true, nil
	}
	if owner.UID != "" && replicaSet.UID != owner.UID {
		return metav1.OwnerReference{}, false, nil
	}
	if workload := metav1.GetControllerOf(replicaSet); workload != nil {
		resolvedOwners[key] = *workload
		return *workload, true, nil
	}
	resolvedOwners[key] = *owner
	return *owner, true, nil
}

func sameOwnerReference(left, right metav1.OwnerReference) bool {
	if left.APIVersion != right.APIVersion || left.Kind != right.Kind || left.Name != right.Name {
		return false
	}
	return left.UID == right.UID
}

func mapHelmRelease(name, namespace string, labels map[string]string, createdAt time.Time, storageDriver string) domainresource.HelmReleaseView {
	releaseName := strings.TrimSpace(labels["name"])
	if releaseName == "" {
		releaseName = parseHelmReleaseName(name)
	}
	revision := strings.TrimSpace(labels["version"])
	if revision == "" {
		revision = parseHelmRevision(name)
	}
	status := strings.TrimSpace(labels["status"])
	if status == "" {
		status = "unknown"
	}
	chart := strings.TrimSpace(labels["helm.sh/chart"])
	appVersion := strings.TrimSpace(labels["app.kubernetes.io/version"])
	return domainresource.HelmReleaseView{
		Name:           releaseName,
		Namespace:      namespace,
		Revision:       revision,
		Status:         status,
		Chart:          chart,
		AppVersion:     appVersion,
		StorageDriver:  storageDriver,
		AgeSeconds:     secondsSince(createdAt),
		AllowedActions: helmrelease.LegacyAllowedActions(labels),
	}
}

type helmReleaseRecord struct {
	createdAt time.Time
	labels    map[string]string
	release   *helmrelease.Release
	secret    string
}

func (c *Client) listHelmReleaseRecords(ctx context.Context, namespace string) ([]helmReleaseRecord, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.CoreV1().Secrets(namespace).List(queryCtx, metav1.ListOptions{LabelSelector: "owner=helm"})
	if err != nil {
		return nil, err
	}
	records := make([]helmReleaseRecord, 0, len(items.Items))
	for _, item := range items.Items {
		releaseData := strings.TrimSpace(string(item.Data["release"]))
		if releaseData == "" {
			continue
		}
		release, err := helmrelease.Decode(releaseData, item.Labels)
		if err != nil {
			continue
		}
		if strings.TrimSpace(release.Namespace) == "" {
			release.Namespace = item.Namespace
		}
		records = append(records, helmReleaseRecord{
			createdAt: item.CreationTimestamp.Time,
			labels:    cloneStringMap(item.Labels),
			release:   release,
			secret:    item.Name,
		})
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].release.Namespace != records[j].release.Namespace {
			return records[i].release.Namespace < records[j].release.Namespace
		}
		if records[i].release.Name != records[j].release.Name {
			return records[i].release.Name < records[j].release.Name
		}
		return records[i].release.Version > records[j].release.Version
	})
	return records, nil
}

func (c *Client) getHelmReleaseRecord(ctx context.Context, namespace, name, revision string) (helmReleaseRecord, error) {
	records, err := c.listHelmReleaseRecords(ctx, namespace)
	if err != nil {
		return helmReleaseRecord{}, err
	}
	for _, record := range records {
		if record.release == nil || record.release.Name != name {
			continue
		}
		if revision != "" && strconv.Itoa(record.release.Version) != revision {
			continue
		}
		return record, nil
	}
	return helmReleaseRecord{}, fmt.Errorf("helm release %s not found", name)
}

func mapHelmReleaseDetailRecord(record helmReleaseRecord) domainresource.HelmReleaseDetailView {
	release := record.release
	chartName := ""
	chartVersion := ""
	appVersion := ""
	description := ""
	annotations := map[string]string(nil)
	if release.Chart != nil && release.Chart.Metadata != nil {
		chartName = strings.TrimSpace(release.Chart.Metadata.Name)
		chartVersion = strings.TrimSpace(release.Chart.Metadata.Version)
		appVersion = strings.TrimSpace(release.Chart.Metadata.AppVersion)
		description = strings.TrimSpace(release.Chart.Metadata.Description)
		annotations = cloneStringMap(release.Chart.Metadata.Annotations)
	}
	status := strings.TrimSpace(record.labels["status"])
	if status == "" && release.Info != nil {
		status = strings.TrimSpace(release.Info.Status)
	}
	if status == "" {
		status = "unknown"
	}
	item := domainresource.HelmReleaseDetailView{
		Name:              release.Name,
		Namespace:         release.Namespace,
		Revision:          strconv.Itoa(release.Version),
		Status:            status,
		Chart:             strings.TrimSpace(record.labels["helm.sh/chart"]),
		ChartName:         chartName,
		ChartVersion:      chartVersion,
		AppVersion:        appVersion,
		StorageDriver:     "secret",
		Description:       description,
		Labels:            cloneStringMap(record.labels),
		AllowedActions:    helmrelease.LegacyAllowedActions(record.labels),
		Annotations:       annotations,
		AgeSeconds:        secondsSince(record.createdAt),
		ValuesEditable:    false,
		ValuesDiffEnabled: true,
		CreatedAt:         formatHelmTime(record.createdAt),
	}
	if item.Chart == "" && chartName != "" {
		if chartVersion != "" {
			item.Chart = fmt.Sprintf("%s-%s", chartName, chartVersion)
		} else {
			item.Chart = chartName
		}
	}
	if release.Info != nil {
		item.Status = firstNonEmpty(item.Status, strings.TrimSpace(release.Info.Status))
		item.UpdatedAt = formatHelmTime(release.Info.LastDeployed)
		item.FirstDeployedAt = formatHelmTime(release.Info.FirstDeployed)
		item.LastDeployedAt = formatHelmTime(release.Info.LastDeployed)
		item.Description = firstNonEmpty(strings.TrimSpace(release.Info.Description), item.Description)
		item.Notes = release.Info.Notes
	}
	return item
}

func mapHelmReleaseHistoryRecord(record helmReleaseRecord) domainresource.HelmReleaseHistoryView {
	release := record.release
	item := domainresource.HelmReleaseHistoryView{
		Name:           release.Name,
		Namespace:      release.Namespace,
		Revision:       strconv.Itoa(release.Version),
		Status:         strings.TrimSpace(record.labels["status"]),
		Chart:          strings.TrimSpace(record.labels["helm.sh/chart"]),
		CreatedAt:      formatHelmTime(record.createdAt),
		AllowedActions: helmrelease.LegacyAllowedActions(record.labels),
	}
	if release.Chart != nil && release.Chart.Metadata != nil {
		item.ChartVersion = strings.TrimSpace(release.Chart.Metadata.Version)
		item.AppVersion = strings.TrimSpace(release.Chart.Metadata.AppVersion)
		if item.Chart == "" && release.Chart.Metadata.Name != "" {
			if item.ChartVersion != "" {
				item.Chart = fmt.Sprintf("%s-%s", release.Chart.Metadata.Name, item.ChartVersion)
			} else {
				item.Chart = release.Chart.Metadata.Name
			}
		}
	}
	if release.Info != nil {
		item.Status = firstNonEmpty(item.Status, strings.TrimSpace(release.Info.Status))
		item.Description = strings.TrimSpace(release.Info.Description)
		item.UpdatedAt = formatHelmTime(release.Info.LastDeployed)
	}
	valuesContent, err := helmrelease.ValuesYAML(release)
	if err == nil {
		item.ValuesDigest = helmrelease.Digest(valuesContent)
	}
	item.ManifestDigest = helmrelease.Digest(release.Manifest)
	return item
}

func formatHelmTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func parseHelmReleaseName(name string) string {
	trimmed := strings.TrimPrefix(name, "sh.helm.release.v1.")
	if trimmed == name {
		return name
	}
	index := strings.LastIndex(trimmed, ".v")
	if index <= 0 {
		return trimmed
	}
	return trimmed[:index]
}

func parseHelmRevision(name string) string {
	index := strings.LastIndex(name, ".v")
	if index <= 0 {
		return ""
	}
	return name[index+2:]
}

func dedupeHelmReleases(items []domainresource.HelmReleaseView) []domainresource.HelmReleaseView {
	seen := make(map[string]struct{}, len(items))
	result := make([]domainresource.HelmReleaseView, 0, len(items))
	for _, item := range items {
		key := item.Namespace + "/" + item.Name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result
}

func mapService(item corev1.Service) domainresource.ServiceView {
	ports := make([]string, 0, len(item.Spec.Ports))
	portMappings := make([]domainresource.ServicePortView, 0, len(item.Spec.Ports))
	for _, port := range item.Spec.Ports {
		name := port.Name
		if name != "" {
			name = name + ":"
		}
		description := fmt.Sprintf("%s%d/%s", name, port.Port, strings.ToLower(string(port.Protocol)))
		if port.NodePort > 0 {
			description += fmt.Sprintf(" (nodePort:%d)", port.NodePort)
		}
		ports = append(ports, description)
		targetPort := port.TargetPort.String()
		if targetPort == "" || targetPort == "0" {
			targetPort = strconv.Itoa(int(port.Port))
		}
		portMappings = append(portMappings, domainresource.ServicePortView{
			Name: port.Name, Protocol: string(port.Protocol), TargetPort: targetPort,
			Port: port.Port, NodePort: port.NodePort,
		})
	}
	return domainresource.ServiceView{
		Name:         item.Name,
		Namespace:    item.Namespace,
		Type:         string(item.Spec.Type),
		ClusterIP:    item.Spec.ClusterIP,
		Ports:        ports,
		PortMappings: portMappings,
		Selector:     item.Spec.Selector,
		AgeSeconds:   secondsSince(item.CreationTimestamp.Time),
	}
}

func buildServiceDetail(item corev1.Service, slices []discoveryv1.EndpointSlice, backendPods []domainresource.PodView) domainresource.ServiceDetailView {
	summary := mapService(item)
	endpoints := mapEndpointSliceEndpoints(slices)
	return domainresource.ServiceDetailView{
		Name: summary.Name, Namespace: summary.Namespace, Type: summary.Type, ClusterIP: summary.ClusterIP,
		Ports: summary.Ports, PortMappings: summary.PortMappings, Selector: summary.Selector,
		Labels: item.Labels, Annotations: item.Annotations,
		Endpoints: endpoints, BackendPods: backendPods, AgeSeconds: summary.AgeSeconds,
	}
}

func mapEndpointSliceEndpoints(slices []discoveryv1.EndpointSlice) []domainresource.ServiceEndpointView {
	endpoints := make([]domainresource.ServiceEndpointView, 0)
	for _, slice := range slices {
		for _, endpoint := range slice.Endpoints {
			targetRef := ""
			if endpoint.TargetRef != nil {
				targetRef = strings.Trim(strings.Join([]string{endpoint.TargetRef.Kind, endpoint.TargetRef.Name}, "/"), "/")
			}
			for _, address := range endpoint.Addresses {
				view := domainresource.ServiceEndpointView{
					Address: address, Ready: endpoint.Conditions.Ready, Serving: endpoint.Conditions.Serving,
					Terminating: endpoint.Conditions.Terminating, TargetRef: targetRef,
				}
				if endpoint.NodeName != nil {
					view.NodeName = *endpoint.NodeName
				}
				if endpoint.Zone != nil {
					view.Zone = *endpoint.Zone
				}
				endpoints = append(endpoints, view)
			}
		}
	}
	return endpoints
}

func buildIngressDetail(item networkingv1.Ingress, backends []domainresource.IngressBackendView) domainresource.IngressDetailView {
	summary := mapIngress(item)
	tlsHosts := make([]string, 0)
	for _, tls := range item.Spec.TLS {
		for _, host := range tls.Hosts {
			tlsHosts = append(tlsHosts, host)
		}
	}
	routes := make([]domainresource.IngressRouteView, 0)
	addRoute := func(host, path, pathType string, backend networkingv1.IngressBackend) {
		if backend.Service == nil {
			return
		}
		routes = append(routes, domainresource.IngressRouteView{
			Host: host, Path: path, PathType: pathType, TLS: ingressHostUsesTLS(host, tlsHosts),
			ServiceName: backend.Service.Name, ServicePort: ingressServicePort(backend.Service.Port),
		})
	}
	if item.Spec.DefaultBackend != nil {
		addRoute("", "/", "Default", *item.Spec.DefaultBackend)
	}
	for _, rule := range item.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			pathType := ""
			if path.PathType != nil {
				pathType = string(*path.PathType)
			}
			addRoute(rule.Host, path.Path, pathType, path.Backend)
		}
	}
	return domainresource.IngressDetailView{
		Name: summary.Name, Namespace: summary.Namespace, ClassName: summary.ClassName, Address: summary.Address,
		Labels: item.Labels, Annotations: item.Annotations, Routes: routes, Backends: backends, AgeSeconds: summary.AgeSeconds,
	}
}

func ingressHostUsesTLS(host string, tlsHosts []string) bool {
	for _, tlsHost := range tlsHosts {
		if tlsHost == host {
			return true
		}
		if strings.HasPrefix(tlsHost, "*.") {
			suffix := strings.TrimPrefix(tlsHost, "*.")
			prefix := strings.TrimSuffix(host, "."+suffix)
			if prefix != host && prefix != "" && !strings.Contains(prefix, ".") {
				return true
			}
		}
	}
	return false
}

func ingressServicePort(port networkingv1.ServiceBackendPort) string {
	if port.Name != "" {
		return port.Name
	}
	if port.Number != 0 {
		return strconv.FormatInt(int64(port.Number), 10)
	}
	return ""
}

func podOwnerName(owners []metav1.OwnerReference, kind string) string {
	for _, owner := range owners {
		if owner.Kind == kind {
			return owner.Name
		}
	}
	return ""
}

func buildNetworkRelatedPods(ctx context.Context, client kubernetes.Interface, pods []corev1.Pod) []domainresource.NetworkRelatedPodView {
	replicaSetDeployments := make(map[string]string)
	jobCronJobs := make(map[string]string)
	for _, pod := range pods {
		for _, owner := range pod.OwnerReferences {
			switch owner.Kind {
			case "ReplicaSet":
				if _, ok := replicaSetDeployments[owner.Name]; !ok {
					replicaSetDeployments[owner.Name] = ""
					if item, err := client.AppsV1().ReplicaSets(pod.Namespace).Get(ctx, owner.Name, metav1.GetOptions{}); err == nil {
						replicaSetDeployments[owner.Name] = podOwnerName(item.OwnerReferences, "Deployment")
					}
				}
			case "Job":
				if _, ok := jobCronJobs[owner.Name]; !ok {
					jobCronJobs[owner.Name] = ""
					if item, err := client.BatchV1().Jobs(pod.Namespace).Get(ctx, owner.Name, metav1.GetOptions{}); err == nil {
						jobCronJobs[owner.Name] = podOwnerName(item.OwnerReferences, "CronJob")
					}
				}
			}
		}
	}
	views := make([]domainresource.NetworkRelatedPodView, 0, len(pods))
	for _, pod := range pods {
		workloads := make([]domainresource.PodRelatedResourceView, 0)
		for _, owner := range pod.OwnerReferences {
			switch owner.Kind {
			case "ReplicaSet":
				workloads = append(workloads, domainresource.PodRelatedResourceView{Kind: owner.Kind, Name: owner.Name, Namespace: pod.Namespace})
				if deployment := replicaSetDeployments[owner.Name]; deployment != "" {
					workloads = append(workloads, domainresource.PodRelatedResourceView{Kind: "Deployment", Name: deployment, Namespace: pod.Namespace})
				}
			case "StatefulSet", "DaemonSet", "Job", "CronJob":
				workloads = append(workloads, domainresource.PodRelatedResourceView{Kind: owner.Kind, Name: owner.Name, Namespace: pod.Namespace})
				if owner.Kind == "Job" && jobCronJobs[owner.Name] != "" {
					workloads = append(workloads, domainresource.PodRelatedResourceView{Kind: "CronJob", Name: jobCronJobs[owner.Name], Namespace: pod.Namespace})
				}
			}
		}
		views = append(views, domainresource.NetworkRelatedPodView{PodView: mapPod(pod), Workloads: workloads})
	}
	return views
}

func mapEndpointSlice(item discoveryv1.EndpointSlice) domainresource.EndpointSliceView {
	ports := make([]string, 0, len(item.Ports))
	for _, port := range item.Ports {
		if port.Port == nil {
			continue
		}
		name := ""
		if port.Name != nil && strings.TrimSpace(*port.Name) != "" {
			name = *port.Name + ":"
		}
		protocol := ""
		if port.Protocol != nil {
			protocol = strings.ToLower(string(*port.Protocol))
		}
		ports = append(ports, fmt.Sprintf("%s%d/%s", name, *port.Port, protocol))
	}
	return domainresource.EndpointSliceView{
		Name:        item.Name,
		Namespace:   item.Namespace,
		AddressType: string(item.AddressType),
		Endpoints:   len(item.Endpoints),
		Ports:       ports,
		AgeSeconds:  secondsSince(item.CreationTimestamp.Time),
	}
}

func buildEndpointSliceDetail(item discoveryv1.EndpointSlice) domainresource.EndpointSliceDetailView {
	summary := mapEndpointSlice(item)
	return domainresource.EndpointSliceDetailView{
		Name: summary.Name, Namespace: summary.Namespace, AddressType: summary.AddressType,
		ServiceName: item.Labels[discoveryv1.LabelServiceName], Ports: summary.Ports,
		Labels: item.Labels, Annotations: item.Annotations,
		Endpoints: mapEndpointSliceEndpoints([]discoveryv1.EndpointSlice{item}), AgeSeconds: summary.AgeSeconds,
	}
}

func mapIngress(item networkingv1.Ingress) domainresource.IngressView {
	hosts := make([]string, 0, len(item.Spec.Rules))
	for _, rule := range item.Spec.Rules {
		if strings.TrimSpace(rule.Host) != "" {
			hosts = append(hosts, rule.Host)
		}
	}
	addresses := make([]string, 0, len(item.Status.LoadBalancer.Ingress))
	for _, ingress := range item.Status.LoadBalancer.Ingress {
		if ingress.Hostname != "" {
			addresses = append(addresses, ingress.Hostname)
			continue
		}
		if ingress.IP != "" {
			addresses = append(addresses, ingress.IP)
		}
	}
	className := ""
	if item.Spec.IngressClassName != nil {
		className = *item.Spec.IngressClassName
	}
	return domainresource.IngressView{
		Name:            item.Name,
		Namespace:       item.Namespace,
		ClassName:       className,
		Hosts:           hosts,
		Address:         strings.Join(addresses, ", "),
		BackendServices: extractIngressBackendServices(item),
		AgeSeconds:      secondsSince(item.CreationTimestamp.Time),
	}
}

func mapNetworkPolicy(item networkingv1.NetworkPolicy) domainresource.NetworkPolicyView {
	policyTypes := make([]string, 0, len(item.Spec.PolicyTypes))
	for _, policyType := range item.Spec.PolicyTypes {
		policyTypes = append(policyTypes, string(policyType))
	}
	return domainresource.NetworkPolicyView{
		Name:         item.Name,
		Namespace:    item.Namespace,
		PolicyTypes:  policyTypes,
		IngressRules: len(item.Spec.Ingress),
		EgressRules:  len(item.Spec.Egress),
		AgeSeconds:   secondsSince(item.CreationTimestamp.Time),
	}
}

func buildNetworkPolicyDetail(item networkingv1.NetworkPolicy, pods []domainresource.PodView) domainresource.NetworkPolicyDetailView {
	summary := mapNetworkPolicy(item)
	rules := make([]domainresource.NetworkPolicyRuleView, 0, len(item.Spec.Ingress)+len(item.Spec.Egress))
	for _, rule := range item.Spec.Ingress {
		rules = append(rules, domainresource.NetworkPolicyRuleView{Direction: "Ingress", Peers: mapPolicyPeers(rule.From), Ports: mapPolicyPorts(rule.Ports)})
	}
	for _, rule := range item.Spec.Egress {
		rules = append(rules, domainresource.NetworkPolicyRuleView{Direction: "Egress", Peers: mapPolicyPeers(rule.To), Ports: mapPolicyPorts(rule.Ports)})
	}
	selector, _ := metav1.LabelSelectorAsSelector(&item.Spec.PodSelector)
	return domainresource.NetworkPolicyDetailView{
		Name: summary.Name, Namespace: summary.Namespace, PolicyTypes: summary.PolicyTypes,
		PodSelector: selector.String(), Labels: item.Labels, Annotations: item.Annotations,
		Rules: rules, MatchingPods: pods, AgeSeconds: summary.AgeSeconds,
	}
}

func mapPolicyPeers(peers []networkingv1.NetworkPolicyPeer) []domainresource.NetworkPolicyPeerView {
	views := make([]domainresource.NetworkPolicyPeerView, 0, len(peers))
	for _, peer := range peers {
		view := domainresource.NetworkPolicyPeerView{}
		if peer.PodSelector != nil {
			selector, _ := metav1.LabelSelectorAsSelector(peer.PodSelector)
			view.PodSelector = selector.String()
		}
		if peer.NamespaceSelector != nil {
			selector, _ := metav1.LabelSelectorAsSelector(peer.NamespaceSelector)
			view.NamespaceSelector = selector.String()
		}
		if peer.IPBlock != nil {
			view.IPBlock = peer.IPBlock.CIDR
			if len(peer.IPBlock.Except) > 0 {
				view.IPBlock += " except " + strings.Join(peer.IPBlock.Except, ", ")
			}
		}
		views = append(views, view)
	}
	return views
}

func mapPolicyPorts(ports []networkingv1.NetworkPolicyPort) []domainresource.NetworkPolicyPortView {
	views := make([]domainresource.NetworkPolicyPortView, 0, len(ports))
	for _, port := range ports {
		view := domainresource.NetworkPolicyPortView{}
		if port.Protocol != nil {
			view.Protocol = string(*port.Protocol)
		}
		if port.Port != nil {
			view.Port = port.Port.String()
		}
		if port.EndPort != nil {
			view.EndPort = *port.EndPort
		}
		views = append(views, view)
	}
	return views
}

func extractIngressBackendServices(item networkingv1.Ingress) []string {
	services := make([]string, 0, len(item.Spec.Rules)+1)
	seen := make(map[string]struct{}, len(item.Spec.Rules)+1)
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		services = append(services, name)
	}
	if item.Spec.DefaultBackend != nil && item.Spec.DefaultBackend.Service != nil {
		add(item.Spec.DefaultBackend.Service.Name)
	}
	for _, rule := range item.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service != nil {
				add(path.Backend.Service.Name)
			}
		}
	}
	sort.Strings(services)
	return services
}

func mapPersistentVolumeClaim(item corev1.PersistentVolumeClaim) domainresource.PersistentVolumeClaimView {
	requested := ""
	if quantity, ok := item.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
		requested = quantity.String()
	}
	accessModes := make([]string, 0, len(item.Spec.AccessModes))
	for _, mode := range item.Spec.AccessModes {
		accessModes = append(accessModes, string(mode))
	}
	storageClass := ""
	if item.Spec.StorageClassName != nil {
		storageClass = *item.Spec.StorageClassName
	}
	return domainresource.PersistentVolumeClaimView{
		Name:         item.Name,
		Namespace:    item.Namespace,
		Status:       string(item.Status.Phase),
		VolumeName:   item.Spec.VolumeName,
		StorageClass: storageClass,
		AccessModes:  accessModes,
		Requested:    requested,
		AgeSeconds:   secondsSince(item.CreationTimestamp.Time),
	}
}

func mapPersistentVolume(item corev1.PersistentVolume) domainresource.PersistentVolumeView {
	capacity := ""
	if quantity, ok := item.Spec.Capacity[corev1.ResourceStorage]; ok {
		capacity = quantity.String()
	}
	accessModes := make([]string, 0, len(item.Spec.AccessModes))
	for _, mode := range item.Spec.AccessModes {
		accessModes = append(accessModes, string(mode))
	}
	claimRef := ""
	claimNamespace, claimName := "", ""
	if item.Spec.ClaimRef != nil {
		claimRef = fmt.Sprintf("%s/%s", item.Spec.ClaimRef.Namespace, item.Spec.ClaimRef.Name)
		claimNamespace, claimName = item.Spec.ClaimRef.Namespace, item.Spec.ClaimRef.Name
	}
	volumeMode := ""
	if item.Spec.VolumeMode != nil {
		volumeMode = string(*item.Spec.VolumeMode)
	}
	return domainresource.PersistentVolumeView{
		Name:           item.Name,
		Status:         string(item.Status.Phase),
		StorageClass:   item.Spec.StorageClassName,
		ClaimRef:       claimRef,
		ClaimNamespace: claimNamespace,
		ClaimName:      claimName,
		AccessModes:    accessModes,
		Capacity:       capacity,
		ReclaimPolicy:  string(item.Spec.PersistentVolumeReclaimPolicy),
		VolumeMode:     volumeMode,
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
	}
}

func mapIngressClass(item networkingv1.IngressClass) domainresource.IngressClassView {
	isDefault := false
	if v, ok := item.Annotations["ingressclass.kubernetes.io/is-default-class"]; ok && strings.EqualFold(strings.TrimSpace(v), "true") {
		isDefault = true
	}
	parameters := ""
	if item.Spec.Parameters != nil {
		parameters = fmt.Sprintf("%s/%s", item.Spec.Parameters.Kind, item.Spec.Parameters.Name)
	}
	return domainresource.IngressClassView{
		Name:       item.Name,
		Controller: item.Spec.Controller,
		IsDefault:  isDefault,
		Parameters: parameters,
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapPriorityClass(item schedulingv1.PriorityClass) domainresource.PriorityClassView {
	preemptionPolicy := ""
	if item.PreemptionPolicy != nil {
		preemptionPolicy = string(*item.PreemptionPolicy)
	}
	return domainresource.PriorityClassView{
		Name:             item.Name,
		Value:            item.Value,
		GlobalDefault:    item.GlobalDefault,
		PreemptionPolicy: preemptionPolicy,
		Description:      item.Description,
		AgeSeconds:       secondsSince(item.CreationTimestamp.Time),
	}
}

func mapRuntimeClass(item nodev1.RuntimeClass) domainresource.RuntimeClassView {
	return domainresource.RuntimeClassView{
		Name:       item.Name,
		Handler:    item.Handler,
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapClusterRoleDetail(item rbacv1.ClusterRole) domainresource.ClusterRoleDetailView {
	aggregation := 0
	if item.AggregationRule != nil {
		aggregation = len(item.AggregationRule.ClusterRoleSelectors)
	}
	return domainresource.ClusterRoleDetailView{
		Name:             item.Name,
		Labels:           item.Labels,
		Annotations:      item.Annotations,
		Rules:            len(item.Rules),
		AggregationRules: aggregation,
		RuleSummaries:    summarizeRBACPolicyRules(item.Rules),
		CreatedAt:        item.CreationTimestamp.Time.Format(time.RFC3339),
		AgeSeconds:       secondsSince(item.CreationTimestamp.Time),
	}
}

func mapClusterRoleBinding(item rbacv1.ClusterRoleBinding) domainresource.ClusterRoleBindingView {
	return domainresource.ClusterRoleBindingView{
		Name:       item.Name,
		RoleRef:    fmt.Sprintf("%s/%s", item.RoleRef.Kind, item.RoleRef.Name),
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapClusterRoleBindingDetail(item rbacv1.ClusterRoleBinding) domainresource.ClusterRoleBindingDetailView {
	subjects := make([]string, 0, len(item.Subjects))
	for _, subject := range item.Subjects {
		if strings.TrimSpace(subject.Namespace) != "" {
			subjects = append(subjects, fmt.Sprintf("%s:%s/%s", subject.Kind, subject.Namespace, subject.Name))
			continue
		}
		subjects = append(subjects, fmt.Sprintf("%s:%s", subject.Kind, subject.Name))
	}
	sort.Strings(subjects)
	return domainresource.ClusterRoleBindingDetailView{
		Name:        item.Name,
		Labels:      item.Labels,
		Annotations: item.Annotations,
		RoleRef:     fmt.Sprintf("%s/%s", item.RoleRef.Kind, item.RoleRef.Name),
		Subjects:    subjects,
		CreatedAt:   item.CreationTimestamp.Time.Format(time.RFC3339),
		AgeSeconds:  secondsSince(item.CreationTimestamp.Time),
	}
}

func mapMutatingWebhookConfigurationDetail(item admissionregistrationv1.MutatingWebhookConfiguration) domainresource.AdmissionWebhookConfigurationDetailView {
	webhooks := make([]domainresource.AdmissionWebhookView, 0, len(item.Webhooks))
	for _, webhook := range item.Webhooks {
		view := mapAdmissionWebhook(webhook.Name, webhook.ClientConfig, webhook.Rules)
		view.FailurePolicy = stringValue(webhook.FailurePolicy)
		view.MatchPolicy = stringValue(webhook.MatchPolicy)
		view.SideEffects = stringValue(webhook.SideEffects)
		view.TimeoutSeconds = int32Value(webhook.TimeoutSeconds)
		view.AdmissionReviewVersions = webhook.AdmissionReviewVersions
		view.NamespaceSelector = formatLabelSelector(webhook.NamespaceSelector)
		view.ObjectSelector = formatLabelSelector(webhook.ObjectSelector)
		webhooks = append(webhooks, view)
	}
	return domainresource.AdmissionWebhookConfigurationDetailView{
		Name:        item.Name,
		Labels:      item.Labels,
		Annotations: item.Annotations,
		CreatedAt:   item.CreationTimestamp.Time.Format(time.RFC3339),
		AgeSeconds:  secondsSince(item.CreationTimestamp.Time),
		Webhooks:    webhooks,
	}
}

func mapValidatingWebhookConfigurationDetail(item admissionregistrationv1.ValidatingWebhookConfiguration) domainresource.AdmissionWebhookConfigurationDetailView {
	webhooks := make([]domainresource.AdmissionWebhookView, 0, len(item.Webhooks))
	for _, webhook := range item.Webhooks {
		view := mapAdmissionWebhook(webhook.Name, webhook.ClientConfig, webhook.Rules)
		view.FailurePolicy = stringValue(webhook.FailurePolicy)
		view.MatchPolicy = stringValue(webhook.MatchPolicy)
		view.SideEffects = stringValue(webhook.SideEffects)
		view.TimeoutSeconds = int32Value(webhook.TimeoutSeconds)
		view.AdmissionReviewVersions = webhook.AdmissionReviewVersions
		view.NamespaceSelector = formatLabelSelector(webhook.NamespaceSelector)
		view.ObjectSelector = formatLabelSelector(webhook.ObjectSelector)
		webhooks = append(webhooks, view)
	}
	return domainresource.AdmissionWebhookConfigurationDetailView{
		Name:        item.Name,
		Labels:      item.Labels,
		Annotations: item.Annotations,
		CreatedAt:   item.CreationTimestamp.Time.Format(time.RFC3339),
		AgeSeconds:  secondsSince(item.CreationTimestamp.Time),
		Webhooks:    webhooks,
	}
}

func mapAdmissionWebhook(
	name string,
	client admissionregistrationv1.WebhookClientConfig,
	rules []admissionregistrationv1.RuleWithOperations,
) domainresource.AdmissionWebhookView {
	view := domainresource.AdmissionWebhookView{
		Name:               name,
		CABundleConfigured: len(client.CABundle) > 0,
		Rules:              mapAdmissionWebhookRules(rules),
	}
	if client.URL != nil {
		view.URL = *client.URL
		view.ClientTarget = *client.URL
	}
	if client.Service != nil {
		view.ServiceName = client.Service.Name
		view.ServiceNamespace = client.Service.Namespace
		view.ClientTarget = client.Service.Namespace + "/" + client.Service.Name
		if client.Service.Port != nil {
			view.ServicePort = *client.Service.Port
			view.ClientTarget += ":" + strconv.FormatInt(int64(*client.Service.Port), 10)
		}
		if client.Service.Path != nil {
			view.ServicePath = *client.Service.Path
			view.ClientTarget += *client.Service.Path
		}
	}
	return view
}

func mapAdmissionWebhookRules(rules []admissionregistrationv1.RuleWithOperations) []domainresource.AdmissionWebhookRuleView {
	views := make([]domainresource.AdmissionWebhookRuleView, 0, len(rules))
	for _, rule := range rules {
		operations := make([]string, 0, len(rule.Operations))
		for _, operation := range rule.Operations {
			operations = append(operations, string(operation))
		}
		views = append(views, domainresource.AdmissionWebhookRuleView{
			Operations:  operations,
			APIGroups:   rule.APIGroups,
			APIVersions: rule.APIVersions,
			Resources:   rule.Resources,
			Scope:       stringValue(rule.Scope),
		})
	}
	return views
}

func formatLabelSelector(selector *metav1.LabelSelector) string {
	if selector == nil {
		return ""
	}
	parsed, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return ""
	}
	return parsed.String()
}

func stringValue[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func int32Value(value *int32) int32 {
	if value == nil {
		return 0
	}
	return *value
}

func mapResourceQuota(item corev1.ResourceQuota) domainresource.ResourceQuotaView {
	scopes := make([]string, 0, len(item.Spec.Scopes))
	for _, scope := range item.Spec.Scopes {
		scopes = append(scopes, string(scope))
	}
	hard := make(map[string]string, len(item.Status.Hard))
	for k, v := range item.Status.Hard {
		hard[string(k)] = v.String()
	}
	used := make(map[string]string, len(item.Status.Used))
	for k, v := range item.Status.Used {
		used[string(k)] = v.String()
	}
	return domainresource.ResourceQuotaView{
		Name:       item.Name,
		Namespace:  item.Namespace,
		Scopes:     scopes,
		Hard:       hard,
		Used:       used,
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapResourceQuotaDetail(item corev1.ResourceQuota) domainresource.ResourceQuotaDetailView {
	return domainresource.ResourceQuotaDetailView{
		ResourceQuotaView: mapResourceQuota(item),
		Labels:            item.Labels,
		Annotations:       item.Annotations,
		CreatedAt:         item.CreationTimestamp.Time.Format(time.RFC3339),
	}
}

func mapLimitRange(item corev1.LimitRange) domainresource.LimitRangeView {
	return domainresource.LimitRangeView{
		Name:       item.Name,
		Namespace:  item.Namespace,
		Limits:     len(item.Spec.Limits),
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapLimitRangeDetail(item corev1.LimitRange) domainresource.LimitRangeDetailView {
	rules := make([]domainresource.LimitRangeRuleView, 0, len(item.Spec.Limits))
	for _, rule := range item.Spec.Limits {
		rules = append(rules, domainresource.LimitRangeRuleView{
			Type:                 string(rule.Type),
			Min:                  resourceListStrings(rule.Min),
			Max:                  resourceListStrings(rule.Max),
			Default:              resourceListStrings(rule.Default),
			DefaultRequest:       resourceListStrings(rule.DefaultRequest),
			MaxLimitRequestRatio: resourceListStrings(rule.MaxLimitRequestRatio),
		})
	}
	return domainresource.LimitRangeDetailView{
		LimitRangeView: mapLimitRange(item),
		Labels:         item.Labels,
		Annotations:    item.Annotations,
		CreatedAt:      item.CreationTimestamp.Time.Format(time.RFC3339),
		Rules:          rules,
	}
}

func resourceListStrings(resources corev1.ResourceList) map[string]string {
	values := make(map[string]string, len(resources))
	for name, quantity := range resources {
		values[string(name)] = quantity.String()
	}
	return values
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
	acquire := ""
	if item.Spec.AcquireTime != nil {
		acquire = item.Spec.AcquireTime.UTC().Format(time.RFC3339)
	}
	renew := ""
	if item.Spec.RenewTime != nil {
		renew = item.Spec.RenewTime.UTC().Format(time.RFC3339)
	}
	return domainresource.LeaseView{
		Name:                 item.Name,
		Namespace:            item.Namespace,
		HolderIdentity:       holder,
		LeaseDurationSeconds: duration,
		AcquireTime:          acquire,
		RenewTime:            renew,
		AgeSeconds:           secondsSince(item.CreationTimestamp.Time),
	}
}

func mapReplicationController(item corev1.ReplicationController) domainresource.ReplicationControllerView {
	desired := int32(0)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	return domainresource.ReplicationControllerView{
		Name:              item.Name,
		Namespace:         item.Namespace,
		DesiredReplicas:   desired,
		CurrentReplicas:   item.Status.Replicas,
		ReadyReplicas:     item.Status.ReadyReplicas,
		AvailableReplicas: item.Status.AvailableReplicas,
		AgeSeconds:        secondsSince(item.CreationTimestamp.Time),
	}
}

func mapClusterEvent(item corev1.Event) domainresource.ClusterEventView {
	last := item.LastTimestamp.Time
	if last.IsZero() {
		last = item.EventTime.Time
	}
	if last.IsZero() {
		last = item.CreationTimestamp.Time
	}
	return domainresource.ClusterEventView{
		Name:          item.Name,
		Namespace:     item.Namespace,
		Type:          item.Type,
		Reason:        item.Reason,
		InvolvedKind:  item.InvolvedObject.Kind,
		InvolvedName:  item.InvolvedObject.Name,
		Message:       item.Message,
		Count:         item.Count,
		LastTimestamp: last.UTC().Format(time.RFC3339),
		AgeSeconds:    secondsSince(item.CreationTimestamp.Time),
	}
}

func secondsSince(timestamp time.Time) int64 {
	return int64(time.Since(timestamp).Seconds())
}

func containerState(state corev1.ContainerState) string {
	switch {
	case state.Running != nil:
		return "running"
	case state.Waiting != nil:
		if state.Waiting.Reason != "" {
			return "waiting:" + state.Waiting.Reason
		}
		return "waiting"
	case state.Terminated != nil:
		if state.Terminated.Reason != "" {
			return "terminated:" + state.Terminated.Reason
		}
		return "terminated"
	default:
		return ""
	}
}

func ownedByDeployment(owners []metav1.OwnerReference, uid types.UID) bool {
	for _, owner := range owners {
		if owner.UID == uid && owner.Kind == "Deployment" {
			return true
		}
	}
	return false
}
