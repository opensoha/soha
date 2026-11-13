package kubernetes

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/streamlimit"
)

const (
	aggregateLogMaxPods        = 50
	aggregateLogMaxSources     = 50
	aggregateLogMaxConcurrency = 5
	aggregateLogDefaultTail    = 200
	aggregateLogMaxEntries     = 5000
	aggregateLogMaxBytes       = 2 * 1024 * 1024
	aggregateLogSourceMaxBytes = 256 * 1024
	aggregateLogQueryTimeout   = 8 * time.Second
	aggregateLogStreamSources  = 20
	aggregateLogStreamDuration = 30 * time.Minute
	aggregateLogHeartbeat      = 15 * time.Second
)

var ErrInvalidLogQuery = errors.New("invalid log query")

type aggregateLogSource struct {
	pod       corev1.Pod
	container string
	source    domainresource.LogSource
}

type aggregateLogReadResult struct {
	entries   []domainresource.LogEntry
	source    domainresource.LogSource
	err       error
	truncated bool
}

func (c *Client) QueryPodLogs(ctx context.Context, query domainresource.LogQuery) (domainresource.LogPage, error) {
	queryCtx, cancel := context.WithTimeout(ctx, aggregateLogQueryTimeout)
	defer cancel()

	sources, warnings, err := c.resolveAggregateLogSources(queryCtx, query, aggregateLogMaxSources)
	if err != nil {
		return domainresource.LogPage{}, err
	}
	page := domainresource.LogPage{
		Entries:  make([]domainresource.LogEntry, 0),
		Warnings: warnings,
		Coverage: &domainresource.LogCoverage{ResolvedSources: len(sources)},
		Partial:  len(warnings) > 0,
	}
	if len(sources) == 0 {
		return page, nil
	}

	results := c.readAggregateLogSources(queryCtx, query, sources)
	for result := range results {
		if result.err != nil {
			page.Partial = true
			page.Coverage.FailedSources++
			page.Warnings = append(page.Warnings, aggregateLogWarning("source_unavailable", result.source))
			continue
		}
		page.Coverage.SuccessfulSources++
		page.Truncated = page.Truncated || result.truncated
		page.Entries = append(page.Entries, result.entries...)
		sortAggregateLogEntries(page.Entries, query.Direction)
		page.Entries, page.Truncated = boundAggregateLogEntries(page.Entries, query.Limit, page.Truncated)
	}

	sortAggregateLogEntries(page.Entries, query.Direction)
	page.Entries, page.Truncated = boundAggregateLogEntries(page.Entries, query.Limit, page.Truncated)
	return page, nil
}

func (c *Client) StreamPodLogEvents(ctx context.Context, query domainresource.LogQuery, emit func(domainresource.LogStreamEvent) error) error {
	if emit == nil {
		return fmt.Errorf("%w: event writer is required", ErrInvalidLogQuery)
	}
	streamCtx, cancel := context.WithTimeout(ctx, aggregateLogStreamDuration)
	defer cancel()

	sources, warnings, err := c.resolveAggregateLogSources(streamCtx, query, aggregateLogStreamSources)
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
		if err := emit(domainresource.LogStreamEvent{Type: "source_error", Status: &domainresource.LogStreamStatus{State: "degraded", Message: "log source unavailable"}}); err != nil {
			return err
		}
	}
	if len(sources) == 0 {
		return emit(domainresource.LogStreamEvent{Type: "end", Status: &domainresource.LogStreamStatus{State: "ended"}})
	}

	events := make(chan domainresource.LogStreamEvent, 256)
	var workers sync.WaitGroup
	for _, source := range sources {
		workers.Add(1)
		go func() {
			defer workers.Done()
			c.streamAggregateLogSource(streamCtx, query, source, events)
		}()
	}
	go func() {
		workers.Wait()
		close(events)
	}()

	heartbeat := time.NewTicker(aggregateLogHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return emit(domainresource.LogStreamEvent{Type: "end", Status: &domainresource.LogStreamStatus{State: "ended"}})
			}
			if err := emit(event); err != nil {
				return err
			}
		case <-heartbeat.C:
			if err := emit(domainresource.LogStreamEvent{Type: "heartbeat"}); err != nil {
				return err
			}
		case <-streamCtx.Done():
			return emit(domainresource.LogStreamEvent{Type: "end", Status: &domainresource.LogStreamStatus{State: "ended"}})
		}
	}
}

