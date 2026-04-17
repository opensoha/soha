package kubernetes

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/yaml"

	cfgpkg "github.com/kubecrux/kubecrux/internal/agent/config"
	domaincluster "github.com/kubecrux/kubecrux/internal/domain/cluster"
	domainresource "github.com/kubecrux/kubecrux/internal/domain/resource"
	"github.com/kubecrux/kubecrux/internal/platform/streamlimit"
)

type Client struct {
	cfg        cfgpkg.KubernetesConfig
	typed      kubernetes.Interface
	dynamic    dynamic.Interface
	discovery  discovery.DiscoveryInterface
	restConfig *rest.Config
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
	return &Client{cfg: cfg, typed: typedClient, dynamic: dynamicClient, discovery: discoveryClient, restConfig: restConfig}, nil
}

func (c *Client) Summary(_ context.Context) domaincluster.Summary {
	summary := domaincluster.Summary{
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

	capabilities := make([]string, 0, len(groups.Groups))
	for _, group := range groups.Groups {
		if strings.TrimSpace(group.Name) == "" {
			continue
		}
		capabilities = append(capabilities, group.Name)
		if len(capabilities) == 8 {
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
	items, err := c.typed.CoreV1().Namespaces().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.NamespaceView, 0, len(items.Items))
	for _, item := range items.Items {
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
	items, err := c.typed.CoreV1().Nodes().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	pods, err := c.typed.CoreV1().Pods(metav1.NamespaceAll).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return buildNodeViews(items.Items, pods.Items), nil
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
	items, err := c.typed.CoreV1().Pods(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.PodView, 0, len(items.Items))
	for _, item := range items.Items {
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
	return mapPodDetail(*item), nil
}

func (c *Client) GetPodLogs(ctx context.Context, namespace, name, container string, tailLines, sinceSeconds int64, previous bool) (domainresource.PodLogsView, error) {
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
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.AppsV1().Deployments(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.DeploymentView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapDeployment(item))
	}
	return views, nil
}

func (c *Client) GetDeploymentDetail(ctx context.Context, namespace, name string) (domainresource.DeploymentDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.DeploymentDetailView{}, err
	}
	return mapDeploymentDetail(*item), nil
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
	items, err := c.typed.AppsV1().StatefulSets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.StatefulSetView, 0, len(items.Items))
	for _, item := range items.Items {
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
	return mapStatefulSetDetail(*item), nil
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
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.AppsV1().DaemonSets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.DaemonSetView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapDaemonSet(item))
	}
	return views, nil
}

func (c *Client) GetDaemonSetDetail(ctx context.Context, namespace, name string) (domainresource.DaemonSetDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.AppsV1().DaemonSets(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.DaemonSetDetailView{}, err
	}
	return mapDaemonSetDetail(*item), nil
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
	items, err := c.typed.BatchV1().Jobs(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.JobView, 0, len(items.Items))
	for _, item := range items.Items {
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
	return mapJobDetail(*item), nil
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
	items, err := c.typed.BatchV1().CronJobs(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.CronJobView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapCronJob(item))
	}
	return views, nil
}

func (c *Client) ListReplicaSets(ctx context.Context, namespace string) ([]domainresource.ReplicaSetView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.AppsV1().ReplicaSets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ReplicaSetView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapReplicaSet(item))
	}
	return views, nil
}

func (c *Client) ListConfigMaps(ctx context.Context, namespace string) ([]domainresource.ConfigMapView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.CoreV1().ConfigMaps(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ConfigMapView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapConfigMap(item))
	}
	return views, nil
}

func (c *Client) ListSecrets(ctx context.Context, namespace string) ([]domainresource.SecretView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.CoreV1().Secrets(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.SecretView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapSecret(item))
	}
	return views, nil
}

func (c *Client) ListServiceAccounts(ctx context.Context, namespace string) ([]domainresource.ServiceAccountView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.CoreV1().ServiceAccounts(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ServiceAccountView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapServiceAccount(item))
	}
	return views, nil
}

