package resourcebackend

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/streamlimit"
	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/yaml"
)

func (d *Direct) ListPods(ctx context.Context, clusterID, namespace string) ([]domainresource.PodView, string, error) {
	var (
		pods   []corev1.Pod
		source string
	)
	if d.cache != nil {
		if items, err := d.cache.ListPods(clusterID, namespace); err == nil {
			pods = items
			source = "cache"
		} else if !d.cache.CacheUnavailable(err) {
			return nil, "cache", err
		}
	}
	if source == "" {
		bundle, err := d.directClients(ctx, clusterID)
		if err != nil {
			return nil, "live", err
		}
		queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		items, err := bundle.Typed.CoreV1().Pods(namespace).List(queryCtx, metav1.ListOptions{})
		if err != nil {
			return nil, "live", err
		}
		pods = items.Items
		source = "live"
	}
	views := make([]domainresource.PodView, 0, len(pods))
	for _, pod := range pods {
		views = append(views, mapPodView(pod))
	}
	return views, source, nil
}

func (d *Direct) ListPodsBySelector(ctx context.Context, clusterID, namespace string, selector map[string]string) ([]domainresource.PodView, error) {
	if len(selector) == 0 {
		return nil, nil
	}
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := bundle.Typed.CoreV1().Pods(namespace).List(queryCtx, metav1.ListOptions{LabelSelector: labels.Set(selector).AsSelector().String()})
	if err != nil {
		return nil, err
	}
	return mapResourceItems(items.Items, mapPodView), nil
}

func (d *Direct) GetPodDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.PodDetailView, error) {
	pod, err := d.getPod(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.PodDetailView{}, err
	}
	return d.buildPodDetail(ctx, clusterID, *pod), nil
}

func (d *Direct) DeletePod(ctx context.Context, clusterID, namespace, name string) error {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	return bundle.Typed.CoreV1().Pods(namespace).Delete(queryCtx, name, metav1.DeleteOptions{})
}

func (d *Direct) GetPodLogs(ctx context.Context, clusterID, namespace, name, container string, tailLines, sinceSeconds int64, previous bool) (domainresource.PodLogsView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
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
	stream, err := bundle.Typed.CoreV1().Pods(namespace).GetLogs(name, options).Stream(queryCtx)
	if err != nil {
		return domainresource.PodLogsView{}, err
	}
	defer func() { _ = stream.Close() }()
	content, totalBytes, contentTruncated, err := streamlimit.ReadString(stream, domainresource.PodLogsMaxContentBytes)
	if err != nil {
		return domainresource.PodLogsView{}, err
	}
	return domainresource.PodLogsView{
		PodName: name, Namespace: namespace, Container: container, Content: content,
		ContentBytes: totalBytes, MaxBytes: domainresource.PodLogsMaxContentBytes,
		TailLines: tailLines, Previous: previous, Truncated: tailLines > 0 || contentTruncated,
	}, nil
}

func (d *Direct) StreamPodLogs(ctx context.Context, clusterID, namespace, name, container string, tailLines, sinceSeconds int64, stdout io.Writer) error {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return err
	}
	options := &corev1.PodLogOptions{Container: container, Follow: true}
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
	defer func() { _ = stream.Close() }()
	_, err = io.Copy(stdout, stream)
	return err
}