func (c *Client) resolveAggregateLogSources(ctx context.Context, query domainresource.LogQuery, maxSources int) ([]aggregateLogSource, []domainresource.LogWarning, error) {
	selector, err := validateAggregateLogQuery(query)
	if err != nil {
		return nil, nil, err
	}
	selector, err = c.resolveAggregateWorkloadSelector(ctx, selector)
	if err != nil {
		return nil, nil, err
	}
	pods, missingPods, err := c.listAggregateLogPods(ctx, selector)
	if err != nil {
		return nil, nil, err
	}
	if len(pods) > aggregateLogMaxPods {
		return nil, nil, fmt.Errorf("%w: selector resolves more than %d pods", ErrInvalidLogQuery, aggregateLogMaxPods)
	}

	sources := make([]aggregateLogSource, 0, len(pods))
	warnings := make([]domainresource.LogWarning, 0, len(missingPods))
	for _, name := range missingPods {
		source := domainresource.LogSource{Domain: sohaapi.LogSourceDomainKubernetes, ClusterID: c.cfg.ID, Namespace: selector.Namespace, PodName: name, WorkloadKind: selector.WorkloadKind, WorkloadName: selector.WorkloadName}
		warnings = append(warnings, aggregateLogWarning("pod_unavailable", source))
	}
	for _, pod := range pods {
		containers, missing := selectAggregateLogContainers(pod, selector)
		for _, name := range missing {
			source := aggregateLogSourceIdentity(c.cfg.ID, query, pod, name)
			warnings = append(warnings, aggregateLogWarning("container_not_found", source))
		}
		for _, container := range containers {
			sources = append(sources, aggregateLogSource{pod: pod, container: container, source: aggregateLogSourceIdentity(c.cfg.ID, query, pod, container)})
			if len(sources) > maxSources {
				return nil, nil, fmt.Errorf("%w: selector resolves more than %d log sources", ErrInvalidLogQuery, maxSources)
			}
		}
	}
	return sources, warnings, nil
}

func validateAggregateLogQuery(query domainresource.LogQuery) (domainresource.LogSourceSelector, error) {
	if err := validateAggregateLogQueryOptions(query); err != nil {
		return domainresource.LogSourceSelector{}, err
	}
	if query.Selector == nil {
		return domainresource.LogSourceSelector{}, fmt.Errorf("%w: selector is required", ErrInvalidLogQuery)
	}
	return validateAggregateLogSelector(*query.Selector)
}

func validateAggregateLogQueryOptions(query domainresource.LogQuery) error {
	if query.SourceMode != "" && query.SourceMode != sohaapi.LogSourceModeRuntime && query.SourceMode != sohaapi.LogSourceModeAuto {
		return fmt.Errorf("%w: only runtime logs are supported", ErrInvalidLogQuery)
	}
	if query.Cursor != "" || len(query.Severities) > 0 {
		return fmt.Errorf("%w: cursor and severity filters require durable logs", ErrInvalidLogQuery)
	}
	if query.Tail < 0 || query.Tail > aggregateLogMaxEntries || query.Limit < 0 || query.Limit > aggregateLogMaxEntries {
		return fmt.Errorf("%w: tail and limit must not exceed %d", ErrInvalidLogQuery, aggregateLogMaxEntries)
	}
	if len(query.Text) > 2048 {
		return fmt.Errorf("%w: text filter is too long", ErrInvalidLogQuery)
	}
	if query.RuntimeOptions != nil && (query.RuntimeOptions.SinceSeconds < 0 || query.RuntimeOptions.SinceSeconds > 604800) {
		return fmt.Errorf("%w: sinceSeconds is outside the supported range", ErrInvalidLogQuery)
	}
	if query.Direction != "" && query.Direction != sohaapi.LogDirectionBackward && query.Direction != sohaapi.LogDirectionForward {
		return fmt.Errorf("%w: unsupported direction", ErrInvalidLogQuery)
	}
	if query.From != nil && query.To != nil && query.From.After(*query.To) {
		return fmt.Errorf("%w: from must not be after to", ErrInvalidLogQuery)
	}
	return nil
}

