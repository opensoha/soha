package executionbackend

import (
	"context"
	"fmt"
	"strings"
	"time"

	appexecution "github.com/opensoha/soha/internal/application/execution"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type clusterManager interface {
	ClusterIDs() []string
	Bundle(context.Context, string) (*k8sinfra.Bundle, error)
}

type Clusters struct {
	manager clusterManager
}

func NewClusters(manager clusterManager) *Clusters {
	return &Clusters{manager: manager}
}

func (c *Clusters) ClusterIDs() []string {
	if c == nil || c.manager == nil {
		return nil
	}
	return c.manager.ClusterIDs()
}

func (c *Clusters) CreateExecutionJob(ctx context.Context, clusterID string, request appexecution.ExecutionJobRequest) (appexecution.ExecutionJobRef, error) {
	bundle, err := c.bundle(ctx, clusterID)
	if err != nil {
		return appexecution.ExecutionJobRef{}, err
	}
	namespace := strings.TrimSpace(request.Namespace)
	if namespace == "" {
		return appexecution.ExecutionJobRef{}, fmt.Errorf("%w: execution job namespace is required", apperrors.ErrInvalidArgument)
	}
	if err := ensureNamespaceExists(ctx, bundle, namespace); err != nil {
		return appexecution.ExecutionJobRef{}, err
	}
	job, err := buildExecutionJob(request)
	if err != nil {
		return appexecution.ExecutionJobRef{}, err
	}
	created, err := bundle.Typed.BatchV1().Jobs(namespace).Create(ctx, &job, metav1.CreateOptions{})
	if err != nil {
		return appexecution.ExecutionJobRef{}, err
	}
	return appexecution.ExecutionJobRef{
		ClusterID: strings.TrimSpace(clusterID),
		Namespace: created.Namespace,
		Name:      created.Name,
	}, nil
}

func (c *Clusters) InspectExecutionJob(ctx context.Context, ref appexecution.ExecutionJobRef) (appexecution.ExecutionJobInspection, error) {
	bundle, err := c.bundle(ctx, ref.ClusterID)
	if err != nil {
		return appexecution.ExecutionJobInspection{}, err
	}
	job, err := bundle.Typed.BatchV1().Jobs(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return appexecution.ExecutionJobInspection{}, fmt.Errorf("%w: execution job %s/%s", apperrors.ErrNotFound, ref.Namespace, ref.Name)
		}
		return appexecution.ExecutionJobInspection{}, err
	}
	inspection := appexecution.ExecutionJobInspection{State: appexecution.ExecutionJobRunning}
	switch {
	case job.Status.Succeeded > 0:
		inspection.State = appexecution.ExecutionJobSucceeded
	case job.Status.Failed > 0:
		inspection.State = appexecution.ExecutionJobFailed
	default:
		return inspection, nil
	}
	inspection.Logs, _ = executionJobLogs(ctx, bundle, ref.Namespace, ref.Name)
	return inspection, nil
}

func (c *Clusters) DeleteExecutionJob(ctx context.Context, ref appexecution.ExecutionJobRef) error {
	bundle, err := c.bundle(ctx, ref.ClusterID)
	if err != nil {
		return err
	}
	propagation := metav1.DeletePropagationBackground
	err = bundle.Typed.BatchV1().Jobs(ref.Namespace).Delete(ctx, ref.Name, metav1.DeleteOptions{PropagationPolicy: &propagation})
	if k8serrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (c *Clusters) bundle(ctx context.Context, clusterID string) (*k8sinfra.Bundle, error) {
	if c == nil || c.manager == nil {
		return nil, fmt.Errorf("kubernetes cluster manager is not configured")
	}
	bundle, err := c.manager.Bundle(ctx, strings.TrimSpace(clusterID))
	if err != nil {
		return nil, err
	}
	if bundle == nil || bundle.Typed == nil {
		return nil, fmt.Errorf("kubernetes typed client is not available")
	}
	return bundle, nil
}

func ensureNamespaceExists(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) error {
	if _, err := bundle.Typed.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{}); err == nil {
		return nil
	} else if !k8serrors.IsNotFound(err) {
		return err
	}
	_, err := bundle.Typed.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	}, metav1.CreateOptions{})
	return err
}

