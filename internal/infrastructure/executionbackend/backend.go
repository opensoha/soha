package executionbackend

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	appexecution "github.com/opensoha/soha/internal/application/execution"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
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
	if k8serrors.IsAlreadyExists(err) && request.Name != "" {
		created, err = bundle.Typed.BatchV1().Jobs(namespace).Get(ctx, job.Name, metav1.GetOptions{})
		if err == nil && (created.Annotations["soha.io/execution-task"] != request.TaskID || created.Annotations["soha.io/execution-request"] != job.Annotations["soha.io/execution-request"]) {
			return appexecution.ExecutionJobRef{}, fmt.Errorf("%w: execution Job identity or frozen input differs", apperrors.ErrConflict)
		}
	}
	if err != nil {
		return appexecution.ExecutionJobRef{}, err
	}
	return appexecution.ExecutionJobRef{
		ClusterID: strings.TrimSpace(clusterID),
		Namespace: created.Namespace,
		Name:      created.Name,
		TaskID:    request.TaskID,
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
	if ref.TaskID != "" && job.Annotations["soha.io/execution-task"] != ref.TaskID {
		return inspection, fmt.Errorf("%w: execution Job belongs to another task", apperrors.ErrConflict)
	}
	switch {
	case ref.TaskID != "":
		inspection.State = deliveryJobTerminalState(job)
	case job.Status.Succeeded > 0:
		inspection.State = appexecution.ExecutionJobSucceeded
	case job.Status.Failed > 0:
		inspection.State = appexecution.ExecutionJobFailed
	}
	for _, condition := range job.Status.Conditions {
		if condition.Type == batchv1.JobFailed && condition.Status == corev1.ConditionTrue {
			inspection.State, inspection.FailureReason = appexecution.ExecutionJobFailed, condition.Reason
		}
	}
	if inspection.State == appexecution.ExecutionJobRunning {
		return inspection, nil
	}
	inspection.Logs, _ = executionJobLogs(ctx, bundle, ref.Namespace, ref.Name)
	if inspection.State == appexecution.ExecutionJobSucceeded {
		inspection.ImageDigest, err = executionJobDigest(ctx, bundle, job)
		if err != nil {
			return inspection, err
		}
	}
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
	if k8serrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func buildExecutionJob(request appexecution.ExecutionJobRequest) (batchv1.Job, error) {
	commands := trimmedStrings(request.Commands)
	if len(commands) == 0 {
		return batchv1.Job{}, fmt.Errorf("%w: execution commands are required", apperrors.ErrInvalidArgument)
	}
	runtime := request.Runtime
	workspace := request.Workspace
	checkouts := checkoutValues(workspace)
	jobName := buildExecutionJobName(request.TaskID)
	if request.Name != "" {
		jobName = request.Name
	}
	if len(validation.IsDNS1123Label(jobName)) != 0 {
		return batchv1.Job{}, fmt.Errorf("%w: invalid execution Job name", apperrors.ErrInvalidArgument)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return batchv1.Job{}, err
	}
	requestDigest := sha256.Sum256(encoded)
	identity := map[string]string{"soha.io/execution-task": request.TaskID, "soha.io/execution-request": fmt.Sprintf("%x", requestDigest)}
	taskLabel := strings.TrimPrefix(appexecution.DeliveryJobName(request.TaskID), "soha-exec-")
	shell := firstNonEmpty(stringValue(runtime["shell"]), "/bin/sh")
	script := "set -e\n" + strings.Join(commands, "\n")
	if request.TaskKind == "build" {
		// The same build artifact file is consumed by the Agent runner. The
		// kubelet retains the termination message even when log reads fail.
		script += "\nif [ -f .soha-image-digest ]; then head -c 1024 .soha-image-digest > /dev/termination-log; fi"
	}
	workingDir := "/workspace"
	if commandDir := stringValue(runtime["commandDir"]); commandDir != "" && commandDir != "." {
		var err error
		workingDir, err = checkoutDestination(commandDir)
		if err != nil {
			return batchv1.Job{}, err
		}
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
	checkoutScript, err := buildCheckoutScript(checkouts)
	if err != nil {
		return batchv1.Job{}, err
	}
	if checkoutScript != "" {
		podSpec.InitContainers = []corev1.Container{{
			Name:            "checkout",
			Image:           firstNonEmpty(stringValue(runtime["checkoutImage"]), request.DefaultGitImage),
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"/bin/sh", "-lc", checkoutScript},
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
	ttlPointer := &ttl
	if request.Retain {
		// Batch recovery must observe a finished Job before it can be removed.
		ttlPointer = nil
	}
	backoff := int32(0)
	deadline := int64(request.TimeoutSeconds)
	if deadline <= 0 {
		deadline = 300
	}
	return batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        jobName,
			Namespace:   strings.TrimSpace(request.Namespace),
			Annotations: identity,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "soha",
				"soha.io/execution-task":       taskLabel,
				"soha.io/task-kind":            request.TaskKind,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			ActiveDeadlineSeconds:   &deadline,
			TTLSecondsAfterFinished: ttlPointer,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
					"app.kubernetes.io/managed-by": "soha",
					"soha.io/execution-task":       taskLabel,
				}},
				Spec: podSpec,
			},
		},
	}, nil
}

