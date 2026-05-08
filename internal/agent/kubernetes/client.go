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
	helmrelease "github.com/kubecrux/kubecrux/internal/platform/helmrelease"
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
	return c.buildPodDetail(queryCtx, *item), nil
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
		Name:        record.release.Name,
		Namespace:   record.release.Namespace,
		Revision:    strconv.Itoa(record.release.Version),
		Content:     content,
		Original:    content,
		Editable:    false,
		DiffEnabled: true,
	}, nil
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
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.RbacV1().ClusterRoles().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ClusterRoleView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapClusterRole(item))
	}
	return views, nil
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
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.RbacV1().ClusterRoleBindings().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ClusterRoleBindingView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapClusterRoleBinding(item))
	}
	return views, nil
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
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.AdmissionregistrationV1().MutatingWebhookConfigurations().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.MutatingWebhookConfigurationView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapMutatingWebhookConfiguration(item))
	}
	return views, nil
}

func (c *Client) ListValidatingWebhookConfigurations(ctx context.Context) ([]domainresource.ValidatingWebhookConfigurationView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	items, err := c.typed.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ValidatingWebhookConfigurationView, 0, len(items.Items))
	for _, item := range items.Items {
		views = append(views, mapValidatingWebhookConfiguration(item))
	}
	return views, nil
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
	containers := make([]domainresource.WorkloadContainerView, 0, len(item.Spec.Containers))
	statusMap := make(map[string]corev1.ContainerStatus, len(item.Status.ContainerStatuses))
	for _, status := range item.Status.ContainerStatuses {
		statusMap[status.Name] = status
	}
	for _, container := range item.Spec.Containers {
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
		containers = append(containers, domainresource.WorkloadContainerView{
			Name:         container.Name,
			Image:        container.Image,
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

func mapRole(item rbacv1.Role) domainresource.RoleView {
	return domainresource.RoleView{
		Name:       item.Name,
		Namespace:  item.Namespace,
		Rules:      len(item.Rules),
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
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
		Name:      release.Name,
		Namespace: release.Namespace,
		Revision:  strconv.Itoa(release.Version),
		Status:    strings.TrimSpace(record.labels["status"]),
		Chart:     strings.TrimSpace(record.labels["helm.sh/chart"]),
		CreatedAt: formatHelmTime(record.createdAt),
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

func mapClusterRole(item rbacv1.ClusterRole) domainresource.ClusterRoleView {
	aggregation := 0
	if item.AggregationRule != nil {
		aggregation = len(item.AggregationRule.ClusterRoleSelectors)
	}
	return domainresource.ClusterRoleView{
		Name:             item.Name,
		Rules:            len(item.Rules),
		AggregationRules: aggregation,
		AgeSeconds:       secondsSince(item.CreationTimestamp.Time),
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
	subjects := make([]string, 0, len(item.Subjects))
	for _, subject := range item.Subjects {
		if strings.TrimSpace(subject.Namespace) != "" {
			subjects = append(subjects, fmt.Sprintf("%s:%s/%s", subject.Kind, subject.Namespace, subject.Name))
			continue
		}
		subjects = append(subjects, fmt.Sprintf("%s:%s", subject.Kind, subject.Name))
	}
	return domainresource.ClusterRoleBindingView{
		Name:       item.Name,
		RoleRef:    fmt.Sprintf("%s/%s", item.RoleRef.Kind, item.RoleRef.Name),
		Subjects:   subjects,
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

func mapMutatingWebhookConfiguration(item admissionregistrationv1.MutatingWebhookConfiguration) domainresource.MutatingWebhookConfigurationView {
	return domainresource.MutatingWebhookConfigurationView{
		Name:       item.Name,
		Webhooks:   len(item.Webhooks),
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapValidatingWebhookConfiguration(item admissionregistrationv1.ValidatingWebhookConfiguration) domainresource.ValidatingWebhookConfigurationView {
	return domainresource.ValidatingWebhookConfigurationView{
		Name:       item.Name,
		Webhooks:   len(item.Webhooks),
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
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

func mapLimitRange(item corev1.LimitRange) domainresource.LimitRangeView {
	return domainresource.LimitRangeView{
		Name:       item.Name,
		Namespace:  item.Namespace,
		Limits:     len(item.Spec.Limits),
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
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
