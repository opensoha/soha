package resourcebackend

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/streamlimit"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

const (
	directLogMaxPods        = 50
	directLogMaxSources     = 50
	directLogMaxConcurrency = 5
	directLogDefaultTail    = 200
	directLogMaxEntries     = 5000
	directLogMaxBytes       = 2 * 1024 * 1024
	directLogSourceMaxBytes = 256 * 1024
	directLogQueryTimeout   = 8 * time.Second
	directLogStreamSources  = 20
	directLogStreamDuration = 30 * time.Minute
	directLogHeartbeat      = 15 * time.Second
)

type directLogRuntime struct {
	clusterID string
	typed     kubernetes.Interface
}

type directLogSource struct {
	pod       corev1.Pod
	container string
	source    domainresource.LogSource
}

type directLogReadResult struct {
	entries   []domainresource.LogEntry
	source    domainresource.LogSource
	err       error
	truncated bool
}

func (d *Direct) QueryPodLogs(ctx context.Context, clusterID string, query domainresource.LogQuery) (domainresource.LogPage, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.LogPage{}, err
	}
	return (&directLogRuntime{clusterID: clusterID, typed: bundle.Typed}).query(ctx, query)
}

func (d *Direct) StreamPodLogEvents(ctx context.Context, clusterID string, query domainresource.LogQuery, emit func(domainresource.LogStreamEvent) error) error {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return err
	}
	return (&directLogRuntime{clusterID: clusterID, typed: bundle.Typed}).stream(ctx, query, emit)
}

func (r *directLogRuntime) query(ctx context.Context, query domainresource.LogQuery) (domainresource.LogPage, error) {
	queryCtx, cancel := context.WithTimeout(ctx, directLogQueryTimeout)
	defer cancel()
	sources, warnings, err := r.resolveSources(queryCtx, query, directLogMaxSources)
	if err != nil {
		return domainresource.LogPage{}, err
	}
	page := domainresource.LogPage{
		Entries:  []domainresource.LogEntry{},
		Warnings: warnings,
		Coverage: &domainresource.LogCoverage{ResolvedSources: len(sources)},
		Partial:  len(warnings) > 0,
	}
	for result := range r.readSources(queryCtx, query, sources) {
		if result.err != nil {
			page.Partial = true
			page.Coverage.FailedSources++
			page.Warnings = append(page.Warnings, directLogWarning("source_unavailable", result.source))
			continue
		}
		page.Coverage.SuccessfulSources++
		page.Truncated = page.Truncated || result.truncated
		page.Entries = append(page.Entries, result.entries...)
	}
	sortDirectLogEntries(page.Entries, query.Direction)
	page.Entries, page.Truncated = boundDirectLogEntries(page.Entries, query.Limit, page.Truncated)
	return page, nil
}

func (r *directLogRuntime) stream(ctx context.Context, query domainresource.LogQuery, emit func(domainresource.LogStreamEvent) error) error {
	if emit == nil {
		return fmt.Errorf("%w: event writer is required", apperrors.ErrInvalidArgument)
	}
	streamCtx, cancel := context.WithTimeout(ctx, directLogStreamDuration)
	defer cancel()
	sources, warnings, err := r.resolveSources(streamCtx, query, directLogStreamSources)
	if err != nil {
		return err
	}
	state := "live"
	if len(warnings) > 0 {
		state = "degraded"
	}
	if err := emit(domainresource.LogStreamEvent{Type: "status", Status: &domainresource.LogStreamStatus{State: state}}); err != nil {
		return err
	}
	for range warnings {
		if err := emit(directLogSourceErrorEvent()); err != nil {
			return err
		}
	}
	if len(sources) == 0 {
		return emit(directLogEndEvent())
	}
	return r.runStreams(streamCtx, query, sources, emit)
}