var jobDigestPattern = regexp.MustCompile(`^(?:[^\s@]+@)?(sha256:[a-fA-F0-9]{64})$`)

func executionJobDigest(ctx context.Context, bundle *k8sinfra.Bundle, job *batchv1.Job) (string, error) {
	pods, err := bundle.Typed.CoreV1().Pods(job.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + job.Name})
	if err != nil {
		return "", err
	}
	for _, pod := range pods.Items {
		if !metav1.IsControlledBy(&pod, job) {
			continue
		}
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == "runner" && status.State.Terminated != nil && status.State.Terminated.ExitCode == 0 {
				match := jobDigestPattern.FindStringSubmatch(strings.TrimSpace(status.State.Terminated.Message))
				if len(match) == 2 {
					return strings.ToLower(match[1]), nil
				}
			}
		}
	}
	return "", nil
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

func buildCheckoutScript(checkouts []map[string]any) (string, error) {
	lines := []string{"set -e"}
	seenDestinations := map[string]struct{}{}
	for _, checkout := range checkouts {
		if !boolValue(checkout["enabled"], true) {
			continue
		}
		repositoryURL := firstNonEmpty(stringValue(checkout["repositoryURL"]), stringValue(checkout["repositoryUrl"]))
		if repositoryURL == "" {
			continue
		}
		destination, err := checkoutDestination(stringValue(checkout["checkoutPath"]))
		if err != nil {
			return "", err
		}
		if _, exists := seenDestinations[destination]; exists {
			return "", fmt.Errorf("%w: duplicate checkout destination %q", apperrors.ErrInvalidArgument, destination)
		}
		seenDestinations[destination] = struct{}{}
		if destination != "/workspace" {
			lines = append(lines, "mkdir -p "+shellQuote(path.Dir(destination)))
		}
		lines = append(lines, "git clone "+shellQuote(repositoryURL)+" "+shellQuote(destination), "cd "+shellQuote(destination))
		refType := firstNonEmpty(stringValue(checkout["refType"]), "branch")
		refName := stringValue(checkout["refName"])
		if refName == "" && refType == "branch" {
			refName = stringValue(checkout["defaultBranch"])
		}
		if refName != "" {
			if refType == "tag" {
				refName = "tags/" + refName
			}
			lines = append(lines, "git checkout "+shellQuote(refName))
		}
		if boolValue(checkout["submodules"], false) {
			lines = append(lines, "git submodule update --init --recursive")
		}
	}
	if len(lines) == 1 {
		return "", nil
	}
	return strings.Join(lines, "\n"), nil
}

func checkoutValues(workspace map[string]any) []map[string]any {
	result := make([]map[string]any, 0)
	switch values := workspace["checkouts"].(type) {
	case []map[string]any:
		result = append(result, values...)
	case []any:
		for _, value := range values {
			if item, ok := value.(map[string]any); ok {
				result = append(result, item)
			}
		}
	}
	if len(result) == 0 {
		if checkout := mapValue(workspace["checkout"]); len(checkout) > 0 {
			result = append(result, checkout)
		}
	}
	return result
}

func checkoutDestination(value string) (string, error) {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	if value == "" || value == "." {
		return "/workspace", nil
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return "", fmt.Errorf("%w: path must stay inside the build workspace", apperrors.ErrInvalidArgument)
		}
	}
	cleaned := path.Clean(value)
	if path.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%w: path must stay inside the build workspace", apperrors.ErrInvalidArgument)
	}
	if cleaned == "." {
		return "/workspace", nil
	}
	return "/workspace/" + cleaned, nil
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

func boolValue(value any, fallback bool) bool {
	if result, ok := value.(bool); ok {
		return result
	}
	return fallback
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

func int64Pointer(value int64) *int64 {
	return &value
}

var _ appexecution.ClusterRuntime = (*Clusters)(nil)