func (d *Direct) ExecPod(ctx context.Context, clusterID, namespace, name, container, command string, timeoutSeconds int64) (domainresource.PodExecView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.PodExecView{}, err
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 10
	}
	queryCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	request := bundle.Typed.CoreV1().RESTClient().Post().Resource("pods").Name(name).Namespace(namespace).SubResource("exec")
	request.VersionedParams(&corev1.PodExecOptions{
		Container: container, Command: []string{"/bin/sh", "-lc", command}, Stdout: true, Stderr: true,
	}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(bundle.RESTConfig, http.MethodPost, request.URL())
	if err != nil {
		return domainresource.PodExecView{}, err
	}
	stdout := streamlimit.NewLimitedBuffer(domainresource.PodExecMaxOutputBytes)
	stderr := streamlimit.NewLimitedBuffer(domainresource.PodExecMaxOutputBytes)
	execErr := executor.StreamWithContext(queryCtx, remotecommand.StreamOptions{Stdout: stdout, Stderr: stderr})
	exitMessage := ""
	if execErr != nil {
		exitMessage = execErr.Error()
	}
	return domainresource.PodExecView{
		PodName: name, Namespace: namespace, Container: container, Command: command,
		Stdout: stdout.String(), Stderr: stderr.String(), StdoutBytes: stdout.TotalBytes(), StderrBytes: stderr.TotalBytes(),
		MaxBytes: domainresource.PodExecMaxOutputBytes, StdoutTruncated: stdout.Truncated(), StderrTruncated: stderr.Truncated(),
		Success: execErr == nil, ExitMessage: exitMessage, ExecutedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (d *Direct) StreamPodTerminal(ctx context.Context, clusterID, namespace, name, container, shell string, stdin io.Reader, stdout, stderr io.Writer, sizeQueue domainresource.TerminalSizeQueue) error {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return err
	}
	request := bundle.Typed.CoreV1().RESTClient().Post().Resource("pods").Name(name).Namespace(namespace).SubResource("exec")
	request.VersionedParams(&corev1.PodExecOptions{
		Container: container, Command: []string{shell}, Stdin: true, Stdout: true, Stderr: true, TTY: true,
	}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(bundle.RESTConfig, http.MethodPost, request.URL())
	if err != nil {
		return err
	}
	var remoteSizeQueue remotecommand.TerminalSizeQueue
	if sizeQueue != nil {
		remoteSizeQueue = terminalSizeQueueAdapter{source: sizeQueue}
	}
	return executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin: stdin, Stdout: stdout, Stderr: stderr, Tty: true, TerminalSizeQueue: remoteSizeQueue,
	})
}

type terminalSizeQueueAdapter struct {
	source domainresource.TerminalSizeQueue
}

func (a terminalSizeQueueAdapter) Next() *remotecommand.TerminalSize {
	size := a.source.Next()
	if size == nil {
		return nil
	}
	return &remotecommand.TerminalSize{Width: size.Width, Height: size.Height}
}

func (d *Direct) GetPodYAML(ctx context.Context, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	pod, err := d.getPod(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	copyPod := pod.DeepCopy()
	copyPod.ManagedFields = nil
	content, err := yaml.Marshal(copyPod)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{Kind: "Pod", Name: name, Namespace: namespace, Content: string(content)}, nil
}

func (d *Direct) getPod(ctx context.Context, clusterID, namespace, name string) (*corev1.Pod, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	return bundle.Typed.CoreV1().Pods(namespace).Get(queryCtx, name, metav1.GetOptions{})
}

func mapPodView(pod corev1.Pod) domainresource.PodView {
	requests, limits := podResourceTotals(pod)
	claims := make([]string, 0)
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil && strings.TrimSpace(volume.PersistentVolumeClaim.ClaimName) != "" {
			claims = append(claims, volume.PersistentVolumeClaim.ClaimName)
		}
	}
	return domainresource.PodView{
		Name: pod.Name, Namespace: pod.Namespace, Phase: string(pod.Status.Phase), NodeName: pod.Spec.NodeName,
		PodIP: pod.Status.PodIP, CreatedAt: pod.CreationTimestamp.Format(time.RFC3339),
		Requests: formatTotals(requests), Limits: formatTotals(limits), Labels: cloneMap(pod.Labels),
		PersistentVolumeClaims: claims, ReadyContainers: readyContainers(pod), Restarts: podRestartCount(pod),
		AgeSeconds: secondsSince(pod.CreationTimestamp.Time),
	}
}

