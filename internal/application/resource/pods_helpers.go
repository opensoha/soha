package resource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/yaml"

	domainaccess "github.com/soha/soha/internal/domain/access"
	domaincluster "github.com/soha/soha/internal/domain/cluster"
	domainresource "github.com/soha/soha/internal/domain/resource"
	informerinfra "github.com/soha/soha/internal/infrastructure/informer"
	k8sinfra "github.com/soha/soha/internal/infrastructure/kubernetes"
	"github.com/soha/soha/internal/platform/apperrors"
	"github.com/soha/soha/internal/platform/streamlimit"
)

func (s *Service) listDirectPods(ctx context.Context, clusterID, namespace string) ([]corev1.Pod, string, error) {
	if items, err := s.cache.ListPods(clusterID, namespace); err == nil {
		return items, "cache", nil
	} else if !errors.Is(err, informerinfra.ErrCacheNotReady) {
		return nil, "cache", err
	}
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, "live", err
	}
	defer cancel()
	items, err := listPodsLive(queryCtx, bundle, namespace)
	if err != nil {
		return nil, "live", err
	}
	return items, "live", nil
}
func (s *Service) getDirectPod(ctx context.Context, clusterID, namespace, name string) (*corev1.Pod, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	item, err := bundle.Typed.CoreV1().Pods(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return item, nil
}
func (s *Service) deleteDirectPod(ctx context.Context, clusterID, namespace, name string) error {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return err
	}
	defer cancel()
	return bundle.Typed.CoreV1().Pods(namespace).Delete(queryCtx, name, metav1.DeleteOptions{})
}
func (s *Service) getDirectPodLogs(ctx context.Context, clusterID, namespace, name, container string, tailLines, sinceSeconds int64, previous bool) (domainresource.PodLogsView, error) {
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 8*time.Second)
	if err != nil {
		return domainresource.PodLogsView{}, err
	}
	defer cancel()
	options := &corev1.PodLogOptions{Container: container, Previous: previous}
	if tailLines > 0 {
		options.TailLines = &tailLines
	}
	if sinceSeconds > 0 {
		options.SinceSeconds = &sinceSeconds
	}
	stream, err := bundle.Typed.CoreV1().Pods(namespace).GetLogs(name, options).Stream(queryCtx)
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
func (s *Service) execDirectPod(ctx context.Context, clusterID, namespace, name, container, command string, timeoutSeconds int64) (domainresource.PodExecView, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return domainresource.PodExecView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 10
	}
	queryCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	request := bundle.Typed.CoreV1().RESTClient().Post().
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

	executor, err := remotecommand.NewSPDYExecutor(bundle.RESTConfig, http.MethodPost, request.URL())
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
func (s *Service) streamDirectPodLogs(ctx context.Context, clusterID, namespace, name, container string, tailLines, sinceSeconds int64, stdout io.Writer) error {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	options := &corev1.PodLogOptions{
		Container: container,
		Follow:    true,
	}
	if tailLines > 0 {
		options.TailLines = &tailLines
	}
	if sinceSeconds > 0 {
		options.SinceSeconds = &sinceSeconds
	}
	stream, err := bundle.Typed.CoreV1().Pods(namespace).GetLogs(name, options).Stream(ctx)
	if err != nil {
		return err
	}
	defer stream.Close()
	_, err = io.Copy(stdout, stream)
	return err
}
func (s *Service) streamDirectPodTerminal(ctx context.Context, clusterID, namespace, name, container, shell string, stdin io.Reader, stdout, stderr io.Writer, sizeQueue remotecommand.TerminalSizeQueue) error {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}

	request := bundle.Typed.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(name).
		Namespace(namespace).
		SubResource("exec")
	request.VersionedParams(&corev1.PodExecOptions{
		Container: container,
		Command:   []string{shell},
		Stdin:     true,
		Stdout:    true,
		Stderr:    true,
		TTY:       true,
	}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(bundle.RESTConfig, http.MethodPost, request.URL())
	if err != nil {
		return err
	}

	return executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             stdin,
		Stdout:            stdout,
		Stderr:            stderr,
		Tty:               true,
		TerminalSizeQueue: sizeQueue,
	})
}
func normalizeTerminalShell(shell string) string {
	switch strings.TrimSpace(shell) {
	case "/bin/bash":
		return "/bin/bash"
	case "/bin/ash":
		return "/bin/ash"
	case "/busybox/sh":
		return "/busybox/sh"
	default:
		return "/bin/sh"
	}
}
func (s *Service) getDirectPodYAML(ctx context.Context, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	item, err := s.getDirectPod(ctx, clusterID, namespace, name)
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
func (s *Service) listPodsAcrossNamespaces(ctx context.Context, clusterID string) ([]corev1.Pod, error) {
	return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]corev1.Pod, error) {
		return listPodsLive(queryCtx, bundle, namespace)
	})
}
func listPodsLive(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]corev1.Pod, error) {
	var list corev1.PodList
	if err := bundle.Typed.CoreV1().RESTClient().Get().
		Namespace(namespace).
		Resource("pods").
		VersionedParams(&metav1.ListOptions{}, scheme.ParameterCodec).
		Do(ctx).
		Into(&list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

// Namespace-scoped cache is reliable, but the current all-namespaces path can
// return incomplete data from the informer branch. Use live queries there.
func shouldUseInformerCache(namespace string) bool {
	return strings.TrimSpace(namespace) != ""
}
func shouldPopulatePodUsageSummaries(namespace string) bool {
	return strings.TrimSpace(namespace) != ""
}
func mapPod(item corev1.Pod, decision domainaccess.Decision) domainresource.PodView {
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
		Requests:               formatResourceTotals(requests),
		Limits:                 formatResourceTotals(limits),
		Labels:                 item.Labels,
		PersistentVolumeClaims: claims,
		ReadyContainers:        fmt.Sprintf("%d/%d", ready, len(item.Status.ContainerStatuses)),
		Restarts:               restarts,
		AgeSeconds:             secondsSince(item.CreationTimestamp.Time),
		AllowedActions:         stringifyActions(decision.AllowedActions),
	}
}
func buildWorkloadOverview(clusterID, namespace, source string, items []domainresource.PodView) domainresource.WorkloadOverviewView {
	view := domainresource.WorkloadOverviewView{
		ClusterID:   clusterID,
		Namespace:   strings.TrimSpace(namespace),
		Source:      source,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
	namespaceSummary := make(map[string]*domainresource.WorkloadOverviewNamespaceView, len(items))
	problematicPods := make([]domainresource.WorkloadOverviewPodView, 0)

	for _, item := range items {
		view.TotalPods++
		phase := normalizedPodPhase(item.Phase)
		switch phase {
		case "Running":
			view.RunningPods++
		case "Pending":
			view.PendingPods++
		case "Succeeded":
			view.SucceededPods++
		case "Failed":
			view.FailedPods++
		default:
			view.UnknownPods++
		}
		if item.Restarts > 0 {
			view.RestartingPods++
		}
		if podNeedsAttention(item) {
			view.AtRiskPods++
			problematicPods = append(problematicPods, domainresource.WorkloadOverviewPodView{
				Name:            item.Name,
				Namespace:       item.Namespace,
				Phase:           phase,
				ReadyContainers: item.ReadyContainers,
				Restarts:        item.Restarts,
				NodeName:        item.NodeName,
				AgeSeconds:      item.AgeSeconds,
			})
		}

		summary, ok := namespaceSummary[item.Namespace]
		if !ok {
			summary = &domainresource.WorkloadOverviewNamespaceView{Namespace: item.Namespace}
			namespaceSummary[item.Namespace] = summary
		}
		summary.TotalPods++
		if phase == "Running" {
			summary.RunningPods++
		}
		if item.Restarts > 0 {
			summary.RestartingPods++
		}
		if podNeedsAttention(item) {
			summary.AtRiskPods++
		}
	}

	view.NamespaceBreakdown = make([]domainresource.WorkloadOverviewNamespaceView, 0, len(namespaceSummary))
	for _, item := range namespaceSummary {
		view.NamespaceBreakdown = append(view.NamespaceBreakdown, *item)
	}
	sort.SliceStable(view.NamespaceBreakdown, func(i, j int) bool {
		if view.NamespaceBreakdown[i].AtRiskPods != view.NamespaceBreakdown[j].AtRiskPods {
			return view.NamespaceBreakdown[i].AtRiskPods > view.NamespaceBreakdown[j].AtRiskPods
		}
		if view.NamespaceBreakdown[i].RestartingPods != view.NamespaceBreakdown[j].RestartingPods {
			return view.NamespaceBreakdown[i].RestartingPods > view.NamespaceBreakdown[j].RestartingPods
		}
		if view.NamespaceBreakdown[i].TotalPods != view.NamespaceBreakdown[j].TotalPods {
			return view.NamespaceBreakdown[i].TotalPods > view.NamespaceBreakdown[j].TotalPods
		}
		return view.NamespaceBreakdown[i].Namespace < view.NamespaceBreakdown[j].Namespace
	})
	if len(view.NamespaceBreakdown) > 6 {
		view.NamespaceBreakdown = view.NamespaceBreakdown[:6]
	}

	sort.SliceStable(problematicPods, func(i, j int) bool {
		if problematicPods[i].Restarts != problematicPods[j].Restarts {
			return problematicPods[i].Restarts > problematicPods[j].Restarts
		}
		if podPhaseSeverity(problematicPods[i].Phase) != podPhaseSeverity(problematicPods[j].Phase) {
			return podPhaseSeverity(problematicPods[i].Phase) > podPhaseSeverity(problematicPods[j].Phase)
		}
		if problematicPods[i].AgeSeconds != problematicPods[j].AgeSeconds {
			return problematicPods[i].AgeSeconds > problematicPods[j].AgeSeconds
		}
		if problematicPods[i].Namespace != problematicPods[j].Namespace {
			return problematicPods[i].Namespace < problematicPods[j].Namespace
		}
		return problematicPods[i].Name < problematicPods[j].Name
	})
	if len(problematicPods) > 8 {
		problematicPods = problematicPods[:8]
	}
	view.ProblematicPods = problematicPods
	return view
}
func normalizedPodPhase(phase string) string {
	trimmed := strings.TrimSpace(phase)
	if trimmed == "" {
		return "Unknown"
	}
	return trimmed
}
func podNeedsAttention(item domainresource.PodView) bool {
	if item.Restarts > 0 {
		return true
	}
	switch normalizedPodPhase(item.Phase) {
	case "Pending", "Failed", "Unknown":
		return true
	default:
		return false
	}
}
func podPhaseSeverity(phase string) int {
	switch normalizedPodPhase(phase) {
	case "Failed":
		return 4
	case "Pending":
		return 3
	case "Unknown":
		return 2
	case "Running":
		return 1
	default:
		return 0
	}
}
func mapPodDetail(item corev1.Pod, decision domainaccess.Decision) domainresource.PodDetailView {
	containers := make([]domainresource.WorkloadContainerView, 0, len(item.Spec.Containers))
	requests, limits := podResourceTotals(item)
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
		CreatedAt:          item.CreationTimestamp.Time.Format(time.RFC3339),
		StartTime:          startTime,
		Requests:           formatResourceTotals(requests),
		Limits:             formatResourceTotals(limits),
		Labels:             item.Labels,
		Annotations:        item.Annotations,
		Containers:         containers,
		Conditions:         conditions,
		AllowedActions:     stringifyActions(decision.AllowedActions),
	}
}
func (s *Service) buildPodDetailView(ctx context.Context, clusterID string, decision domainaccess.Decision, item corev1.Pod) domainresource.PodDetailView {
	view := mapPodDetail(item, decision)
	volumeSourceRefs := buildPodVolumeSourceRefs(item)
	view.Containers = buildDetailedPodContainers(item)
	view.Volumes = buildPodVolumes(item, volumeSourceRefs)
	view.RelatedResources = s.buildPodRelatedResources(ctx, clusterID, item, volumeSourceRefs)
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
		if containerStatus.State.Running != nil && containerStatus.State.Running.StartedAt.Time.UTC().Format(time.RFC3339) != "0001-01-01T00:00:00Z" {
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
			if startedAt == "" && containerStatus.State.Terminated.StartedAt.Time.UTC().Format(time.RFC3339) != "0001-01-01T00:00:00Z" {
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
				if source.ServiceAccountToken != nil {
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
	sort.SliceStable(volumes, func(i, j int) bool {
		return volumes[i].Name < volumes[j].Name
	})
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
func (s *Service) buildPodRelatedResources(ctx context.Context, clusterID string, item corev1.Pod, refs podVolumeSourceRefSet) []domainresource.PodRelatedResourceView {
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

	s.buildDirectPodRelatedResources(ctx, clusterID, item, add)

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
func (s *Service) buildDirectPodRelatedResources(ctx context.Context, clusterID string, item corev1.Pod, add func(kind, namespace, name, relation string, details ...string)) {
	if services, _, err := s.listDirectServices(ctx, clusterID, item.Namespace); err == nil {
		serviceNames := map[string]struct{}{}
		for _, svc := range services {
			if selectorMatchesPodLabels(svc.Spec.Selector, item.Labels) {
				add("Service", svc.Namespace, svc.Name, "selected-by-service", fmt.Sprintf("Type: %s", svc.Spec.Type))
				serviceNames[svc.Name] = struct{}{}
			}
		}
		if ingresses, _, err := s.listDirectIngresses(ctx, clusterID, item.Namespace); err == nil {
			for _, ingress := range ingresses {
				for _, serviceName := range ingressBackendServiceNames(ingress) {
					if _, ok := serviceNames[serviceName]; ok {
						add("Ingress", ingress.Namespace, ingress.Name, "routes-service", fmt.Sprintf("Service: %s", serviceName))
					}
				}
			}
		}
	}
	if replicasets, err := s.listDirectReplicaSets(ctx, clusterID, item.Namespace); err == nil {
		for _, rs := range replicasets {
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
	if deployments, _, err := s.listDirectDeployments(ctx, clusterID, item.Namespace); err == nil {
		for _, deployment := range deployments {
			if selectorMatchesPodLabels(deployment.Spec.Selector.MatchLabels, item.Labels) {
				add("Deployment", deployment.Namespace, deployment.Name, "selector-match")
			}
		}
	}
}
func selectorMatchesPodLabels(selector, labels map[string]string) bool {
	entries := make([]string, 0, len(selector))
	for key, value := range selector {
		entries = append(entries, key+"="+value)
	}
	if len(entries) == 0 {
		return false
	}
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}
func ingressBackendServiceNames(item networkingv1.Ingress) []string {
	names := make([]string, 0)
	if item.Spec.DefaultBackend != nil && item.Spec.DefaultBackend.Service != nil && strings.TrimSpace(item.Spec.DefaultBackend.Service.Name) != "" {
		names = append(names, item.Spec.DefaultBackend.Service.Name)
	}
	for _, rule := range item.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service != nil && strings.TrimSpace(path.Backend.Service.Name) != "" {
				names = append(names, path.Backend.Service.Name)
			}
		}
	}
	return uniqueSortedStrings(names)
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
func (s *Service) listPodViews(ctx context.Context, clusterID, namespace string, connection domaincluster.Connection, decision domainaccess.Decision, includeUsage bool) ([]domainresource.PodView, string, error) {
	var (
		items  []domainresource.PodView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, "", err
		}
		items, err = client.ListPods(ctx, namespace)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, rawSource, err := s.listDirectPods(ctx, clusterID, namespace)
		if err != nil {
			return nil, "", err
		}
		items = make([]domainresource.PodView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapPod(item, decision))
		}
		if includeUsage && shouldPopulatePodUsageSummaries(namespace) {
			metricsCtx, metricsCancel := context.WithTimeout(ctx, 1200*time.Millisecond)
			if metrics := s.listPodUsageSummaries(metricsCtx, clusterID, namespace, items); len(metrics) > 0 {
				for index := range items {
					if usage, ok := metrics[podMetricsKey(items[index].Namespace, items[index].Name)]; ok {
						items[index].CPU = usage.CPU
						items[index].Memory = usage.Memory
					}
				}
			}
			metricsCancel()
		}
		source = rawSource
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.PodView) string { return item.Namespace })
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].Namespace < items[j].Namespace
	})
	return items, source, nil
}
func populateAllowedActionsPods(items []domainresource.PodView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}