func (r *directLogRuntime) runStreams(ctx context.Context, query domainresource.LogQuery, sources []directLogSource, emit func(domainresource.LogStreamEvent) error) error {
	events := make(chan domainresource.LogStreamEvent, 256)
	var workers sync.WaitGroup
	for _, source := range sources {
		workers.Add(1)
		go func(source directLogSource) {
			defer workers.Done()
			r.streamSource(ctx, query, source, events)
		}(source)
	}
	go func() {
		workers.Wait()
		close(events)
	}()
	heartbeat := time.NewTicker(directLogHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return emit(directLogEndEvent())
			}
			if err := emit(event); err != nil {
				return err
			}
		case <-heartbeat.C:
			if err := emit(domainresource.LogStreamEvent{Type: "heartbeat"}); err != nil {
				return err
			}
		case <-ctx.Done():
			return emit(directLogEndEvent())
		}
	}
}

func (r *directLogRuntime) resolveSources(ctx context.Context, query domainresource.LogQuery, maxSources int) ([]directLogSource, []domainresource.LogWarning, error) {
	selector, err := validateDirectLogQuery(query)
	if err != nil {
		return nil, nil, err
	}
	selector, err = r.resolveWorkloadSelector(ctx, selector)
	if err != nil {
		return nil, nil, err
	}
	pods, missingPods, err := r.listPods(ctx, selector)
	if err != nil {
		return nil, nil, err
	}
	if len(pods) > directLogMaxPods {
		return nil, nil, fmt.Errorf("%w: selector resolves more than %d pods", apperrors.ErrInvalidArgument, directLogMaxPods)
	}
	warnings := make([]domainresource.LogWarning, 0, len(missingPods))
	for _, name := range missingPods {
		source := domainresource.LogSource{Domain: sohaapi.LogSourceDomainKubernetes, ClusterID: r.clusterID, Namespace: selector.Namespace, PodName: name, WorkloadKind: selector.WorkloadKind, WorkloadName: selector.WorkloadName}
		warnings = append(warnings, directLogWarning("pod_unavailable", source))
	}
	sources := make([]directLogSource, 0, len(pods))
	for _, pod := range pods {
		containers, missing := selectDirectLogContainers(pod, selector)
		for _, name := range missing {
			warnings = append(warnings, directLogWarning("container_not_found", directLogSourceIdentity(r.clusterID, query, pod, name)))
		}
		for _, container := range containers {
			sources = append(sources, directLogSource{pod: pod, container: container, source: directLogSourceIdentity(r.clusterID, query, pod, container)})
			if len(sources) > maxSources {
				return nil, nil, fmt.Errorf("%w: selector resolves more than %d log sources", apperrors.ErrInvalidArgument, maxSources)
			}
		}
	}
	return sources, warnings, nil
}

func validateDirectLogQuery(query domainresource.LogQuery) (domainresource.LogSourceSelector, error) {
	if query.Selector == nil {
		return domainresource.LogSourceSelector{}, fmt.Errorf("%w: selector is required", apperrors.ErrInvalidArgument)
	}
	selector := *query.Selector
	selector.Namespace = strings.TrimSpace(selector.Namespace)
	if selector.Namespace == "" {
		selector.Namespace = "default"
	}
	if len(selector.PodNames) > directLogMaxPods || len(selector.Containers) > directLogMaxSources {
		return selector, fmt.Errorf("%w: selector exceeds runtime log limits", apperrors.ErrInvalidArgument)
	}
	if _, err := labels.Parse(selector.LabelSelector); err != nil {
		return selector, fmt.Errorf("%w: invalid label selector", apperrors.ErrInvalidArgument)
	}
	return selector, nil
}

func (r *directLogRuntime) resolveWorkloadSelector(ctx context.Context, selector domainresource.LogSourceSelector) (domainresource.LogSourceSelector, error) {
	if selector.WorkloadKind == "" {
		return selector, nil
	}
	workloadSelector, err := r.getWorkloadSelector(ctx, selector)
	if err != nil {
		return selector, err
	}
	parsed, err := metav1.LabelSelectorAsSelector(workloadSelector)
	if err != nil {
		return selector, fmt.Errorf("%w: invalid workload selector", apperrors.ErrInvalidArgument)
	}
	if selector.LabelSelector == "" {
		selector.LabelSelector = parsed.String()
		return selector, nil
	}
	combined, err := labels.Parse(parsed.String() + "," + selector.LabelSelector)
	if err != nil {
		return selector, fmt.Errorf("%w: invalid combined workload selector", apperrors.ErrInvalidArgument)
	}
	selector.LabelSelector = combined.String()
	return selector, nil
}