func (d *Direct) buildPodDetail(ctx context.Context, clusterID string, pod corev1.Pod) domainresource.PodDetailView {
	requests, limits := podResourceTotals(pod)
	startTime := ""
	if pod.Status.StartTime != nil {
		startTime = pod.Status.StartTime.Format(time.RFC3339)
	}
	conditions := make([]domainresource.WorkloadConditionView, 0, len(pod.Status.Conditions))
	for _, condition := range pod.Status.Conditions {
		conditions = append(conditions, domainresource.WorkloadConditionView{
			Type: string(condition.Type), Status: string(condition.Status), Reason: condition.Reason,
			Message: condition.Message, LastTransitionTime: condition.LastTransitionTime.Format(time.RFC3339),
		})
	}
	refs := buildPodSourceRefs(pod)
	return domainresource.PodDetailView{
		Name: pod.Name, Namespace: pod.Namespace, Phase: string(pod.Status.Phase), PodIP: pod.Status.PodIP,
		HostIP: pod.Status.HostIP, NodeName: pod.Spec.NodeName, ServiceAccountName: pod.Spec.ServiceAccountName,
		QOSClass: string(pod.Status.QOSClass), CreatedAt: pod.CreationTimestamp.Format(time.RFC3339), StartTime: startTime,
		Requests: formatTotals(requests), Limits: formatTotals(limits), Labels: cloneMap(pod.Labels), Annotations: cloneMap(pod.Annotations),
		Containers: buildPodContainers(pod), Conditions: conditions, Volumes: buildPodVolumes(pod),
		RelatedResources: d.buildPodRelatedResources(ctx, clusterID, pod, refs),
	}
}

func buildPodContainers(pod corev1.Pod) []domainresource.WorkloadContainerView {
	build := func(specs []corev1.Container, statuses []corev1.ContainerStatus, role string) []domainresource.WorkloadContainerView {
		statusMap := make(map[string]corev1.ContainerStatus, len(statuses))
		for _, status := range statuses {
			statusMap[status.Name] = status
		}
		views := make([]domainresource.WorkloadContainerView, 0, len(specs))
		for index, container := range specs {
			status := statusMap[container.Name]
			state, lastState := podContainerState(status.State), podContainerState(status.LastTerminationState)
			startedAt, reason, message := podContainerStatusDetails(status.State)
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
			views = append(views, domainresource.WorkloadContainerView{
				Name: container.Name, Image: container.Image, Role: containerRole, Ready: status.Ready, RestartCount: status.RestartCount,
				State: state, LastState: lastState, ContainerID: strings.TrimSpace(status.ContainerID),
				StartedAt: startedAt, Reason: reason, Message: message,
			})
		}
		return views
	}
	containers := build(pod.Spec.InitContainers, pod.Status.InitContainerStatuses, "init")
	return append(containers, build(pod.Spec.Containers, pod.Status.ContainerStatuses, "")...)
}

func podContainerStatusDetails(state corev1.ContainerState) (string, string, string) {
	if state.Running != nil {
		return formatNonZeroTime(state.Running.StartedAt.Time), "", ""
	}
	if state.Waiting != nil {
		return "", strings.TrimSpace(state.Waiting.Reason), strings.TrimSpace(state.Waiting.Message)
	}
	if state.Terminated != nil {
		return formatNonZeroTime(state.Terminated.StartedAt.Time), strings.TrimSpace(state.Terminated.Reason), strings.TrimSpace(state.Terminated.Message)
	}
	return "", "", ""
}

func formatNonZeroTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func podContainerState(state corev1.ContainerState) string {
	switch {
	case state.Running != nil:
		return "running"
	case state.Waiting != nil && state.Waiting.Reason != "":
		return "waiting:" + state.Waiting.Reason
	case state.Waiting != nil:
		return "waiting"
	case state.Terminated != nil && state.Terminated.Reason != "":
		return "terminated:" + state.Terminated.Reason
	case state.Terminated != nil:
		return "terminated"
	default:
		return ""
	}
}

type podSourceRefs struct {
	configMaps map[string]struct{}
	secrets    map[string]struct{}
	pvcs       map[string]struct{}
}