func validateAggregateLogSelector(selector domainresource.LogSourceSelector) (domainresource.LogSourceSelector, error) {
	selector.Namespace = strings.TrimSpace(selector.Namespace)
	if selector.Namespace == "" {
		return selector, fmt.Errorf("%w: namespace is required", ErrInvalidLogQuery)
	}
	if len(selector.PodNames) > aggregateLogMaxPods {
		return selector, fmt.Errorf("%w: at most %d pods are allowed", ErrInvalidLogQuery, aggregateLogMaxPods)
	}
	if len(selector.Containers) > aggregateLogMaxSources {
		return selector, fmt.Errorf("%w: at most %d containers are allowed", ErrInvalidLogQuery, aggregateLogMaxSources)
	}
	selector.WorkloadKind = strings.TrimSpace(selector.WorkloadKind)
	selector.WorkloadName = strings.TrimSpace(selector.WorkloadName)
	if (selector.WorkloadKind == "") != (selector.WorkloadName == "") {
		return selector, fmt.Errorf("%w: workload kind and name must be provided together", ErrInvalidLogQuery)
	}
	if err := validateAggregateLogNames(selector.PodNames, "pod"); err != nil {
		return selector, err
	}
	if err := validateAggregateLogNames(selector.Containers, "container"); err != nil {
		return selector, err
	}
	if _, err := labels.Parse(selector.LabelSelector); err != nil {
		return selector, fmt.Errorf("%w: invalid label selector", ErrInvalidLogQuery)
	}
	return selector, nil
}

func validateAggregateLogNames(values []string, kind string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return fmt.Errorf("%w: %s names must not be empty", ErrInvalidLogQuery, kind)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%w: %s names must be unique", ErrInvalidLogQuery, kind)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func (c *Client) resolveAggregateWorkloadSelector(ctx context.Context, selector domainresource.LogSourceSelector) (domainresource.LogSourceSelector, error) {
	if selector.WorkloadKind == "" {
		return selector, nil
	}
	var workloadSelector *metav1.LabelSelector
	switch strings.ToLower(selector.WorkloadKind) {
	case "deployment", "deployments":
		item, err := c.typed.AppsV1().Deployments(selector.Namespace).Get(ctx, selector.WorkloadName, metav1.GetOptions{})
		if err != nil {
			return selector, err
		}
		workloadSelector = item.Spec.Selector
	case "statefulset", "statefulsets":
		item, err := c.typed.AppsV1().StatefulSets(selector.Namespace).Get(ctx, selector.WorkloadName, metav1.GetOptions{})
		if err != nil {
			return selector, err
		}
		workloadSelector = item.Spec.Selector
	case "daemonset", "daemonsets":
		item, err := c.typed.AppsV1().DaemonSets(selector.Namespace).Get(ctx, selector.WorkloadName, metav1.GetOptions{})
		if err != nil {
			return selector, err
		}
		workloadSelector = item.Spec.Selector
	case "replicaset", "replicasets":
		item, err := c.typed.AppsV1().ReplicaSets(selector.Namespace).Get(ctx, selector.WorkloadName, metav1.GetOptions{})
		if err != nil {
			return selector, err
		}
		workloadSelector = item.Spec.Selector
	case "job", "jobs":
		item, err := c.typed.BatchV1().Jobs(selector.Namespace).Get(ctx, selector.WorkloadName, metav1.GetOptions{})
		if err != nil {
			return selector, err
		}
		workloadSelector = item.Spec.Selector
	default:
		return selector, fmt.Errorf("%w: unsupported workload kind", ErrInvalidLogQuery)
	}
	parsed, err := metav1.LabelSelectorAsSelector(workloadSelector)
	if err != nil {
		return selector, fmt.Errorf("%w: invalid workload selector", ErrInvalidLogQuery)
	}
	if selector.LabelSelector == "" {
		selector.LabelSelector = parsed.String()
		return selector, nil
	}
	combined, err := labels.Parse(parsed.String() + "," + selector.LabelSelector)
	if err != nil {
		return selector, fmt.Errorf("%w: invalid combined workload selector", ErrInvalidLogQuery)
	}
	selector.LabelSelector = combined.String()
	return selector, nil
}