func (r *directLogRuntime) getWorkloadSelector(ctx context.Context, selector domainresource.LogSourceSelector) (*metav1.LabelSelector, error) {
	switch strings.ToLower(selector.WorkloadKind) {
	case "deployment", "deployments":
		item, err := r.typed.AppsV1().Deployments(selector.Namespace).Get(ctx, selector.WorkloadName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return item.Spec.Selector, nil
	case "statefulset", "statefulsets":
		item, err := r.typed.AppsV1().StatefulSets(selector.Namespace).Get(ctx, selector.WorkloadName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return item.Spec.Selector, nil
	case "daemonset", "daemonsets":
		item, err := r.typed.AppsV1().DaemonSets(selector.Namespace).Get(ctx, selector.WorkloadName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return item.Spec.Selector, nil
	case "replicaset", "replicasets":
		item, err := r.typed.AppsV1().ReplicaSets(selector.Namespace).Get(ctx, selector.WorkloadName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return item.Spec.Selector, nil
	case "job", "jobs":
		item, err := r.typed.BatchV1().Jobs(selector.Namespace).Get(ctx, selector.WorkloadName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return item.Spec.Selector, nil
	default:
		return nil, fmt.Errorf("%w: unsupported workload kind", apperrors.ErrInvalidArgument)
	}
}

func (r *directLogRuntime) listPods(ctx context.Context, selector domainresource.LogSourceSelector) ([]corev1.Pod, []string, error) {
	if selector.LabelSelector == "" && len(selector.PodNames) > 0 {
		return r.getNamedPods(ctx, selector)
	}
	items, err := r.typed.CoreV1().Pods(selector.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.LabelSelector})
	if err != nil {
		return nil, nil, err
	}
	if len(selector.PodNames) == 0 {
		return items.Items, nil, nil
	}
	wanted := make(map[string]struct{}, len(selector.PodNames))
	for _, name := range selector.PodNames {
		wanted[strings.TrimSpace(name)] = struct{}{}
	}
	pods := make([]corev1.Pod, 0, len(wanted))
	for _, pod := range items.Items {
		if _, ok := wanted[pod.Name]; ok {
			pods = append(pods, pod)
			delete(wanted, pod.Name)
		}
	}
	missing := make([]string, 0, len(wanted))
	for name := range wanted {
		missing = append(missing, name)
	}
	sort.Strings(missing)
	return pods, missing, nil
}

func (r *directLogRuntime) getNamedPods(ctx context.Context, selector domainresource.LogSourceSelector) ([]corev1.Pod, []string, error) {
	pods := make([]corev1.Pod, 0, len(selector.PodNames))
	missing := make([]string, 0)
	for _, name := range selector.PodNames {
		pod, err := r.typed.CoreV1().Pods(selector.Namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			missing = append(missing, name)
			continue
		}
		pods = append(pods, *pod)
	}
	return pods, missing, nil
}

func selectDirectLogContainers(pod corev1.Pod, selector domainresource.LogSourceSelector) ([]string, []string) {
	available := make(map[string]struct{})
	regular := make([]string, 0, len(pod.Spec.Containers))
	for _, container := range pod.Spec.Containers {
		available[container.Name] = struct{}{}
		regular = append(regular, container.Name)
	}
	for _, container := range pod.Spec.InitContainers {
		available[container.Name] = struct{}{}
	}
	for _, container := range pod.Spec.EphemeralContainers {
		available[container.Name] = struct{}{}
	}
	if selector.AllContainers {
		containers := append([]string(nil), regular...)
		for _, container := range pod.Spec.InitContainers {
			containers = append(containers, container.Name)
		}
		for _, container := range pod.Spec.EphemeralContainers {
			containers = append(containers, container.Name)
		}
		return containers, nil
	}
	if len(selector.Containers) > 0 {
		return filterDirectLogContainers(selector.Containers, available)
	}
	if name := pod.Annotations["kubectl.kubernetes.io/default-container"]; name != "" {
		if _, ok := available[name]; ok {
			return []string{name}, nil
		}
	}
	if len(regular) == 0 {
		return nil, nil
	}
	return regular[:1], nil
}

func filterDirectLogContainers(requested []string, available map[string]struct{}) ([]string, []string) {
	selected := make([]string, 0, len(requested))
	missing := make([]string, 0)
	for _, name := range requested {
		if _, ok := available[name]; ok {
			selected = append(selected, name)
		} else {
			missing = append(missing, name)
		}
	}
	return selected, missing
}

func (r *directLogRuntime) readSources(ctx context.Context, query domainresource.LogQuery, sources []directLogSource) <-chan directLogReadResult {
	jobs := make(chan directLogSource)
	results := make(chan directLogReadResult, len(sources))
	var workers sync.WaitGroup
	workerCount := min(directLogMaxConcurrency, len(sources))
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for source := range jobs {
				entries, truncated, err := r.readSource(ctx, query, source)
				results <- directLogReadResult{entries: entries, source: source.source, err: err, truncated: truncated}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, source := range sources {
			jobs <- source
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()
	return results
}

func (r *directLogRuntime) readSource(ctx context.Context, query domainresource.LogQuery, source directLogSource) ([]domainresource.LogEntry, bool, error) {
	stream, err := r.typed.CoreV1().Pods(source.pod.Namespace).GetLogs(source.pod.Name, directPodLogOptions(query, source.container, false)).Stream(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = stream.Close() }()
	content, _, truncated, err := streamlimit.ReadString(stream, directLogSourceMaxBytes)
	if err != nil {
		return nil, false, err
	}
	return parseDirectLogContent(content, query, source.source), truncated, nil
}

func (r *directLogRuntime) streamSource(ctx context.Context, query domainresource.LogQuery, source directLogSource, events chan<- domainresource.LogStreamEvent) {
	stream, err := r.typed.CoreV1().Pods(source.pod.Namespace).GetLogs(source.pod.Name, directPodLogOptions(query, source.container, true)).Stream(ctx)
	if err != nil {
		sendDirectLogEvent(ctx, events, directLogSourceErrorEvent())
		return
	}
	defer func() { _ = stream.Close() }()
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 64*1024), directLogSourceMaxBytes)
	for scanner.Scan() {
		if scanner.Text() == "" {
			continue
		}
		entry := parseDirectLogLine(scanner.Text(), source.source)
		if directLogEntryMatches(entry, query) && !sendDirectLogEvent(ctx, events, domainresource.LogStreamEvent{Type: "entry", Entry: &entry}) {
			return
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		sendDirectLogEvent(ctx, events, directLogSourceErrorEvent())
	}
}

func directPodLogOptions(query domainresource.LogQuery, container string, follow bool) *corev1.PodLogOptions {
	tail := int64(query.Tail)
	if tail <= 0 && (follow || query.From == nil) {
		tail = directLogDefaultTail
	}
	options := &corev1.PodLogOptions{Container: container, Follow: follow, Timestamps: true}
	if tail > 0 {
		options.TailLines = &tail
	}
	if query.RuntimeOptions != nil {
		options.Previous = query.RuntimeOptions.Previous
		if query.RuntimeOptions.SinceSeconds > 0 {
			since := query.RuntimeOptions.SinceSeconds
			options.SinceSeconds = &since
		}
	}
	if options.SinceSeconds == nil && query.From != nil {
		options.SinceTime = &metav1.Time{Time: *query.From}
	}
	return options
}

func parseDirectLogContent(content string, query domainresource.LogQuery, source domainresource.LogSource) []domainresource.LogEntry {
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return []domainresource.LogEntry{}
	}
	entries := make([]domainresource.LogEntry, 0)
	for _, line := range strings.Split(content, "\n") {
		if line == "" {
			continue
		}
		entry := parseDirectLogLine(line, source)
		if directLogEntryMatches(entry, query) {
			entries = append(entries, entry)
		}
	}
	return entries
}

func parseDirectLogLine(line string, source domainresource.LogSource) domainresource.LogEntry {
	observedAt := time.Now().UTC()
	timestamp, message := observedAt, line
	if rawTimestamp, remainder, ok := strings.Cut(line, " "); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, rawTimestamp); err == nil {
			timestamp, message = parsed.UTC(), remainder
		}
	}
	return domainresource.LogEntry{Timestamp: timestamp, ObservedAt: &observedAt, Message: message, Source: source, SourceMode: sohaapi.LogSourceModeRuntime}
}

func directLogEntryMatches(entry domainresource.LogEntry, query domainresource.LogQuery) bool {
	if query.From != nil && entry.Timestamp.Before(*query.From) {
		return false
	}
	if query.To != nil && entry.Timestamp.After(*query.To) {
		return false
	}
	return query.Text == "" || strings.Contains(entry.Message, query.Text)
}

func sortDirectLogEntries(entries []domainresource.LogEntry, direction sohaapi.LogDirection) {
	sort.SliceStable(entries, func(i, j int) bool {
		left, right := entries[i], entries[j]
		if !left.Timestamp.Equal(right.Timestamp) {
			if direction == sohaapi.LogDirectionForward {
				return left.Timestamp.Before(right.Timestamp)
			}
			return left.Timestamp.After(right.Timestamp)
		}
		leftKey := left.Source.Namespace + "\x00" + left.Source.PodName + "\x00" + left.Source.ContainerName + "\x00" + left.Message
		rightKey := right.Source.Namespace + "\x00" + right.Source.PodName + "\x00" + right.Source.ContainerName + "\x00" + right.Message
		return leftKey < rightKey
	})
}

func boundDirectLogEntries(entries []domainresource.LogEntry, requestedLimit int, truncated bool) ([]domainresource.LogEntry, bool) {
	limit := requestedLimit
	if limit <= 0 || limit > directLogMaxEntries {
		limit = directLogMaxEntries
	}
	if len(entries) > limit {
		entries = entries[:limit]
		truncated = true
	}
	used := 0
	for index, entry := range entries {
		encoded, _ := json.Marshal(entry)
		used += len(encoded) + 1
		if used > directLogMaxBytes-32*1024 {
			return entries[:index], true
		}
	}
	return entries, truncated
}

func directLogSourceIdentity(clusterID string, query domainresource.LogQuery, pod corev1.Pod, container string) domainresource.LogSource {
	source := domainresource.LogSource{Domain: sohaapi.LogSourceDomainKubernetes, ClusterID: clusterID, Namespace: pod.Namespace, PodName: pod.Name, PodUID: string(pod.UID), ContainerName: container}
	if query.Selector != nil {
		source.WorkloadKind = query.Selector.WorkloadKind
		source.WorkloadName = query.Selector.WorkloadName
	}
	return source
}

func directLogWarning(code string, source domainresource.LogSource) domainresource.LogWarning {
	return domainresource.LogWarning{Code: code, Message: "one or more authorized log sources could not be read", Source: &source}
}

func directLogSourceErrorEvent() domainresource.LogStreamEvent {
	return domainresource.LogStreamEvent{Type: "source_error", Status: &domainresource.LogStreamStatus{State: "degraded", Message: "log source unavailable"}}
}

func directLogEndEvent() domainresource.LogStreamEvent {
	return domainresource.LogStreamEvent{Type: "end", Status: &domainresource.LogStreamStatus{State: "ended"}}
}

func sendDirectLogEvent(ctx context.Context, events chan<- domainresource.LogStreamEvent, event domainresource.LogStreamEvent) bool {
	select {
	case events <- event:
		return true
	case <-ctx.Done():
		return false
	}
}