func buildPodSourceRefs(pod corev1.Pod) podSourceRefs {
	refs := podSourceRefs{
		configMaps: make(map[string]struct{}),
		secrets:    make(map[string]struct{}),
		pvcs:       make(map[string]struct{}),
	}
	for _, volume := range pod.Spec.Volumes {
		if volume.ConfigMap != nil {
			addName(refs.configMaps, volume.ConfigMap.Name)
		}
		if volume.Secret != nil {
			addName(refs.secrets, volume.Secret.SecretName)
		}
		if volume.PersistentVolumeClaim != nil {
			addName(refs.pvcs, volume.PersistentVolumeClaim.ClaimName)
		}
		if volume.Projected != nil {
			for _, source := range volume.Projected.Sources {
				if source.ConfigMap != nil {
					addName(refs.configMaps, source.ConfigMap.Name)
				}
				if source.Secret != nil {
					addName(refs.secrets, source.Secret.Name)
				}
			}
		}
	}
	for _, container := range pod.Spec.Containers {
		collectPodContainerRefs(container, &refs)
	}
	for _, container := range pod.Spec.InitContainers {
		collectPodContainerRefs(container, &refs)
	}
	return refs
}

func collectPodContainerRefs(container corev1.Container, refs *podSourceRefs) {
	for _, env := range container.Env {
		if env.ValueFrom == nil {
			continue
		}
		if env.ValueFrom.ConfigMapKeyRef != nil {
			addName(refs.configMaps, env.ValueFrom.ConfigMapKeyRef.Name)
		}
		if env.ValueFrom.SecretKeyRef != nil {
			addName(refs.secrets, env.ValueFrom.SecretKeyRef.Name)
		}
	}
	for _, envFrom := range container.EnvFrom {
		if envFrom.ConfigMapRef != nil {
			addName(refs.configMaps, envFrom.ConfigMapRef.Name)
		}
		if envFrom.SecretRef != nil {
			addName(refs.secrets, envFrom.SecretRef.Name)
		}
	}
}

func addName(items map[string]struct{}, name string) {
	if name = strings.TrimSpace(name); name != "" {
		items[name] = struct{}{}
	}
}