func (c *Client) listAggregateLogPods(ctx context.Context, selector domainresource.LogSourceSelector) ([]corev1.Pod, []string, error) {
	if selector.LabelSelector == "" && len(selector.PodNames) > 0 {
		pods := make([]corev1.Pod, 0, len(selector.PodNames))
		missing := make([]string, 0)
		seen := make(map[string]struct{}, len(selector.PodNames))
		for _, name := range selector.PodNames {
			name = strings.TrimSpace(name)
			if name == "" {
				return nil, nil, fmt.Errorf("%w: pod names must not be empty", ErrInvalidLogQuery)
			}
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = struct{}{}
			pod, err := c.typed.CoreV1().Pods(selector.Namespace).Get(ctx, name, metav1.GetOptions{})
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

	items, err := c.typed.CoreV1().Pods(selector.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.LabelSelector})
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

func selectAggregateLogContainers(pod corev1.Pod, selector domainresource.LogSourceSelector) ([]string, []string) {
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
		selected := make([]string, 0, len(selector.Containers))
		missing := make([]string, 0)
		for _, name := range selector.Containers {
			if _, ok := available[name]; ok {
				selected = append(selected, name)
			} else {
				missing = append(missing, name)
			}
		}
		return selected, missing
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

func (c *Client) readAggregateLogSources(ctx context.Context, query domainresource.LogQuery, sources []aggregateLogSource) <-chan aggregateLogReadResult {
	jobs := make(chan aggregateLogSource)
	workerCount := min(aggregateLogMaxConcurrency, len(sources))
	results := make(chan aggregateLogReadResult, workerCount)
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for source := range jobs {
				entries, truncated, err := c.readAggregateLogSource(ctx, query, source)
				results <- aggregateLogReadResult{entries: entries, source: source.source, err: err, truncated: truncated}
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

func (c *Client) readAggregateLogSource(ctx context.Context, query domainresource.LogQuery, source aggregateLogSource) ([]domainresource.LogEntry, bool, error) {
	options := aggregatePodLogOptions(query, source.container, false)
	stream, err := c.typed.CoreV1().Pods(source.pod.Namespace).GetLogs(source.pod.Name, options).Stream(ctx)
	if err != nil {
		return nil, false, err
	}
	defer stream.Close()
	content, _, truncated, err := streamlimit.ReadString(stream, aggregateLogSourceMaxBytes)
	if err != nil {
		return nil, false, err
	}
	return parseAggregateLogContent(content, query, source.source), truncated, nil
}

func (c *Client) streamAggregateLogSource(ctx context.Context, query domainresource.LogQuery, source aggregateLogSource, events chan<- domainresource.LogStreamEvent) {
	stream, err := c.typed.CoreV1().Pods(source.pod.Namespace).GetLogs(source.pod.Name, aggregatePodLogOptions(query, source.container, true)).Stream(ctx)
	if err != nil {
		sendAggregateLogEvent(ctx, events, domainresource.LogStreamEvent{Type: "source_error", Status: &domainresource.LogStreamStatus{State: "degraded", Message: "log source unavailable"}})
		return
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 64*1024), aggregateLogSourceMaxBytes)
	for scanner.Scan() {
		if scanner.Text() == "" {
			continue
		}
		entry := parseAggregateLogLine(scanner.Text(), source.source)
		if aggregateLogEntryMatches(entry, query) {
			if !sendAggregateLogEvent(ctx, events, domainresource.LogStreamEvent{Type: "entry", Entry: &entry}) {
				return
			}
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		sendAggregateLogEvent(ctx, events, domainresource.LogStreamEvent{Type: "source_error", Status: &domainresource.LogStreamStatus{State: "degraded", Message: "log source unavailable"}})
	}
}

func aggregatePodLogOptions(query domainresource.LogQuery, container string, follow bool) *corev1.PodLogOptions {
	tail := int64(query.Tail)
	if tail <= 0 && (follow || query.From == nil) {
		tail = aggregateLogDefaultTail
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

func parseAggregateLogContent(content string, query domainresource.LogQuery, source domainresource.LogSource) []domainresource.LogEntry {
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return []domainresource.LogEntry{}
	}
	lines := strings.Split(content, "\n")
	entries := make([]domainresource.LogEntry, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		entry := parseAggregateLogLine(line, source)
		if aggregateLogEntryMatches(entry, query) {
			entries = append(entries, entry)
		}
	}
	return entries
}

func parseAggregateLogLine(line string, source domainresource.LogSource) domainresource.LogEntry {
	observedAt := time.Now().UTC()
	timestamp, message := observedAt, line
	if rawTimestamp, remainder, ok := strings.Cut(line, " "); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, rawTimestamp); err == nil {
			timestamp, message = parsed.UTC(), remainder
		}
	}
	return domainresource.LogEntry{Timestamp: timestamp, ObservedAt: &observedAt, Message: message, Source: source, SourceMode: sohaapi.LogSourceModeRuntime}
}

func aggregateLogEntryMatches(entry domainresource.LogEntry, query domainresource.LogQuery) bool {
	if query.From != nil && entry.Timestamp.Before(*query.From) {
		return false
	}
	if query.To != nil && entry.Timestamp.After(*query.To) {
		return false
	}
	return query.Text == "" || strings.Contains(entry.Message, query.Text)
}

func sortAggregateLogEntries(entries []domainresource.LogEntry, direction sohaapi.LogDirection) {
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

func boundAggregateLogEntries(entries []domainresource.LogEntry, requestedLimit int, alreadyTruncated bool) ([]domainresource.LogEntry, bool) {
	limit := requestedLimit
	if limit <= 0 || limit > aggregateLogMaxEntries {
		limit = aggregateLogMaxEntries
	}
	if len(entries) > limit {
		entries = entries[:limit]
		alreadyTruncated = true
	}
	byteBudget := aggregateLogMaxBytes - 32*1024
	used := 0
	for index, entry := range entries {
		encoded, _ := json.Marshal(entry)
		used += len(encoded) + 1
		if used > byteBudget {
			return entries[:index], true
		}
	}
	return entries, alreadyTruncated
}

func aggregateLogSourceIdentity(clusterID string, query domainresource.LogQuery, pod corev1.Pod, container string) domainresource.LogSource {
	source := domainresource.LogSource{Domain: sohaapi.LogSourceDomainKubernetes, ClusterID: clusterID, Namespace: pod.Namespace, PodName: pod.Name, PodUID: string(pod.UID), ContainerName: container}
	if query.Selector != nil {
		source.WorkloadKind = query.Selector.WorkloadKind
		source.WorkloadName = query.Selector.WorkloadName
	}
	return source
}

func aggregateLogWarning(code string, source domainresource.LogSource) domainresource.LogWarning {
	return domainresource.LogWarning{Code: code, Message: "one or more authorized log sources could not be read", Source: &source}
}

func sendAggregateLogEvent(ctx context.Context, events chan<- domainresource.LogStreamEvent, event domainresource.LogStreamEvent) bool {
	select {
	case events <- event:
		return true
	case <-ctx.Done():
		return false
	}
}