func buildExecutionJob(request appexecution.ExecutionJobRequest) (batchv1.Job, error) {
	commands := trimmedStrings(request.Commands)
	if len(commands) == 0 {
		return batchv1.Job{}, fmt.Errorf("%w: execution commands are required", apperrors.ErrInvalidArgument)
	}
	runtime := request.Runtime
	workspace := request.Workspace
	checkout := mapValue(workspace["checkout"])
	jobName := buildExecutionJobName(request.TaskID)
	shell := firstNonEmpty(stringValue(runtime["shell"]), "/bin/sh")
	script := "set -e\n" + strings.Join(commands, "\n")
	workingDir := "/workspace"
	if commandDir := stringValue(runtime["commandDir"]); commandDir != "" && commandDir != "." {
		workingDir = "/workspace/" + trimRelativePath(commandDir)
	}
	container := corev1.Container{
		Name:            "runner",
		Image:           firstNonEmpty(stringValue(runtime["image"]), request.DefaultImage),
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{shell, "-lc", script},
		WorkingDir:      workingDir,
		VolumeMounts: []corev1.VolumeMount{
			{Name: "workspace", MountPath: "/workspace"},
		},
	}
	podSpec := corev1.PodSpec{
		RestartPolicy: corev1.RestartPolicyNever,
		Volumes: []corev1.Volume{
			{Name: "workspace", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		},
		Containers: []corev1.Container{container},
	}
	if repositoryURL := firstNonEmpty(stringValue(checkout["repositoryURL"]), stringValue(checkout["repositoryUrl"])); repositoryURL != "" {
		podSpec.InitContainers = []corev1.Container{{
			Name:            "checkout",
			Image:           firstNonEmpty(stringValue(runtime["checkoutImage"]), request.DefaultGitImage),
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/bin/sh", "-lc", buildCheckoutScript(checkout, repositoryURL)},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "workspace", MountPath: "/workspace"},
			},
		}}
	}
	ttlSeconds := request.TTLSeconds
	if ttlSeconds <= 0 {
		ttlSeconds = 3600
	}
	if ttlSeconds > 2147483647 {
		return batchv1.Job{}, fmt.Errorf("%w: execution job TTL is too large", apperrors.ErrInvalidArgument)
	}
	ttl := int32(ttlSeconds) //nolint:gosec // bounded by the MaxInt32 check above
	backoff := int32(0)
	return batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: strings.TrimSpace(request.Namespace),
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "soha",
				"soha.io/execution-task":       request.TaskID,
				"soha.io/task-kind":            request.TaskKind,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
					"app.kubernetes.io/managed-by": "soha",
					"soha.io/execution-task":       request.TaskID,
				}},
				Spec: podSpec,
			},
		},
	}, nil
}

func executionJobLogs(ctx context.Context, bundle *k8sinfra.Bundle, namespace, jobName string) ([]appexecution.ExecutionJobLog, error) {
	pods, err := bundle.Typed.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + jobName})
	if err != nil {
		return nil, err
	}
	logs := make([]appexecution.ExecutionJobLog, 0)
	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			raw, readErr := bundle.Typed.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
				Container: container.Name,
				TailLines: int64Pointer(200),
			}).DoRaw(ctx)
			if readErr != nil {
				continue
			}
			for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				if line = strings.TrimSpace(line); line != "" {
					logs = append(logs, appexecution.ExecutionJobLog{Message: line, PodName: pod.Name, ContainerName: container.Name})
				}
			}
		}
	}
	return logs, nil
}

func buildExecutionJobName(taskID string) string {
	base := strings.NewReplacer(":", "-", "_", "-", "/", "-").Replace(strings.TrimSpace(taskID))
	if len(base) > 38 {
		base = base[len(base)-38:]
	}
	return fmt.Sprintf("soha-exec-%s-%d", base, time.Now().UTC().Unix()%100000)
}

func buildCheckoutScript(checkout map[string]any, repositoryURL string) string {
	refType := firstNonEmpty(stringValue(checkout["refType"]), "branch")
	refName := stringValue(checkout["refName"])
	if refName == "" && refType == "branch" {
		refName = stringValue(checkout["defaultBranch"])
	}
	lines := []string{"set -e", "git clone " + shellQuote(repositoryURL) + " /workspace", "cd /workspace"}
	if refName == "" {
		return strings.Join(lines, "\n")
	}
	if refType == "tag" {
		lines = append(lines, "git checkout tags/"+shellQuote(refName))
	} else {
		lines = append(lines, "git checkout "+shellQuote(refName))
	}
	return strings.Join(lines, "\n")
}

func trimmedStrings(values []string) []string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			items = append(items, value)
		}
	}
	return items
}

func mapValue(value any) map[string]any {
	result, _ := value.(map[string]any)
	return result
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func trimRelativePath(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "./")
	value = strings.TrimPrefix(value, "/")
	for strings.HasPrefix(value, "../") {
		value = strings.TrimPrefix(value, "../")
	}
	if value == "." {
		return ""
	}
	return value
}

func int64Pointer(value int64) *int64 {
	return &value
}

var _ appexecution.ClusterRuntime = (*Clusters)(nil)