func (c *Client) ListRoles(ctx context.Context, namespace string) ([]domainresource.RoleView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.RbacV1().Roles(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.RoleView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapRole(item))
	}
	return views, nil
}

func (c *Client) ListRoleBindings(ctx context.Context, namespace string) ([]domainresource.RoleBindingView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.RbacV1().RoleBindings(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.RoleBindingView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapRoleBinding(item))
	}
	return views, nil
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

func (c *Client) GetCronJobDetail(ctx context.Context, namespace, name string) (domainresource.CronJobDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.BatchV1().CronJobs(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.CronJobDetailView{}, err
	}
	return mapCronJobDetail(*item), nil
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

func (c *Client) ListCRDs(ctx context.Context) ([]domainresource.CRDView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	gvr := schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}
	items, err := c.dynamic.Resource(gvr).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.CRDView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapCRD(item))
	}
	return views, nil
}

func (c *Client) ListHelmReleases(ctx context.Context, namespace string) ([]domainresource.HelmReleaseView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.CoreV1().Secrets(namespace).List(queryCtx, metav1.ListOptions{LabelSelector: "owner=helm"})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.HelmReleaseView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapHelmRelease(item.Name, item.Namespace, item.Labels, item.CreationTimestamp.Time, "secret"))
	}
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].Namespace != views[j].Namespace {
			return views[i].Namespace < views[j].Namespace
		}
		if views[i].Name != views[j].Name {
			return views[i].Name < views[j].Name
		}
		return views[i].Revision > views[j].Revision
	})
	return dedupeHelmReleases(views), nil
}

func (c *Client) ListServices(ctx context.Context, namespace string) ([]domainresource.ServiceView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.CoreV1().Services(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ServiceView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapService(item))
	}
	return views, nil
}

func (c *Client) ListIngresses(ctx context.Context, namespace string) ([]domainresource.IngressView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.NetworkingV1().Ingresses(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.IngressView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapIngress(item))
	}
	return views, nil
}

func (c *Client) ListEndpointSlices(ctx context.Context, namespace string) ([]domainresource.EndpointSliceView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.DiscoveryV1().EndpointSlices(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.EndpointSliceView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapEndpointSlice(item))
	}
	return views, nil
}

func (c *Client) ListNetworkPolicies(ctx context.Context, namespace string) ([]domainresource.NetworkPolicyView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.NetworkingV1().NetworkPolicies(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.NetworkPolicyView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapNetworkPolicy(item))
	}
	return views, nil
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
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.StorageV1().StorageClasses().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.StorageClassView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapStorageClass(item))
	}
	return views, nil
}

func (c *Client) ListClusterEvents(ctx context.Context, namespace string, limit int) ([]domainresource.ClusterEventView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.CoreV1().Events(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ClusterEventView, 0, len(items.Items))
	for _, item := range items.Items {
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
	deployment.Spec.Replicas = &replicas
	_, err = c.typed.AppsV1().Deployments(namespace).Update(queryCtx, deployment, metav1.UpdateOptions{})
	return err
}

func (c *Client) UpdateDeploymentImage(ctx context.Context, namespace, name, containerName, image string) (string, string, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	deployment, err := c.typed.AppsV1().Deployments(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
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
		Name:               item.Name,
		Namespace:          item.Namespace,
		Phase:              string(item.Status.Phase),
		PodIP:              item.Status.PodIP,
		HostIP:             item.Status.HostIP,
		NodeName:           item.Spec.NodeName,
		ServiceAccountName: item.Spec.ServiceAccountName,
		QOSClass:           string(item.Status.QOSClass),
		StartTime:          startTime,
		Labels:             item.Labels,
		Annotations:        item.Annotations,
		Containers:         containers,
		Conditions:         conditions,
	}
}

func mapDeployment(item appsv1.Deployment) domainresource.DeploymentView {
	desired := int32(1)
	if item.Spec.Replicas != nil {
		desired = *item.Spec.Replicas
	}
	return domainresource.DeploymentView{
		Name:            item.Name,
		Namespace:       item.Namespace,
		Labels:          item.Labels,
		DesiredReplicas: desired,
		ReadyReplicas:   item.Status.ReadyReplicas,
		UpdatedReplicas: item.Status.UpdatedReplicas,
		Available:       item.Status.AvailableReplicas,
		AgeSeconds:      secondsSince(item.CreationTimestamp.Time),
	}
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
		Labels:          item.Labels,
		Annotations:     item.Annotations,
		Selector:        item.Spec.Selector.MatchLabels,
	}
}