func buildPodVolumes(pod corev1.Pod) []domainresource.PodVolumeView {
	mountsByVolume := make(map[string][]domainresource.PodVolumeMountView)
	appendMounts := func(containerName string, mounts []corev1.VolumeMount) {
		for _, mount := range mounts {
			if strings.TrimSpace(mount.Name) == "" {
				continue
			}
			mountsByVolume[mount.Name] = append(mountsByVolume[mount.Name], domainresource.PodVolumeMountView{
				Name: containerName, MountPath: mount.MountPath, SubPath: mount.SubPath,
				ReadOnly: mount.ReadOnly, Description: containerName,
			})
		}
	}
	for _, container := range pod.Spec.InitContainers {
		appendMounts(container.Name, container.VolumeMounts)
	}
	for _, container := range pod.Spec.Containers {
		appendMounts(container.Name, container.VolumeMounts)
	}
	volumes := make([]domainresource.PodVolumeView, 0, len(pod.Spec.Volumes))
	for _, volume := range pod.Spec.Volumes {
		volumeType, sourceName, readOnly, details := describePodVolume(volume)
		mounts := append([]domainresource.PodVolumeMountView(nil), mountsByVolume[volume.Name]...)
		for i := range mounts {
			mounts[i].VolumeType = volumeType
			mounts[i].SourceName = sourceName
		}
		sort.SliceStable(mounts, func(i, j int) bool {
			if mounts[i].Name != mounts[j].Name {
				return mounts[i].Name < mounts[j].Name
			}
			return mounts[i].MountPath < mounts[j].MountPath
		})
		volumes = append(volumes, domainresource.PodVolumeView{
			Name: volume.Name, Type: volumeType, SourceName: sourceName, ReadOnly: readOnly,
			Details: details, VolumeMounts: mounts, ReferencedConfigMaps: podVolumeConfigMaps(volume),
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
		return "Projected", projectedSourceNames(volume.Projected.Sources), false, details
	default:
		return describePodStorageVolume(volume)
	}
}

func describePodStorageVolume(volume corev1.Volume) (string, string, bool, []string) {
	switch {
	case volume.EmptyDir != nil:
		details := make([]string, 0, 2)
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
			details = append(details, fmt.Sprintf("HostPathType: %s", *volume.HostPath.Type))
		}
		return "HostPath", volume.HostPath.Path, false, details
	case volume.DownwardAPI != nil:
		return "DownwardAPI", "", false, []string{fmt.Sprintf("Items: %d", len(volume.DownwardAPI.Items))}
	case volume.CSI != nil:
		return "CSI", volume.CSI.Driver, volume.CSI.ReadOnly != nil && *volume.CSI.ReadOnly, nil
	case volume.NFS != nil:
		return "NFS", volume.NFS.Server, volume.NFS.ReadOnly, nil
	case volume.Ephemeral != nil:
		return "Ephemeral", "", false, nil
	default:
		return "Other", "", false, nil
	}
}

func projectedSourceNames(sources []corev1.VolumeProjection) string {
	names := make([]string, 0, len(sources))
	for _, source := range sources {
		switch {
		case source.ConfigMap != nil:
			names = append(names, source.ConfigMap.Name)
		case source.Secret != nil:
			names = append(names, source.Secret.Name)
		case source.ServiceAccountToken != nil:
			names = append(names, "serviceAccountToken")
		case source.DownwardAPI != nil:
			names = append(names, "downwardAPI")
		case source.ClusterTrustBundle != nil && source.ClusterTrustBundle.Name != nil:
			names = append(names, *source.ClusterTrustBundle.Name)
		}
	}
	return strings.Join(uniqueSortedStrings(names), ", ")
}

func podVolumeConfigMaps(volume corev1.Volume) []string {
	names := make([]string, 0, 2)
	if volume.ConfigMap != nil {
		names = append(names, volume.ConfigMap.Name)
	}
	if volume.Projected != nil {
		for _, source := range volume.Projected.Sources {
			if source.ConfigMap != nil {
				names = append(names, source.ConfigMap.Name)
			}
		}
	}
	return uniqueSortedStrings(names)
}

type podRelatedResource struct {
	kind, namespace, name string
	relations, details    map[string]struct{}
}

func (d *Direct) buildPodRelatedResources(ctx context.Context, clusterID string, pod corev1.Pod, refs podSourceRefs) []domainresource.PodRelatedResourceView {
	items := make(map[string]*podRelatedResource)
	add := func(kind, namespace, name, relation string, details ...string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		key := kind + "/" + namespace + "/" + name
		item := items[key]
		if item == nil {
			item = &podRelatedResource{kind: kind, namespace: namespace, name: name, relations: make(map[string]struct{}), details: make(map[string]struct{})}
			items[key] = item
		}
		addName(item.relations, relation)
		for _, detail := range details {
			addName(item.details, detail)
		}
	}
	add("ServiceAccount", pod.Namespace, pod.Spec.ServiceAccountName, "service-account")
	for name := range refs.configMaps {
		add("ConfigMap", pod.Namespace, name, "config")
	}
	for name := range refs.secrets {
		add("Secret", pod.Namespace, name, "secret")
	}
	for name := range refs.pvcs {
		add("PersistentVolumeClaim", pod.Namespace, name, "volume")
	}
	for _, owner := range pod.OwnerReferences {
		switch owner.Kind {
		case "ReplicaSet", "StatefulSet", "DaemonSet", "Job", "CronJob":
			add(owner.Kind, pod.Namespace, owner.Name, "owner")
		}
	}
	d.addPodNetworkRelations(ctx, clusterID, pod, add)
	d.addPodWorkloadRelations(ctx, clusterID, pod, add)
	result := make([]domainresource.PodRelatedResourceView, 0, len(items))
	for _, item := range items {
		result = append(result, domainresource.PodRelatedResourceView{
			Kind: item.kind, Namespace: item.namespace, Name: item.name,
			Relations: sortedSet(item.relations), Details: sortedSet(item.details),
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

func (d *Direct) addPodNetworkRelations(ctx context.Context, clusterID string, pod corev1.Pod, add func(string, string, string, string, ...string)) {
	services, _, err := d.ListServices(ctx, clusterID, pod.Namespace)
	if err != nil {
		return
	}
	serviceNames := make(map[string]struct{})
	for _, service := range services {
		if selectorMatchesLabels(service.Selector, pod.Labels) {
			add("Service", service.Namespace, service.Name, "selected-by-service", fmt.Sprintf("Type: %s", service.Type))
			serviceNames[service.Name] = struct{}{}
		}
	}
	ingresses, _, err := d.ListIngresses(ctx, clusterID, pod.Namespace)
	if err != nil {
		return
	}
	for _, ingress := range ingresses {
		for _, serviceName := range ingress.BackendServices {
			if _, ok := serviceNames[serviceName]; ok {
				add("Ingress", ingress.Namespace, ingress.Name, "routes-service", fmt.Sprintf("Service: %s", serviceName))
			}
		}
	}
}

func (d *Direct) addPodWorkloadRelations(ctx context.Context, clusterID string, pod corev1.Pod, add func(string, string, string, string, ...string)) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if replicaSetName := podOwnerName(pod.OwnerReferences, "ReplicaSet"); replicaSetName != "" {
		replicaSet, getErr := bundle.Typed.AppsV1().ReplicaSets(pod.Namespace).Get(queryCtx, replicaSetName, metav1.GetOptions{})
		if getErr == nil {
			addReplicaSetRelations(*replicaSet, add)
			return
		}
	}
	selector := labels.Set(pod.Labels).AsSelector().String()
	replicaSets, err := bundle.Typed.AppsV1().ReplicaSets(pod.Namespace).List(queryCtx, metav1.ListOptions{LabelSelector: selector})
	if err == nil {
		for _, replicaSet := range replicaSets.Items {
			if selectorMatchesLabels(replicaSet.Spec.Selector.MatchLabels, pod.Labels) {
				addReplicaSetRelations(replicaSet, add)
			}
		}
	}
	deployments, err := bundle.Typed.AppsV1().Deployments(pod.Namespace).List(queryCtx, metav1.ListOptions{LabelSelector: selector})
	if err == nil {
		addMatchingDeployments(deployments.Items, pod.Labels, add)
	}
}

func podOwnerName(owners []metav1.OwnerReference, kind string) string {
	for _, owner := range owners {
		if owner.Kind == kind {
			return owner.Name
		}
	}
	return ""
}

func addReplicaSetRelations(replicaSet appsv1.ReplicaSet, add func(string, string, string, string, ...string)) {
	add("ReplicaSet", replicaSet.Namespace, replicaSet.Name, "selector-match")
	for _, owner := range replicaSet.OwnerReferences {
		if owner.Kind == "Deployment" {
			add("Deployment", replicaSet.Namespace, owner.Name, "managed-by-replicaset", fmt.Sprintf("ReplicaSet: %s", replicaSet.Name))
		}
	}
}

func addMatchingDeployments(deployments []appsv1.Deployment, labels map[string]string, add func(string, string, string, string, ...string)) {
	for _, deployment := range deployments {
		if selectorMatchesLabels(deployment.Spec.Selector.MatchLabels, labels) {
			add("Deployment", deployment.Namespace, deployment.Name, "selector-match")
		}
	}
}

func selectorMatchesLabels(selector, labels map[string]string) bool {
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
		addName(set, item)
	}
	return sortedSet(set)
}

func sortedSet(items map[string]struct{}) []string {
	values := make([]string, 0, len(items))
	for item := range items {
		values = append(values, item)
	}
	sort.Strings(values)
	return values
}

var _ appresource.DirectPods = (*Direct)(nil)