func mapDaemonSet(item appsv1.DaemonSet) domainresource.DaemonSetView {
	return domainresource.DaemonSetView{
		Name:            item.Name,
		Namespace:       item.Namespace,
		DesiredNumber:   item.Status.DesiredNumberScheduled,
		CurrentNumber:   item.Status.CurrentNumberScheduled,
		ReadyNumber:     item.Status.NumberReady,
		AvailableNumber: item.Status.NumberAvailable,
		UpdatedNumber:   item.Status.UpdatedNumberScheduled,
		AgeSeconds:      secondsSince(item.CreationTimestamp.Time),
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

func mapConfigMap(item corev1.ConfigMap) domainresource.ConfigMapView {
	return domainresource.ConfigMapView{
		Name:          item.Name,
		Namespace:     item.Namespace,
		DataEntries:   len(item.Data),
		BinaryEntries: len(item.BinaryData),
		Immutable:     item.Immutable != nil && *item.Immutable,
		AgeSeconds:    secondsSince(item.CreationTimestamp.Time),
	}
}

func mapSecret(item corev1.Secret) domainresource.SecretView {
	return domainresource.SecretView{
		Name:        item.Name,
		Namespace:   item.Namespace,
		Type:        string(item.Type),
		DataEntries: len(item.Data),
		Immutable:   item.Immutable != nil && *item.Immutable,
		AgeSeconds:  secondsSince(item.CreationTimestamp.Time),
	}
}

func mapServiceAccount(item corev1.ServiceAccount) domainresource.ServiceAccountView {
	return domainresource.ServiceAccountView{
		Name:             item.Name,
		Namespace:        item.Namespace,
		Secrets:          len(item.Secrets),
		ImagePullSecrets: len(item.ImagePullSecrets),
		AutomountSAToken: item.AutomountServiceAccountToken != nil && *item.AutomountServiceAccountToken,
		AgeSeconds:       secondsSince(item.CreationTimestamp.Time),
	}
}

func mapRole(item rbacv1.Role) domainresource.RoleView {
	return domainresource.RoleView{
		Name:       item.Name,
		Namespace:  item.Namespace,
		Rules:      len(item.Rules),
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapRoleBinding(item rbacv1.RoleBinding) domainresource.RoleBindingView {
	subjects := make([]string, 0, len(item.Subjects))
	for _, subject := range item.Subjects {
		if strings.TrimSpace(subject.Namespace) != "" {
			subjects = append(subjects, fmt.Sprintf("%s:%s/%s", subject.Kind, subject.Namespace, subject.Name))
			continue
		}
		subjects = append(subjects, fmt.Sprintf("%s:%s", subject.Kind, subject.Name))
	}
	return domainresource.RoleBindingView{
		Name:       item.Name,
		Namespace:  item.Namespace,
		RoleRef:    fmt.Sprintf("%s/%s", item.RoleRef.Kind, item.RoleRef.Name),
		Subjects:   subjects,
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
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

func mapCRD(item unstructured.Unstructured) domainresource.CRDView {
	group, _, _ := unstructured.NestedString(item.Object, "spec", "group")
	scope, _, _ := unstructured.NestedString(item.Object, "spec", "scope")
	kind, _, _ := unstructured.NestedString(item.Object, "spec", "names", "kind")
	plural, _, _ := unstructured.NestedString(item.Object, "spec", "names", "plural")
	versionItems, _, _ := unstructured.NestedSlice(item.Object, "spec", "versions")
	versions := make([]string, 0, len(versionItems))
	for _, raw := range versionItems {
		value, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := value["name"].(string)
		if strings.TrimSpace(name) != "" {
			versions = append(versions, name)
		}
	}
	return domainresource.CRDView{
		Name:       item.GetName(),
		Group:      group,
		Scope:      scope,
		Kind:       kind,
		Plural:     plural,
		Versions:   versions,
		AgeSeconds: secondsSince(item.GetCreationTimestamp().Time),
	}
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
		Name:          releaseName,
		Namespace:     namespace,
		Revision:      revision,
		Status:        status,
		Chart:         chart,
		AppVersion:    appVersion,
		StorageDriver: storageDriver,
		AgeSeconds:    secondsSince(createdAt),
	}
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
		key := item.Namespace + "/" + item.Name + "/" + item.Revision
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
	for _, port := range item.Spec.Ports {
		name := port.Name
		if name != "" {
			name = name + ":"
		}
		ports = append(ports, fmt.Sprintf("%s%d/%s", name, port.Port, strings.ToLower(string(port.Protocol))))
	}
	return domainresource.ServiceView{
		Name:       item.Name,
		Namespace:  item.Namespace,
		Type:       string(item.Spec.Type),
		ClusterIP:  item.Spec.ClusterIP,
		Ports:      ports,
		Selector:   item.Spec.Selector,
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
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
	if item.Spec.ClaimRef != nil {
		claimRef = fmt.Sprintf("%s/%s", item.Spec.ClaimRef.Namespace, item.Spec.ClaimRef.Name)
	}
	volumeMode := ""
	if item.Spec.VolumeMode != nil {
		volumeMode = string(*item.Spec.VolumeMode)
	}
	return domainresource.PersistentVolumeView{
		Name:          item.Name,
		Status:        string(item.Status.Phase),
		StorageClass:  item.Spec.StorageClassName,
		ClaimRef:      claimRef,
		AccessModes:   accessModes,
		Capacity:      capacity,
		ReclaimPolicy: string(item.Spec.PersistentVolumeReclaimPolicy),
		VolumeMode:    volumeMode,
		AgeSeconds:    secondsSince(item.CreationTimestamp.Time),
	}
}

func mapStorageClass(item storagev1.StorageClass) domainresource.StorageClassView {
	reclaimPolicy := ""
	if item.ReclaimPolicy != nil {
		reclaimPolicy = string(*item.ReclaimPolicy)
	}
	volumeBindingMode := ""
	if item.VolumeBindingMode != nil {
		volumeBindingMode = string(*item.VolumeBindingMode)
	}
	allowVolumeExpansion := false
	if item.AllowVolumeExpansion != nil {
		allowVolumeExpansion = *item.AllowVolumeExpansion
	}
	return domainresource.StorageClassView{
		Name:                 item.Name,
		Provisioner:          item.Provisioner,
		ReclaimPolicy:        reclaimPolicy,
		VolumeBindingMode:    volumeBindingMode,
		AllowVolumeExpansion: allowVolumeExpansion,
		Parameters:           item.Parameters,
		AgeSeconds:           secondsSince(item.CreationTimestamp.Time),
	}
}

func mapNode(item corev1.Node) domainresource.NodeView {
	roles := make([]string, 0)
	for key := range item.Labels {
		if strings.HasPrefix(key, "node-role.kubernetes.io/") {
			roles = append(roles, strings.TrimPrefix(key, "node-role.kubernetes.io/"))
		}
	}
	sort.Strings(roles)
	internalIP := ""
	for _, address := range item.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			internalIP = address.Address
			break
		}
	}
	status := "unknown"
	for _, condition := range item.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			if condition.Status == corev1.ConditionTrue {
				status = "ready"
			} else {
				status = "not_ready"
			}
			break
		}
	}
	return domainresource.NodeView{
		Name:       item.Name,
		Status:     status,
		Roles:      roles,
		Version:    item.Status.NodeInfo.KubeletVersion,
		InternalIP: internalIP,
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
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
